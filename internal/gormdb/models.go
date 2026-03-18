package gormdb

import (
	"time"

	"gorm.io/gorm"
)

// User 用户模型
type User struct {
	ID              int        `gorm:"primaryKey;autoIncrement" json:"id"`
	Name            string     `gorm:"type:varchar(255);uniqueIndex;column:name" json:"name"`
	CallSign        string     `gorm:"type:varchar(32);index;column:callsign" json:"callsign"`
	Gird            string     `gorm:"type:varchar(255);column:gird" json:"gird"`
	Phone           string     `gorm:"type:varchar(32);index;column:phone" json:"phone"`
	Password        string     `gorm:"type:varchar(255);column:password" json:"-"`
	Birthday        string     `gorm:"type:varchar(32);column:birthday" json:"birthday"`
	Sex             int        `gorm:"type:tinyint;default:0;column:sex" json:"sex"`
	Avatar          string     `gorm:"type:varchar(512);column:avatar" json:"avatar"`
	Address         string     `gorm:"type:varchar(512);column:address" json:"address"`
	Roles           string     `gorm:"type:varchar(32);column:roles;default:user" json:"roles"` // 单角色：user 或 admin
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
	OpenID          string     `gorm:"type:varchar(255);index;column:openid" json:"openid"`
	NickName        string     `gorm:"type:varchar(255);column:nickname" json:"nickname"`
	PID             string     `gorm:"type:varchar(255);column:pid" json:"pid"`
	LastLoginIP     string     `gorm:"type:varchar(64);column:last_login_ip" json:"last_login_ip"`
	DMRID           int        `gorm:"type:int;default:0;column:dmrid" json:"dmrid"`
	MDCID           string     `gorm:"type:varchar(255);default:'';column:mdcid" json:"mdcid"`
	DevicePassword  string     `gorm:"type:varchar(255);column:device_password" json:"-"` // 设备准入密码(bcrypt哈希)
	LastGroupID     int        `gorm:"type:int;default:999;column:last_group_id" json:"last_group_id"` // 用户最后选中的群组
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

// HasRole 检查用户是否有指定角色（单角色系统）
func (u *User) HasRole(role string) bool {
	if u.Roles == "" {
		return role == "user"
	}
	return u.Roles == role
}

// GetRoles 返回用户的角色列表（单角色系统，返回单元素数组）
func (u *User) GetRoles() []string {
	if u.Roles == "" {
		return []string{"user"}
	}
	return []string{u.Roles}
}

// Device 设备模型
type Device struct {
	ID          int       `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	Name        string    `gorm:"type:varchar(255);column:name" json:"name"`
	DMRID       int64     `gorm:"type:bigint;index;column:dmrid" json:"dmrid"`
	SSID        uint8     `gorm:"type:tinyint unsigned;index:idx_owner_ssid,priority:2;column:ssid" json:"ssid"`
	OwnerID     int       `gorm:"type:int;index:idx_owner_ssid,priority:1;column:owner_id" json:"owner_id"` // 外键关联 users.id
	QTH         string    `gorm:"type:varchar(255);column:qth" json:"qth"`                                   // 位置信息 (原 gird 字段)
	DevModel    int       `gorm:"type:int;column:dev_model" json:"dev_model"`
	GroupID     int       `gorm:"type:int;index;index:idx_group_online,priority:1;column:group_id" json:"group_id"` // 性能优化：复合索引用于在线设备统计
	Status      int8      `gorm:"type:tinyint;default:1;column:status" json:"status"`
	IsCerted    bool      `gorm:"type:tinyint(1);default:0;column:is_certed" json:"is_certed"`
	Priority    int       `gorm:"type:int;default:100;column:priority" json:"priority"`
	DisableSend bool      `gorm:"type:tinyint(1);default:0;column:disable_send" json:"disable_send"` // 设备级禁发
	DisableRecv bool      `gorm:"type:tinyint(1);default:0;column:disable_recv" json:"disable_recv"` // 设备级禁收
	ISOnline    bool      `gorm:"type:tinyint(1);default:0;index:idx_group_online,priority:2;column:is_online" json:"is_online"` // 性能优化：复合索引
	OnlineTime  time.Time `gorm:"type:datetime;column:online_time" json:"online_time"`
	Note        string    `gorm:"type:text;column:note" json:"note"`
	CreateTime  time.Time `gorm:"autoCreateTime;column:create_time" json:"create_time"`
	UpdateTime  time.Time `gorm:"autoUpdateTime;column:update_time" json:"update_time"`
}

// TableName 指定表名
func (Device) TableName() string {
	return "devices"
}

// BeforeCreate GORM hook: 新设备创建时，设置上线时间为当前时间
func (d *Device) BeforeCreate(tx *gorm.DB) error {
	if d.OnlineTime.IsZero() {
		d.OnlineTime = time.Now()
	}
	return nil
}

// Group 群组模型
type Group struct {
	ID                int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name              string    `gorm:"type:varchar(255);column:name" json:"name"`
	Type              int       `gorm:"type:int;column:type" json:"type"`
	CallSign          string    `gorm:"type:varchar(255);column:call_sign" json:"callsign"`
	Password          string    `gorm:"type:varchar(255);column:password" json:"password"`
	AllowCallSignSSID string    `gorm:"type:text;column:allow_callsign_ssid" json:"allow_callsign_ssid"`
	OwerID            int       `gorm:"type:int;column:ower_id" json:"ower_id"`
	MasterServer      int       `gorm:"type:int;column:master_server" json:"master_server"`
	SlaveServer       int       `gorm:"type:int;column:slave_server" json:"slave_server"`
	Status            int       `gorm:"type:int;default:1;column:status" json:"status"`
	IsVirtual         bool      `gorm:"type:tinyint(1);default:0;column:is_virtual" json:"is_virtual"` // 是否为虚拟互联组
	CreateTime        time.Time `gorm:"autoCreateTime;column:create_time" json:"create_time"`
	UpdateTime        time.Time `gorm:"autoUpdateTime;column:update_time" json:"update_time"`
	Note              string    `gorm:"type:text;column:note" json:"note"`

	// 关联
	Devices []*Device `gorm:"-" json:"devices,omitempty"`
}

// TableName 指定表名
func (Group) TableName() string {
	return "public_groups"
}

// GroupLink 群组互联关联模型
type GroupLink struct {
	ID            int       `gorm:"primaryKey;autoIncrement" json:"id"`
	LinkGroupID   int       `gorm:"type:int;not null;uniqueIndex:uk_link_target,priority:1;column:link_group_id" json:"link_group_id"`   // 互联组ID
	TargetGroupID int       `gorm:"type:int;not null;uniqueIndex:uk_link_target,priority:2;column:target_group_id" json:"target_group_id"` // 目标群组ID
	CreatedAt     time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`
}

// TableName 指定表名
func (GroupLink) TableName() string {
	return "group_links"
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

// OperatorCert 操作证模型
type OperatorCert struct {
	ID          int        `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      int        `gorm:"type:int;index;column:user_id" json:"user_id"`
	CallSign    string     `gorm:"type:varchar(32);column:callsign" json:"callsign"` // 操作证上的呼号
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

// SiteConfig 站点配置模型
type SiteConfig struct {
	ID          int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Key         string    `gorm:"type:varchar(100);uniqueIndex;not null;column:config_key" json:"key"`
	Value       string    `gorm:"type:text;column:config_value" json:"value"`
	Category    string    `gorm:"type:varchar(50);index;column:category" json:"category"` // icp, system, aprs
	Description string    `gorm:"type:varchar(255);column:description" json:"description"`
	CreateTime  time.Time `gorm:"autoCreateTime;column:create_time" json:"create_time"`
	UpdateTime  time.Time `gorm:"autoUpdateTime;column:update_time" json:"update_time"`
}

// TableName 指定表名
func (SiteConfig) TableName() string {
	return "site_configs"
}

// GroupMember 群组成员关系（用户与群组的验证关系）
type GroupMember struct {
	ID           int        `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID      int        `gorm:"index:idx_group_user;column:group_id" json:"group_id"`
	UserID       int        `gorm:"index:idx_group_user;column:user_id" json:"user_id"`
	IsVerified   bool       `gorm:"type:tinyint(1);default:0;column:is_verified" json:"is_verified"`
	JoinTime     time.Time  `gorm:"autoCreateTime;column:join_time" json:"join_time"`
	LastVerify   time.Time  `gorm:"autoUpdateTime;column:last_verify" json:"last_verify"`
	DeviceID     *int       `gorm:"index;column:device_id" json:"device_id,omitempty"`
	DisableSend  bool       `gorm:"type:tinyint(1);default:0;column:disable_send" json:"disable_send"`
	DisableRecv  bool       `gorm:"type:tinyint(1);default:0;column:disable_recv" json:"disable_recv"`
	CreateTime   time.Time  `gorm:"autoCreateTime;column:create_time" json:"created_at"`
	UpdateTime   time.Time  `gorm:"autoUpdateTime;column:update_time" json:"updated_at"`
}

// TableName 指定表名
func (GroupMember) TableName() string {
	return "group_members"
}

// CommRecord 通信记录（精简版，名称通过联表查询获取）
type CommRecord struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID   uint      `gorm:"index;not null;column:device_id" json:"device_id"`    // 发送设备ID（0=幽灵设备，>0=普通设备）
	DeviceSSID uint8     `gorm:"column:device_ssid" json:"device_ssid"`               // 设备 SSID（冗余，便于查询）
	GroupID    *uint     `gorm:"index;index:idx_group_start,priority:1;column:group_id" json:"group_id"` // 性能优化：复合索引用于按群组查询
	UserID     *uint     `gorm:"index;index:idx_user_start,priority:1;column:user_id" json:"user_id"`   // 性能优化：复合索引用于按用户查询
	StartTime  time.Time `gorm:"index;index:idx_group_start,priority:2;index:idx_user_start,priority:2;not null;column:start_time" json:"start_time"` // 性能优化：复合索引
	EndTime    time.Time `gorm:"column:end_time" json:"end_time"`                     // 通信结束时间
	DurationMs int       `gorm:"column:duration_ms" json:"duration_ms"`               // 通信时长（毫秒）
	AudioPath  string    `gorm:"type:varchar(255);column:audio_path" json:"audio_path"` // MinIO 音频文件路径
	AudioSize  int64     `gorm:"column:audio_size" json:"audio_size"`                 // 音频文件大小（字节）
	Status     int       `gorm:"default:0;index;column:status" json:"status"`         // 状态：0=录制中,1=待上传,2=已完成,3=上传失败
	CreatedAt  time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
}

// TableName 指定表名
func (CommRecord) TableName() string {
	return "comm_records"
}

// Asset 资源管理模型（虚拟文件系统）
type Asset struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ParentID  *uint     `gorm:"index;column:parent_id" json:"parent_id"`           // 父目录ID（NULL表示根目录）
	Name      string    `gorm:"type:varchar(255);not null;column:name" json:"name"` // 显示名称（虚拟）
	Type      string    `gorm:"type:varchar(20);not null;column:type" json:"type"`  // "folder" | "file"
	Path      string    `gorm:"type:varchar(512);column:path" json:"path"`          // MinIO真实路径（仅文件有值）
	Size      int64     `gorm:"column:size" json:"size"`                            // 文件大小（字节）
	MimeType  string    `gorm:"type:varchar(100);column:mime_type" json:"mime_type"` // MIME类型
	Remark    string    `gorm:"type:text;column:remark" json:"remark"`              // 备注
	SortOrder int       `gorm:"default:0;column:sort_order" json:"sort_order"`      // 排序权重
	CreatedAt time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`
}

// TableName 指定表名
func (Asset) TableName() string {
	return "assets"
}

// IsFolder 判断是否为文件夹
func (a *Asset) IsFolder() bool {
	return a.Type == "folder"
}

// IsFile 判断是否为文件
func (a *Asset) IsFile() bool {
	return a.Type == "file"
}

// AutoMigrate 自动迁移表结构
func AutoMigrate() error {
	return Get().AutoMigrate(
		&User{},
		&Device{},
		&Group{},
		&GroupLink{},
		&Server{},
		&Relay{},
		&OperatorLog{},
		&OperatorCert{},
		&SiteConfig{},
		&GroupMember{},
		&CommRecord{},
		&Asset{},
	)
}
