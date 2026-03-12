package gormdb

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	dbManager *DBManager
	once      sync.Once
)

// DBManager 数据库连接管理器
type DBManager struct {
	mu         sync.RWMutex
	db         *gorm.DB
	cfg        *Config
	lastHealth time.Time
}

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
		dbManager = &DBManager{
			cfg:        cfg,
			lastHealth: time.Now(),
		}

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

		dbManager.db, err = gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
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
		sqlDB, err := dbManager.db.DB()
		if err != nil {
			err = fmt.Errorf("failed to get sql.DB: %w", err)
			return
		}

		// 设置连接池参数
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.MaxLifetime) * time.Second)
		// 设置连接最大空闲时间，MySQL wait_timeout 默认 8 小时，设置 5 分钟确保连接有效
		sqlDB.SetConnMaxIdleTime(5 * time.Minute)

		// 验证连接
		if err = sqlDB.Ping(); err != nil {
			err = fmt.Errorf("failed to ping database: %w", err)
			return
		}

		log.Println("GORM database connected successfully")

		// 启动健康检查协程
		go dbManager.healthCheck()
	})
	return err
}

// healthCheck 定期检查数据库连接健康状态
func (m *DBManager) healthCheck() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.checkHealth()
	}
}

// checkHealth 检查连接健康状态
func (m *DBManager) checkHealth() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.db == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sqlDB, err := m.db.DB()
	if err != nil {
		log.Printf("Database health check failed to get sql.DB: %v", err)
		return false
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		log.Printf("Database health check ping failed: %v", err)
		return false
	}

	m.lastHealth = time.Now()
	return true
}

// GetDB 获取一个新的数据库会话，避免复用同一个 *gorm.DB 实例
func GetDB() *gorm.DB {
	if dbManager == nil {
		panic("database not initialized, call Init() first")
	}

	dbManager.mu.RLock()
	defer dbManager.mu.RUnlock()

	if dbManager.db == nil {
		panic("database connection is nil")
	}

	// 返回一个新的会话，确保每次请求都使用独立的上下文
	// 这样可以避免连接状态问题
	return dbManager.db.Session(&gorm.Session{
		Context: context.Background(),
		// 跳过默认事务，提高性能
		SkipDefaultTransaction: true,
		// 禁用预编译语句缓存（某些情况下可能有问题）
		PrepareStmt: true,
	})
}

// Get 获取 GORM 数据库连接（兼容旧代码）
func Get() *gorm.DB {
	return GetDB()
}

// Close 关闭数据库连接
func Close() error {
	if dbManager != nil {
		dbManager.mu.Lock()
		defer dbManager.mu.Unlock()

		if dbManager.db != nil {
			sqlDB, err := dbManager.db.DB()
			if err != nil {
				return err
			}
			return sqlDB.Close()
		}
	}
	return nil
}

// Transaction 执行事务
func Transaction(fn func(tx *gorm.DB) error) error {
	return GetDB().Transaction(fn)
}

// Ping 检查数据库连接是否正常
func Ping() error {
	if dbManager == nil {
		return fmt.Errorf("database manager not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sqlDB, err := dbManager.db.DB()
	if err != nil {
		return err
	}

	return sqlDB.PingContext(ctx)
}

