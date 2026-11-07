package gormdb

import (
	"context"
	"database/sql"
	"errors"
)

func SQLDB() (*sql.DB, error) {
	managerMu.Lock()
	defer managerMu.Unlock()
	if dbManager == nil {
		return nil, errors.New("database not initialized")
	}
	dbManager.mu.RLock()
	defer dbManager.mu.RUnlock()
	if dbManager.db == nil {
		return nil, errors.New("database closed")
	}
	return dbManager.db.DB()
}

func PingContext(ctx context.Context) error {
	db, err := SQLDB()
	if err != nil {
		return err
	}
	return db.PingContext(ctx)
}

func PoolStats() (sql.DBStats, bool) {
	db, err := SQLDB()
	if err != nil {
		return sql.DBStats{}, false
	}
	return db.Stats(), true
}
