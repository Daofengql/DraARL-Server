package handler

import (
	"draarl/internal/groupaccess"
	"time"
)

const (
	groupTypePublic  = groupaccess.TypePublic
	groupTypePrivate = groupaccess.TypePrivate
)

func isSupportedGroupType(groupType int) bool {
	return groupaccess.IsSupportedType(groupType)
}

// GroupInfo 群组信息响应

type GroupInfo struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Type         int    `json:"type"`
	OwerID       int    `json:"ower_id"`
	MasterServer int    `json:"master_server"`
	SlaveServer  int    `json:"slave_server"`
	Status       int    `json:"status"`
	CreateTime   string `json:"create_time,omitempty"`
	UpdateTime   string `json:"update_time,omitempty"`
	Note         string `json:"note"`
}

type GroupMemberInfo struct {
	ID          int       `json:"id"`
	GroupID     int       `json:"group_id"`
	UserID      int       `json:"user_id"`
	Username    string    `json:"username"`
	CallSign    string    `json:"callsign"`
	Nickname    string    `json:"nickname"`
	IsVerified  bool      `json:"is_verified"`
	JoinTime    time.Time `json:"join_time"`
	LastVerify  time.Time `json:"last_verify"`
	DeviceCount int64     `json:"device_count"`
}

// GetGroups 获取当前用户可见的群组列表（公开群组 + 已加入私有群组）。

type CreateGroupRequest struct {
	Name     string `json:"name" binding:"required"`
	Type     int    `json:"type"`
	Password string `json:"password"`
	Note     string `json:"note"`
	Status   int    `json:"status"`
}

// CreateGroup 创建群组

type UpdateGroupRequest struct {
	ID       int     `json:"id"` // 兼容 POST /group/update
	Name     string  `json:"name"`
	Type     int     `json:"type"`
	Password string  `json:"password"`
	Note     *string `json:"note"`
	Status   *int    `json:"status"`
}

// UpdateGroup 更新群组

type UpdateGroupDeviceCommControlRequest struct {
	DisableSend *bool `json:"disable_send"`
	DisableRecv *bool `json:"disable_recv"`
}

// UpdateGroupDeviceCommControl 允许管理员或当前群主临时控制组内普通设备收发。

type SearchGroupsRequest struct {
	Keyword  string `json:"keyword"`
	Query    string `json:"query"` // 兼容旧电台客户端
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// SearchGroups 搜索群组

type JoinGroupRequest struct {
	Password string `json:"password" binding:"required"`
}

// JoinGroup 加入群组（验证密码）
