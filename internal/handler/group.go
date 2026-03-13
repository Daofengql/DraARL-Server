package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	gormdb "nrllink/internal/gormdb"
)

// GroupInfo 群组信息响应
type GroupInfo struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Type              int    `json:"type"`
	CallSign          string `json:"callsign"`
	Password          string `json:"password,omitempty"`
	AllowCallSignSSID string `json:"allow_callsign_ssid"`
	OwerID            int    `json:"ower_id"`
	OwerCallSign      string `json:"ower_callsign"`
	DevList           string `json:"devlist"`
	MasterServer      int    `json:"master_server"`
	SlaveServer       int    `json:"slave_server"`
	Status            int    `json:"status"`
	CreateTime        string `json:"create_time,omitempty"`
	UpdateTime        string `json:"update_time,omitempty"`
	Note              string `json:"note"`
}

// GetGroups 获取群组列表（区分公开/私有）
func GetGroups(c *gin.Context) {
	// 获取查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")

	// 获取当前用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))

	var groups []*gormdb.Group
	var total int64
	var err error

	if keyword != "" {
		// 关键词搜索
		groups, err = gormdb.NewGroupRepository().SearchGroups(keyword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "查询群组列表失败",
			})
			return
		}
	} else {
		// 获取所有可见群组
		// 公开群组所有人可见，私有群组只对已验证用户可见
		if currentUser != nil {
			// 获取用户已验证的群组ID列表
			memberRepo := gormdb.NewGroupMemberRepository()
			members, err := memberRepo.ListGroupsByUser(currentUser.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    500,
					"message": "获取用户群组失败",
				})
				return
			}

			// 获取所有公开群组
			publicGroups, err := gormdb.NewGroupRepository().ListPublicGroups()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    500,
					"message": "获取公开群组失败",
				})
				return
			}

			// 获取用户已验证的私有群组
			groupIDs := make([]int, 0, len(members))
			for _, m := range members {
				groupIDs = append(groupIDs, m.GroupID)
			}
			var privateGroups []*gormdb.Group
			if len(groupIDs) > 0 {
				privateGroups, err = gormdb.NewGroupRepository().GetGroupsByIDs(groupIDs)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code":    500,
						"message": "获取私有群组失败",
					})
					return
				}
			}

			// 合并公开群组和私��群组，并去重
			groups = append(publicGroups, privateGroups...)
			// 使用 map 去重
			seen := make(map[int]bool)
			uniqueGroups := make([]*gormdb.Group, 0, len(groups))
			for _, g := range groups {
				if !seen[g.ID] {
					seen[g.ID] = true
					uniqueGroups = append(uniqueGroups, g)
				}
			}
			groups = uniqueGroups
		} else {
			// 管理员查看所有群组
			groups, err = gormdb.NewGroupRepository().ListGroups()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    500,
					"message": "查询群组列表失败",
				})
				return
			}
		}
	}

	// 计算总数和分页
	total = int64(len(groups))
	offset := (page - 1) * pageSize
	if int64(offset) >= total {
		groups = []*gormdb.Group{}
	} else if offset+pageSize > int(total) {
		groups = groups[offset:]
	} else {
		groups = groups[offset : offset+pageSize]
	}

	// 获取所有设备，用于统计各个群组的在线/总设备数
	deviceRepo := gormdb.NewDeviceRepository()
	allDevices, _, _ := deviceRepo.ListDevices(10000, 1)

	type groupStats struct {
		online int
		total  int
	}
	groupDeviceStats := make(map[int]groupStats)
	for _, d := range allDevices {
		stat := groupDeviceStats[d.GroupID]
		stat.total++
		if d.ISOnline {
			stat.online++
		}
		groupDeviceStats[d.GroupID] = stat
	}

	// 获取当前用户已加入的群组ID列表（用于判断is_joined）
	memberRepo := gormdb.NewGroupMemberRepository()
	var joinedGroupIDs map[int]bool
	if currentUser != nil {
		members, _ := memberRepo.ListGroupsByUser(currentUser.ID)
		joinedGroupIDs = make(map[int]bool, len(members))
		for _, m := range members {
			joinedGroupIDs[m.GroupID] = true
		}
	}

	// 获取当前用户ID用于判断是否是群组所有者
	currentUserID := 0
	if currentUser != nil {
		currentUserID = currentUser.ID
	}

	// 转换为前端期望的带有扩展状态的结构
	resultItems := make([]gin.H, 0, len(groups))
	for _, g := range groups {
		isJoined := false

		// 判断用户是否已加入该群组
		if g.Type == 1 {
			// 公开群组默认视为已加入
			isJoined = true
		} else if g.Type == 2 && currentUser != nil {
			// 私有群组检查是否在已加入列表中
			isJoined = joinedGroupIDs[g.ID]
		}

		// 判断当前用户是否是群组所有者
		isOwner := g.OwerID == currentUserID

		stat := groupDeviceStats[g.ID]

		resultItems = append(resultItems, gin.H{
			"id":                  g.ID,
			"name":                g.Name,
			"type":                g.Type,
			"callsign":            g.CallSign,
			"allow_callsign_ssid": g.AllowCallSignSSID,
			"ower_id":             g.OwerID,
			"ower_callsign":       g.OwerCallSign,
			"master_server":       g.MasterServer,
			"slave_server":        g.SlaveServer,
			"status":              g.Status,
			"note":                g.Note,
			"is_joined":           isJoined,   // 提供给前端用于渲染已加入标识
			"is_owner":            isOwner,    // 提供给前端用于判断是否显示编辑/删除按钮
			"online_count":        stat.online, // 实时在线设备数
			"total_count":         stat.total,  // 总设备数
			"create_time":         g.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time":         g.UpdateTime.Format("2006-01-02 15:04:05"),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items":     resultItems, // 返回组装后的数据
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// GetGroup 获取单个群组
func GetGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	// 获取当前用户ID用于判断是否是群组所有者
	username, _ := c.Get("username")
	var currentUserID int
	if username != nil {
		userRepo := gormdb.NewUserRepository()
		currentUser, _ := userRepo.GetUserByName(username.(string))
		if currentUser != nil {
			currentUserID = currentUser.ID
		}
	}

	// 判断当前用户是否是群组所有者
	isOwner := group.OwerID == currentUserID

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":                  group.ID,
			"name":                group.Name,
			"type":                group.Type,
			"callsign":            group.CallSign,
			"allow_callsign_ssid": group.AllowCallSignSSID,
			"ower_id":             group.OwerID,
			"ower_callsign":       group.OwerCallSign,
			"devlist":             group.DevList,
			"master_server":       group.MasterServer,
			"slave_server":        group.SlaveServer,
			"status":              group.Status,
			"note":                group.Note,
			"is_owner":            isOwner,
			"create_time":         group.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time":         group.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// CreateGroupRequest 创建群组请求
type CreateGroupRequest struct {
	Name              string `json:"name" binding:"required"`
	Type              int    `json:"type"`
	CallSign          string `json:"callsign"`
	Password          string `json:"password"`
	AllowCallSignSSID string `json:"allow_callsign_ssid"`
	AllowDMRID        string `json:"allow_dmrid"`
	OwerCallSign      string `json:"ower_callsign"`
	Note              string `json:"note"`
	Status            int    `json:"status"`
}

// CreateGroup 创建群组
func CreateGroup(c *gin.Context) {
	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 1. 获取当前登录用户 (从上下文中提取)
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 拥有者呼号直接使用用户名（username）
	owerCallSign := currentUser.Name

	repo := gormdb.NewGroupRepository()
	group := &gormdb.Group{
		Name:              req.Name,
		Type:              req.Type,
		CallSign:          req.CallSign,
		Password:          req.Password,
		AllowCallSignSSID: req.AllowCallSignSSID,
		AllowDMRID:        req.AllowDMRID,
		// 2. 修复：强制绑定拥有者 ID 和 拥有者呼号
		OwerID:            currentUser.ID,
		OwerCallSign:      owerCallSign,
		Note:              req.Note,
		Status:            1,
		DevList:           "",
	}

	// 3. 写入群组表
	if err := repo.CreateGroup(group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建群组失败",
		})
		return
	}

	// 4. 修复：自动将创建者加入到该群组的已验证成员中
	// 这样创建者无需搜索和输入密码，就能在"我的群组"列表中看到并管理自己创建的群组
	memberRepo := gormdb.NewGroupMemberRepository()
	groupMember := &gormdb.GroupMember{
		GroupID:    group.ID,
		UserID:     currentUser.ID,
		IsVerified: true,
		JoinTime:   time.Now(),
		LastVerify: time.Now(),
	}
	_ = memberRepo.CreateMember(groupMember)

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "创建成功",
		"data": gin.H{
			"id": group.ID,
		},
	})
}

// UpdateGroupRequest 更新群组请求
type UpdateGroupRequest struct {
	Name              string `json:"name"`
	Type              int    `json:"type"`
	CallSign          string `json:"callsign"`
	Password          string `json:"password"`
	AllowCallSignSSID string `json:"allow_callsign_ssid"`
	Note              string `json:"note"`
	Status            *int   `json:"status"`
}

// UpdateGroup 更新群组
func UpdateGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	var req UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	repo := gormdb.NewGroupRepository()

	// 先获取现有群组
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	// 更新字段
	if req.Name != "" {
		group.Name = req.Name
	}
	if req.Type > 0 {
		group.Type = req.Type
	}
	if req.CallSign != "" {
		group.CallSign = req.CallSign
	}
	if req.Password != "" {
		group.Password = req.Password
	}
	if req.AllowCallSignSSID != "" {
		group.AllowCallSignSSID = req.AllowCallSignSSID
	}
	if req.Note != "" {
		group.Note = req.Note
	}
	if req.Status != nil {
		group.Status = *req.Status
	}

	if err := repo.UpdateGroup(group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新群组失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id":                  group.ID,
			"name":                group.Name,
			"type":                group.Type,
			"callsign":            group.CallSign,
			"allow_callsign_ssid": group.AllowCallSignSSID,
			"ower_id":             group.OwerID,
			"ower_callsign":       group.OwerCallSign,
			"devlist":             group.DevList,
			"master_server":       group.MasterServer,
			"slave_server":        group.SlaveServer,
			"status":              group.Status,
			"note":                group.Note,
			"create_time":         group.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time":         group.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// DeleteGroup 删除群组
func DeleteGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	repo := gormdb.NewGroupRepository()
	if err := repo.DeleteGroup(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除群组失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// GetGroupDevices 获取群组设备列表
func GetGroupDevices(c *gin.Context) {
	groupIDStr := c.Param("id")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	repo := gormdb.NewDeviceRepository()
	devicesRaw, err := repo.ListDevicesByGroupID(groupID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询设备列表失败",
		})
		return
	}

	// 获取群组成员记录，用于获取群组级别的禁发/禁收设置
	memberRepo := gormdb.NewGroupMemberRepository()
	groupMembers, _ := memberRepo.ListMembersByGroup(groupID)

	// 构建设备ID到群组成员状态的映射
	type memberStatus struct {
		disableSend bool
		disableRecv bool
	}
	deviceMemberStatus := make(map[int]memberStatus)
	for _, m := range groupMembers {
		if m.DeviceID != nil {
			deviceMemberStatus[*m.DeviceID] = memberStatus{
				disableSend: m.DisableSend,
				disableRecv: m.DisableRecv,
			}
		}
	}

	// 转换为响应格式
	devices := make([]gin.H, 0, len(devicesRaw))
	for _, d := range devicesRaw {
		// 获取设备级别的禁发/禁收状态
		deviceDisableSend := d.DisableSend
		deviceDisableRecv := d.DisableRecv

		// 如果有群组成员级别的设置，需要进行合并（设备级别优先）
		if memberStatus, ok := deviceMemberStatus[d.ID]; ok {
			// 最终状态 = 设备级 OR 群组成员级（任一禁用则禁用）
			finalDisableSend := deviceDisableSend || memberStatus.disableSend
			finalDisableRecv := deviceDisableRecv || memberStatus.disableRecv

			devices = append(devices, gin.H{
				"id":           d.ID,
				"name":         d.Name,
				"callsign":     d.CallSign,
				"ssid":         d.SSID,
				"dev_model":    d.DevModel,
				"group_id":     d.GroupID,
				"status":       d.Status,
				"priority":     d.Priority,
				"is_online":    d.ISOnline,
				"disable_send": finalDisableSend, // 合并后的禁发状态
				"disable_recv": finalDisableRecv, // 合并后的禁收状态
				"create_time":  d.CreateTime.Format("2006-01-02 15:04:05"),
				"update_time":  d.UpdateTime.Format("2006-01-02 15:04:05"),
			})
		} else {
			// 没有群组成员记录，只使用设备级别的设置
			devices = append(devices, gin.H{
				"id":           d.ID,
				"name":         d.Name,
				"callsign":     d.CallSign,
				"ssid":         d.SSID,
				"dev_model":    d.DevModel,
				"group_id":     d.GroupID,
				"status":       d.Status,
				"priority":     d.Priority,
				"is_online":    d.ISOnline,
				"disable_send": deviceDisableSend,
				"disable_recv": deviceDisableRecv,
				"create_time":  d.CreateTime.Format("2006-01-02 15:04:05"),
				"update_time":  d.UpdateTime.Format("2006-01-02 15:04:05"),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total": int64(len(devices)),
			"items": devices,
		},
	})
}

// GetRelays 获取中继台列表
func GetRelays(c *gin.Context) {
	repo := gormdb.NewRelayRepository()
	relays, err := repo.ListRelays()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询中继台列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": relays,
		},
	})
}

// GetServers 获取服务器列表
func GetServers(c *gin.Context) {
	repo := gormdb.NewServerRepository()
	servers, err := repo.ListServers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询服务器列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": servers,
		},
	})
}

// SearchGroupsRequest 搜索群组请求
type SearchGroupsRequest struct {
	Keyword string `json:"keyword"`
	Page    int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// SearchGroups 搜索群组
func SearchGroups(c *gin.Context) {
	var req SearchGroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	repo := gormdb.NewGroupRepository()
	groups, err := repo.SearchGroups(req.Keyword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "搜索群组失败",
		})
		return
	}

	// 获取当前用户，用于判断是否已加入私有群组
	username, _ := c.Get("username")
	var currentUser *gormdb.User
	if username != nil {
		userRepo := gormdb.NewUserRepository()
		currentUser, _ = userRepo.GetUserByName(username.(string))
	}

	memberRepo := gormdb.NewGroupMemberRepository()

	// 重新组装响应数据，添加用户状态
	resultItems := make([]gin.H, 0, len(groups))
	for _, g := range groups {
		isJoined := false // 统一使用 is_joined 字段名
		requirePassword := false

		if g.Type == 2 {
			// 私有群组需要密码
			requirePassword = true
			if currentUser != nil {
				// 检查用户是否已经验证过
				isJoined = memberRepo.IsVerifiedMember(g.ID, currentUser.ID)
			}
		}

		resultItems = append(resultItems, gin.H{
			"id":               g.ID,
			"name":             g.Name,
			"type":             g.Type,
			"callsign":         g.CallSign,
			"allow_callsign_ssid": g.AllowCallSignSSID,
			"ower_id":          g.OwerID,
			"ower_callsign":    g.OwerCallSign,
			"master_server":    g.MasterServer,
			"slave_server":     g.SlaveServer,
			"status":           g.Status,
			"note":             g.Note,
			"require_password": requirePassword, // 告知前端是否需要密码
			"is_joined":        isJoined,        // 统一使用 is_joined 字段名
			"create_time":      g.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time":      g.UpdateTime.Format("2006-01-02 15:04:05"),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": resultItems,
			"total":  len(groups),
		},
	})
}

// JoinGroupRequest 加入群组请求
type JoinGroupRequest struct {
	Password string `json:"password" binding:"required"`
}

// JoinGroup 加入群组（验��密码）
func JoinGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	var req JoinGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 检查群组是否存在
	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	// 检查群组类型（Type=2 才需要密码）
	if group.Type != 2 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该群组不需要密码验证",
		})
		return
	}

	// 验证密码是否正确
	if group.Password != req.Password {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    401,
			"message": "密码错误",
		})
		return
	}

	// 检查群组是否被禁用
	if group.Status != 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "群组已禁用",
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

	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	memberRepo := gormdb.NewGroupMemberRepository()

	// 检查用户是否已加入
	member, err := memberRepo.GetMemberByGroupAndUser(id, currentUser.ID)
	var isJoined bool
	if err == nil {
		isJoined = member != nil
	} else {
		// 兼容旧数据
		isJoined = memberRepo.IsVerifiedMember(group.ID, currentUser.ID)
	}

	var groupMember gormdb.GroupMember
	if isJoined {
		// 已加入，更新最后验证时间
		err = memberRepo.UpdateMemberVerification(id, currentUser.ID, true)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "更新验证时间失败",
			})
			return
		}
	} else {
		// 未加入，创建成员记录
		groupMember = gormdb.GroupMember{
			GroupID:    id,
			UserID:     currentUser.ID,
			IsVerified: true,
			JoinTime:   time.Now(),
			LastVerify:  time.Now(),
		}
		err = memberRepo.CreateMember(&groupMember)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "加入群组失败",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "加入成功",
		"data": gin.H{
			"group_id":   id,
			"is_verified": true,
			"join_time": time.Now().Format("2006-01-02 15:04:05"),
		},
	})
}

// GetGroupMembers 获取群组成员列表
func GetGroupMembers(c *gin.Context) {
	groupIDStr := c.Param("id")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	// 检查权限：只有群组创建者可查看
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(groupID)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是群组创建者
	if group.OwerID != currentUser.ID {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要群组创建者权限",
		})
		return
	}

	// 查询成员列表
	memberRepo := gormdb.NewGroupMemberRepository()
	members, err := memberRepo.ListVerifiedMembersByGroup(groupID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询成员列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": members,
			"total":  len(members),
		},
	})
}

// UpdateDeviceStatusRequest 设置设备禁发/禁收请求
type UpdateDeviceStatusRequest struct {
	DisableSend bool `json:"disable_send"`
	DisableRecv bool `json:"disable_recv"`
}

// UpdateDeviceStatus 设置设备禁发/禁收
func UpdateDeviceStatus(c *gin.Context) {
	groupIDStr := c.Param("id")
	deviceIDStr := c.Param("deviceId")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}
	deviceID, err := strconv.Atoi(deviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的设备ID",
		})
		return
	}

	var req UpdateDeviceStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 检查权限：只有群组创建者可操作
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(groupID)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查权限：群组创建者或管理员可操作
	isAdmin := hasRoleGORM(currentUser, "admin")
	if group.OwerID != currentUser.ID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要群组创建者或管理员权限",
		})
		return
	}

	// 更新GroupMember表的设备状态
	memberRepo := gormdb.NewGroupMemberRepository()
	err = memberRepo.UpdateMemberDeviceStatusByDevice(groupID, deviceID, req.DisableSend, req.DisableRecv)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新设备状态失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
	})
}

// KickDevice 踢出设备
func KickDevice(c *gin.Context) {
	groupIDStr := c.Param("id")
	deviceIDStr := c.Param("deviceId")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}
	deviceID, err := strconv.Atoi(deviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的设备ID",
		})
		return
	}

	// 检查权限：只有群组创建者可操作
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(groupID)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是群组创建者
	if group.OwerID != currentUser.ID {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要群组创建者权限",
		})
		return
	}

	// 检查设备是否属于该群组
	deviceRepo := gormdb.NewDeviceRepository()
	device, err := deviceRepo.GetDeviceByID(deviceID)
	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	if device.GroupID != groupID {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "设备不属于该群组",
		})
		return
	}

	// 删除GroupMember记录
	memberRepo := gormdb.NewGroupMemberRepository()
	err = memberRepo.DeleteMember(groupID, currentUser.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "踢出设备失败",
		})
		return
	}

	// 将设备移到默认群组（id=1）
	err = deviceRepo.UpdateDeviceFields(deviceID, map[string]interface{}{
		"group_id": 1,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "移动设备失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "踢出成功",
	})
}

// LeaveGroup 离开群组
func LeaveGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	// 检查群组是否存在
	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	// 检查是否是私有群组
	if group.Type != 2 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该群组不支持离开",
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

	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 删除GroupMember记录
	memberRepo := gormdb.NewGroupMemberRepository()
	err = memberRepo.DeleteMember(id, currentUser.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "离开群组失败",
		})
		return
	}

	// 将用户在该群组中的所有设备移到默认群组
	deviceRepo := gormdb.NewDeviceRepository()
	devices, _ := deviceRepo.ListDevicesByGroupID(id)
	for _, device := range devices {
		if device.Username == currentUser.Name {
			err = deviceRepo.UpdateDeviceFields(device.ID, map[string]interface{}{
				"group_id": 1,
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    500,
					"message": "移动设备失败",
				})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "离开成功",
	})
}
