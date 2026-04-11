package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
var Config *Configuration
var configFilePath string
var configMu sync.RWMutex
var releaseBuild atomic.Bool

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

	// MinIO 对象存储配置
	MinIO struct {
		Endpoint  string `yaml:"Endpoint" json:"endpoint"`    // localhost:9000
		AccessKey string `yaml:"AccessKey" json:"access_key"` // minioadmin
		SecretKey string `yaml:"SecretKey" json:"secret_key"` // minioadmin
		UseSSL    bool   `yaml:"UseSSL" json:"use_ssl"`       // 是否使用HTTPS
		Bucket    string `yaml:"Bucket" json:"bucket"`        // 默认存储桶
		BasePath  string `yaml:"BasePath" json:"base_path"`   // URL基础路径
	} `yaml:"MinIO" json:"minio"`

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
		configPath = filepath.Join(dir, "udphub.yaml")
	}

	yamlFile, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("udphub.yaml open err: %w", err)
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

	// 规范化 MinIO BasePath：去除尾部斜杠，避免 URL 拼接时产生双斜杠
	if c.MinIO.BasePath != "" {
		c.MinIO.BasePath = strings.TrimRight(c.MinIO.BasePath, "/")
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
