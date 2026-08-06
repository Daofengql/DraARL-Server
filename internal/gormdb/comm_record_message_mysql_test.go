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
	if err := db.AutoMigrate(&User{}, &Device{}, &Group{}, &CommRecord{}, &CommRecordDeliveryGroup{}); err != nil {
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
	group := &Group{Name: "message-migration-group-" + suffix, Type: 1, OwerID: user.ID, Status: 1}
	if err := db.Create(group).Error; err != nil {
		t.Fatal(err)
	}
	userID := uint(user.ID)
	groupID := uint(group.ID)
	now := time.Now().UTC().Truncate(time.Millisecond)
	legacyText := &CommRecord{DeviceID: uint(device.ID), DeviceSSID: device.SSID, GroupID: &groupID, UserID: &userID, StartTime: now, EndTime: now, AudioPath: "text:legacy migration text", Status: 2}
	legacyGhost := &CommRecord{DeviceID: 0, DeviceSSID: 101, GroupID: &groupID, UserID: &userID, StartTime: now.Add(time.Millisecond), EndTime: now.Add(time.Millisecond), Status: 2}
	if err := db.Create(legacyText).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(legacyGhost).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Delete(&CommRecord{}, []uint{legacyText.ID, legacyGhost.ID}).Error
		_ = db.Delete(&Group{}, group.ID).Error
		_ = db.Delete(&Device{}, device.ID).Error
		_ = db.Delete(&User{}, user.ID).Error
	})

	if err := backfillCommRecordMessages(db); err != nil {
		t.Fatal(err)
	}
	if err := BackfillCommRecordDeliveryGroups(db); err != nil {
		t.Fatal(err)
	}
	if err := BackfillCommRecordDeliveryGroups(db); err != nil {
		t.Fatalf("repeat delivery snapshot backfill: %v", err)
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
	var snapshots []CommRecordDeliveryGroup
	if err := db.Where("record_id IN ?", []uint{legacyText.ID, legacyGhost.ID}).Order("record_id ASC").Find(&snapshots).Error; err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].GroupID != groupID || snapshots[1].GroupID != groupID {
		t.Fatalf("source-group delivery snapshots=%#v want two rows for group %d", snapshots, groupID)
	}
	currentDB := ""
	if err := db.Raw("SELECT DATABASE()").Scan(&currentDB).Error; err != nil {
		t.Fatal(err)
	}
	var indexColumns []string
	if err := db.Raw(`
		SELECT column_name
		FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = 'comm_record_delivery_groups'
			AND index_name = 'idx_delivery_group_record'
		ORDER BY seq_in_index
	`, currentDB).Scan(&indexColumns).Error; err != nil {
		t.Fatal(err)
	}
	if len(indexColumns) != 2 || indexColumns[0] != "group_id" || indexColumns[1] != "record_id" {
		t.Fatalf("delivery snapshot index columns=%v want=[group_id record_id]", indexColumns)
	}
	if err := db.Delete(&CommRecord{}, legacyGhost.ID).Error; err != nil {
		t.Fatal(err)
	}
	var snapshotCount int64
	if err := db.Model(&CommRecordDeliveryGroup{}).Where("record_id = ?", legacyGhost.ID).Count(&snapshotCount).Error; err != nil {
		t.Fatal(err)
	}
	if snapshotCount != 0 {
		t.Fatalf("delivery snapshots remained after record deletion: %d", snapshotCount)
	}
}
