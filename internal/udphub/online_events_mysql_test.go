package udphub

import (
	"os"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/models"
	drivermysql "github.com/go-sql-driver/mysql"
)

func TestOnlineEventsPersistLatestSessionMySQL(t *testing.T) {
	if os.Getenv("DRAARL_ONLINE_EVENTS_E2E") != "true" {
		t.Skip("requires disposable MySQL")
	}
	parsed, err := drivermysql.ParseDSN(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if err != nil || parsed == nil || !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatal("disposable draarl_test_ database required")
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 5, MaxIdleConns: 2, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	if err := gormdb.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	db := gormdb.Get()
	owner := &gormdb.User{Name: "online-events", Email: "online-events@example.invalid", CallSign: "EVENTS", Status: 1}
	if err := db.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	row := &gormdb.Device{Name: "online-events-1", OwnerID: owner.ID, SSID: 1, Status: 1}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	old := &models.Device{ID: row.ID, OwnerID: owner.ID, SSID: 1, ISOnline: false}
	latest := &models.Device{ID: row.ID, OwnerID: owner.ID, SSID: 1, ISOnline: true, OnlineTime: time.Now(), LastOnlineIP: "192.0.2.5"}
	runtimeIndexMu.Lock()
	previous := devOwnerSSIDMap
	devOwnerSSIDMap = map[string]*models.Device{getOwnerSSIDKey(owner.ID, 1): latest}
	runtimeIndexMu.Unlock()
	t.Cleanup(func() { runtimeIndexMu.Lock(); devOwnerSSIDMap = previous; runtimeIndexMu.Unlock() })
	if err := syncDeviceOnlineEvent(old); err != nil {
		t.Fatal(err)
	}
	if err := db.First(row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !row.ISOnline || row.LastOnlineIP != "192.0.2.5" {
		t.Fatalf("old notification replaced latest session: %+v", row)
	}
	latest.UpdateRuntime(func(current *models.Device) { current.ISOnline = false })
	if err := syncDeviceOnlineEvent(latest); err != nil {
		t.Fatal(err)
	}
	if err := db.First(row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.ISOnline {
		t.Fatal("offline event was not persisted")
	}
}
