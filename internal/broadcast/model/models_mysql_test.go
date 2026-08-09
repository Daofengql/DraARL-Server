package model_test

import (
	"os"
	"strings"
	"testing"

	"draarl/internal/broadcast/model"
	"draarl/internal/gormdb"
)

func TestBroadcastSchemaMigrationMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_BROADCAST_SCHEMA_E2E")), "true") {
		t.Skip("set DRAARL_BROADCAST_SCHEMA_E2E=true and DRAARL_TEST_MYSQL_DSN to run the broadcast schema E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("DRAARL_TEST_MYSQL_DSN is required")
	}
	if err := gormdb.Init(&gormdb.Config{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 1, MaxLifetime: 60, LogLevel: "error"}); err != nil {
		t.Fatalf("initialize mysql: %v", err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })

	if err := gormdb.AutoMigrate(); err != nil {
		t.Fatalf("initial migration: %v", err)
	}
	db := gormdb.Get()
	user := &gormdb.User{Name: "broadcast-schema-owner", Email: "broadcast-schema@example.invalid", CallSign: "BCAST01", Roles: "admin", Status: 1}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create migration owner: %v", err)
	}
	virtualGroup := &gormdb.Group{Name: "broadcast-schema-virtual", Type: 1, OwerID: user.ID, Status: 0, IsVirtual: true}
	if err := db.Create(virtualGroup).Error; err != nil {
		t.Fatalf("create legacy virtual group: %v", err)
	}

	if err := gormdb.AutoMigrate(); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	for _, table := range []any{
		&model.BroadcastAudio{},
		&model.BroadcastSchedule{},
		&model.VirtualGroupBroadcastPolicy{},
		&model.BroadcastRun{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing table for %T", table)
		}
	}
	if !db.Migrator().HasColumn(&gormdb.CommRecord{}, "is_auto_broadcast") {
		t.Fatal("comm_records.is_auto_broadcast was not migrated")
	}
	if !db.Migrator().HasIndex(&model.BroadcastRun{}, "uk_broadcast_run_occurrence") {
		t.Fatal("broadcast run occurrence unique index was not migrated")
	}

	var policy model.VirtualGroupBroadcastPolicy
	if err := db.First(&policy, "virtual_group_id = ?", virtualGroup.ID).Error; err != nil {
		t.Fatalf("load backfilled policy: %v", err)
	}
	if policy.Mode != model.PolicySuspendAll || policy.AllowedSourceGroupID != nil || policy.UpdatedBy != user.ID {
		t.Fatalf("unexpected backfilled policy: %#v", policy)
	}
}
