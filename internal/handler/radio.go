package handler

import (
	"net/http"
	"strconv"

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

// GetRadioConfig 获取在线收发配置 (API-001)
func GetRadioConfig(c *gin.Context) {
	// 获取当前用户
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}
	userID := int(userIDVal.(uint))

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

	// 获取当前用户
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}
	userID := int(userIDVal.(uint))

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
	// 获取当前用户
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}
	userID := int(userIDVal.(uint))

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

	// 获取 WebSocket 设备
	wsDevices := ws.GlobalManager.GetDevicesByGroup(groupID)
	devices := make([]RadioDeviceResponse, 0, len(wsDevices))

	for _, device := range wsDevices {
		dev := RadioDeviceResponse{
			ID:          device.GetDeviceID(),
			Username:    device.GetUsername(),
			CallSign:    device.GetCallSign(),
			SSID:        int(device.GetSSID()),
			GroupID:     device.GetGroupID(),
			IsGhost:     device.IsGhost(),
			DisableSend: device.DisableSend,
			DisableRecv: device.IsDisabledRecv(),
			DevModel:    int(device.GetDevModel()),
		}

		devices = append(devices, dev)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": devices,
	})
}
