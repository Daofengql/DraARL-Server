package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
var Config *Configuration
var configFilePath string
var once sync.Once

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

	Web struct {
		Host        string `yaml:"Host" json:"host"`
		Port        string `yaml:"Port" json:"port"`
		FrontendURL string `yaml:"FrontendURL" json:"frontend_url"` // 前端地址，用于SSO回调重定向
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

// Load 加载配置文件
func Load(configPath string) (*Configuration, error) {
	var loadErr error
	once.Do(func() {
		if configPath == "" {
			dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
			if err != nil {
				loadErr = fmt.Errorf("get config filepath err: %w", err)
				return
			}
			configPath = filepath.Join(dir, "udphub.yaml")
		}
		configFilePath = configPath

		yamlFile, err := os.ReadFile(configPath)
		if err != nil {
			loadErr = fmt.Errorf("udphub.yaml open err: %w", err)
			return
		}

		Config = &Configuration{}
		err = yaml.Unmarshal(yamlFile, Config)
		if err != nil {
			loadErr = fmt.Errorf("Unmarshal: %w", err)
			return
		}

		// 设置默认值
		Config.SetDefaults()
	})

	if Config == nil {
		return nil, loadErr
	}

	return Config, nil
}

// SetDefaults 设置默认配置值
func (c *Configuration) SetDefaults() {
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

// GetConfigPath 获取配置文件路径
func GetConfigPath() string {
	return configFilePath
}
