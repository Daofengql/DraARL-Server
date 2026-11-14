package main

import (
	"draarl/internal/config"
	"draarl/internal/gormdb"
)

// migrateDatabaseOnly runs before Redis, authentication, background workers and
// network listeners are initialized by the normal startup path.
func migrateDatabaseOnly(cfg *config.Configuration) error {
	if err := gormdb.Init(&gormdb.Config{
		DSN:          cfg.GetDSN(),
		MaxOpenConns: cfg.Database.MaxOpenConns,
		MaxIdleConns: cfg.Database.MaxIdleConns,
		MaxLifetime:  cfg.Database.MaxLifetime,
		MaxIdleTime:  cfg.Database.MaxIdleTime,
		LogLevel:     "error",
		Timezone:     cfg.Database.Timezone,
	}); err != nil {
		return err
	}
	defer func() { _ = gormdb.Close() }()
	return gormdb.AutoMigrate()
}
