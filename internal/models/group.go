package models

import (
	"net"
	"sync"
	"time"
)

// Group 群组信息
type Group struct {
	ID          int                    `json:"id"`
	Name        string                 `json:"name"`
	Type        int                    `json:"type"`        // 1: 中继, 7: 会议
	CallSign    string                 `json:"callsign"`
	Password    string                 `json:"password"`
	AllowDMRID  string                 `json:"allow_dmrid"`
	AllowCallSignSSID string           `json:"allow_callsign_ssid"`
	OwerID      int                    `json:"ower_id"`
	OwerCallSign string                `json:"ower_callsign"`
	DevList     string                 `json:"devlist"`
	MasterServer int                   `json:"master_server"`
	SlaveServer  int                   `json:"slave_server"`
	Status       int                   `json:"status"`
	CreateTime   string                `json:"create_time"`
	UpdateTime   string                `json:"update_time"`
	Note         string                `json:"note"`

	// Runtime fields (not stored in DB)
	DevMap    map[int]*Device          `json:"dev_map,omitempty"`
	connPool  *currentConnPool         `json:"-"`
}

// currentConnPool 当前连接池
type currentConnPool struct {
	UDPAddr       *net.UDPAddr
	lastVoiceTime time.Time
	lastCtlTime   time.Time
	lastPriority  int

	devConnMap  map[string]*Device
	devConnList []*Device
}

// GroupMap 群组映射（线程安全）
type GroupMap struct {
	sync.RWMutex
	m map[int]*Group
}

// NewGroupMap 创建新的群组映射
func NewGroupMap() *GroupMap {
	return &GroupMap{
		m: make(map[int]*Group),
	}
}

// Load 读取群组
func (gm *GroupMap) Load(key int) (*Group, bool) {
	gm.RLock()
	defer gm.RUnlock()
	g, ok := gm.m[key]
	return g, ok
}

// Store 存储群组
func (gm *GroupMap) Store(key int, value *Group) {
	gm.Lock()
	defer gm.Unlock()
	gm.m[key] = value
}

// Delete 删除群组
func (gm *GroupMap) Delete(key int) {
	gm.Lock()
	defer gm.Unlock()
	delete(gm.m, key)
}

// Range 遍历群组
func (gm *GroupMap) Range(fn func(key int, value *Group) bool) {
	gm.RLock()
	defer gm.RUnlock()
	for k, v := range gm.m {
		if !fn(k, v) {
			break
		}
	}
}

// Len 返回群组数量
func (gm *GroupMap) Len() int {
	gm.RLock()
	defer gm.RUnlock()
	return len(gm.m)
}
