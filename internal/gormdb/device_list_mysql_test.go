package gormdb

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeviceListOnlineFilterMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_DEVICE_LIST_E2E")), "true") {
		t.Skip("set DRAARL_DEVICE_LIST_E2E=true and DRAARL_TEST_MYSQL_DSN to run device-list E2E")
	}

	parsed, err := drivermysql.ParseDSN(strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	db, err := gorm.Open(gormmysql.Open(parsed.FormatDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&User{}, &Device{}); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	owner := &User{
		Name:           "device-list-owner-" + suffix,
		Email:          "device-list-owner-" + suffix + "@example.invalid",
		CallSign:       "DLO" + suffix[len(suffix)-8:],
		Status:         1,
		ApprovalStatus: 1,
	}
	if err := db.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	devices := []*Device{
		{Name: "device-list-online-" + suffix, OwnerID: owner.ID, SSID: 1, ISOnline: true, Status: 1},
		{Name: "device-list-offline-" + suffix, OwnerID: owner.ID, SSID: 2, ISOnline: false, Status: 1},
	}
	if err := db.Create(&devices).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Unscoped().Where("id IN ?", []int{devices[0].ID, devices[1].ID}).Delete(&Device{}).Error
		_ = db.Unscoped().Delete(&User{}, owner.ID).Error
	})

	repo := &DeviceRepository{db: db}
	online := true
	got, total, err := repo.ListDevicesPaginated(DeviceListFilter{OwnerID: owner.ID, IsOnline: &online}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(got) != 1 || !got[0].ISOnline {
		t.Fatalf("online page=(%d,%d,%v), want one online device", total, len(got), got)
	}

	offline := false
	got, total, err = repo.ListDevicesPaginated(DeviceListFilter{OwnerID: owner.ID, IsOnline: &offline}, 20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(got) != 1 || got[0].ISOnline {
		t.Fatalf("offline page=(%d,%d,%v), want one offline device", total, len(got), got)
	}
}
