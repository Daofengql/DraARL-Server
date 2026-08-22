package gormdb

import (
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const migrationRecordE2EEnv = "DRAARL_MIGRATION_RECORD_E2E"

func TestRecordMigrationVersionRecoveryMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(migrationRecordE2EEnv)), "true") {
		t.Skip("set " + migrationRecordE2EEnv + "=true and DRAARL_TEST_MYSQL_DSN to run migration record recovery E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL test DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	db, err := gorm.Open(gormmysql.Open(parsed.FormatDSN()), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("open MySQL test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := ensureMigrationTable(db); err != nil {
		t.Fatalf("ensure migration table: %v", err)
	}
	if err := db.Where("version IN ?", []int{91, 92, 93}).Delete(&schemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("version IN ?", []int{91, 92, 93}).Delete(&schemaMigration{}).Error
	})

	var beforeAttempts atomic.Int32
	beforeName := "codex:migration-record-before-create"
	if err := db.Callback().Create().Before("gorm:create").Register(beforeName, func(tx *gorm.DB) {
		if tx.Statement.Table == migrationVersionTable && beforeAttempts.Add(1) == 1 {
			tx.AddError(errors.New("injected transient create failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := recordMigrationVersion(db, 91); err != nil {
		t.Fatalf("record after transient failure: %v", err)
	}
	db.Callback().Create().Remove(beforeName)
	if beforeAttempts.Load() != 2 {
		t.Fatalf("transient failure attempts=%d want=2", beforeAttempts.Load())
	}

	var afterAttempts atomic.Int32
	afterName := "codex:migration-record-after-create"
	if err := db.Callback().Create().After("gorm:create").Register(afterName, func(tx *gorm.DB) {
		if tx.Statement.Table == migrationVersionTable && afterAttempts.Add(1) == 1 {
			tx.AddError(errors.New("injected lost commit result"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := recordMigrationVersion(db, 92); err != nil {
		t.Fatalf("record after ambiguous commit: %v", err)
	}
	db.Callback().Create().Remove(afterName)
	if afterAttempts.Load() != 1 {
		t.Fatalf("ambiguous commit attempts=%d want=1", afterAttempts.Load())
	}

	var count int64
	if err := db.Model(&schemaMigration{}).Where("version IN ?", []int{91, 92}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("durable migration rows=%d want=2", count)
	}

	if err := startMigrationVersion(db, 93); err != nil {
		t.Fatalf("start migration intent: %v", err)
	}
	var pending schemaMigration
	if err := db.Where("version = ?", 93).First(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending.State != migrationStateRunning {
		t.Fatalf("pending migration state=%q want %q", pending.State, migrationStateRunning)
	}
	if err := completeMigrationVersion(db, 93); err != nil {
		t.Fatalf("complete migration intent: %v", err)
	}
	if err := db.Where("version = ?", 93).First(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending.State != migrationStateCompleted {
		t.Fatalf("completed migration state=%q want %q", pending.State, migrationStateCompleted)
	}
}

func TestAppliedMigrationVersionIgnoresRunningStateMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(migrationRecordE2EEnv)), "true") {
		t.Skip("set " + migrationRecordE2EEnv + "=true and DRAARL_TEST_MYSQL_DSN to run migration state E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL test database DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	db, err := gorm.Open(gormmysql.Open(parsed.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open MySQL test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := ensureMigrationTable(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("version IN ?", []int{1, 2}).Delete(&schemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Where("version IN ?", []int{1, 2}).Delete(&schemaMigration{}).Error })
	if err := db.Create(&schemaMigration{Version: 1, AppliedAt: time.Now(), State: migrationStateCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&schemaMigration{Version: 2, AppliedAt: time.Now(), State: migrationStateRunning}).Error; err != nil {
		t.Fatal(err)
	}
	version, err := appliedMigrationVersion(db)
	if err != nil || version != 1 {
		t.Fatalf("applied version=%d err=%v, want running version ignored and result 1", version, err)
	}
}
