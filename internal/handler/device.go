package handler

import (
	"log"
	"net/http"
	"strconv"
	"time"

	gormdb "draarl/internal/gormdb"
	"draarl/internal/udphub"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
)

// DeviceListRequest 设备列表请求
type DeviceListRequest struct {
	Limit    int    `json:"limit"`
	Page     int    `json:"page"`
	CallSign string `json:"callsign"`
	GroupID  string `json:"group_id"`
	IsOnline bool   `json:"isonline"`
	Sort     string `json:"sort"`
}

// DeviceInfo 设备信息响应
type DeviceInfo struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	CallSign      string `json:"callsign"`
	SSID          uint8  `json:"ssid"`
	DevModel      int    `json:"dev_model"`
	GroupID       int    `json:"group_id"`
	Status        int8   `json:"status"`
	Priority      int    `json:"priority"`
	IsOnline      bool   `json:"is_online"`
	DisableSend   bool   `json:"disable_send"`
	DisableRecv   bool   `json:"disable_recv"`
	QTH           string `json:"qth"`
	Note          string `json:"note"`
	OwnerID       int    `json:"owner_id,omitempty"`
	OwnerName     string `json:"owner_name,omitempty"`
	OwnerCallSign string `json:"owner_callsign,omitempty"`
	OnlineTime    string `json:"online_time,omitempty"`
	CreateTime    string `json:"create_time,omitempty"`
	UpdateTime    string `json:"update_time,omitempty"`
}

// GetDevices 获取设备列表
func GetDevices(c *gin.Context) {
	// 获取查询参数
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	callsign := c.Query("callsign")
	groupID := c.Query("group_id")
	_ = c.Query("isonline") == "true" // TODO: 实现在线状态过滤

	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}

	ctx := c.Request.Context()
	deviceCache := cache.GetDeviceCache()

	// 获取当前用户信息（使用缓存）
	username, _ := c.Get("username")
	var currentUser *gormdb.User
	if userCache := cache.GetUserCache(); userCache != nil {
		currentUser, _ = userCache.GetUserByName(ctx, username.(string))
	}
	if currentUser == nil {
		userRepo := gormdb.NewUserRepository()
		currentUser, _ = userRepo.GetUserByName(username.(string))
	}

	var devices []*gormdb.Device
	var total int64
	var err error

	repo := gormdb.NewDeviceRepository()

	// 根据查询条件选择不同的查询方法（全部使用数据库分页）
	if callsign != "" {
		// 按呼号搜索（数据库层分页）
		ownerID := 0
		if currentUser != nil && !hasRoleGORM(currentUser, "admin") {
			ownerID = currentUser.ID // 非管理员只能看到自己的设备
		}
		devices, total, _ = repo.ListDevicesByCallSignPaginated(callsign, ownerID, limit, page)
	} else if groupID != "" {
		// 按群组过滤（数据库层分页）
		gid, _ := strconv.Atoi(groupID)
		ownerID := 0
		if currentUser != nil && !hasRoleGORM(currentUser, "admin") {
			ownerID = currentUser.ID // 非管理员只能看到自己的设备
		}
		devices, total, _ = repo.ListDevicesByGroupIDPaginated(gid, ownerID, limit, page)
	} else {
		// 普通用户只获取自己的设备，管理员获取所有设备
		if currentUser != nil && hasRoleGORM(currentUser, "admin") {
			// 管理员获取所有设备（使用缓存）
			if deviceCache != nil {
				devices, total, err = deviceCache.GetDeviceList(ctx, page, limit)
			} else {
				devices, total, err = repo.ListDevices(limit, page)
			}
		} else {
			// 普通用户获取自己的设备（数据库层分页）
			devices, total, _ = repo.ListDevicesByOwnerIDPaginated(currentUser.ID, limit, page)
		}
		if err != nil && (currentUser == nil || hasRoleGORM(currentUser, "admin")) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "查询设备列表失败",
			})
			return
		}
	}

	// 批量获取所有需要的用户信息（解决 N+1 查询问题）
	userRepo := gormdb.NewUserRepository()
	ownerIDs := make([]int, 0, len(devices))
	for _, d := range devices {
		if d.OwnerID > 0 {
			ownerIDs = append(ownerIDs, d.OwnerID)
		}
	}
	// 去重
	ownerIDSet := make(map[int]bool)
	uniqueOwnerIDs := make([]int, 0, len(ownerIDs))
	for _, id := range ownerIDs {
		if !ownerIDSet[id] {
			ownerIDSet[id] = true
			uniqueOwnerIDs = append(uniqueOwnerIDs, id)
		}
	}
	// 批量查询用户简要信息
	userCache := cache.GetUserCache()
	ownerMap, _ := userRepo.GetUserBriefByIDs(uniqueOwnerIDs)

	// 转换为响应格式
	items := make([]*DeviceInfo, 0, len(devices))
	for _, d := range devices {
		info := &DeviceInfo{
			ID:          d.ID,
			Name:        d.Name,
			SSID:        d.SSID,
			DevModel:    d.DevModel,
			GroupID:     d.GroupID,
			Status:      d.Status,
			Priority:    d.Priority,
			IsOnline:    d.ISOnline,
			DisableSend: d.DisableSend, // 补充设备级禁发状态
			DisableRecv: d.DisableRecv, // 补充设备级禁收状态
			QTH:         d.QTH,
			Note:        d.Note,
			CreateTime:  d.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:  d.UpdateTime.Format("2006-01-02 15:04:05"),
		}

		// 从批量查询结果中获取设备所有者信息
		if d.OwnerID > 0 {
			// 优先从缓存获取
			var owner *gormdb.User
			if userCache != nil {
				owner, _ = userCache.GetUserByID(ctx, d.OwnerID)
			}
			if owner == nil {
				// 从批量查询结果获取
				if brief, ok := ownerMap[d.OwnerID]; ok {
					info.OwnerID = brief.ID
					info.OwnerName = brief.NickName
					if info.OwnerName == "" {
						info.OwnerName = brief.Name
					}
					info.OwnerCallSign = brief.CallSign
					info.CallSign = brief.CallSign
				}
			} else {
				info.OwnerID = owner.ID
				info.OwnerName = owner.NickName
				if info.OwnerName == "" {
					info.OwnerName = owner.Name
				}
				info.OwnerCallSign = owner.CallSign
				info.CallSign = owner.CallSign
			}
		}

		items = append(items, info)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total":     total,
			"items":     items,
			"page":      page,
			"page_size": limit,
		},
	})
}

// GetDevice 获取单个设备
func GetDevice(c *gin.Context) {
	idStr := c.Query("id")

	var device *gormdb.Device
	var err error

	// 尝试使用缓存
	deviceCache := cache.GetDeviceCache()
	ctx := c.Request.Context()

	// 必须使用ID查询
	if idStr != "" {
		id, _ := strconv.Atoi(idStr)
		if deviceCache != nil {
			device, err = deviceCache.GetDeviceByID(ctx, id)
		} else {
			repo := gormdb.NewDeviceRepository()
			device, err = repo.GetDeviceByID(id)
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "缺少设备ID",
		})
		return
	}

	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	// 获取所有者呼号
	var callsign string
	userRepo := gormdb.NewUserRepository()
	if device.OwnerID > 0 {
		var owner *gormdb.User
		if userCache := cache.GetUserCache(); userCache != nil {
			owner, _ = userCache.GetUserByID(ctx, device.OwnerID)
		}
		if owner == nil {
			owner, _ = userRepo.GetUserByID(device.OwnerID)
		}
		if owner != nil {
			callsign = owner.CallSign
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":          device.ID,
			"name":        device.Name,
			"callsign":    callsign,
			"ssid":        device.SSID,
			"dev_model":   device.DevModel,
			"group_id":    device.GroupID,
			"status":      device.Status,
			"priority":    device.Priority,
			"is_online":   device.ISOnline,
			"owner_id":    device.OwnerID,
			"note":        device.Note,
			"create_time": device.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time": device.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// UpdateDeviceRequest 更新设备请求
type UpdateDeviceRequest struct {
	Name        string `json:"name"`
	GroupID     int    `json:"group_id"`
	Status      int8   `json:"status"`
	Priority    int    `json:"priority"`
	Note        string `json:"note"`
	DevModel    int    `json:"dev_model"`
	DisableSend *bool  `json:"disable_send"` // 设备级禁发（可选字段）
	DisableRecv *bool  `json:"disable_recv"` // 设备级禁收（可选字段）
}

// UpdateDevice 更新设备
func UpdateDevice(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的设备ID",
		})
		return
	}

	var req UpdateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	repo := gormdb.NewDeviceRepository()

	// 检查设备是否存在
	device, err := repo.GetDeviceByID(id)
	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	// 检查权限：只有设备所有者或管理员可以修改
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))

	if currentUser != nil && !hasRoleGORM(currentUser, "admin") && device.OwnerID != currentUser.ID {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "无权修改此设备",
		})
		return
	}

	// 更新字段
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
		device.Name = req.Name
	}
	if req.GroupID > 0 {
		updates["group_id"] = req.GroupID
		device.GroupID = req.GroupID
	}
	if req.Status > 0 {
		updates["status"] = req.Status
		device.Status = req.Status
	}
	if req.Priority > 0 {
		updates["priority"] = req.Priority
		device.Priority = req.Priority
	}
	if req.Note != "" {
		updates["note"] = req.Note
		device.Note = req.Note
	}
	if req.DevModel > 0 {
		updates["dev_model"] = req.DevModel
		device.DevModel = req.DevModel
	}
	// 设备级别的收发控制（设备所有者和管理员可设置）
	// 仅在请求显式传入字段时更新，避免未传字段被默认值覆盖。
	if req.DisableSend != nil {
		updates["disable_send"] = *req.DisableSend
		device.DisableSend = *req.DisableSend
	}
	if req.DisableRecv != nil {
		updates["disable_recv"] = *req.DisableRecv
		device.DisableRecv = *req.DisableRecv
	}

	// 记录旧群组ID（用于缓存失效）
	oldGroupID := device.GroupID

	if err := repo.UpdateDeviceFields(id, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新设备失败",
		})
		return
	}

	// 立即同步 UDP Hub 运行时内存，避免等待定时器导致收发控制生效延迟
	if req.DisableSend != nil || req.DisableRecv != nil {
		udphub.SyncDeviceCommControlByID(id, device.DisableSend, device.DisableRecv)
	}

	// 使设备详情缓存失效，并在群组改变时使新旧群组设备列表缓存失效
	ctx := c.Request.Context()
	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		// 1. 失效单个设备详情（使用 OwnerID 作为缓存键）
		_ = deviceCache.InvalidateDevice(ctx, id, device.OwnerID, uint8(device.SSID))

		// 2. 主动清理全局设备分页列表（设备属性修改后列表应更新）
		_ = deviceCache.InvalidateDeviceList(ctx)

		// 3. 如果群组改变，使新旧群组设备列表缓存都失效
		if req.GroupID > 0 && oldGroupID != req.GroupID {
			if oldGroupID > 0 {
				_ = deviceCache.InvalidateDevicesByGroup(ctx, oldGroupID)
			}
			_ = deviceCache.InvalidateDevicesByGroup(ctx, req.GroupID)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id": id,
		},
	})
}

// DeleteDevice 删除设备
func DeleteDevice(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err == nil {
		// 通过ID删除
		repo := gormdb.NewDeviceRepository()
		// 检查设备是否存在
		device, err := repo.GetDeviceByID(id)
		if err != nil || device == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "设备不存在",
			})
			return
		}

		// 检查权限：只有设备所有者或管理员可以删除
		username, _ := c.Get("username")
		userRepo := gormdb.NewUserRepository()
		currentUser, _ := userRepo.GetUserByName(username.(string))

		if currentUser != nil && !hasRoleGORM(currentUser, "admin") && device.OwnerID != currentUser.ID {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "无权删除此设备",
			})
			return
		}

		if err := repo.DeleteDeviceByID(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "删除设备失败",
			})
			return
		}

		// 使设备详情、设备列表和群组设备列表缓存失效
		ctx := c.Request.Context()
		if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
			// 使用 OwnerID 作为缓存键（不再查询呼号）
			_ = deviceCache.InvalidateDevice(ctx, id, device.OwnerID, uint8(device.SSID))
			_ = deviceCache.InvalidateDeviceList(ctx)
			if device.GroupID > 0 {
				_ = deviceCache.InvalidateDevicesByGroup(ctx, device.GroupID)
			}
		}
	} else {
		// 通过 owner_id 和 ssid 删除（兼容旧接口，需要先查询获取设备ID）
		ownerIDStr := c.Query("owner_id")
		ssidStr := c.Query("ssid")
		ssid := uint8(0)

		if ssidStr != "" {
			s, err := strconv.ParseUint(ssidStr, 10, 8)
			if err == nil {
				ssid = uint8(s)
			}
		}

		if ownerIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "缺少设备标识",
			})
			return
		}

		ownerID, err := strconv.Atoi(ownerIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的所有者ID",
			})
			return
		}

		repo := gormdb.NewDeviceRepository()
		device, err := repo.GetDeviceByOwnerSSID(ownerID, ssid)
		if err != nil || device == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "设备不存在",
			})
			return
		}

		if err := repo.DeleteDeviceByID(device.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "删除设备失败",
			})
			return
		}

		// 使设备详情、设备列表和群组设备列表缓存失效
		ctx := c.Request.Context()
		if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
			// 使用 OwnerID 作为缓存键
			_ = deviceCache.InvalidateDevice(ctx, device.ID, device.OwnerID, uint8(device.SSID))
			_ = deviceCache.InvalidateDeviceList(ctx)
			if device.GroupID > 0 {
				_ = deviceCache.InvalidateDevicesByGroup(ctx, device.GroupID)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// ChangeDeviceGroupRequest 切换设备群组请求
type ChangeDeviceGroupRequest struct {
	DeviceID int    `json:"device_id" binding:"required"`
	GroupID  int    `json:"group_id" binding:"required"`
	Password string `json:"password"` // 私有群组且未验证时需要
}

// ChangeDeviceGroup 修改设备群组
func ChangeDeviceGroup(c *gin.Context) {
	var req ChangeDeviceGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 获取当前用户
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	repo := gormdb.NewDeviceRepository()
	device, err := repo.GetDeviceByID(req.DeviceID)
	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	// 检查群组是否存在
	groupRepo := gormdb.NewGroupRepository()
	group, err := groupRepo.GetGroupByID(req.GroupID)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	// 检查权限：公开群组所有人可见，私有群组需要已验证
	if group.Type == 1 {
		// 公开群组，直接允许切换
	} else {
		// 私有群组，需要已验证
		userRepo := gormdb.NewUserRepository()
		currentUser, _ := userRepo.GetUserByName(username.(string))
		if currentUser == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "用户不存在",
			})
			return
		}

		// 检查用户是否已验证
		memberRepo := gormdb.NewGroupMemberRepository()
		isVerified := memberRepo.IsVerifiedMember(req.GroupID, currentUser.ID)
		if !isVerified {
			// 未验证用户，需要提供密码
			if req.Password == "" {
				c.JSON(http.StatusForbidden, gin.H{
					"code":    403,
					"message": "需要先验证密码才能加入该群组",
				})
				return
			}

			// 验证密码
			if req.Password != group.Password {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code":    401,
					"message": "密码错误",
				})
				return
			}

			// 密码验证成功，创建 GroupMember 记录
			if err := memberRepo.CreateMember(&gormdb.GroupMember{
				GroupID:    req.GroupID,
				UserID:     currentUser.ID,
				IsVerified: true,
				JoinTime:   time.Now(),
				LastVerify: time.Now(),
			}); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    500,
					"message": "创建成员记录失败",
				})
				return
			}
		}
	}

	// 更新设备的群组（数据库）
	oldGroupID := device.GroupID
	if err := repo.UpdateDeviceFields(req.DeviceID, map[string]interface{}{
		"group_id": req.GroupID,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "修改设备群组失败",
		})
		return
	}

	// 使设备详情、设备列表和新旧群组设备列表缓存失效
	ctx := c.Request.Context()
	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		// 使用 OwnerID 作为缓存键（不再查询呼号）
		_ = deviceCache.InvalidateDevice(ctx, req.DeviceID, device.OwnerID, uint8(device.SSID))
		_ = deviceCache.InvalidateDeviceList(ctx)
		// 使旧群组设备列表缓存失效
		if oldGroupID > 0 {
			_ = deviceCache.InvalidateDevicesByGroup(ctx, oldGroupID)
		}
		// 使新群组设备列表缓存失效
		_ = deviceCache.InvalidateDevicesByGroup(ctx, req.GroupID)
	}

	// 更新内存中的 UDP 设备群组
	if err := udphub.ChangeDeviceGroupByID(req.DeviceID, req.GroupID); err != nil {
		// 内存更新失败只记录日志，不影响响应（数据库已更新成功）
		log.Printf("[WARN] Failed to update UDP device group in memory: %v", err)
	}

	// 注：WS 只支持 JWT 幽灵设备，幽灵设备群组切换通过前端直接调用 WebSocket 发送 Config 包实现

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "修改成功",
	})
}

// GetDeviceQTHs 获取设备位置列表
func GetDeviceQTHs(c *gin.Context) {
	ctx := c.Request.Context()
	deviceCache := cache.GetDeviceCache()

	var devicesRaw []*gormdb.Device
	var err error

	// 使用缓存获取设备列表
	if deviceCache != nil {
		devicesRaw, _, err = deviceCache.GetDeviceList(ctx, 1, 1000)
	} else {
		repo := gormdb.NewDeviceRepository()
		devicesRaw, _, err = repo.ListDevices(1000, 1)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询设备列表失败",
		})
		return
	}

	// 批量获取所有者呼号（解决 N+1 查询问题）
	userRepo := gormdb.NewUserRepository()
	ownerIDs := make([]int, 0, len(devicesRaw))
	for _, d := range devicesRaw {
		if d.OwnerID > 0 {
			ownerIDs = append(ownerIDs, d.OwnerID)
		}
	}
	// 去重
	ownerIDSet := make(map[int]bool)
	uniqueOwnerIDs := make([]int, 0, len(ownerIDs))
	for _, id := range ownerIDs {
		if !ownerIDSet[id] {
			ownerIDSet[id] = true
			uniqueOwnerIDs = append(uniqueOwnerIDs, id)
		}
	}
	ownerMap, _ := userRepo.GetUserBriefByIDs(uniqueOwnerIDs)

	// 转换为响应格式
	devices := make([]gin.H, 0, len(devicesRaw))
	for _, d := range devicesRaw {
		// 从批量查询结果中获取呼号
		var callsign string
		if d.OwnerID > 0 {
			if brief, ok := ownerMap[d.OwnerID]; ok {
				callsign = brief.CallSign
			}
		}
		devices = append(devices, gin.H{
			"id":       d.ID,
			"name":     d.Name,
			"callsign": callsign,
			"ssid":     d.SSID,
			"qth":      d.QTH,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": devices,
		},
	})
}
