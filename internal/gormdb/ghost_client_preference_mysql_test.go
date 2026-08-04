package gormdb

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestGhostClientPreferenceMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_GHOST_PREFERENCE_E2E")), "true") {
		t.Skip("set DRAARL_GHOST_PREFERENCE_E2E=true and DRAARL_TEST_MYSQL_DSN to run the preference E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("DRAARL_TEST_MYSQL_DSN is required")
	}
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
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
	if err := db.AutoMigrate(
		&User{}, &Group{}, &GroupMember{}, &GroupLink{}, &Device{}, &DeviceConfig{}, &CommRecord{},
		&OperatorCert{}, &Logbook{}, &UserDevicePreference{}, &GhostClientPreference{},
		&GhostClientSubscription{}, &UserRadioPreset{},
	); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := &User{Name: "ghost-pref-" + suffix, Email: "ghost-pref-" + suffix + "@example.invalid", CallSign: "GP" + suffix[len(suffix)-8:], Status: 1, ApprovalStatus: 1}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	firstGroup := &Group{Name: "ghost-pref-first-" + suffix, Type: 1, Status: 1}
	secondGroup := &Group{Name: "ghost-pref-second-" + suffix, Type: 1, Status: 1}
	if err := db.Create(firstGroup).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(secondGroup).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = deleteGhostClientPreferencesByUser(db, user.ID)
		_ = db.Where("user_id = ?", user.ID).Delete(&UserDevicePreference{}).Error
		_ = db.Delete(&Group{}, []int{firstGroup.ID, secondGroup.ID}).Error
		_ = db.Delete(&User{}, user.ID).Error
	})

	if err := NewUserRepository().UpsertUserDevicePreference(user.ID, 101, firstGroup.ID); err != nil {
		t.Fatal(err)
	}
	instanceID := uuid.NewString()
	repository := NewGhostClientPreferenceRepository()
	pref, err := repository.GetOrCreate(user.ID, 101, instanceID, firstGroup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pref.TxGroupID != firstGroup.ID || len(pref.RxGroupIDs) != 1 || pref.RxGroupIDs[0] != firstGroup.ID {
		t.Fatalf("legacy preference was not used to initialize instance: %#v", pref)
	}

	if err := repository.ReplaceRouting(user.ID, 101, instanceID, secondGroup.ID, []int{secondGroup.ID, firstGroup.ID, secondGroup.ID}); err != nil {
		t.Fatal(err)
	}
	pref, err = repository.Get(user.ID, 101, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	if pref.TxGroupID != secondGroup.ID || len(pref.RxGroupIDs) != 2 || pref.RxGroupIDs[0] != firstGroup.ID || pref.RxGroupIDs[1] != secondGroup.ID {
		t.Fatalf("routing replacement was not atomic or normalized: %#v", pref)
	}

	if err := repository.ReplaceRouting(user.ID, 101, instanceID, 2147483000, []int{firstGroup.ID}); err == nil {
		t.Fatal("invalid transmit group unexpectedly committed")
	}
	pref, err = repository.Get(user.ID, 101, instanceID)
	if err != nil || pref.TxGroupID != secondGroup.ID || len(pref.RxGroupIDs) != 2 {
		t.Fatalf("failed replacement changed committed routing: pref=%#v err=%v", pref, err)
	}

	if err := NewGroupRepository().DeleteGroupWithCascade(secondGroup.ID); err != nil {
		t.Fatal(err)
	}
	pref, err = repository.Get(user.ID, 101, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	if pref.TxGroupID != 0 || len(pref.RxGroupIDs) != 1 || pref.RxGroupIDs[0] != firstGroup.ID {
		t.Fatalf("deleted group remained in instance routing: %#v", pref)
	}

	if _, err := NewUserRepository().DeleteUserWithCascade(user.ID); err != nil {
		t.Fatal(err)
	}
	pref, err = repository.Get(user.ID, 101, instanceID)
	if err != nil || pref != nil {
		t.Fatalf("deleted user retained instance preference: pref=%#v err=%v", pref, err)
	}
}
