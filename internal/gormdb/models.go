package gormdb

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"
)

// User 用户模型
type User struct {
	ID             int        `gorm:"primaryKey;autoIncrement" json:"id"`
	Name           string     `gorm:"type:varchar(255);uniqueIndex;column:name" json:"name"`
	Email          string     `gorm:"type:varchar(255);uniqueIndex;column:email" json:"email"`
	EmailVerified  bool       `gorm:"type:tinyint(1);default:0;column:email_verified" json:"email_verified"`
	CallSign       string     `gorm:"type:varchar(32);uniqueIndex:uk_users_callsign;column:callsign" json:"callsign"`
	Gird           string     `gorm:"type:varchar(255);column:gird" json:"gird"`
	Phone          string     `gorm:"type:varchar(32);index;column:phone" json:"phone"`
	Password       string     `gorm:"type:varchar(255);column:password" json:"-"`
	Birthday       string     `gorm:"type:varchar(32);column:birthday" json:"birthday"`
	Sex            int        `gorm:"type:tinyint;default:0;column:sex" json:"sex"`
	Avatar         string     `gorm:"type:varchar(512);column:avatar" json:"avatar"`
	Address        string     `gorm:"type:varchar(512);column:address" json:"address"`
	Roles          string     `gorm:"type:varchar(32);column:roles;default:user" json:"roles"` // 单角色：user 或 admin
	Introduction   string     `gorm:"type:text;column:introduction" json:"introduction"`
	AlarmMsg       bool       `gorm:"type:tinyint(1);default:0;column:alarm_msg" json:"alarm_msg"`
	Status         int        `gorm:"type:tinyint;default:1;column:status" json:"status"`
	ApprovalStatus int        `gorm:"type:tinyint;default:0;column:approval_status" json:"approval_status"` // 0=待审核, 1=已通过, 2=已拒绝
	ReviewerID     *int       `gorm:"type:int;column:reviewer_id" json:"reviewer_id"`                       // 审核人ID
	ReviewNote     string     `gorm:"type:text;column:review_note" json:"review_note"`                      // 审核备注
	ReviewTime     *time.Time `gorm:"type:datetime;column:review_time" json:"review_time"`                  // 审核时间
	UpdateTime     time.Time  `gorm:"autoUpdateTime;column:update_time" json:"update_time"`
	LastLoginTime  *time.Time `gorm:"type:datetime;column:last_login_time" json:"last_login_time"`
	LoginErrTimes  int        `gorm:"type:int;default:0;column:login_err_times" json:"login_err_times"`
	CreateTime     time.Time  `gorm:"autoCreateTime;column:create_time" json:"create_time"`
	OpenID         string     `gorm:"type:varchar(255);index;column:openid" json:"openid"`
	NickName       string     `gorm:"type:varchar(255);column:nickname" json:"nickname"`
	PID            string     `gorm:"type:varchar(255);column:pid" json:"pid"`
	LastLoginIP    string     `gorm:"type:varchar(64);column:last_login_ip" json:"last_login_ip"`
	DMRID          int        `gorm:"type:int;default:0;column:dmrid" json:"dmrid"`
	MDCID          string     `gorm:"type:varchar(255);default:'';column:mdcid" json:"mdcid"`
	DevicePassword string     `gorm:"type:varchar(255);column:device_password" json:"-"` // 设备准入密码（AES 可逆密文；兼容历史 bcrypt 并在认证后自动迁移）
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
	ID           int       `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	Name         string    `gorm:"type:varchar(255);column:name" json:"name"`
	DMRID        int64     `gorm:"type:bigint;index;column:dmrid" json:"dmrid"`
	SSID         uint8     `gorm:"type:tinyint unsigned;uniqueIndex:idx_owner_ssid,priority:2;column:ssid" json:"ssid"`
	OwnerID      int       `gorm:"uniqueIndex:idx_owner_ssid,priority:1;column:owner_id" json:"owner_id"` // 外键关联 users.id
	QTH          string    `gorm:"type:varchar(255);column:qth" json:"qth"`                               // 位置信息 (原 gird 字段)
	LastOnlineIP string    `gorm:"type:varchar(64);column:last_online_ip" json:"last_online_ip"`          // 设备最近一次上线时的客户端 IP
	DevModel     int       `gorm:"type:int;column:dev_model" json:"dev_model"`
	GroupID      int       `gorm:"type:int;index;index:idx_group_online,priority:1;column:group_id" json:"group_id"` // 性能优化：复合索引用于在线设备统计
	Status       int8      `gorm:"type:tinyint;default:1;column:status" json:"status"`
	IsCerted     bool      `gorm:"type:tinyint(1);default:0;column:is_certed" json:"is_certed"`
	Priority     int       `gorm:"type:int;default:100;column:priority" json:"priority"`
	DisableSend  bool      `gorm:"type:tinyint(1);default:0;column:disable_send" json:"disable_send"`                             // 设备级禁发
	DisableRecv  bool      `gorm:"type:tinyint(1);default:0;column:disable_recv" json:"disable_recv"`                             // 设备级禁收
	ISOnline     bool      `gorm:"type:tinyint(1);default:0;index:idx_group_online,priority:2;column:is_online" json:"is_online"` // 性能优化：复合索引
	OnlineTime   time.Time `gorm:"type:datetime;column:online_time" json:"online_time"`
	Note         string    `gorm:"type:text;column:note" json:"note"`
	CreateTime   time.Time `gorm:"autoCreateTime;column:create_time" json:"create_time"`
	UpdateTime   time.Time `gorm:"autoUpdateTime;column:update_time" json:"update_time"`

	// 关联定义：配置与 User 表的外键约束。
	// 当引用的 User 被删除时，数据库引擎会自动连带删除该 OwnerID 下的所有 Device 记录。
	Owner *User `gorm:"foreignKey:OwnerID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"owner,omitempty"`
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
	OwerID            int       `gorm:"type:int;index:idx_ower_id;column:ower_id" json:"ower_id"` // 性能优化：添加索引，加速按所有者查询
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
	LinkGroupID   int       `gorm:"not null;uniqueIndex:uk_link_target,priority:1;column:link_group_id" json:"link_group_id"`     // 互联组ID
	TargetGroupID int       `gorm:"not null;uniqueIndex:uk_link_target,priority:2;column:target_group_id" json:"target_group_id"` // 目标群组ID
	CreatedAt     time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	// 关联定义：双向级联删除。无论 LinkGroup 还是 TargetGroup 被删，此记录均消亡
	LinkGroup   *Group `gorm:"foreignKey:LinkGroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"link_group,omitempty"`
	TargetGroup *Group `gorm:"foreignKey:TargetGroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"target_group,omitempty"`
}

// TableName 指定表名
func (GroupLink) TableName() string {
	return "group_links"
}

// Server 服务器模型
type Server struct {
	ID           int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name         string    `gorm:"type:varchar(255)" json:"name"`
	ServerType   int       `gorm:"type:int" json:"server_type"`
	JoinKey      string    `gorm:"type:varchar(255)" json:"join_key"`
	CPUType      string    `gorm:"type:varchar(255)" json:"cpu_type"`
	MemSize      string    `gorm:"type:varchar(255)" json:"mem_size"`
	InputRate    int       `gorm:"type:int" json:"input_rate"`
	OutputRate   int       `gorm:"type:int" json:"output_rate"`
	Netcard      string    `gorm:"type:varchar(255)" json:"netcard"`
	IPType       int       `gorm:"type:int" json:"ip_type"`
	IPAddr       string    `gorm:"type:varchar(255)" json:"ip_addr"`
	DNSName      string    `gorm:"type:varchar(255)" json:"dns_name"`
	GroupList    int       `gorm:"type:int" json:"group_list"`
	OwerID       string    `gorm:"type:varchar(255)" json:"ower_id"`
	OwerCallSign string    `gorm:"type:varchar(255)" json:"ower_callsign"`
	IsOnline     bool      `gorm:"type:tinyint(1)" json:"is_online"`
	Status       int       `gorm:"type:int" json:"status"`
	CreateTime   time.Time `gorm:"autoCreateTime" json:"create_time"`
	UpdateTime   time.Time `gorm:"autoUpdateTime" json:"update_time"`
	Note         string    `gorm:"type:text" json:"note"`
	UDPPort      int       `gorm:"type:int" json:"udp_port"`
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
	ID           int       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name         string    `gorm:"type:varchar(255)" json:"name"`
	UpFreq       string    `gorm:"type:varchar(255)" json:"up_freq"`
	DownFreq     string    `gorm:"type:varchar(255)" json:"down_freq"`
	SendCTSS     string    `gorm:"type:varchar(255)" json:"send_ctss"`
	ReciveCTSS   string    `gorm:"type:varchar(255)" json:"recive_ctss"`
	OwerCallSign string    `gorm:"type:varchar(255)" json:"ower_callsign"`
	Location     string    `gorm:"type:varchar(255)" json:"location"`
	CreateTime   time.Time `gorm:"autoCreateTime" json:"create_time"`
	UpdateTime   time.Time `gorm:"autoUpdateTime" json:"update_time"`
	Status       int       `gorm:"type:int;default:1" json:"status"`
	Note         string    `gorm:"type:text" json:"note"`
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
	UserID      int        `gorm:"index;column:user_id" json:"user_id"`
	CallSign    string     `gorm:"type:varchar(32);column:callsign" json:"callsign"` // 操作证上的呼号
	FileName    string     `gorm:"type:varchar(255);column:file_name" json:"file_name"`
	MinioBucket string     `gorm:"type:varchar(255);column:minio_bucket" json:"minio_bucket"`
	MinioPath   string     `gorm:"type:varchar(512);column:minio_path" json:"minio_path"`
	FileSize    int64      `gorm:"type:bigint;column:file_size" json:"file_size"`
	FileType    string     `gorm:"type:varchar(100);column:file_type" json:"file_type"`
	UploadTime  time.Time  `gorm:"autoCreateTime;column:upload_time" json:"upload_time"`
	Status      int        `gorm:"type:tinyint;default:0;column:status" json:"status"`  // 0=待审核, 1=已通过, 2=已拒绝/已替换
	OldCertID   *int       `gorm:"type:int;column:old_cert_id" json:"old_cert_id"`      // 被替换的旧证书ID
	ReviewNote  string     `gorm:"type:text;column:review_note" json:"review_note"`     // 审核备注
	ReviewTime  *time.Time `gorm:"type:datetime;column:review_time" json:"review_time"` // 审核时间
	ReviewerID  *int       `gorm:"type:int;column:reviewer_id" json:"reviewer_id"`      // 审核人ID

	// 关联定义：人员离网，证书销毁
	User *User `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"user,omitempty"`
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
	ID         int       `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID    int       `gorm:"uniqueIndex:uk_group_user,priority:1;column:group_id;constraint:OnDelete:CASCADE" json:"group_id"`
	UserID     int       `gorm:"uniqueIndex:uk_group_user,priority:2;column:user_id;constraint:OnDelete:CASCADE" json:"user_id"`
	IsVerified bool      `gorm:"type:tinyint(1);default:0;column:is_verified" json:"is_verified"`
	JoinTime   time.Time `gorm:"autoCreateTime;column:join_time" json:"join_time"`
	LastVerify time.Time `gorm:"autoUpdateTime;column:last_verify" json:"last_verify"`
	CreateTime time.Time `gorm:"autoCreateTime;column:create_time" json:"created_at"`
	UpdateTime time.Time `gorm:"autoUpdateTime;column:update_time" json:"updated_at"`

	// 关联定义：群解散 或 人销号，都会清理当前的加群记录
	Group *Group `gorm:"foreignKey:GroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"group,omitempty"`
	User  *User  `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"user,omitempty"`
}

// TableName 指定表名
func (GroupMember) TableName() string {
	return "group_members"
}

// CommRecord 通信记录（精简版，名称通过联表查询获取）
type CommRecord struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID   uint      `gorm:"index;not null;column:device_id" json:"device_id"`                                                                    // 发送设备ID（0=幽灵设备，>0=普通设备）
	DeviceSSID uint8     `gorm:"column:device_ssid" json:"device_ssid"`                                                                               // 设备 SSID（冗余，便于查询）
	GroupID    *uint     `gorm:"index;index:idx_group_start,priority:1;column:group_id" json:"group_id"`                                              // 性能优化：复合索引用于按群组查询
	UserID     *uint     `gorm:"index;index:idx_user_start,priority:1;column:user_id" json:"user_id"`                                                 // 性能优化：复合索引用于按用户查询
	StartTime  time.Time `gorm:"index;index:idx_group_start,priority:2;index:idx_user_start,priority:2;not null;column:start_time" json:"start_time"` // 性能优化：复合索引
	EndTime    time.Time `gorm:"column:end_time" json:"end_time"`                                                                                     // 通信结束时间
	DurationMs int       `gorm:"column:duration_ms" json:"duration_ms"`                                                                               // 通信时长（毫秒）
	AudioPath  string    `gorm:"type:varchar(255);column:audio_path" json:"audio_path"`                                                               // MinIO 音频文件路径
	AudioSize  int64     `gorm:"column:audio_size" json:"audio_size"`                                                                                 // 音频文件大小（字节）
	Status     int       `gorm:"default:0;index;column:status" json:"status"`                                                                         // 状态：0=录制中,1=待上传,2=已完成,3=上传失败
	CreatedAt  time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
}

// TableName 指定表名
func (CommRecord) TableName() string {
	return "comm_records"
}

// Asset 资源管理模型（虚拟文件系统）
type Asset struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ParentID  *uint     `gorm:"index;column:parent_id" json:"parent_id"`             // 父目录ID（NULL表示根目录）
	Name      string    `gorm:"type:varchar(255);not null;column:name" json:"name"`  // 显示名称（虚拟）
	Type      string    `gorm:"type:varchar(20);not null;column:type" json:"type"`   // "folder" | "file"
	Path      string    `gorm:"type:varchar(512);column:path" json:"path"`           // MinIO真实路径（仅文件有值）
	Size      int64     `gorm:"column:size" json:"size"`                             // 文件大小（字节）
	MimeType  string    `gorm:"type:varchar(100);column:mime_type" json:"mime_type"` // MIME类型
	Remark    string    `gorm:"type:text;column:remark" json:"remark"`               // 备注
	SortOrder int       `gorm:"default:0;column:sort_order" json:"sort_order"`       // 排序权重
	CreatedAt time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	// 关联定义：自引用约束。父目录删除时，级联删除其下所有子内容
	Parent *Asset `gorm:"foreignKey:ParentID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
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

// UserDevicePreference 用户设备偏好设置
// 用于存储各平台（Android/iOS/Windows/macOS/Web）的独立偏好设置
type UserDevicePreference struct {
	ID          int       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      int       `gorm:"not null;uniqueIndex:uk_user_devmodel;column:user_id" json:"user_id"`     // 用户ID，外键关联 users表
	DevModel    uint8     `gorm:"not null;uniqueIndex:uk_user_devmodel;column:dev_model" json:"dev_model"` // 设备型号: 101=Android, 102=iOS, 103=Windows, 104=macOS, 105=Web
	LastGroupID int       `gorm:"default:0;column:last_group_id" json:"last_group_id"`                     // 该平台最后使用的群组ID
	CreatedAt   time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	// 关联定义：用户删除时，偏好设置级联销毁
	User *User `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

// TableName 指定表名
func (UserDevicePreference) TableName() string {
	return "user_device_preferences"
}

// DeviceConfig 设备配置模型
// 用于存储 UDP 普通设备的参数配置，支持双向同步
// 前置逻辑：配置参数依附于具体设备存在，设备销毁时配置毫无保留价值。
type DeviceConfig struct {
	ID          int       `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID    int       `gorm:"not null;uniqueIndex:uk_device_key;index;column:device_id" json:"device_id"` // 关联 devices.id
	ConfigKey   string    `gorm:"type:varchar(64);not null;uniqueIndex:uk_device_key;column:config_key" json:"config_key"`
	ConfigValue string    `gorm:"type:text;column:config_value" json:"config_value"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	// 关联定义：配置与 Device 表的外键级联约束
	Device *Device `gorm:"foreignKey:DeviceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

// TableName 指定表名
func (DeviceConfig) TableName() string {
	return "device_configs"
}

// Logbook 通联日志模型
type Logbook struct {
	ID           int       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       int       `gorm:"index;not null;column:user_id" json:"user_id"`                // 创建者ID
	MyCallSign   string    `gorm:"type:varchar(32);column:my_callsign" json:"my_callsign"`      // 我方呼号（冗余存储，支持客席发射）
	TimeUTC      time.Time `gorm:"index;column:time_utc" json:"time_utc"`                       // UTC时间（数据库统一存储UTC，前端自行换算BJT）
	TxFrequency  float64   `gorm:"type:decimal(10,4);column:tx_frequency" json:"tx_frequency"`  // 发射频率 (MHz)
	RxFrequency  float64   `gorm:"type:decimal(10,4);column:rx_frequency" json:"rx_frequency"`  // 接收频率 (MHz)
	CQZone       int       `gorm:"type:tinyint;column:cq_zone" json:"cq_zone"`                  // CQ分区
	ITUZone      int       `gorm:"type:tinyint;column:itu_zone" json:"itu_zone"`                // ITU分区
	Mode         string    `gorm:"type:varchar(32);column:mode" json:"mode"`                    // 通信模式
	CallSign     string    `gorm:"type:varchar(32);index;column:callsign" json:"callsign"`      // 对方呼号
	TheirRST     string    `gorm:"type:varchar(16);column:their_rst" json:"their_rst"`          // 对方信号报告
	TheirPower   *int      `gorm:"type:int;column:their_power" json:"their_power,omitempty"`    // 对方功率 (W)
	TheirQTH     string    `gorm:"type:varchar(255);column:their_qth" json:"their_qth"`         // 对方QTH
	TheirRadio   string    `gorm:"type:varchar(255);column:their_radio" json:"their_radio"`     // 对方电台型号
	TheirAntenna string    `gorm:"type:varchar(255);column:their_antenna" json:"their_antenna"` // 对方天线
	MyRST        string    `gorm:"type:varchar(16);column:my_rst" json:"my_rst"`                // 我方信号报告
	MyPower      *int      `gorm:"type:int;column:my_power" json:"my_power,omitempty"`          // 我方功率 (W)
	MyQTH        string    `gorm:"type:varchar(255);column:my_qth" json:"my_qth"`               // 我方QTH
	MyRadio      string    `gorm:"type:varchar(255);column:my_radio" json:"my_radio"`           // 我方电台型号
	MyAntenna    string    `gorm:"type:varchar(255);column:my_antenna" json:"my_antenna"`       // 我方天线
	Notes        string    `gorm:"type:text;column:notes" json:"notes"`                         // 备注
	CreatedAt    time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	// 关联定义：操作员销号，日志级联销毁
	User *User `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

// TableName 指定表名
func (Logbook) TableName() string {
	return "logbooks"
}

// UserRadioPreset 用户电台预设
type UserRadioPreset struct {
	ID        int       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    int       `gorm:"not null;index;column:user_id" json:"user_id"`      // 所属用户ID
	Name      string    `gorm:"type:varchar(64);not null;column:name" json:"name"` // 预设名称（如"家里台"、"车载台"）
	Radio     string    `gorm:"type:varchar(64);column:radio" json:"radio"`        // 电台型号
	Antenna   string    `gorm:"type:varchar(64);column:antenna" json:"antenna"`    // 天线类型
	Power     *int      `gorm:"type:int;column:power" json:"power,omitempty"`      // 功率 (W)
	QTH       string    `gorm:"type:varchar(255);column:qth" json:"qth"`           // QTH位置
	SortOrder int       `gorm:"default:0;column:sort_order" json:"sort_order"`     // 排序权重
	CreatedAt time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	// 关联定义：用户删除时，预设级联销毁
	User *User `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

// TableName 指定表名
func (UserRadioPreset) TableName() string {
	return "user_radio_presets"
}

// AutoMigrate 自动清洗脏数据并迁移主结构与外键约束
// 前置逻辑：
// 1. 使用原生 SQL 优先清理违反外键原则的悬空记录。
// 2. 移除重复数据，为唯一索引的建立扫除障碍。
// 3. 将最终的约束控制权完全交接给 GORM。
func AutoMigrate() error {
	db := Get()

	// ==========================================
	// 阶段一：数据清洗 (Data Cleansing)
	// ==========================================

	// 1. 清理 users 表中的重复记录 (保留 ID 较小的记录)
	// 原理：使用内连接查找手机号相同且 ID 较大的冗余行进行删除
	cleanupDupUserSQL := `
		DELETE u1 FROM users u1
		INNER JOIN users u2
		WHERE u1.id > u2.id AND u1.phone = u2.phone AND u1.phone != ''
	`
	if err := db.Exec(cleanupDupUserSQL).Error; err != nil {
		log.Printf("[Migration Warning] 清理重复用户数据失败: %v", err)
	}

	// 1.1 在建立唯一索引前巡检呼号重复数据
	logDuplicateCallSigns(db)
	logDuplicateOwnerSSIDs(db)

	// 2. 清理各大子表中的"孤儿数据" (Orphaned Records)
	// 原理：子表的关联 ID 如果在主表 (如 users, public_groups) 中找不到了，就必须被抹除
	cleanups := []struct {
		Desc string
		SQL  string
	}{
		{"操作证孤儿记录", "DELETE FROM operator_certs WHERE user_id NOT IN (SELECT id FROM users)"},
		{"组成员(无群组)", "DELETE FROM group_members WHERE group_id NOT IN (SELECT id FROM public_groups)"},
		{"组成员(无用户)", "DELETE FROM group_members WHERE user_id NOT IN (SELECT id FROM users)"},
		{"群互联(源群丢失)", "DELETE FROM group_links WHERE link_group_id NOT IN (SELECT id FROM public_groups)"},
		{"群互联(目标丢失)", "DELETE FROM group_links WHERE target_group_id NOT IN (SELECT id FROM public_groups)"},
		{"设备(无所有者)", "DELETE FROM devices WHERE owner_id NOT IN (SELECT id FROM users)"},
		{"日志(无所有者)", "DELETE FROM logbooks WHERE user_id NOT IN (SELECT id FROM users)"},
		{"设备配置(无设备)", "DELETE FROM device_configs WHERE device_id NOT IN (SELECT id FROM devices)"},
		{"电台预设(无用户)", "DELETE FROM user_radio_presets WHERE user_id NOT IN (SELECT id FROM users)"},
		{"设备偏好(无用户)", "DELETE FROM user_device_preferences WHERE user_id NOT IN (SELECT id FROM users)"},
		// 自引用约束的孤儿数据需要特殊处理：使用临时表避免 MySQL 不支持在同一查询中删除和查询同一表
		{"资产(孤儿文件)", "DELETE FROM assets WHERE parent_id IS NOT NULL AND parent_id NOT IN (SELECT id FROM (SELECT id FROM assets) AS tmp)"},
	}

	for _, task := range cleanups {
		res := db.Exec(task.SQL)
		if res.Error != nil {
			log.Printf("[Migration Warning] %s 清理异常: %v", task.Desc, res.Error)
		} else if res.RowsAffected > 0 {
			log.Printf("[Migration Info] %s: 成功清理 %d 条脏数据", task.Desc, res.RowsAffected)
		}
	}

	// ==========================================
	// 阶段二：执行 GORM 标准化迁移 (Schema Mapping)
	// ==========================================
	// GORM 底层会进行计算，比对现有数据库结构与代码中的结构体。
	// 只有在缺失表、缺失字段、或缺失外键时，才会发送 ALTER TABLE 语句，非常安全。
	log.Println("[Migration Info] 正在启动 GORM 核心迁移机制，建立级联外键约束...")
	err := db.AutoMigrate(
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
		&UserDevicePreference{},
		&DeviceConfig{},
		&Logbook{},
		&UserRadioPreset{},
	)

	if err != nil {
		return err
	}

	// 阶段三：下线 group_members 历史设备级字段（仅保留成员资格语义）
	pruneLegacyGroupMemberColumns(db)
	if err := ensureExpectedUniqueIndexes(db); err != nil {
		return err
	}

	log.Println("[Migration Success] 数据库表结构及外键约束已全部迁移完成！")
	return nil
}

func ensureExpectedUniqueIndexes(db *gorm.DB) error {
	if err := ensureMySQLUniqueIndex(db, "users", "uk_users_callsign", []string{"callsign"}); err != nil {
		return err
	}
	if err := ensureMySQLUniqueIndex(db, "devices", "idx_owner_ssid", []string{"owner_id", "ssid"}); err != nil {
		return err
	}
	return nil
}

func ensureMySQLUniqueIndex(db *gorm.DB, tableName, indexName string, columns []string) error {
	currentDB := ""
	if err := db.Raw("SELECT DATABASE()").Scan(&currentDB).Error; err != nil {
		return fmt.Errorf("query current database for %s.%s failed: %w", tableName, indexName, err)
	}
	if strings.TrimSpace(currentDB) == "" {
		return fmt.Errorf("current database is empty while checking %s.%s", tableName, indexName)
	}

	var rows []struct {
		NonUnique int `gorm:"column:non_unique"`
	}
	err := db.Raw(`
		SELECT non_unique
		FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = ? AND index_name = ?
		ORDER BY seq_in_index
	`, currentDB, tableName, indexName).Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("inspect index %s.%s failed: %w", tableName, indexName, err)
	}

	if len(rows) == 0 {
		sql := fmt.Sprintf(
			"CREATE UNIQUE INDEX `%s` ON `%s` (%s)",
			indexName,
			tableName,
			quotedColumns(columns),
		)
		log.Printf("[Migration Info] 缺失唯一索引 %s.%s，正在创建", tableName, indexName)
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("create unique index %s.%s failed: %w", tableName, indexName, err)
		}
		return nil
	}

	if rows[0].NonUnique == 0 {
		return nil
	}

	sql := fmt.Sprintf(
		"ALTER TABLE `%s` DROP INDEX `%s`, ADD UNIQUE INDEX `%s` (%s)",
		tableName,
		indexName,
		indexName,
		quotedColumns(columns),
	)
	log.Printf("[Migration Info] 索引 %s.%s 当前为非唯一，正在纠正为唯一索引", tableName, indexName)
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("upgrade index %s.%s to unique failed: %w", tableName, indexName, err)
	}
	return nil
}

func quotedColumns(columns []string) string {
	quoted := make([]string, 0, len(columns))
	for _, col := range columns {
		quoted = append(quoted, fmt.Sprintf("`%s`", col))
	}
	return strings.Join(quoted, ", ")
}

func logDuplicateCallSigns(db *gorm.DB) {
	var rows []struct {
		CallSign string `gorm:"column:callsign"`
		Count    int64  `gorm:"column:cnt"`
		UserIDs  string `gorm:"column:user_ids"`
	}

	sql := `
		SELECT callsign, COUNT(*) AS cnt, GROUP_CONCAT(id ORDER BY id) AS user_ids
		FROM users
		GROUP BY callsign
		HAVING COUNT(*) > 1
	`
	if err := db.Raw(sql).Scan(&rows).Error; err != nil {
		log.Printf("[Migration Warning] 巡检重复 callsign 失败: %v", err)
		return
	}

	for _, row := range rows {
		log.Printf("[Migration Warning] users.callsign 重复: callsign=%q count=%d user_ids=%s",
			row.CallSign, row.Count, row.UserIDs)
	}
}

func logDuplicateOwnerSSIDs(db *gorm.DB) {
	var rows []struct {
		OwnerID int    `gorm:"column:owner_id"`
		SSID    uint8  `gorm:"column:ssid"`
		Count   int64  `gorm:"column:cnt"`
		DevIDs  string `gorm:"column:device_ids"`
	}

	sql := `
		SELECT owner_id, ssid, COUNT(*) AS cnt, GROUP_CONCAT(id ORDER BY id) AS device_ids
		FROM devices
		GROUP BY owner_id, ssid
		HAVING COUNT(*) > 1
	`
	if err := db.Raw(sql).Scan(&rows).Error; err != nil {
		log.Printf("[Migration Warning] 巡检重复 owner_id + ssid 失败: %v", err)
		return
	}

	for _, row := range rows {
		log.Printf("[Migration Warning] devices(owner_id, ssid) 重复: owner_id=%d ssid=%d count=%d device_ids=%s",
			row.OwnerID, row.SSID, row.Count, row.DevIDs)
	}
}

// pruneLegacyGroupMemberColumns 安全下线 group_members 历史字段：
// - device_id
// - disable_send
// - disable_recv
func pruneLegacyGroupMemberColumns(db *gorm.DB) {
	if !db.Migrator().HasTable(&GroupMember{}) {
		return
	}

	legacyColumns := []string{"device_id", "disable_send", "disable_recv"}
	for _, col := range legacyColumns {
		if !db.Migrator().HasColumn(&GroupMember{}, col) {
			continue
		}

		// device_id 可能存在历史外键，先按信息架构查询并移除约束
		if col == "device_id" {
			var dbName string
			if err := db.Raw("SELECT DATABASE()").Scan(&dbName).Error; err == nil && dbName != "" {
				var rows []struct {
					ConstraintName string `gorm:"column:constraint_name"`
				}
				fkSQL := `
					SELECT DISTINCT constraint_name
					FROM information_schema.key_column_usage
					WHERE table_schema = ?
					  AND table_name = 'group_members'
					  AND column_name = 'device_id'
					  AND referenced_table_name IS NOT NULL
				`
				if err := db.Raw(fkSQL, dbName).Scan(&rows).Error; err == nil {
					for _, row := range rows {
						if row.ConstraintName == "" {
							continue
						}
						if err := dropGroupMemberDeviceForeignKey(db, row.ConstraintName); err != nil {
							log.Printf("[Migration Warning] 删除 group_members.%s 外键 %s 失败: %v", col, row.ConstraintName, err)
						}
					}
				}
			}
		}

		if err := db.Migrator().DropColumn(&GroupMember{}, col); err != nil {
			log.Printf("[Migration Warning] 删除 group_members.%s 失败: %v", col, err)
			continue
		}
		log.Printf("[Migration Info] 已删除遗留字段 group_members.%s", col)
	}
}

// dropGroupMemberDeviceForeignKey 删除 group_members.device_id 的历史外键。
// 兼容策略：
// 1) 优先使用 GORM Migrator（跨数据库）
// 2) 若失败，回退到 MySQL 语法 DROP FOREIGN KEY
func dropGroupMemberDeviceForeignKey(db *gorm.DB, constraintName string) error {
	if constraintName == "" {
		return nil
	}

	// MySQL/MariaDB 仅支持 DROP FOREIGN KEY，直接使用正确语法，避免先触发 1064 噪音日志。
	if strings.EqualFold(db.Dialector.Name(), "mysql") {
		sql := fmt.Sprintf("ALTER TABLE `group_members` DROP FOREIGN KEY `%s`", constraintName)
		if err := db.Exec(sql).Error; err == nil {
			log.Printf("[Migration Info] 已使用 MySQL 语法删除外键: %s", constraintName)
			return nil
		} else {
			// 1091: 外键不存在（可能并发迁移或已被手工清理），按成功处理。
			if strings.Contains(err.Error(), "Error 1091") {
				log.Printf("[Migration Info] 外键 %s 不存在，跳过删除", constraintName)
				return nil
			}
			return fmt.Errorf("mysql drop foreign key failed: %w", err)
		}
	}

	if err := db.Migrator().DropConstraint(&GroupMember{}, constraintName); err == nil {
		return nil
	} else {
		return fmt.Errorf("gorm drop constraint failed: %w", err)
	}
}
