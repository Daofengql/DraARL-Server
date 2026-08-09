package gormdb

import (
	"errors"
	"strings"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

var (
	ErrCallSignConflict  = errors.New("callsign already in use")
	ErrOwnerSSIDConflict = errors.New("device owner ssid already in use")
)

// NormalizeCallSign 标准化呼号输入，避免不同入口出现大小写和空格漂移。
func NormalizeCallSign(callsign string) string {
	return strings.ToUpper(strings.TrimSpace(callsign))
}

// IsDuplicateKeyError 判断是否为数据库唯一键冲突。
func IsDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") && strings.Contains(msg, "entry")
}

// IsDuplicateColumnError 判断唯一键冲突是否指向特定列。
func IsDuplicateColumnError(err error, column string) bool {
	if !IsDuplicateKeyError(err) {
		return false
	}
	if column == "" {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(column))
}
