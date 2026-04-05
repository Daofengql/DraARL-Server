package models

import (
	"net"
	"sync"
	"time"
)

// Group 群组信息
type Group struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Type              int    `json:"type"` // 1: 公开, 2: 私有
	CallSign          string `json:"callsign"`
	Password          string `json:"password"`
	AllowCallSignSSID string `json:"allow_callsign_ssid"`
	OwerID            int    `json:"ower_id"`
	DevList           []int  `json:"devlist"` // 内存中动态计算的设备ID列表
	MasterServer      int    `json:"master_server"`
	SlaveServer       int    `json:"slave_server"`
	Status            int    `json:"status"`
	IsVirtual         bool   `json:"is_virtual"` // 是否为虚拟互联组
	CreateTime        string `json:"create_time"`
	UpdateTime        string `json:"update_time"`
	Note              string `json:"note"`

	// Runtime fields (not stored in DB)
	DevMap          map[int]*Device `json:"dev_map,omitempty"`
	OnlineDevNumber int             `json:"online_dev_number"`
	TotalDevNumber  int             `json:"total_dev_number"`
	ConnPool        interface{}     `json:"-"` // Will be set to *udphub.CurrentConnPool
}

// CurrentConnPool 当前连接池
type CurrentConnPool struct {
	UDPAddr       *net.UDPAddr
	LastVoiceTime time.Time
	LastCtlTime   time.Time
	LastPriority  int

	DevConnMap  map[string]*Device
	DevConnList []*Device
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

// GetConnPool 获取连接池（需要类型断言）
func (g *Group) GetConnPool() interface{} {
	return g.ConnPool
}

// SetConnPool 设置连接池
func (g *Group) SetConnPool(pool interface{}) {
	g.ConnPool = pool
}
