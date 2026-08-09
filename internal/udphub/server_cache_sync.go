package udphub

import (
	"context"
	"log"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/pkg/cache"
)

// ==========================================
// 架构重构：全局群组缓存管理
// ==========================================

// StartGroupCacheSync 启动群组和设备缓存定时同步后台任务
func StartGroupCacheSync() {
	// 启动时立即执行一次，确保服务器刚启动就有数据
	refreshGroupCache()
	refreshDeviceCache()
	InitGroupLinkCache() // 初始化群组互联缓存

	go func() {
		// 每隔 10 秒同步一次数据库中的群组和设备状态
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-udpShutdown:
				return
			case <-ticker.C:
				refreshGroupCache()
				refreshDeviceCache()
				refreshGroupLinkCache() // 同步群组互联缓存
			}
		}
	}()
	log.Println("[CACHE] 数据库群组和设备定时同步任务已启动 (间隔: 10s)")
}

// refreshGroupCache 执行具体的数据库查询与内存缓存增量合并更新
// 核心原则：只更新静态配置属性，绝对不碰动态连接池(ConnPool)
// 性能优化：使用 RCU 模式，构建新 map 后原子替换，避免阻塞读取
func refreshGroupCache() {
	groupCacheMutex.Lock()
	defer groupCacheMutex.Unlock()

	repo := gormdb.NewGroupRepository()
	dbGroups, err := repo.ListGroups()
	if err != nil {
		log.Printf("[CACHE] 从数据库加载群组失败: %v", err)
		return
	}

	// 获取当前缓存（用于合并）
	oldCache := globalGroupCacheAtomic.Load()
	var oldGroupCache map[int]*models.Group
	if oldCache != nil {
		oldGroupCache = oldCache.(map[int]*models.Group)
	} else {
		oldGroupCache = make(map[int]*models.Group)
	}

	// 性能优化：RCU 模式 - 构建新的 map，不阻塞读取
	newGroupCache := make(map[int]*models.Group, len(dbGroups)+2)
	receiverRoutingChanged := false

	// 记录当前数据库中存在的群组 ID
	validGroupIDs := make(map[int]bool, len(dbGroups)+2)

	// 协议级公共群组 999 始终有效；0 仅表示设备未分组，不是群组对象。
	validGroupIDs[models.GroupIDPublicMin] = true

	for _, dbGroup := range dbGroups {
		modelGroup := dbGroup.ToModelGroup()
		validGroupIDs[modelGroup.ID] = true

		// 检查群组是否已经在内存中
		if existingGroup, exists := oldGroupCache[modelGroup.ID]; exists {
			// RCU 发布前构建新静态对象，避免原地修改已被并发读者持有的群组。
			// 动态设备集合与连接池继续复用，在线设备不会因配置刷新而断开。
			if existingGroup.Status != modelGroup.Status {
				receiverRoutingChanged = true
			}
			groupRuntimeMu.RLock()
			modelGroup.DevMap = existingGroup.DevMap
			modelGroup.DevList = append([]int(nil), existingGroup.DevList...)
			modelGroup.OnlineDevNumber = existingGroup.OnlineDevNumber
			modelGroup.TotalDevNumber = existingGroup.TotalDevNumber
			groupRuntimeMu.RUnlock()
			modelGroup.ConnPool = existingGroup.ConnPool
			newGroupCache[modelGroup.ID] = modelGroup
		} else {
			receiverRoutingChanged = true
			// 【关键操作】：如果是不存在的新群组，初始化它的动态连接池
			newGroup := modelGroup
			// 性能优化：预分配连接池容量
			pool := &CurrentConnPool{
				DevConnMap: make(map[string]*models.Device, 32),
			}
			pool.storeConnList(make([]*models.Device, 0, 32))
			newGroup.ConnPool = pool
			newGroup.DevMap = make(map[int]*models.Device, 32)

			newGroupCache[newGroup.ID] = newGroup
			log.Printf("[CACHE] 新群组已加载: %d - %s", newGroup.ID, newGroup.Name)
		}
	}
	// 999 是协议级系统群组，即使历史数据库没有对应行也必须保留。
	// 旧实现只把它标为 valid，却没有复制到 newGroupCache，首次刷新后会丢失。
	if _, exists := newGroupCache[models.GroupIDPublicMin]; !exists {
		if existing, ok := oldGroupCache[models.GroupIDPublicMin]; ok && existing != nil {
			newGroupCache[models.GroupIDPublicMin] = existing
		} else {
			newGroupCache[models.GroupIDPublicMin] = &models.Group{
				ID:         models.GroupIDPublicMin,
				Name:       "全网互联",
				Type:       models.GroupTypeRelay,
				Status:     1,
				DevMap:     make(map[int]*models.Device),
				CreateTime: time.Now().Format("2006-01-02 15:04:05"),
				UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
				ConnPool:   newConnPool(),
			}
		}
	}

	// 复制旧缓存中仍有效的群组（数据库中未变更的）
	for id := range oldGroupCache {
		if _, valid := validGroupIDs[id]; valid {
			// 已经在上面处理过，跳过
			continue
		}
		// 数据库中已删除的群组，不复制到新缓存
		receiverRoutingChanged = true
		log.Printf("[CACHE] 群组 %d 已从数据库移除，清理缓存", id)
	}

	// 原子替换缓存指针（RCU 模式）
	globalGroupCacheAtomic.Store(newGroupCache)

	// 同时更新 publicGroupMap 以保持向后兼容
	publicGroupMap = newGroupCache
	if receiverRoutingChanged {
		resetDomainGroupReverseCache()
		InvalidateDomainReceiverCache()
	}

	log.Printf("[CACHE] 群组状态同步完成，当前加载了 %d 个有效群组", len(newGroupCache))
}

// refreshDeviceCache 同步设备状态从数据库到内存
// 核心原则：只更新动态属性（GroupID, DisableSend, DisableRecv, Priority），不碰连接状态
// 同时将内存中的在线状态同步回数据库，供 Web 端查询
func refreshDeviceCache() {
	repo := gormdb.NewDeviceRepository()
	dbDevices, err := repo.ListAllDevices()
	if err != nil {
		log.Printf("[CACHE] 从数据库加载设备失败: %v", err)
		return
	}

	updatedCount := 0
	onlineSyncCount := 0
	removedCount := 0
	receiverRoutingChanged := false

	dbDeviceKeys := make(map[string]struct{}, len(dbDevices))
	for _, dbDev := range dbDevices {
		dbDeviceKeys[getOwnerSSIDKey(dbDev.OwnerID, dbDev.SSID)] = struct{}{}
	}

	userCache := loadDeviceOwnerCache(dbDevices)

	for _, dbDev := range dbDevices {
		memDev := findDeviceByOwnerSSIDFromMemory(dbDev.OwnerID, dbDev.SSID)
		if memDev == nil {
			continue
		}

		if dbDev.OwnerID > 0 {
			owner := userCache[dbDev.OwnerID]
			if owner != nil {
				if memDev.Username != owner.Name {
					removeRuntimeUsernameKey(memDev, memDev.Username)
					memDev.Username = owner.Name
					indexRuntimeDevice(memDev)
				}
				if memDev.CallSign != owner.CallSign {
					removeRuntimeCallSignKey(memDev, memDev.CallSign)
					memDev.CallSign = owner.CallSign
					indexRuntimeDevice(memDev)
				}
				memDev.Nickname = owner.NickName
			}
		}

		// 群组变化必须走统一的 detach/attach 流程，不能只修改 GroupID 字段，
		// 否则旧连接池仍会继续向该设备转发。
		if memDev.GroupID != dbDev.GroupID {
			if _, err := changeDeviceGroup(memDev, dbDev.GroupID); err != nil {
				log.Printf("[CACHE] 同步设备 %d 群组 %d -> %d 失败: %v", memDev.ID, memDev.GroupID, dbDev.GroupID, err)
			} else {
				receiverRoutingChanged = true
				updatedCount++
			}
		}
		if memDev.DisableSend != dbDev.DisableSend || memDev.DisableRecv != dbDev.DisableRecv || memDev.Priority != dbDev.Priority {
			if memDev.DisableRecv != dbDev.DisableRecv {
				receiverRoutingChanged = true
			}
			memDev.DisableSend = dbDev.DisableSend
			memDev.DisableRecv = dbDev.DisableRecv
			memDev.Priority = dbDev.Priority
			updatedCount++
		}

		onlineStateChanged := memDev.ISOnline != dbDev.ISOnline
		lastOnlineIPChanged := memDev.LastOnlineIP != "" && memDev.LastOnlineIP != dbDev.LastOnlineIP

		// 在线状态与最近上线 IP 的变更都需要同步到数据库，并使缓存失效。
		if onlineStateChanged || lastOnlineIPChanged {
			onlineTime := ""
			if onlineStateChanged && memDev.ISOnline && !memDev.OnlineTime.IsZero() {
				onlineTime = memDev.OnlineTime.Format("2006-01-02 15:04:05")
			}
			repo.UpdateDeviceOnlineStatus(memDev.OwnerID, uint8(memDev.SSID), memDev.ISOnline, onlineTime, memDev.LastOnlineIP)
			onlineSyncCount++

			// 获取缓存接口实例
			if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
				ctx := context.Background()

				// 1. 失效单个设备的详细信息缓存
				_ = deviceCache.InvalidateDevice(ctx, memDev.ID, memDev.OwnerID, uint8(memDev.SSID))

				// 2. 失效全局设备分页列表缓存，确保前端 "所有设备" 页面能刷新状态
				_ = deviceCache.InvalidateDeviceList(ctx)

				// 3. 如果设备已经加入某个群组，还要失效该群组的设备列表缓存
				// 确保前端 "群组内的设备列表" 也能立刻体现设备的上下线情况
				if memDev.GroupID > 0 {
					_ = deviceCache.InvalidateDevicesByGroup(ctx, memDev.GroupID)
				}
			}
		}
	}
	if receiverRoutingChanged {
		InvalidateDomainReceiverCache()
	}

	missingDevices := make([]*models.Device, 0)
	for _, memDev := range getOwnerDeviceMapSnapshot() {
		if memDev == nil {
			continue
		}
		if _, exists := dbDeviceKeys[getOwnerSSIDKey(memDev.OwnerID, memDev.SSID)]; exists {
			continue
		}
		missingDevices = append(missingDevices, memDev)
	}
	for _, missingDev := range missingDevices {
		if RemoveRuntimeDevice(missingDev.OwnerID, missingDev.SSID) {
			removedCount++
		}
	}
	if removedCount > 0 {
		if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
			ctx := context.Background()
			_ = deviceCache.InvalidateDeviceList(ctx)
			for _, missingDev := range missingDevices {
				_ = deviceCache.InvalidateDevice(ctx, missingDev.ID, missingDev.OwnerID, uint8(missingDev.SSID))
				if missingDev.GroupID > 0 {
					_ = deviceCache.InvalidateDevicesByGroup(ctx, missingDev.GroupID)
				}
			}
		}
	}

	if updatedCount > 0 {
		log.Printf("[CACHE] 设备属性同步完成，更新了 %d 个设备", updatedCount)
	}
	if onlineSyncCount > 0 {
		log.Printf("[CACHE] 设备在线状态/IP 已同步到数据库，更新了 %d 个设备", onlineSyncCount)
	}
	if removedCount > 0 {
		log.Printf("[CACHE] 已清理 %d 个数据库中已不存在的运行时设备", removedCount)
	}
}

// RefreshDeviceCache 从数据库重新同步设备动态属性和运行时索引。用于数据库
// 已提交、但单设备增量同步失败时立即自愈，而不是等待后台轮询。
func RefreshDeviceCache() {
	refreshDeviceCache()
}

// GetGroupFromCache 从缓存中获取群组（线程安全）
// 性能优化：使用 RCU 模式，无锁读取
func GetGroupFromCache(groupID int) (*models.Group, bool) {
	cache := globalGroupCacheAtomic.Load()
	if cache == nil {
		return nil, false
	}
	groupCache := cache.(map[int]*models.Group)
	gp, ok := groupCache[groupID]
	return gp, ok
}

// GetAllGroupsFromCache 获取所有群组（线程安全）
func GetAllGroupsFromCache() map[int]*models.Group {
	cache := globalGroupCacheAtomic.Load()
	if cache == nil {
		return make(map[int]*models.Group)
	}
	groupCache := cache.(map[int]*models.Group)

	// 返回副本以避免外部修改
	result := make(map[int]*models.Group, len(groupCache))
	for k, v := range groupCache {
		result[k] = v
	}
	return result
}
