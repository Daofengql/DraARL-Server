package gormdb

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
)

func TestCommRecordMessageMigrationMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_MESSAGE_MIGRATION_E2E")), "true") {
		t.Skip("set DRAARL_MESSAGE_MIGRATION_E2E=true and DRAARL_TEST_MYSQL_DSN to run the migration E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("DRAARL_TEST_MYSQL_DSN is required")
	}
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	if err := Init(&Config{DSN: parsed.FormatDSN(), MaxOpenConns: 10, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	db := Get()
	if err := db.AutoMigrate(&User{}, &Device{}, &CommRecord{}); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := &User{Name: "message-migration-" + suffix, Email: "message-migration-" + suffix + "@example.invalid", CallSign: "MM" + suffix[len(suffix)-8:], NickName: "Migration Sender", Status: 1, ApprovalStatus: 1}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	device := &Device{Name: "message-migration-device-" + suffix, OwnerID: user.ID, SSID: 1, DevModel: 23, Status: 1}
	if err := db.Create(device).Error; err != nil {
		t.Fatal(err)
	}
	userID := uint(user.ID)
	now := time.Now().UTC().Truncate(time.Millisecond)
	legacyText := &CommRecord{DeviceID: uint(device.ID), DeviceSSID: device.SSID, UserID: &userID, StartTime: now, EndTime: now, AudioPath: "text:legacy migration text", Status: 2}
	legacyGhost := &CommRecord{DeviceID: 0, DeviceSSID: 101, UserID: &userID, StartTime: now.Add(time.Millisecond), EndTime: now.Add(time.Millisecond), Status: 2}
	if err := db.Create(legacyText).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(legacyGhost).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Delete(&CommRecord{}, []uint{legacyText.ID, legacyGhost.ID}).Error
		_ = db.Delete(&Device{}, device.ID).Error
		_ = db.Delete(&User{}, user.ID).Error
	})

	if err := backfillCommRecordMessages(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(legacyText, legacyText.ID).Error; err != nil {
		t.Fatal(err)
	}
	if legacyText.MessageType != CommMessageTypeText || legacyText.TextContent != "legacy migration text" || legacyText.AudioPath != "" {
		t.Fatalf("legacy text was not migrated: %#v", legacyText)
	}
	if legacyText.SenderUsername != user.Name || legacyText.SenderCallSign != user.CallSign || legacyText.SenderNickname != user.NickName || legacyText.SenderDevModel != device.DevModel {
		t.Fatalf("physical sender snapshot was not backfilled: %#v", legacyText)
	}
	if err := db.First(legacyGhost, legacyGhost.ID).Error; err != nil {
		t.Fatal(err)
	}
	if legacyGhost.SenderUsername != user.Name || legacyGhost.SenderDevModel != int(legacyGhost.DeviceSSID) {
		t.Fatalf("ghost sender snapshot was not backfilled: %#v", legacyGhost)
	}
	if !db.Migrator().HasIndex(&CommRecord{}, "idx_comm_records_group_status_start_id") {
		t.Fatal("message cursor index was not created")
	}
	if !db.Migrator().HasIndex(&CommRecord{}, "idx_comm_records_group_status_type_start_id") {
		t.Fatal("typed message cursor index was not created")
	}
}
