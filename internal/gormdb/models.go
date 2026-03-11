package gormdb

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// User 用户模型
type User struct {
	ID              int        `gorm:"primaryKey;autoIncrement" json:"id"`
	Name            string     `gorm:"type:varchar(255);column:name" json:"name"`
	CallSign        string     `gorm:"type:varchar(32);uniqueIndex;column:callsign" json:"callsign"`
	Gird            string     `gorm:"type:varchar(255);column:gird" json:"gird"`
	Phone           string     `gorm:"type:varchar(32);uniqueIndex;column:phone" json:"phone"`
	Password        string     `gorm:"type:varchar(255);column:password" json:"-"`
	Birthday        string     `gorm:"type:varchar(32);column:birthday" json:"birthday"`
	Sex             int        `gorm:"type:tinyint;default:0;column:sex" json:"sex"`
	Avatar          string     `gorm:"type:varchar(512);column:avatar" json:"avatar"`
	AvatarThumb     string     `gorm:"type:varchar(512);column:avatar_thumb" json:"avatar_thumb"` // 头像缩略图
	Address         string     `gorm:"type:varchar(512);column:address" json:"address"`
	Roles           string     `gorm:"type:text;column:roles" json:"roles"` // JSON array string
	Introduction    string     `gorm:"type:text;column:introduction" json:"introduction"`
	AlarmMsg        bool       `gorm:"type:tinyint(1);default:0;column:alarm_msg" json:"alarm_msg"`
	Status          int        `gorm:"type:tinyint;default:1;column:status" json:"status"`
	ApprovalStatus  int        `gorm:"type:tinyint;default:0;column:approval_status" json:"approval_status"` // 0=待审核, 1=已通过, 2=已拒绝
	ReviewerID      *int       `gorm:"type:int;column:reviewer_id" json:"reviewer_id"`                    // 审核人ID
	ReviewNote      string     `gorm:"type:text;column:review_note" json:"review_note"`                // 审核备注
	ReviewTime      *time.Time `gorm:"type:datetime;column:review_time" json:"review_time"`            // 审核时间
	UpdateTime      time.Time  `gorm:"autoUpdateTime;column:update_time" json:"update_time"`
	LastLoginTime   *time.Time `gorm:"type:datetime;column:last_login_time" json:"last_login_time"`
	LoginErrTimes   int        `gorm:"type:int;default:0;column:login_err_times" json:"login_err_times"`
	CreateTime      time.Time  `gorm:"autoCreateTime;column:create_time" json:"create_time"`
	OpenID          string     `gorm:"type:varchar(255);uniqueIndex;column:openid" json:"openid"`
	NickName        string     `gorm:"type:varchar(255);column:nickname" json:"nickname"`
	PID             string     `gorm:"type:varchar(255);column:pid" json:"pid"`
	LastLoginIP     string     `gorm:"type:varchar(64);column:last_login_ip" json:"last_login_ip"`
	DMRID           int        `gorm:"type:int;default:0;column:dmrid" json:"dmrid"`
	MDCID           string     `gorm:"type:varchar(255);default:'';column:mdcid" json:"mdcid"`

	// 关联
	Groups []*Group `gorm:"many2many:user_groups;" json:"groups,omitempty"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}

// BeforeCreate GORM hook: 新用户注册时，将最后登录时间设置为当前时间
func (u *User) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	u.LastLoginTime = &now
	return nil
}

// HasRole 检查用户是否有指定角色
func (u *User) HasRole(role string) bool {
	// TODO: 解析 roles JSON 字符串并检查
	if u.Roles == "" {
		return role == "user"
	}
	// 简单检查：如果 roles 包含 admin 字符串
	return u.Roles == "[\""+role+"\"]" || u.Roles == "["+role+"]"
}

// GetRoles 实现 UserWithRoles 接口
func (u *User) GetRoles() []string {
	if u.Roles == "" {
		return []string{"user"}
	}
	// 尝试解析 JSON 数组格式
	rolesStr := u.Roles
	// 移除最外层的方括号和引号
	rolesStr = strings.Trim(rolesStr, "[]\"'")
	// 按逗号或空格分割
	parts := strings.FieldsFunc(rolesStr, func(r rune) bool {
		return r == ',' || r == ' ' || r == '"'
	})
	// 清理空字符串
	result := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		return []string{"user"}
	}
	return result
}

// Device 设备模型
type Device struct {
	ID         int       `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	Name       string    `gorm:"type:varchar(255);column:name" json:"name"`
	DMRID      int64     `gorm:"type:bigint;index;column:dmrid" json:"dmrid"`
	CallSign   string    `gorm:"type:varchar(32);index:idx_callsign_ssid,priority:1;column:callsign" json:"callsign"`
	SSID       uint8     `gorm:"type:tinyint unsigned;index:idx_callsign_ssid,priority:2;column:ssid" json:"ssid"`
	Password   string    `gorm:"type:varchar(255);column:password" json:"password"`
	Gird       string    `gorm:"type:varchar(255);column:gird" json:"gird"`
	DevType    int       `gorm:"type:int;column:dev_type" json:"dev_type"`
	DevModel   int       `gorm:"type:int;column:dev_model" json:"dev_model"`
	GroupID    int       `gorm:"type:int;index;column:group_id" json:"group_id"`
	Status     int8      `gorm:"type:tinyint;default:1;column:status" json:"status"`
	IsCerted   bool      `gorm:"type:tinyint(1);default:0;column:is_certed" json:"is_certed"`
	ChanName   string    `gorm:"type:text;column:chan_name" json:"chan_name"`
	OnlineTime time.Time `gorm:"type:datetime;column:online_time" json:"online_time"`
	CreateTime time.Time `gorm:"autoCreateTime;column:create_time" json:"create_time"`
	UpdateTime time.Time `gorm:"autoUpdateTime;column:update_time" json:"update_time"`
	Note       string    `gorm:"type:text;column:note" json:"note"`
	Priority   int       `gorm:"type:int;default:100;column:priority" json:"priority"`
	ISOnline   bool      `gorm:"-" json:"is_online"` // 运行时字段
}

// TableName 指定表名
func (Device) TableName() string {
	return "devices"
}

// Group 群组模型
type Group struct {
	ID                int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name              string    `gorm:"type:varchar(255)" json:"name"`
	Type              int       `gorm:"type:int" json:"type"`
	CallSign          string    `gorm:"type:varchar(255)" json:"callsign"`
	Password          string    `gorm:"type:varchar(255)" json:"password"`
	AllowDMRID        string    `gorm:"type:text" json:"allow_dmrid"`
	AllowCallSignSSID string    `gorm:"type:text" json:"allow_callsign_ssid"`
	OwerID            int       `gorm:"type:int" json:"ower_id"`
	OwerCallSign      string    `gorm:"type:varchar(255)" json:"ower_callsign"`
	DevList           string    `gorm:"type:text" json:"devlist"`
	MasterServer      int       `gorm:"type:int" json:"master_server"`
	SlaveServer       int       `gorm:"type:int" json:"slave_server"`
	Status            int       `gorm:"type:int;default:1" json:"status"`
	CreateTime        time.Time `gorm:"autoCreateTime" json:"create_time"`
	UpdateTime        time.Time `gorm:"autoUpdateTime" json:"update_time"`
	Note              string    `gorm:"type:text" json:"note"`

	// 关联
	Devices []*Device `gorm:"-" json:"devices,omitempty"`
}

// TableName 指定表名
func (Group) TableName() string {
	return "public_groups"
}

// Server 服务器模型
type Server struct {
	ID          int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"type:varchar(255)" json:"name"`
	ServerType  int       `gorm:"type:int" json:"server_type"`
	JoinKey     string    `gorm:"type:varchar(255)" json:"join_key"`
	CPUType     string    `gorm:"type:varchar(255)" json:"cpu_type"`
	MemSize     string    `gorm:"type:varchar(255)" json:"mem_size"`
	InputRate   int       `gorm:"type:int" json:"input_rate"`
	OutputRate  int       `gorm:"type:int" json:"output_rate"`
	Netcard     string    `gorm:"type:varchar(255)" json:"netcard"`
	IPType      int       `gorm:"type:int" json:"ip_type"`
	IPAddr      string    `gorm:"type:varchar(255)" json:"ip_addr"`
	DNSName     string    `gorm:"type:varchar(255)" json:"dns_name"`
	GroupList   int       `gorm:"type:int" json:"group_list"`
	OwerID      string    `gorm:"type:varchar(255)" json:"ower_id"`
	OwerCallSign string   `gorm:"type:varchar(255)" json:"ower_callsign"`
	IsOnline    bool      `gorm:"type:tinyint(1)" json:"is_online"`
	Status      int       `gorm:"type:int" json:"status"`
	CreateTime  time.Time `gorm:"autoCreateTime" json:"create_time"`
	UpdateTime  time.Time `gorm:"autoUpdateTime" json:"update_time"`
	Note        string    `gorm:"type:text" json:"note"`
	UDPPort     int       `gorm:"type:int" json:"udp_port"`
}

// TableName 指定表名
func (Server) TableName() string {
	return "servers"
}

// String 返回服务器的字符串表示
func (s *Server) String() string {
	return s.Name
}

// Relay 中继台模型
type Relay struct {
	ID         int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name       string    `gorm:"type:varchar(255)" json:"name"`
	UpFreq     string    `gorm:"type:varchar(255)" json:"up_freq"`
	DownFreq   string    `gorm:"type:varchar(255)" json:"down_freq"`
	SendCTSS   string    `gorm:"type:varchar(255)" json:"send_ctss"`
	ReciveCTSS string    `gorm:"type:varchar(255)" json:"recive_ctss"`
	OwerCallSign string  `gorm:"type:varchar(255)" json:"ower_callsign"`
	CreateTime time.Time `gorm:"autoCreateTime" json:"create_time"`
	UpdateTime time.Time `gorm:"autoUpdateTime" json:"update_time"`
	Status     int       `gorm:"type:int;default:1" json:"status"`
	Note       string    `gorm:"type:text" json:"note"`
}

// TableName 指定表名
func (Relay) TableName() string {
	return "relay"
}

// String 返回中继台的字符串表示
func (r *Relay) String() string {
	return r.Name
}

// OperatorLog 操作日志模型
type OperatorLog struct {
	ID         int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Timestamp  time.Time `gorm:"autoCreateTime" json:"timestamp"`
	Content    string    `gorm:"type:text" json:"content"`
	EventType  string    `gorm:"type:varchar(255);index" json:"event_type"`
	Operator   string    `gorm:"type:varchar(255)" json:"operator"`
	OperatorID int       `gorm:"type:int;index" json:"operator_id"`
}

// TableName 指定表名
func (OperatorLog) TableName() string {
	return "operator_log"
}

// Role 角色模型
type Role struct {
	ID          int    `gorm:"primaryKey;autoIncrement" json:"id"`
	NameKey     string `gorm:"type:varchar(255)" json:"name_key"`
	Name        string `gorm:"type:varchar(255)" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Routes      string `gorm:"type:text" json:"routes"`
}

// TableName 指定表名
func (Role) TableName() string {
	return "roles"
}

// OperatorCert 操作证模型
type OperatorCert struct {
	ID          int        `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      int        `gorm:"type:int;index;column:user_id" json:"user_id"`
	FileName    string     `gorm:"type:varchar(255);column:file_name" json:"file_name"`
	MinioBucket string     `gorm:"type:varchar(255);column:minio_bucket" json:"minio_bucket"`
	MinioPath   string     `gorm:"type:varchar(512);column:minio_path" json:"minio_path"`
	FileSize    int64      `gorm:"type:bigint;column:file_size" json:"file_size"`
	FileType    string     `gorm:"type:varchar(100);column:file_type" json:"file_type"`
	UploadTime  time.Time  `gorm:"autoCreateTime;column:upload_time" json:"upload_time"`
	Status      int        `gorm:"type:tinyint;default:0;column:status" json:"status"`        // 0=待审核, 1=已通过, 2=已拒绝/已替换
	OldCertID   *int       `gorm:"type:int;column:old_cert_id" json:"old_cert_id"`       // 被替换的旧证书ID
	ReviewNote  string     `gorm:"type:text;column:review_note" json:"review_note"`    // 审核备注
	ReviewTime  *time.Time `gorm:"type:datetime;column:review_time" json:"review_time"` // 审核时间
	ReviewerID  *int       `gorm:"type:int;column:reviewer_id" json:"reviewer_id"`    // 审核人ID
}

// TableName 指定表名
func (OperatorCert) TableName() string {
	return "operator_certs"
}

// AutoMigrate 自动迁移表结构
func AutoMigrate() error {
	return Get().AutoMigrate(
		&User{},
		&Device{},
		&Group{},
		&Server{},
		&Relay{},
		&OperatorLog{},
		&Role{},
		&OperatorCert{},
	)
}
