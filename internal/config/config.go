package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"draarl/internal/accesspoint"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
var Config *Configuration
var configFilePath string
var configMu sync.RWMutex
var releaseBuild atomic.Bool

const DefaultConfigFileName = "config.yaml"

type MinIOConfig struct {
	Endpoint  string `yaml:"Endpoint" json:"endpoint"`
	AccessKey string `yaml:"AccessKey" json:"access_key"`
	SecretKey string `yaml:"SecretKey" json:"secret_key"`
	UseSSL    bool   `yaml:"UseSSL" json:"use_ssl"`
	Bucket    string `yaml:"Bucket" json:"bucket"`
	BasePath  string `yaml:"BasePath" json:"base_path"`
}

type UDPConfig struct {
	SendWorkers      int `yaml:"SendWorkers" json:"send_workers"`
	IngressWorkers   int `yaml:"IngressWorkers" json:"ingress_workers"`
	FrameQueueSize   int `yaml:"FrameQueueSize" json:"frame_queue_size"`
	MaxFrameAgeMS    int `yaml:"MaxFrameAgeMS" json:"max_frame_age_ms"`
	ReadBufferBytes  int `yaml:"ReadBufferBytes" json:"read_buffer_bytes"`
	WriteBufferBytes int `yaml:"WriteBufferBytes" json:"write_buffer_bytes"`
}

// InterconnectConfig controls the optional centre-side Type 0 node services.
// It is ignored unless Enabled is true, preserving existing single-node startup.
type InterconnectConfig struct {
	Enabled              bool   `yaml:"Enabled" json:"enabled"`
	ControlListen        string `yaml:"ControlListen" json:"control_listen"`
	TLSCertFile          string `yaml:"TLSCertFile" json:"tls_cert_file"`
	TLSKeyFile           string `yaml:"TLSKeyFile" json:"tls_key_file"`
	TLSClientCAFile      string `yaml:"TLSClientCAFile" json:"tls_client_ca_file"`
	AllowSelfSigned      bool   `yaml:"AllowSelfSigned" json:"allow_self_signed"`
	RegistrationTokenTTL int    `yaml:"RegistrationTokenTTL" json:"registration_token_ttl"`
	// NodeTokens is a development/bootstrap map. Production deployments should
	// replace it with hashed, rotatable credentials managed by the admin API.
	NodeTokens map[string]string          `yaml:"NodeTokens" json:"node_tokens"`
	Resources  InterconnectResourceConfig `yaml:"Resources" json:"resources"`
}

type InterconnectResourceConfig struct {
	MaxNodes                   int `yaml:"MaxNodes" json:"max_nodes"`
	MaxPendingHandshakes       int `yaml:"MaxPendingHandshakes" json:"max_pending_handshakes"`
	AuthAttemptsPerMinutePerIP int `yaml:"AuthAttemptsPerMinutePerIP" json:"auth_attempts_per_minute_per_ip"`
	DataSoftPPSPerNode         int `yaml:"DataSoftPPSPerNode" json:"data_soft_pps_per_node"`
	DataHardPPSPerNode         int `yaml:"DataHardPPSPerNode" json:"data_hard_pps_per_node"`
	DataHardMbpsPerNode        int `yaml:"DataHardMbpsPerNode" json:"data_hard_mbps_per_node"`
	DataQueuePerNode           int `yaml:"DataQueuePerNode" json:"data_queue_per_node"`
	DataQueueGlobal            int `yaml:"DataQueueGlobal" json:"data_queue_global"`
	DataWorkers                int `yaml:"DataWorkers" json:"data_workers"`
	DataMaxQueueAgeMS          int `yaml:"DataMaxQueueAgeMS" json:"data_max_queue_age_ms"`
	ControlSoftPPSPerNode      int `yaml:"ControlSoftPPSPerNode" json:"control_soft_pps_per_node"`
	ControlHardPPSPerNode      int `yaml:"ControlHardPPSPerNode" json:"control_hard_pps_per_node"`
	ControlHardMbpsPerNode     int `yaml:"ControlHardMbpsPerNode" json:"control_hard_mbps_per_node"`
	DeviceAuthPPSPerNode       int `yaml:"DeviceAuthPPSPerNode" json:"device_auth_pps_per_node"`
	MaxDeviceSessionsPerNode   int `yaml:"MaxDeviceSessionsPerNode" json:"max_device_sessions_per_node"`
}

type AccessDiscoveryConfig struct {
	Enabled              bool `yaml:"Enabled" json:"enabled"`
	TokenTTLSeconds      int  `yaml:"TokenTTLSeconds" json:"token_ttl_seconds"`
	EdgeHealthTTLSeconds int  `yaml:"EdgeHealthTTLSeconds" json:"edge_health_ttl_seconds"`
	CacheMaxAgeSeconds   int  `yaml:"CacheMaxAgeSeconds" json:"cache_max_age_seconds"`
	Center               struct {
		Enabled     bool   `yaml:"Enabled" json:"enabled"`
		PublicID    string `yaml:"PublicID" json:"public_id"`
		DisplayName string `yaml:"DisplayName" json:"display_name"`
		UDPHost     string `yaml:"UDPHost" json:"udp_host"`
		UDPPort     int    `yaml:"UDPPort" json:"udp_port"`
		Region      string `yaml:"Region" json:"region"`
		Network     string `yaml:"Network" json:"network"`
		Priority    int    `yaml:"Priority" json:"priority"`
	} `yaml:"Center" json:"center"`
}

// SetReleaseBuild 设置是否为 release 构建产物。
func SetReleaseBuild(release bool) {
	releaseBuild.Store(release)
}

// IsReleaseBuild 返回当前是否为 release 构建产物。
func IsReleaseBuild() bool {
	return releaseBuild.Load()
}

// Configuration 系统配置
type Configuration struct {
	System struct {
		Port          string `yaml:"Port" json:"port"`
		Host          string `yaml:"Host" json:"host"`
		LogPath       string `yaml:"LogPath" json:"log_path"`
		IPFile        string `yaml:"IPfile" json:"ipfile"`
		ProxyProtocol string `yaml:"ProxyProtocol" json:"proxy_protocol"` // PROXY Protocol 版本: "", "v1", "v2"
	} `yaml:"System" json:"system"`

	UDP             UDPConfig             `yaml:"UDP" json:"udp"`
	Interconnect    InterconnectConfig    `yaml:"Interconnect" json:"interconnect"`
	AccessDiscovery AccessDiscoveryConfig `yaml:"AccessDiscovery" json:"access_discovery"`

	Database struct {
		Host     string `yaml:"Host" json:"host"`
		Port     int    `yaml:"Port" json:"port"`
		User     string `yaml:"User" json:"user"`
		Password string `yaml:"Password" json:"password"`
		DBName   string `yaml:"DBName" json:"dbname"`
		Charset  string `yaml:"Charset" json:"charset"`
		Collate  string `yaml:"Collate" json:"collate"`

		// 连接池配置
		MaxOpenConns int `yaml:"MaxOpenConns" json:"max_open_conns"`
		MaxIdleConns int `yaml:"MaxIdleConns" json:"max_idle_conns"`
		MaxLifetime  int `yaml:"MaxLifetime" json:"max_lifetime"` // 秒
	} `yaml:"Database" json:"database"`

	Redis struct {
		Host            string `yaml:"Host" json:"host"`
		Port            int    `yaml:"Port" json:"port"`
		Password        string `yaml:"Password" json:"password"`
		DB              int    `yaml:"DB" json:"db"`
		Prefix          string `yaml:"Prefix" json:"prefix"`
		DialTimeoutSec  int    `yaml:"DialTimeoutSec" json:"dial_timeout_sec"`
		ReadTimeoutSec  int    `yaml:"ReadTimeoutSec" json:"read_timeout_sec"`
		WriteTimeoutSec int    `yaml:"WriteTimeoutSec" json:"write_timeout_sec"`
		PoolSize        int    `yaml:"PoolSize" json:"pool_size"`
	} `yaml:"Redis" json:"redis"`

	Web struct {
		Host        string `yaml:"Host" json:"host"`
		Port        string `yaml:"Port" json:"port"`
		FrontendURL string `yaml:"FrontendURL" json:"frontend_url"` // 前端地址，用于SSO回调重定向
		// 允许访问 API / WebSocket 的页面来源白名单（格式: https://example.com）。
		// FrontendURL 对应的 Origin 会自动加入此集合；若 index.html 由后端提供，通常这里应配置后端对外页面域名。
		AllowedOrigins []string `yaml:"AllowedOrigins" json:"allowed_origins"`
		FrontendCDN    struct {
			Enabled      bool   `yaml:"Enabled" json:"enabled"`
			ObjectPrefix string `yaml:"ObjectPrefix" json:"object_prefix"` // MinIO 中的前端资源前缀目录
		} `yaml:"FrontendCDN" json:"frontend_cdn"`
	} `yaml:"Web" json:"web"`

	// Keycloak SSO 配置
	Keycloak struct {
		Enabled      bool   `yaml:"Enabled" json:"enabled"`
		Name         string `yaml:"Name" json:"name"`                  // 显示名称，如 "企业SSO"、"Keycloak"
		BaseURL      string `yaml:"BaseURL" json:"base_url"`           // http://localhost:8080
		Realm        string `yaml:"Realm" json:"realm"`                // draarl
		ClientID     string `yaml:"ClientID" json:"client_id"`         // draarl-frontend
		ClientSecret string `yaml:"ClientSecret" json:"client_secret"` // 客户端密钥
		RedirectURI  string `yaml:"RedirectURI" json:"redirect_uri"`   // http://localhost:9000/callback
	} `yaml:"Keycloak" json:"keycloak"`

	// Storage 存储主配置（Driver 为空时按 Storage.MinIO.Endpoint 推断）
	Storage struct {
		Driver string `yaml:"Driver" json:"driver"` // minio | local；后续可扩展 oss/cos/r2
		Local  struct {
			RootPath string `yaml:"RootPath" json:"root_path"`
			BaseURL  string `yaml:"BaseURL" json:"base_url"`
		} `yaml:"Local" json:"local"`
		MinIO MinIOConfig `yaml:"MinIO" json:"minio"`
	} `yaml:"Storage" json:"storage"`
	// LegacyMinIO is read only for upgrading configurations created before Storage was introduced.
	LegacyMinIO MinIOConfig `yaml:"MinIO,omitempty" json:"-"`

	// JWT 配置
	JWT struct {
		Secret string `yaml:"Secret" json:"secret"` // JWT 签名密钥，最少32字符
	} `yaml:"JWT" json:"jwt"`

	// 设备认证配置
	DeviceAuth struct {
		AESKey string `yaml:"AESKey" json:"aes_key"` // AES 加密密钥，用于设备密码加密存储，必须为 16、24 或 32 字节
	} `yaml:"DeviceAuth" json:"device_auth"`
}

// GetDSN 获取MySQL连接字符串
func (c *Configuration) GetDSN() string {
	charset := c.Database.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	collate := c.Database.Collate
	if collate == "" {
		collate = "utf8mb4_unicode_ci"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&collation=%s&parseTime=true&loc=Local",
		c.Database.User,
		c.Database.Password,
		c.Database.Host,
		c.Database.Port,
		c.Database.DBName,
		charset,
		collate,
	)
}

// RedisAddr 返回 Redis 地址。
func (c *Configuration) RedisAddr() string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(c.Redis.Host), c.Redis.Port)
}

// Load 加载配置文件
func Load(configPath string) (*Configuration, error) {
	if configPath == "" {
		dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			return nil, fmt.Errorf("get config filepath err: %w", err)
		}
		configPath = filepath.Join(dir, DefaultConfigFileName)
	}

	yamlFile, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("config file open err: %w", err)
	}

	cfg := &Configuration{}
	if err = yaml.Unmarshal(yamlFile, cfg); err != nil {
		return nil, fmt.Errorf("Unmarshal: %w", err)
	}

	configFilePath = configPath
	if err := cfg.SetDefaults(); err != nil {
		return nil, err
	}

	configMu.Lock()
	Config = cfg
	configMu.Unlock()

	return cfg, nil
}

// SetDefaults 设置默认配置值
func (c *Configuration) SetDefaults() error {
	c.migrateLegacyStorageConfig()

	if c.UDP.SendWorkers < 0 {
		c.UDP.SendWorkers = 0
	}
	if c.UDP.IngressWorkers < 0 {
		c.UDP.IngressWorkers = 0
	}
	if c.UDP.FrameQueueSize <= 0 {
		c.UDP.FrameQueueSize = 64
	}
	if c.UDP.MaxFrameAgeMS <= 0 {
		c.UDP.MaxFrameAgeMS = 120
	}
	if c.UDP.ReadBufferBytes <= 0 {
		c.UDP.ReadBufferBytes = 4 * 1024 * 1024
	}
	if c.UDP.WriteBufferBytes <= 0 {
		c.UDP.WriteBufferBytes = 4 * 1024 * 1024
	}
	if strings.TrimSpace(c.Interconnect.ControlListen) == "" {
		c.Interconnect.ControlListen = ":60100"
	}
	if c.Interconnect.RegistrationTokenTTL <= 0 {
		c.Interconnect.RegistrationTokenTTL = 24 * 60 * 60
	}
	resources := &c.Interconnect.Resources
	if resources.MaxNodes == 0 {
		resources.MaxNodes = 256
	}
	if resources.MaxPendingHandshakes == 0 {
		resources.MaxPendingHandshakes = 64
	}
	if resources.AuthAttemptsPerMinutePerIP == 0 {
		resources.AuthAttemptsPerMinutePerIP = 30
	}
	if resources.DataSoftPPSPerNode == 0 {
		resources.DataSoftPPSPerNode = 50000
	}
	if resources.DataHardPPSPerNode == 0 {
		resources.DataHardPPSPerNode = 100000
	}
	if resources.DataHardMbpsPerNode == 0 {
		resources.DataHardMbpsPerNode = 1000
	}
	if resources.DataQueuePerNode == 0 {
		resources.DataQueuePerNode = 512
	}
	if resources.DataQueueGlobal == 0 {
		resources.DataQueueGlobal = 4096
	}
	if resources.DataMaxQueueAgeMS == 0 {
		resources.DataMaxQueueAgeMS = 200
	}
	if resources.ControlSoftPPSPerNode == 0 {
		resources.ControlSoftPPSPerNode = 1000
	}
	if resources.ControlHardPPSPerNode == 0 {
		resources.ControlHardPPSPerNode = 2000
	}
	if resources.ControlHardMbpsPerNode == 0 {
		resources.ControlHardMbpsPerNode = 256
	}
	if resources.DeviceAuthPPSPerNode == 0 {
		resources.DeviceAuthPPSPerNode = 500
	}
	if resources.MaxDeviceSessionsPerNode == 0 {
		resources.MaxDeviceSessionsPerNode = 25000
	}
	if c.AccessDiscovery.TokenTTLSeconds <= 0 || c.AccessDiscovery.TokenTTLSeconds > 300 {
		c.AccessDiscovery.TokenTTLSeconds = 300
	}
	if c.AccessDiscovery.EdgeHealthTTLSeconds <= 0 || c.AccessDiscovery.EdgeHealthTTLSeconds > 300 {
		c.AccessDiscovery.EdgeHealthTTLSeconds = 20
	}
	if c.AccessDiscovery.CacheMaxAgeSeconds <= 0 || c.AccessDiscovery.CacheMaxAgeSeconds > 30 {
		c.AccessDiscovery.CacheMaxAgeSeconds = 5
	}
	if strings.TrimSpace(c.AccessDiscovery.Center.DisplayName) == "" {
		c.AccessDiscovery.Center.DisplayName = "中心直连"
	}
	if c.AccessDiscovery.Center.UDPPort <= 0 {
		if port, err := strconv.Atoi(c.System.Port); err == nil && port > 0 && port <= 65535 {
			c.AccessDiscovery.Center.UDPPort = port
		} else {
			c.AccessDiscovery.Center.UDPPort = 60050
		}
	}
	if strings.TrimSpace(c.AccessDiscovery.Center.PublicID) == "" {
		c.AccessDiscovery.Center.PublicID = "center"
	}
	if c.AccessDiscovery.Enabled && c.AccessDiscovery.Center.Enabled {
		publicID, err := accesspoint.NormalizePublicID(c.AccessDiscovery.Center.PublicID)
		if err != nil {
			return fmt.Errorf("AccessDiscovery.Center.PublicID: %w", err)
		}
		displayName, err := accesspoint.NormalizeLabel(c.AccessDiscovery.Center.DisplayName, 100)
		if err != nil || displayName == "" {
			return fmt.Errorf("AccessDiscovery.Center.DisplayName is invalid")
		}
		host, err := accesspoint.NormalizeUDPHost(c.AccessDiscovery.Center.UDPHost)
		if err != nil {
			return fmt.Errorf("AccessDiscovery.Center.UDPHost: %w", err)
		}
		if err := accesspoint.ValidateUDPPort(c.AccessDiscovery.Center.UDPPort); err != nil {
			return fmt.Errorf("AccessDiscovery.Center.UDPPort: %w", err)
		}
		region, err := accesspoint.NormalizeLabel(c.AccessDiscovery.Center.Region, 100)
		if err != nil {
			return fmt.Errorf("AccessDiscovery.Center.Region: %w", err)
		}
		network, err := accesspoint.NormalizeLabel(c.AccessDiscovery.Center.Network, 100)
		if err != nil {
			return fmt.Errorf("AccessDiscovery.Center.Network: %w", err)
		}
		c.AccessDiscovery.Center.PublicID = publicID
		c.AccessDiscovery.Center.DisplayName = displayName
		c.AccessDiscovery.Center.UDPHost = host
		c.AccessDiscovery.Center.Region = region
		c.AccessDiscovery.Center.Network = network
	}

	// 数据库默认值
	if c.Database.Port == 0 {
		c.Database.Port = 3306
	}
	if c.Database.Charset == "" {
		c.Database.Charset = "utf8mb4"
	}
	if c.Database.MaxOpenConns == 0 {
		c.Database.MaxOpenConns = 25
	}
	if c.Database.MaxIdleConns == 0 {
		c.Database.MaxIdleConns = 5
	}
	if c.Database.MaxLifetime == 0 {
		c.Database.MaxLifetime = 300 // 5分钟
	}

	// Redis 默认值
	if strings.TrimSpace(c.Redis.Host) == "" {
		c.Redis.Host = "127.0.0.1"
	}
	if c.Redis.Port == 0 {
		c.Redis.Port = 6379
	}
	if c.Redis.DB < 0 {
		c.Redis.DB = 0
	}
	if strings.TrimSpace(c.Redis.Prefix) == "" {
		c.Redis.Prefix = "draarl"
	}
	if c.Redis.DialTimeoutSec <= 0 {
		c.Redis.DialTimeoutSec = 3
	}
	if c.Redis.ReadTimeoutSec <= 0 {
		c.Redis.ReadTimeoutSec = 2
	}
	if c.Redis.WriteTimeoutSec <= 0 {
		c.Redis.WriteTimeoutSec = 2
	}
	if c.Redis.PoolSize <= 0 {
		c.Redis.PoolSize = 20
	}

	// 前端 CDN 默认值
	if strings.TrimSpace(c.Web.FrontendCDN.ObjectPrefix) == "" {
		c.Web.FrontendCDN.ObjectPrefix = "frontend"
	}

	// 存储默认值
	if strings.TrimSpace(c.Storage.Local.RootPath) == "" {
		c.Storage.Local.RootPath = "./data/storage"
	}
	if c.Storage.Local.BaseURL != "" {
		c.Storage.Local.BaseURL = strings.TrimRight(c.Storage.Local.BaseURL, "/")
	}
	if c.Storage.MinIO.BasePath != "" {
		c.Storage.MinIO.BasePath = strings.TrimRight(c.Storage.MinIO.BasePath, "/")
	}

	// AES 密钥默认值：如果不符合要求则自动生成并写入配置文件
	if c.ValidateAESKey() != nil {
		aesKey, err := GenerateAESKey(32) // 默认使用 AES-256
		if err != nil {
			return fmt.Errorf("生成 AES 密钥失败: %w", err)
		}
		c.DeviceAuth.AESKey = aesKey
		// 保存到配置文件
		if err := c.SaveToFile(configFilePath); err != nil {
			return fmt.Errorf("保存配置文件失败: %w", err)
		}
	}

	return nil
}

func (c *Configuration) migrateLegacyStorageConfig() {
	if strings.EqualFold(strings.TrimSpace(c.Storage.Driver), "local") {
		return
	}
	if strings.TrimSpace(c.Storage.MinIO.Endpoint) == "" && strings.TrimSpace(c.LegacyMinIO.Endpoint) != "" {
		c.Storage.MinIO = c.LegacyMinIO
		c.LegacyMinIO = MinIOConfig{}
	}
}

// JWTSecretMinLength JWT密钥最小长度
const JWTSecretMinLength = 32

// ValidateJWTSecret 验证JWT密钥是否符合要求
func (c *Configuration) ValidateJWTSecret() error {
	if len(c.JWT.Secret) < JWTSecretMinLength {
		return fmt.Errorf("JWT密钥长度不足，当前%d字符，最少需要%d字符", len(c.JWT.Secret), JWTSecretMinLength)
	}
	return nil
}

// GenerateJWTSecret 生成安全的随机JWT密钥
func GenerateJWTSecret() (string, error) {
	bytes := make([]byte, 32) // 生成64字符的十六进制字符串
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成随机密钥失败: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// SaveToFile 保存配置到文件
func (c *Configuration) SaveToFile(configPath string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	return os.WriteFile(configPath, data, 0644)
}

// MustLoad 加载配置文件，失败则panic
func MustLoad(configPath string) *Configuration {
	cfg, err := Load(configPath)
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}
	return cfg
}

// Get 获取配置（单例）
func Get() *Configuration {
	if Config == nil {
		panic("config not loaded, call Load() first")
	}
	return Config
}

// TryGet 返回当前配置；配置尚未加载时返回 nil，便于底层组件和单元测试安全取默认值。
func TryGet() *Configuration {
	configMu.RLock()
	defer configMu.RUnlock()
	return Config
}

// AESKeyLengths AES 密钥有效长度
var AESKeyLengths = []int{16, 24, 32}

// ValidateAESKey 验证 AES 密钥是否符合要求
func (c *Configuration) ValidateAESKey() error {
	keyLen := len(c.DeviceAuth.AESKey)
	for _, validLen := range AESKeyLengths {
		if keyLen == validLen {
			return nil
		}
	}
	return fmt.Errorf("AES 密钥长度无效，当前 %d 字节，必须为 16、24 或 32 字节", keyLen)
}

// GenerateAESKey 生成安全的随机 AES 密钥
func GenerateAESKey(keyLen int) (string, error) {
	valid := false
	for _, validLen := range AESKeyLengths {
		if keyLen == validLen {
			valid = true
			break
		}
	}
	if !valid {
		return "", fmt.Errorf("AES 密钥长度必须为 16、24 或 32 字节")
	}
	bytes := make([]byte, keyLen)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成随机密钥失败: %w", err)
	}
	return hex.EncodeToString(bytes)[:keyLen], nil
}

// GetConfigPath 获取配置文件路径
func GetConfigPath() string {
	return configFilePath
}

// GetAllowedOrigins 返回标准化后的 Origin 白名单。
func (c *Configuration) GetAllowedOrigins() []string {
	originSet := make(map[string]struct{})

	for _, item := range c.Web.AllowedOrigins {
		if origin := normalizeOrigin(item); origin != "" {
			originSet[origin] = struct{}{}
		}
	}

	if origin := normalizeOrigin(c.Web.FrontendURL); origin != "" {
		originSet[origin] = struct{}{}
	}

	results := make([]string, 0, len(originSet))
	for origin := range originSet {
		results = append(results, origin)
	}

	return results
}

// ValidateAllowedOrigins 校验 Origin 白名单配置。
func (c *Configuration) ValidateAllowedOrigins() error {
	if c.IsProduction() && len(c.GetAllowedOrigins()) == 0 {
		return fmt.Errorf("生产环境必须配置可解析的 Web.FrontendURL 或 Web.AllowedOrigins")
	}
	return nil
}

// IsProduction 判断当前运行环境是否为生产环境。
func (c *Configuration) IsProduction() bool {
	return IsReleaseBuild()
}

func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}
