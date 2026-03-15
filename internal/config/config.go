package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
var Config *Configuration
var once sync.Once

// Configuration 系统配置
type Configuration struct {
	System struct {
		Port        string `yaml:"Port" json:"port"`
		LogPath     string `yaml:"LogPath" json:"log_path"`
		IPFile      string `yaml:"IPfile" json:"ipfile"`
		CallLogPath string `yaml:"CallLogPath" json:"calllog_path"`
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
		Host        string `yaml:"Host" json:"host"`
		Port        int    `yaml:"Port" json:"port"`
		Password    string `yaml:"Password" json:"password"`
		DB          int    `yaml:"DB" json:"db"`
		PoolSize    int    `yaml:"PoolSize" json:"pool_size"`
		MinIdleConn int    `yaml:"MinIdleConn" json:"min_idle_conn"`

		// 缓存配置
		Cache struct {
			LocalTTL int `yaml:"LocalTTL" json:"local_ttl"`   // 本地缓存TTL(秒)
			RedisTTL int `yaml:"RedisTTL" json:"redis_ttl"`   // Redis缓存TTL(秒)
			MaxSize  int `yaml:"MaxSize" json:"max_size"`     // 本地缓存最大数量
		} `yaml:"Cache" json:"cache"`
	} `yaml:"Redis" json:"redis"`

	Web struct {
		Port   string `yaml:"Port" json:"port"`
		SSLCrt string `yaml:"SSLCrt" json:"ssl_crt"`
		SSLKey string `yaml:"SSLKey" json:"ssl_key"`
	} `yaml:"Web" json:"web"`

	// Keycloak SSO 配置
	Keycloak struct {
		Enabled       bool   `yaml:"Enabled" json:"enabled"`
		BaseURL       string `yaml:"BaseURL" json:"base_url"`           // http://localhost:8080
		Realm        string `yaml:"Realm" json:"realm"`                 // nrllink
		ClientID     string `yaml:"ClientID" json:"client_id"`          // nrllink-frontend
		ClientSecret string `yaml:"ClientSecret" json:"client_secret"`  // 客户端密钥
		RedirectURI  string `yaml:"RedirectURI" json:"redirect_uri"`   // http://localhost:9000/callback
	} `yaml:"Keycloak" json:"keycloak"`

	// MinIO 对象存储配置
	MinIO struct {
		Endpoint string `yaml:"Endpoint" json:"endpoint"`             // localhost:9000
		AccessKey string `yaml:"AccessKey" json:"access_key"`         // minioadmin
		SecretKey string `yaml:"SecretKey" json:"secret_key"`         // minioadmin
		UseSSL    bool   `yaml:"UseSSL" json:"use_ssl"`               // 是否使用HTTPS
		Bucket    string `yaml:"Bucket" json:"bucket"`                // 默认存储桶
		BasePath  string `yaml:"BasePath" json:"base_path"`           // URL基础路径
	} `yaml:"MinIO" json:"minio"`
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

// GetRedisAddr 获取Redis地址
func (c *Configuration) GetRedisAddr() string {
	if c.Redis.Host == "" {
		return "127.0.0.1:6379"
	}
	if c.Redis.Port == 0 {
		c.Redis.Port = 6379
	}
	return fmt.Sprintf("%s:%d", c.Redis.Host, c.Redis.Port)
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

	// Redis默认值
	if c.Redis.Port == 0 {
		c.Redis.Port = 6379
	}
	if c.Redis.DB == 0 {
		c.Redis.DB = 0
	}
	if c.Redis.PoolSize == 0 {
		c.Redis.PoolSize = 100
	}
	if c.Redis.MinIdleConn == 0 {
		c.Redis.MinIdleConn = 10
	}

	// 缓存默认值
	if c.Redis.Cache.LocalTTL == 0 {
		c.Redis.Cache.LocalTTL = 60 // 1分钟
	}
	if c.Redis.Cache.RedisTTL == 0 {
		c.Redis.Cache.RedisTTL = 3600 // 1小时
	}
	if c.Redis.Cache.MaxSize == 0 {
		c.Redis.Cache.MaxSize = 10000
	}
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
