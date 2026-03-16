package interfaces

// WSDeviceInterface WebSocket 设备接口
// 用于解耦 udphub 和 websocket 包
type WSDeviceInterface interface {
	GetIdentifier() string
	GetCallSignSSID() string
	GetGroupID() int
	IsGhost() bool
	GetUserID() int
	GetDeviceID() int
	GetUsername() string
	GetCallSign() string
	GetSSID() byte
	GetDevModel() byte
	IsDisabledRecv() bool
}

// WSManagerInterface WebSocket 连接管理器接口
type WSManagerInterface interface {
	GetDevicesByGroup(groupID int) []WSDeviceInterface
	SendToDevice(device WSDeviceInterface, data []byte, messageType int) error
	// GetOnlineCount 获取在线设备数量（普通设备数，幽灵设备数）
	GetOnlineCount() (normalCount, ghostCount int)
}
