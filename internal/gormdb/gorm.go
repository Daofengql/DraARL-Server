package gormdb

import (
	"fmt"
	"log"
	"sync"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	DB   *gorm.DB
	once sync.Once
)

// Config 数据库配置
type Config struct {
	DSN          string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifetime  int
	LogLevel     string
}

// Init 初始化 GORM 数据库连接
func Init(cfg *Config) error {
	var err error
	once.Do(func() {
		// 配置 GORM logger
		var gormLogger logger.Interface
		switch cfg.LogLevel {
		case "silent":
			gormLogger = logger.Default.LogMode(logger.Silent)
		case "error":
			gormLogger = logger.Default.LogMode(logger.Error)
		case "warn":
			gormLogger = logger.Default.LogMode(logger.Warn)
		case "info":
			gormLogger = logger.Default.LogMode(logger.Info)
		default:
			gormLogger = logger.Default.LogMode(logger.Error)
		}

		DB, err = gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
			Logger: gormLogger,
			NowFunc: func() time.Time {
				return time.Now()
			},
		})

		if err != nil {
			err = fmt.Errorf("failed to connect database: %w", err)
			return
		}

		// 获取底层的 sql.DB
		sqlDB, err := DB.DB()
		if err != nil {
			err = fmt.Errorf("failed to get sql.DB: %w", err)
			return
		}

		// 设置连接池参数
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.MaxLifetime) * time.Second)
		// ��置连接最大空闲时间，MySQL wait_timeout 默认 8 小时，设置 10 分钟确保连接有效
		sqlDB.SetConnMaxIdleTime(10 * time.Minute)

		// 验证连接
		if err = sqlDB.Ping(); err != nil {
			err = fmt.Errorf("failed to ping database: %w", err)
			return
		}

		log.Println("GORM database connected successfully")
	})
	return err
}

// Get 获取 GORM 数据库连接
func Get() *gorm.DB {
	if DB == nil {
		panic("database not initialized, call Init() first")
	}
	return DB
}

// Close 关闭数据库连接
func Close() error {
	if DB != nil {
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// Transaction 执行事务
func Transaction(fn func(tx *gorm.DB) error) error {
	return Get().Transaction(fn)
}
