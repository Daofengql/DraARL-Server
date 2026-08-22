package gormdb

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 数据库迁移版本管理。
// 【H12 安全修复】AutoMigrate 不再每次显式执行都重放高风险数据清洗
// （users 去重 DELETE、孤儿清理、comm_records 全表回填、大表唯一索引重建），
// 改为一次性小步执行并记录已应用版本；后续启动仅做幂等的 GORM schema 同步。

// CurrentMigrationVersion 当前已版本化迁移的版本号。
// 升级版本号只执行对应的小步迁移，不重放旧版本的数据清洗。
const CurrentMigrationVersion = 2

const migrationVersionTable = "schema_migrations"

const (
	migrationStateRunning   = "running"
	migrationStateCompleted = "completed"
)

const (
	migrationAdvisoryLockName = "draarl:schema-migration:v1"
	migrationLockPollInterval = 500 * time.Millisecond
	migrationLockWaitTimeout  = 30 * time.Second
	migrationRecordAttempts   = 3
	migrationRecordRetryDelay = 200 * time.Millisecond
)

type schemaMigration struct {
	Version   int       `gorm:"primaryKey;column:version"`
	AppliedAt time.Time `gorm:"column:applied_at"`
	State     string    `gorm:"column:state;size:16;not null;default:completed"`
}

func (schemaMigration) TableName() string {
	return migrationVersionTable
}

// ensureMigrationTable 确保版本记录表存在（幂等）。
func ensureMigrationTable(db *gorm.DB) error {
	return db.AutoMigrate(&schemaMigration{})
}

// appliedMigrationVersion 读取当前已应用的迁移版本；无记录视为 0。
func appliedMigrationVersion(db *gorm.DB) (int, error) {
	if err := ensureMigrationTable(db); err != nil {
		return 0, err
	}
	var records []schemaMigration
	if err := db.Order("version ASC").Find(&records).Error; err != nil {
		return 0, err
	}
	versions := make([]int, 0, len(records))
	for _, record := range records {
		state := strings.ToLower(strings.TrimSpace(record.State))
		switch state {
		case "", migrationStateCompleted:
			// Empty state is retained as completed for ledgers created before the
			// state column existed.
			versions = append(versions, record.Version)
		case migrationStateRunning:
			// An interrupted migration is intentionally left out. The caller
			// will see the preceding contiguous version and retry this step.
		default:
			return 0, fmt.Errorf("database migration version %d has unknown state %q", record.Version, record.State)
		}
	}
	return validateAppliedMigrationVersions(versions)
}

// validateAppliedMigrationVersions requires a contiguous migration history.
// Taking only MAX(version) would let a manually altered or partially written
// schema_migrations table skip an unapplied destructive step.
func validateAppliedMigrationVersions(versions []int) (int, error) {
	if len(versions) == 0 {
		return 0, nil
	}
	sorted := append([]int(nil), versions...)
	sort.Ints(sorted)
	if sorted[len(sorted)-1] > CurrentMigrationVersion {
		return 0, fmt.Errorf("database migration version %d is newer than supported version %d", sorted[len(sorted)-1], CurrentMigrationVersion)
	}
	expected := 1
	for _, version := range sorted {
		if version != expected {
			return 0, fmt.Errorf("database migration history is not contiguous: expected version %d, found %d", expected, version)
		}
		expected++
	}
	return sorted[len(sorted)-1], nil
}

func applicationSchemaIsEmpty(tables []string) bool {
	for _, table := range tables {
		if !strings.EqualFold(table, migrationVersionTable) {
			return false
		}
	}
	return true
}

// startMigrationVersion records an in-progress migration with bounded retries.
// A durable running row makes a crash between the migration body and its final
// ledger update observable; the next startup retries the idempotent step.
func startMigrationVersion(db *gorm.DB, version int) error {
	return migrationVersionWriteWithRetry(db, version, migrationStateRunning, "开始数据库迁移版本")
}

// completeMigrationVersion marks a previously started migration complete.
func completeMigrationVersion(db *gorm.DB, version int) error {
	if db == nil {
		return errors.New("complete migration version requires database")
	}
	ctx := context.Background()
	if db.Statement != nil && db.Statement.Context != nil {
		ctx = db.Statement.Context
	}
	return retryMigrationVersionRecord(ctx, migrationRecordAttempts, migrationRecordRetryDelay, func() error {
		result := db.Model(&schemaMigration{}).
			Where("version = ?", version).
			Updates(map[string]any{"state": migrationStateCompleted, "applied_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// Recover a missing intent row (for example, if an old schema did
			// not yet have the ledger row) without creating duplicates.
			return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{
				Version: version, AppliedAt: time.Now(), State: migrationStateCompleted,
			}).Error
		}
		return nil
	}, func() (bool, error) {
		var record schemaMigration
		err := db.Where("version = ?", version).First(&record).Error
		if err != nil {
			return false, err
		}
		state := strings.ToLower(strings.TrimSpace(record.State))
		return state == "" || state == migrationStateCompleted, nil
	}, func(attempt int) {
		log.Printf("[Migration Info] 已完成数据库迁移版本 %d（尝试 %d）", version, attempt)
	})
}

func migrationVersionWriteWithRetry(db *gorm.DB, version int, state, message string) error {
	if db == nil {
		return errors.New("record migration version requires database")
	}
	ctx := context.Background()
	if db.Statement != nil && db.Statement.Context != nil {
		ctx = db.Statement.Context
	}
	return retryMigrationVersionRecord(ctx, migrationRecordAttempts, migrationRecordRetryDelay, func() error {
		record := schemaMigration{Version: version, AppliedAt: time.Now(), State: state}
		return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error
	}, func() (bool, error) {
		var count int64
		err := db.Model(&schemaMigration{}).Where("version = ?", version).Count(&count).Error
		return count == 1, err
	}, func(attempt int) {
		log.Printf("[Migration Info] %s %d（尝试 %d）", message, version, attempt)
	})
}

// recordMigrationVersion records a completed migration with bounded retries.
// Every write attempt is followed by an independent read-back: if MySQL
// committed the INSERT but the client only received a connection error, the
// durable row is accepted instead of rerunning an already completed migration.
func recordMigrationVersion(db *gorm.DB, version int) error {
	if err := startMigrationVersion(db, version); err != nil {
		return err
	}
	return completeMigrationVersion(db, version)
}

func retryMigrationVersionRecord(ctx context.Context, attempts int, delay time.Duration, write func() error, verify func() (bool, error), onSuccess func(int)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if attempts <= 0 || write == nil || verify == nil {
		return errors.New("invalid migration version retry configuration")
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		writeErr := write()
		durable, verifyErr := verify()
		if verifyErr == nil && durable {
			if onSuccess != nil {
				onSuccess(attempt)
			}
			return nil
		}
		switch {
		case writeErr != nil && verifyErr != nil:
			lastErr = errors.Join(writeErr, verifyErr)
		case writeErr != nil:
			lastErr = writeErr
		case verifyErr != nil:
			lastErr = verifyErr
		default:
			lastErr = errors.New("migration version row was not durable after write")
		}
		if attempt == attempts {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("record migration version after %d attempts: %w", attempts, lastErr)
}

// withMigrationLock serializes destructive schema/data migration across
// server instances. GET_LOCK is connection-scoped, so Connection keeps the
// lock and every migration statement on the same underlying MySQL session.
// Non-MySQL dialects (unit-test fakes and tooling) retain the old behavior.
func withMigrationLock(ctx context.Context, db *gorm.DB, migrate func(*gorm.DB) error) error {
	if db == nil || migrate == nil {
		return errors.New("migration lock requires database and callback")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if db.Dialector.Name() != "mysql" {
		return migrate(db.WithContext(ctx))
	}
	return db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		deadline := time.NewTimer(migrationLockWaitTimeout)
		defer deadline.Stop()
		ticker := time.NewTicker(migrationLockPollInterval)
		defer ticker.Stop()
		for {
			var acquired int
			if err := conn.Raw("SELECT GET_LOCK(?, 0)", migrationAdvisoryLockName).Scan(&acquired).Error; err != nil {
				return fmt.Errorf("acquire migration lock: %w", err)
			}
			if acquired == 1 {
				defer func() {
					if err := conn.Exec("SELECT RELEASE_LOCK(?)", migrationAdvisoryLockName).Error; err != nil {
						log.Printf("[Migration Warning] release migration lock failed: %v", err)
					}
				}()
				return migrate(conn.WithContext(ctx))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return fmt.Errorf("timed out waiting for migration lock %q", migrationAdvisoryLockName)
			case <-ticker.C:
			}
		}
	})
}
