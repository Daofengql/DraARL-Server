package db

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var (
	db   *sql.DB
	dbMu sync.RWMutex
)

// Init 初始化MySQL数据库连接
func Init(dsn string, maxOpenConns, maxIdleConns, maxLifetime int) error {
	dbMu.Lock()
	defer dbMu.Unlock()

	newDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}

	// 设置连接池参数
	newDB.SetMaxOpenConns(maxOpenConns)
	newDB.SetMaxIdleConns(maxIdleConns)
	newDB.SetConnMaxLifetime(time.Duration(maxLifetime) * time.Second)

	// 验证连接
	if err = newDB.Ping(); err != nil {
		_ = newDB.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// 支持重复初始化（测试/重载场景）
	if db != nil {
		_ = db.Close()
	}
	db = newDB

	log.Println("MySQL database connected successfully")
	return nil
}

// Get 获取数据库连接
func Get() *sql.DB {
	dbMu.RLock()
	defer dbMu.RUnlock()
	if db == nil {
		panic("database not initialized, call Init() first")
	}
	return db
}

// Close 关闭数据库连接
func Close() error {
	dbMu.Lock()
	defer dbMu.Unlock()
	if db != nil {
		err := db.Close()
		db = nil
		return err
	}
	return nil
}

// Exec 执行SQL语句
func Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.Exec(query, args...)
}

// Query 执行查询
func Query(query string, args ...interface{}) (*sql.Rows, error) {
	return db.Query(query, args...)
}

// QueryRow 执行单行查询
func QueryRow(query string, args ...interface{}) *sql.Row {
	return db.QueryRow(query, args...)
}

// Prepare 准备SQL语句
func Prepare(query string) (*sql.Stmt, error) {
	return db.Prepare(query)
}

// Begin 开始事务
func Begin() (*sql.Tx, error) {
	return db.Begin()
}
