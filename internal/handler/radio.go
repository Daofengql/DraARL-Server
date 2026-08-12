package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"draarl/internal/gormdb"
	"draarl/internal/protocol"
	"draarl/internal/udphub"
	ws "draarl/pkg/websocket"

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
	SessionID    string `json:"session_id,omitempty"`
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
	UserID       int    `json:"user_id,omitempty"`
	Username     string `json:"username"`
	CallSign     string `json:"callsign"`
	SSID         int    `json:"ssid"`
	Nickname     string `json:"nickname,omitempty"`
	DeviceName   string `json:"device_name,omitempty"`
	DevModel     int    `json:"dev_model"`
	GroupID      int    `json:"group_id"`
	IsGhost      bool   `json:"is_ghost"`
	DisableSend  bool   `json:"disable_send"`
	DisableRecv  bool   `json:"disable_recv"`
	ConnectTime  string `json:"connect_time,omitempty"`
	LastActivity string `json:"last_activity,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	TxGroupID    int    `json:"tx_group_id,omitempty"`
	RxGroupIDs   []int  `json:"rx_group_ids,omitempty"`
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
	ssid := int(protocol.SSIDGhostWeb)

	sessions := ownerPlatformSessions(userID, protocol.DraARLDevModelBrowser)
	if len(sessions) > 0 {
		isConnected = true
	}
	if len(sessions) == 1 {
		groupID = sessions[0].TxGroupID
	} else if persistedGroupID, err := gormdb.NewUserRepository().GetUserLastGroupID(userID, protocol.DraARLDevModelBrowser); err == nil && persistedGroupID > 0 {
		groupID = persistedGroupID
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

// GetRadioStatus 获取幽灵设备状态 (API-003)
func GetRadioStatus(c *gin.Context) {
	// 获取当前用户 ID
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}

	session, exists, err := resolveOwnedPlatformSession(userID, protocol.DraARLDevModelBrowser, strings.TrimSpace(c.Query("session_id")))
	if err != nil {
		writeSessionRoutingError(c, err)
		return
	}
	if !exists {
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
			SessionID:    session.SessionID,
			GroupID:      session.TxGroupID,
			OnlineSince:  session.CreatedAt.Format("2006-01-02 15:04:05"),
			CallSign:     session.CallSign,
			SSID:         int(session.SSID),
			IsSpeaking:   false, // 语音状态通过 WebSocket 实时推送，API 不再提供
			VoiceSending: false, // 语音状态通过 WebSocket 实时推送，API 不再提供
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
	group, err := gormdb.NewGroupRepository().GetGroupByID(groupID)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "群组不存在"})
		return
	}
	if _, ok := requireGroupViewAccess(c, group); !ok {
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
			UserID:       dev.OwnerID,
			Username:     dev.Username,
			CallSign:     dev.CallSign,
			SSID:         int(dev.SSID),
			Nickname:     dev.Nickname,
			DeviceName:   dev.Name,
			DevModel:     int(dev.DevModel),
			GroupID:      dev.GroupID,
			IsGhost:      false,
			DisableSend:  dev.DisableSend,
			DisableRecv:  dev.DisableRecv,
			ConnectTime:  dev.OnlineTime.Format("2006-01-02 15:04:05"),
			LastActivity: dev.LastPacketTime.Format("2006-01-02 15:04:05"),
		})
	}

	// 2. 获取 UDP 幽灵设备。幽灵会话不落在 devices 表或普通 UDP
	// 连接池中，必须从 Session 接收索引读取，否则多端设备不会出现在
	// 群组在线列表里。
	for _, dev := range udphub.GlobalUDPGhostManager.GetByGroup(groupID) {
		if dev == nil || !dev.ISOnline {
			continue
		}
		key := "udp-ghost-" + dev.GhostSessionID
		if seenDevices[key] {
			continue
		}
		seenDevices[key] = true
		devices = append(devices, RadioDeviceResponse{
			ID:           0,
			Username:     dev.Username,
			CallSign:     dev.CallSign,
			SSID:         int(dev.SSID),
			Nickname:     dev.Nickname,
			DevModel:     int(dev.DevModel),
			GroupID:      dev.GroupID,
			IsGhost:      true,
			DisableSend:  dev.DisableSend,
			DisableRecv:  dev.DisableRecv,
			ConnectTime:  dev.OnlineTime.Format("2006-01-02 15:04:05"),
			LastActivity: dev.LastPacketTime.Format("2006-01-02 15:04:05"),
			SessionID:    dev.GhostSessionID,
			TxGroupID:    dev.GroupID,
			RxGroupIDs:   append([]int(nil), dev.GhostRxGroupIDs...),
		})
	}

	// 3. 获取 WebSocket 设备（包括幽灵设备）
	wsDevices := ws.GlobalManager.GetDevicesByGroup(groupID)
	for _, device := range wsDevices {
		key := "ws-" + device.GetIdentifier()
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
			DisableSend:  device.IsDisabledSend(),
			DisableRecv:  device.IsDisabledRecv(),
			DevModel:     int(device.GetDevModel()),
			ConnectTime:  device.GetConnectTime().Format("2006-01-02 15:04:05"),
			LastActivity: device.GetLastPacketTime().Format("2006-01-02 15:04:05"),
			SessionID:    device.GetSessionID(),
			TxGroupID:    device.GetGroupID(),
			RxGroupIDs:   device.GetRxGroupIDs(),
		}

		devices = append(devices, dev)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": devices,
	})
}

// GetRadioGroupStats 获取用户有权限访问的群组实时统计信息
// 此接口专门为 Radio 页面设计，返回包含 WS 设备的实时统计
// 只返回用户有权限访问的群组（公开群组 + 用户已验证的私有群组）
func GetRadioGroupStats(c *gin.Context) {
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	userID := currentUser.ID

	// 获取用户有权限访问的群组 ID 列表
	// 构建用户有权限的群组 ID 集合
	accessibleGroupIDs := make(map[int]bool)
	if !isAdminUser(currentUser) {
		memberRepo := gormdb.NewGroupMemberRepository()
		members, err := memberRepo.ListGroupsByUser(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取用户群组失败"})
			return
		}
		for _, m := range members {
			accessibleGroupIDs[m.GroupID] = true
		}
	}

	// 获取所有群组统计
	allStats := udphub.GetAllGroupStats()

	// 只返回用户有权限访问的群组（公开群组 type=1 或用户已验证的私有群组）
	result := make([]gin.H, 0, len(allStats))
	for _, s := range allStats {
		// 公开群组（type=1）对所有用户可见
		// 私有群组（type=2）只对已验证用户可见
		if s.Type == groupTypePublic || isAdminUser(currentUser) || s.OwnerID == userID || accessibleGroupIDs[s.ID] {
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
