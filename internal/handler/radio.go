package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"nrllink/internal/gormdb"
	"nrllink/internal/udphub"
	"nrllink/pkg/cache"
	ws "nrllink/pkg/websocket"

	"github.com/gin-gonic/gin"
)

// RadioConfigResponse 在线收发配置响应
type RadioConfigResponse struct {
	SSID         int  `json:"ssid"`
	DefaultGroup int  `json:"default_group"`
	Enabled      bool `json:"enabled"`
}

// RadioStatusResponse 幽灵设备状态响应
type RadioStatusResponse struct {
	Connected    bool   `json:"connected"`
	GroupID      int    `json:"group_id"`
	OnlineSince  string `json:"online_since,omitempty"`
	CallSign     string `json:"callsign"`
	SSID         int    `json:"ssid"`
	IsSpeaking   bool   `json:"is_speaking"`
	VoiceSending bool   `json:"voice_sending"`
}

// RadioDeviceResponse 在线设备响应
type RadioDeviceResponse struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	CallSign     string `json:"callsign"`
	SSID         int    `json:"ssid"`
	Nickname     string `json:"nickname,omitempty"`
	DevModel     int    `json:"dev_model"`
	GroupID      int    `json:"group_id"`
	IsGhost      bool   `json:"is_ghost"`
	DisableSend  bool   `json:"disable_send"`
	DisableRecv  bool   `json:"disable_recv"`
	ConnectTime  string `json:"connect_time,omitempty"`
	LastActivity string `json:"last_activity,omitempty"`
}

// getUserIDFromContext 从 gin context 获取用户 ID
// JWT 中只有 username，需要从数据库查询用户 ID
func getUserIDFromContext(c *gin.Context) (int, bool) {
	username, exists := c.Get("username")
	if !exists {
		return 0, false
	}
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username.(string))
	if err != nil || user == nil {
		return 0, false
	}
	return int(user.ID), true
}

// GetRadioConfig 获取在线收发配置 (API-001)
func GetRadioConfig(c *gin.Context) {
	// 获取当前用户 ID
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}

	// 检查 WebSocket 幽灵设备状态
	isConnected := false
	groupID := 999 // 默认群组
	ssid := 10     // 默认 SSID

	ghostDevice, ok := ws.GlobalGhostManager.GetGhostDevice(userID)
	if ok && ghostDevice != nil {
		isConnected = true
		groupID = ghostDevice.GroupID
		ssid = int(ghostDevice.SSID)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": RadioConfigResponse{
			SSID:         ssid,
			DefaultGroup: groupID,
			Enabled:      true,
		},
		"connected": isConnected,
	})
}

// UpdateRadioSSID 更新 SSID 设置 (API-002)
func UpdateRadioSSID(c *gin.Context) {
	var req struct {
		SSID int `json:"ssid" binding:"required,min=0,max=255"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的 SSID"})
		return
	}

	// 获取当前用户 ID
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}

	// 更新幽灵设备的 SSID
	ghostDevice, ok := ws.GlobalGhostManager.GetGhostDevice(userID)
	if ok && ghostDevice != nil {
		ghostDevice.SSID = byte(req.SSID)
		// 同步更新关联的 WSDevice
		if ghostDevice.Conn != nil {
			ghostDevice.Conn.SSID = byte(req.SSID)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "SSID 已更新",
		"data": gin.H{
			"ssid": req.SSID,
		},
	})
}

// GetRadioStatus 获取幽灵设备状态 (API-003)
func GetRadioStatus(c *gin.Context) {
	// 获取当前用户 ID
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}

	ghostDevice, ok := ws.GlobalGhostManager.GetGhostDevice(userID)
	if !ok || ghostDevice == nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": RadioStatusResponse{
				Connected: false,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": RadioStatusResponse{
			Connected:    true,
			GroupID:      ghostDevice.GroupID,
			OnlineSince:  ghostDevice.Conn.ConnectTime.Format("2006-01-02 15:04:05"),
			CallSign:     ghostDevice.CallSign,
			SSID:         int(ghostDevice.SSID),
			IsSpeaking:   ghostDevice.Conn.IsReceivingVoice,
			VoiceSending: ghostDevice.Conn.IsSendingVoice,
		},
	})
}

// GetRadioGroupDevices 获取群组在线设备（含幽灵设备标记）(API-004)
func GetRadioGroupDevices(c *gin.Context) {
	groupIDStr := c.Param("id")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的群组 ID"})
		return
	}

	devices := make([]RadioDeviceResponse, 0)
	seenDevices := make(map[string]bool) // 用于去重

	// 1. 获取 UDP 设备
	udpDevices := udphub.GetOnlineDevicesByGroup(groupID)
	for _, dev := range udpDevices {
		key := fmt.Sprintf("udp-%d", dev.ID)
		if seenDevices[key] {
			continue
		}
		seenDevices[key] = true

		devices = append(devices, RadioDeviceResponse{
			ID:           dev.ID,
			Username:     dev.Name,
			CallSign:     dev.CallSign,
			SSID:         int(dev.SSID),
			Nickname:     dev.Name,
			DevModel:     int(dev.DevModel),
			GroupID:      dev.GroupID,
			IsGhost:      false,
			DisableSend:  dev.DisableSend,
			DisableRecv:  dev.DisableRecv,
			ConnectTime:  dev.OnlineTime.Format("2006-01-02 15:04:05"),
			LastActivity: dev.LastPacketTime.Format("2006-01-02 15:04:05"),
		})
	}

	// 2. 获取 WebSocket 设备（包括幽灵设备）
	wsDevices := ws.GlobalManager.GetDevicesByGroup(groupID)
	for _, device := range wsDevices {
		key := fmt.Sprintf("ws-%d-%d", device.GetDeviceID(), device.GetSSID())
		if seenDevices[key] {
			continue
		}
		seenDevices[key] = true

		dev := RadioDeviceResponse{
			ID:           device.GetDeviceID(),
			Username:     device.GetUsername(),
			CallSign:     device.GetCallSign(),
			SSID:         int(device.GetSSID()),
			GroupID:      device.GetGroupID(),
			IsGhost:      device.IsGhost(),
			DisableSend:  device.DisableSend,
			DisableRecv:  device.IsDisabledRecv(),
			DevModel:     int(device.GetDevModel()),
			ConnectTime:  device.ConnectTime.Format("2006-01-02 15:04:05"),
			LastActivity: device.LastPacketTime.Format("2006-01-02 15:04:05"),
		}

		devices = append(devices, dev)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": devices,
	})
}

// UpdateRadioGroupRequest 更新幽灵设备群组请求
type UpdateRadioGroupRequest struct {
	GroupID int `json:"group_id" binding:"required"`
}

// UpdateRadioGroup 更新幽灵设备群组 (API-005)
// 【核心修复】同时更新 WSDevice 和 GhostDevice 的 GroupID，确保跨协议路由正确
func UpdateRadioGroup(c *gin.Context) {
	var req UpdateRadioGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的群组 ID"})
		return
	}

	// 获取当前用户 ID
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}

	// 验证群组是否存在
	group, exists := udphub.GetGroupFromCache(req.GroupID)
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "目标群组不存在或未激活"})
		return
	}
	_ = group // 避免未使用变量警告

	// 获取幽灵设备
	ghostDevice, ok := ws.GlobalGhostManager.GetGhostDevice(userID)
	if !ok || ghostDevice == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "幽灵设备未连接，群组设置将在下次连接时生效",
			"data": gin.H{
				"group_id": req.GroupID,
			},
		})
		return
	}

	oldGroupID := ghostDevice.GroupID

	// 【关键修复】同时更新两个管理器中的 GroupID
	// 1. 更新 GhostDeviceManager 中的 GroupID
	if err := ws.GlobalGhostManager.SetGhostDeviceGroup(userID, req.GroupID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新群组失败"})
		return
	}

	// 2. 更新 WSConnectionManager 中的 WSDevice.GroupID（路由时使用这个）
	if ghostDevice.Conn != nil {
		ws.GlobalManager.SetDeviceGroup(ghostDevice.Conn, req.GroupID)
	}

	log.Printf("[RADIO] 幽灵设备群组切换: 用户 %d 从群组 %d 切换到群组 %d", userID, oldGroupID, req.GroupID)

	// 【持久化】将用户的群组偏好保存到数据库，以便下次登录时恢复
	userRepo := gormdb.NewUserRepository()
	if err := userRepo.UpdateLastGroupID(userID, req.GroupID); err != nil {
		log.Printf("[RADIO] 警告: 更新用户 %d 的 LastGroupID 失败: %v", userID, err)
		// 不影响响应，群组切换已成功
	}

	// 【缓存失效】清除用户缓存，确保页面刷新后能读取到最新��群组设置
	// 必须传入 username，否则 GetUserByName 使用的 userByNameKey 缓存不会被清除
	if userCache := cache.GetUserCache(); userCache != nil {
		if err := userCache.InvalidateUser(c.Request.Context(), userID, ghostDevice.Username); err != nil {
			log.Printf("[RADIO] 警告: 失效用户 %d 缓存失败: %v", userID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "群组切换成功",
		"data": gin.H{
			"group_id":     req.GroupID,
			"old_group_id": oldGroupID,
		},
	})
}

// GetRadioGroupStats 获取用户有权限访问的群组实时统计信息
// 此接口专门为 Radio 页面设计，返回包含 WS 设备的实时统计
// 只返回用户有权限访问的群组（公开群组 + 用户已验证的私有群组）
func GetRadioGroupStats(c *gin.Context) {
	// 获取当前用户
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}

	// 获取用户有权限访问的群组 ID 列表
	memberRepo := gormdb.NewGroupMemberRepository()
	members, err := memberRepo.ListGroupsByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取用户群组失败"})
		return
	}

	// 构建用户有权限的群组 ID 集合
	accessibleGroupIDs := make(map[int]bool)
	for _, m := range members {
		accessibleGroupIDs[m.GroupID] = true
	}

	// 获取所有群组统计
	allStats := udphub.GetAllGroupStats()

	// 只返回用户有权限访问的群组（公开群组 type=1 或用户已验证的私有群组）
	result := make([]gin.H, 0, len(allStats))
	for _, s := range allStats {
		// 公开群组（type=1）对所有用户可见
		// 私有群组（type=2）只对已验证用户可见
		if s.Type == 1 || accessibleGroupIDs[s.ID] {
			result = append(result, gin.H{
				"id":                s.ID,
				"name":              s.Name,
				"type":              s.Type,
				"online_dev_number": s.OnlineDevNumber,
				"total_dev_number":  s.TotalDevNumber,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    result,
	})
}

// CheckGhostDeviceConflict 检查幽灵设备连接冲突 (API-007)
// 用于前端在建立 WebSocket 连接前预检查
// 返回 200 表示可以连接，返回 409 表示存在冲突（该用户已有在线的幽灵设备）
func CheckGhostDeviceConflict(c *gin.Context) {
	// 获取当前用户 ID
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}

	// 检查该用户是否已有在线的幽灵设备
	if ws.GlobalManager.IsGhostDeviceOnline(userID) {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "您的账号已在其他页面建立了电台连接，请先断开其他页面的连接",
			"data": gin.H{
				"conflict": true,
			},
		})
		return
	}

	// 没有冲突，可以建立连接
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "可以建立连接",
		"data": gin.H{
			"conflict": false,
		},
	})
}
