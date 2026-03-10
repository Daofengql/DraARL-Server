package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// StringArray 用于处理 PostgreSQL 数组类型
type StringArray []string

// Scan 实现 sql.Scanner 接口
func (a *StringArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, a)
	case string:
		return json.Unmarshal([]byte(v), a)
	default:
		*a = nil
		return nil
	}
}

// Value 实现 driver.Valuer 接口
func (a StringArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return nil, nil
	}
	return json.Marshal(a)
}

// User 用户信息
type User struct {
	ID            int          `json:"id"`
	Name          string       `json:"name"`
	CallSign      string       `json:"callsign"`
	Phone         string       `json:"phone"`
	Password      string       `json:"-"`
	Birthday      string       `json:"birthday"`
	Sex           bool         `json:"sex"`
	Avatar        string       `json:"avatar"`
	Address       string       `json:"address"`
	Roles         []string     `json:"roles"`
	Introduction  string       `json:"introduction"`
	AlarmMsg      bool         `json:"alarm_msg"`
	Status        int          `json:"status"`
	UpdateTime    string       `json:"update_time"`
	LastLoginTime string       `json:"last_login_time"`
	LoginErrTimes int          `json:"login_err_times"`
	CreateTime    string       `json:"create_time"`
	OpenID        string       `json:"openid"`
	NickName      string       `json:"nickname"`
	HeadImgURL    string       `json:"headimgurl"`
	LastLoginIP   string       `json:"last_login_ip"`

	// Runtime fields
	Groups      map[int]*Group `json:"groups,omitempty"`
	DevList     []DeviceInfo  `json:"dev_list,omitempty"`
	TalkDuration int64        `json:"talk_duration"`
	TalkTimes   int           `json:"talk_times"`
	DMRID       uint32        `json:"dmrid"`
	MDCID       string        `json:"mdcid"`
}

// DeviceInfo 简化的设备信息
type DeviceInfo struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	DevModel byte   `json:"dev_model"`
	CallSign string `json:"callsign"`
	SSID     byte   `json:"ssid"`
}

// Role 角色信息
type Role struct {
	ID          int    `json:"id"`
	NameKey     string `json:"name_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Routes      string `json:"routes"`
}

// OperatorLog 操作日志
type OperatorLog struct {
	ID         int       `json:"id" db:"id"`
	Timestamp  time.Time `json:"timestamp" db:"timestamp"`
	Content    string    `json:"content" db:"content"`
	EventType  string    `json:"event_type" db:"event_type"`
	Operator   string    `json:"operator" db:"operator"`
	OperatorID int       `json:"operator_id" db:"operator_id"`
}
