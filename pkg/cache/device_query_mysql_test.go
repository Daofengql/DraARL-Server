package cache

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/gormdb"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestFilteredDeviceCacheMySQL(t *testing.T) {
	if os.Getenv("DRAARL_CACHE_E2E") != "true" {
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
	owners := []*gormdb.User{
		{Name: "cache-owner-a", Email: "cache-a@example.invalid", CallSign: "CACHEA", Status: 1},
		{Name: "cache-owner-b", Email: "cache-b@example.invalid", CallSign: "CACHEB", Status: 1},
	}
	for _, owner := range owners {
		if err := db.Create(owner).Error; err != nil {
			t.Fatal(err)
		}
	}
	devices := []*gormdb.Device{
		{Name: "cache-a-1", OwnerID: owners[0].ID, SSID: 1, Status: 1, ISOnline: true},
		{Name: "cache-a-2", OwnerID: owners[0].ID, SSID: 2, Status: 2},
		{Name: "cache-b-1", OwnerID: owners[1].ID, SSID: 1, Status: 1},
	}
	for _, dev := range devices {
		if err := db.Create(dev).Error; err != nil {
			t.Fatal(err)
		}
	}
	c, err := NewDeviceCache(DeviceCacheConfig{LocalTTL: time.Minute, MaxSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	var queries atomic.Int64
	if err := db.Callback().Query().Before("gorm:query").Register("test:cache_queries", func(tx *gorm.DB) {
		if tx.Statement.Table == "devices" {
			queries.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:cache_queries") })
	ctx := context.Background()
	filter := gormdb.DeviceListFilter{OwnerID: owners[0].ID}
	first, total, err := c.GetFilteredDeviceList(ctx, filter, 1, 1)
	if err != nil || total != 2 || len(first) != 1 {
		t.Fatalf("first=%v total=%d err=%v", first, total, err)
	}
	coldQueries := queries.Load()
	if coldQueries != 2 {
		t.Fatalf("page/count queries=%d, want 2", coldQueries)
	}
	_, _, err = c.GetFilteredDeviceList(ctx, filter, 1, 1)
	if err != nil || queries.Load() != coldQueries {
		t.Fatalf("cache miss: %v", err)
	}
	second, total, err := c.GetFilteredDeviceList(ctx, filter, 2, 1)
	if err != nil || total != 2 || len(second) != 1 || second[0].ID == first[0].ID {
		t.Fatalf("pagination cache collision: %v", err)
	}
	other, total, err := c.GetFilteredDeviceList(ctx, gormdb.DeviceListFilter{OwnerID: owners[1].ID}, 1, 10)
	if err != nil || total != 1 || len(other) != 1 || other[0].OwnerID != owners[1].ID {
		t.Fatalf("owner cache isolation failed: %v", err)
	}
	online := true
	active, total, err := c.GetFilteredDeviceList(ctx, gormdb.DeviceListFilter{OwnerID: owners[0].ID, IsOnline: &online}, 1, 10)
	if err != nil || total != 1 || len(active) != 1 || !active[0].ISOnline {
		t.Fatalf("online filter failed: %v", err)
	}
	if err := db.Model(devices[0]).Update("is_online", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := c.InvalidateDevice(ctx, devices[0].ID, owners[0].ID, 1); err != nil {
		t.Fatal(err)
	}
	active, total, err = c.GetFilteredDeviceList(ctx, gormdb.DeviceListFilter{OwnerID: owners[0].ID, IsOnline: &online}, 1, 10)
	if err != nil || total != 0 || len(active) != 0 {
		t.Fatalf("stale filtered page after mutation: %v", err)
	}
	// Readers and invalidators share publication ordering. A completed final
	// invalidation must prevent a slow earlier query from reintroducing a page.
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 10 {
				_, _, _ = c.GetFilteredDeviceList(ctx, filter, 1, 10)
				_ = c.InvalidateDeviceList(ctx)
			}
		})
	}
	wg.Wait()
	if err := db.Delete(devices[1]).Error; err != nil {
		t.Fatal(err)
	}
	if err := c.InvalidateDeviceList(ctx); err != nil {
		t.Fatal(err)
	}
	_, total, err = c.GetFilteredDeviceList(ctx, filter, 1, 10)
	if err != nil || total != 1 {
		t.Fatalf("stale count after delete: total=%d err=%v", total, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := c.GetFilteredDeviceList(canceled, filter, 1, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cache hit ignored cancellation: %v", err)
	}
}
