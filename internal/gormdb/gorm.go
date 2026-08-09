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
	managerMu sync.Mutex
)

// DBManager 数据库连接管理器
type DBManager struct {
	mu         sync.RWMutex
	db         *gorm.DB
	cfg        *Config
	lastHealth time.Time
	stopHealth chan struct{}
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
	managerMu.Lock()
	defer managerMu.Unlock()

	manager := &DBManager{
		cfg:        cfg,
		lastHealth: time.Now(),
		stopHealth: make(chan struct{}),
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

	newDB, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
		Logger: gormLogger,
		NowFunc: func() time.Time {
			return time.Now()
		},
	})
	if err != nil {
		return fmt.Errorf("failed to connect database: %w", err)
	}
	manager.db = newDB

	// 获取底层的 sql.DB
	sqlDB, err := manager.db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// 设置连接池参数
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.MaxLifetime) * time.Second)
	// 设置连接最大空闲时间为 30 秒，避免使用已失效的连接
	// MySQL wait_timeout 默认 8 小时，但网络环境可能导致连接提前失效
	sqlDB.SetConnMaxIdleTime(30 * time.Second)

	// 验证连接
	if err = sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// 支持重复初始化（测试/重载场景）
	if dbManager != nil {
		close(dbManager.stopHealth)
		if oldSQLDB, oldErr := dbManager.db.DB(); oldErr == nil {
			_ = oldSQLDB.Close()
		}
	}
	dbManager = manager

	log.Println("GORM database connected successfully")

	// 启动健康检查协程
	go dbManager.healthCheck()

	return nil
}

// healthCheck 定期检查数据库连接健康状态
func (m *DBManager) healthCheck() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkHealth()
		case <-m.stopHealth:
			return
		}
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

// GetDB 获取数据库会话
func GetDB() *gorm.DB {
	if dbManager == nil {
		panic("database not initialized, call Init() first")
	}

	dbManager.mu.RLock()
	defer dbManager.mu.RUnlock()

	if dbManager.db == nil {
		panic("database connection is nil")
	}

	// 返回一个新的会话
	return dbManager.db.Session(&gorm.Session{
		Context:                context.Background(),
		SkipDefaultTransaction: true,
		PrepareStmt:            false, // 禁用预编译语句缓存，避免连接失效问题
	})
}

// Get 获取 GORM 数据库连接（兼容旧代码）
func Get() *gorm.DB {
	return GetDB()
}

// Close 关闭数据库连接
func Close() error {
	managerMu.Lock()
	defer managerMu.Unlock()

	if dbManager == nil {
		return nil
	}

	dbManager.mu.Lock()
	defer dbManager.mu.Unlock()

	if dbManager.stopHealth != nil {
		close(dbManager.stopHealth)
		dbManager.stopHealth = nil
	}

	if dbManager.db != nil {
		sqlDB, err := dbManager.db.DB()
		if err != nil {
			return err
		}
		dbManager.db = nil
		dbManager = nil
		return sqlDB.Close()
	}

	dbManager = nil
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
