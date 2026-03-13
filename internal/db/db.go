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
	once sync.Once
)

// Init 初始化MySQL数据库连接
func Init(dsn string, maxOpenConns, maxIdleConns, maxLifetime int) error {
	var err error
	once.Do(func() {
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			log.Fatal(err)
			return
		}

		// 设置连接池参数
		db.SetMaxOpenConns(maxOpenConns)
		db.SetMaxIdleConns(maxIdleConns)
		db.SetConnMaxLifetime(time.Duration(maxLifetime) * time.Second)

		// 验证连接
		if err = db.Ping(); err != nil {
			err = fmt.Errorf("failed to ping database: %w", err)
			return
		}

		log.Println("MySQL database connected successfully")
	})
	return err
}

// Get 获取数据库连接
func Get() *sql.DB {
	if db == nil {
		panic("database not initialized, call Init() first")
	}
	return db
}

// Close 关闭数据库连接
func Close() error {
	if db != nil {
		return db.Close()
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

// UpdateDatabase 执行数据库更新脚本
func UpdateDatabase() error {
	// MySQL兼容的更新 - 使用存储过程检查列是否存在
	for _, stmt := range []struct {
		table string
		column string
		dataType string
		defaultVal string
	}{
		{"devices", "username", "VARCHAR(255)", "''"},
		{"devices", "priority", "INT", "100"},
		{"users", "mdcid", "VARCHAR(255)", "''"},
		{"devices", "dmrid", "INT", "0"},
		{"users", "dmrid", "INT", "0"},
		{"public_groups", "allow_callsign_ssid", "TEXT", "NULL"},
		{"public_groups", "ower_callsign", "VARCHAR(255)", "''"},
	} {
		if err := addColumnIfNotExists(stmt.table, stmt.column, stmt.dataType, stmt.defaultVal); err != nil {
			log.Printf("Warning: could not add column %s.%s: %v", stmt.table, stmt.column, err)
		}
	}

	// 删除重复用户记录（MySQL兼容的方式）
	duplicateSQL := `
		DELETE u FROM users u
		INNER JOIN users u2
		WHERE u.id > u2.id AND u.phone = u2.phone
	`
	log.Printf("Executing SQL: %s", duplicateSQL)
	if _, err := db.Exec(duplicateSQL); err != nil {
		log.Printf("Error executing duplicate cleanup: %v", err)
	} else {
		log.Println("Successfully cleaned up duplicate users")
	}

	return nil
}

// addColumnIfNotExists 添加列（如果不存在）- MySQL兼容方式
func addColumnIfNotExists(table, column, dataType, defaultVal string) error {
	// 检查列是否存在
	var count int
	checkSQL := `
		SELECT COUNT(*)
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = ?
		AND COLUMN_NAME = ?
	`
	err := db.QueryRow(checkSQL, table, column).Scan(&count)
	if err != nil {
		return err
	}

	// 如果列不存在，添加它
	if count == 0 {
		alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s DEFAULT %s", table, column, dataType, defaultVal)
		log.Printf("Executing SQL: %s", alterSQL)
		_, err := db.Exec(alterSQL)
		if err != nil {
			return err
		}
		log.Printf("Successfully added column %s.%s", table, column)
	}
	return nil
}

// CreateTableIfNotExists 创建表（如果不存在）
func CreateTableIfNotExists(createTableSQL string) error {
	_, err := db.Exec(createTableSQL)
	return err
}

// TableExists 检查表是否存在
func TableExists(tableName string) bool {
	query := `SHOW TABLES LIKE ?`
	row := db.QueryRow(query, tableName)
	var name string
	err := row.Scan(&name)
	return err == nil
}

// InitSchema 初始化数据库表结构
func InitSchema() error {
	schemas := getMySQLSchemas()

	for _, schema := range schemas {
		if err := CreateTableIfNotExists(schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

// getMySQLSchemas 获取MySQL表结构
func getMySQLSchemas() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS devices (
			id INT AUTO_INCREMENT UNIQUE,
			name VARCHAR(255),
			dmrid BIGINT,
			callsign VARCHAR(32),
			ssid TINYINT UNSIGNED,
			password VARCHAR(255),
			gird VARCHAR(255),
			dev_type INT,
			dev_model INT,
			group_id INT,
			status TINYINT,
			is_certed TINYINT(1),
			chan_name TEXT,
			online_time DATETIME,
			create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
			update_time DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			note TEXT,
			priority INT DEFAULT 100,
			PRIMARY KEY (id),
			INDEX idx_callsign_ssid (callsign, ssid),
			INDEX idx_dmrid (dmrid)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS servers (
			id INT AUTO_INCREMENT UNIQUE,
			name VARCHAR(255),
			server_type INT,
			join_key VARCHAR(255),
			cpu_type VARCHAR(255),
			mem_size VARCHAR(255),
			input_rate INT,
			output_rate INT,
			netcard VARCHAR(255),
			ip_type INT,
			ip_addr VARCHAR(255),
			dns_name VARCHAR(255),
			group_list INT,
			ower_id VARCHAR(255),
			ower_callsign VARCHAR(255),
			is_online TINYINT(1),
			status INT,
			create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
			update_time DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			note TEXT,
			udp_port INT,
			PRIMARY KEY (id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS public_groups (
			id INT AUTO_INCREMENT UNIQUE,
			name VARCHAR(255),
			type INT,
			call_sign VARCHAR(255),
			password VARCHAR(255),
			allow_dmr_id TEXT,
			allow_callsign_ssid TEXT,
			ower_id INT,
			ower_callsign VARCHAR(255) DEFAULT '',
			dev_list TEXT,
			master_server INT,
			slave_server INT,
			status INT DEFAULT 1,
			create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
			update_time DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			note TEXT,
			PRIMARY KEY (id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS users (
			id INT AUTO_INCREMENT UNIQUE,
			name VARCHAR(255),
			callsign VARCHAR(32),
			gird VARCHAR(255),
			phone VARCHAR(32),
			password VARCHAR(255),
			birthday VARCHAR(32),
			sex TINYINT(1),
			avatar VARCHAR(512),
			address VARCHAR(512),
			roles TEXT,
			introduction TEXT,
			alarm_msg TINYINT(1),
			status TINYINT,
			update_time DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			last_login_time DATETIME,
			login_err_times INT DEFAULT 0,
			create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
			openid VARCHAR(255),
			nickname VARCHAR(255),
			pid VARCHAR(255),
			last_login_ip VARCHAR(64),
			dmrid INT DEFAULT 0,
			mdcid VARCHAR(255) DEFAULT '',
			PRIMARY KEY (id),
			UNIQUE INDEX idx_name (name),
			INDEX idx_callsign (callsign),
			INDEX idx_phone (phone),
			INDEX idx_openid (openid)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS operator_log (
			id INT AUTO_INCREMENT UNIQUE,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			content TEXT,
			event_type VARCHAR(255),
			operator VARCHAR(255),
			operator_id INT,
			PRIMARY KEY (id),
			INDEX idx_event_type (event_type),
			INDEX idx_timestamp (timestamp)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS relay (
			id INT AUTO_INCREMENT UNIQUE,
			name VARCHAR(255),
			up_freq VARCHAR(255),
			down_freq VARCHAR(255),
			send_ctss VARCHAR(255),
			recive_ctss VARCHAR(255),
			ower_callsign VARCHAR(255),
			create_time DATETIME DEFAULT CURRENT_TIMESTAMP,
			update_time DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			status INT,
			note TEXT,
			PRIMARY KEY (id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}
}
