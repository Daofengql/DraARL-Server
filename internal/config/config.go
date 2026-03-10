package config

import (
	"fmt"
	"log"
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
		Enabled     bool   `yaml:"Enabled" json:"enabled"`
		Host        string `yaml:"Host" json:"host"`
		Port        int    `yaml:"Port" json:"port"`
		Password    string `yaml:"Password" json:"password"`
		DB          int    `yaml:"DB" json:"db"`
		PoolSize    int    `yaml:"PoolSize" json:"pool_size"`
		MinIdleConn int    `yaml:"MinIdleConn" json:"min_idle_conn"`

		// 三级缓存配置
		Cache struct {
			LocalTTL int `yaml:"LocalTTL" json:"local_ttl"`   // 本地缓存TTL(秒)
			RedisTTL int `yaml:"RedisTTL" json:"redis_ttl"`   // Redis缓存TTL(秒)
			MaxSize  int `yaml:"MaxSize" json:"max_size"`     // 本地缓存最大数量
		} `yaml:"Cache" json:"cache"`
	} `yaml:"Redis" json:"redis"`

	Web struct {
		Path   string `yaml:"Path" json:"path"`
		Port   string `yaml:"Port" json:"port"`
		ICP    string `yaml:"ICP" json:"icp"`
		SSLCrt string `yaml:"SSLCrt" json:"ssl_crt"`
		SSLKey string `yaml:"SSLKey" json:"ssl_key"`
	} `yaml:"Web" json:"web"`

	SystemInfo struct {
		PlatformName  string `yaml:"Name" json:"name"`
		NameShorthand string `yaml:"NameShorthand" json:"nameshorthand"`
		LogoURL       string `yaml:"LogoURL" json:"logo_url"`
		Language      string `yaml:"Language" json:"language"`
	} `yaml:"SystemInfo" json:"systeminfo"`

	OpenAI struct {
		BaseURL string `yaml:"BaseURL" json:"base_url"`
		APIKEY  string `yaml:"APIKEY" json:"api_key"`
		Engine  string `yaml:"Engine" json:"engine"`
	} `yaml:"OpenAI" json:"openai"`

	APRS struct {
		APRSServerHost string  `yaml:"APRSServerHost" json:"aprs_server_host"`
		APRSServerPort string  `yaml:"APRSServerPort" json:"aprs_server_port"`
		SelfAddress    string  `yaml:"SelfAddress" json:"self_address"`
		SelfPort       string  `yaml:"SelfPort" json:"self_port"`
		CallSign       string  `yaml:"CallSign" json:"callsign"`
		SSID           string  `yaml:"SSID" json:"ssid"`
		Passcode       int     `yaml:"Passcode" json:"passcode"`
		Latitude       float64 `yaml:"Latitude" json:"latitude"`
		Longitude      float64 `yaml:"Longitude" json:"longitude"`
		Altitude       string  `yaml:"Altitude" json:"altitude"`
	} `yaml:"APRS" json:"aprs"`
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
	var err error
	once.Do(func() {
		if configPath == "" {
			dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
			if err != nil {
				log.Printf("get config filepath err #%v ", err)
				return
			}
			configPath = filepath.Join(dir, "udphub.yaml")
		}

		yamlFile, err := os.ReadFile(configPath)
		if err != nil {
			log.Printf("udphub.yaml open err #%v ", err)
			return
		}

		Config = &Configuration{}
		err = yaml.Unmarshal(yamlFile, Config)
		if err != nil {
			log.Fatalf("Unmarshal: %v \n %s", err, yamlFile)
		}

		// 设置默认值
		Config.SetDefaults()
	})

	return Config, err
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
