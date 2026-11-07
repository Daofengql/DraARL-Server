package cache

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"
	drivermysql "github.com/go-sql-driver/mysql"
)

func BenchmarkFilteredDeviceListMySQL(b *testing.B) {
	if os.Getenv("DRAARL_BENCH_MYSQL") != "true" {
		b.Skip("requires DRAARL_BENCH_MYSQL=true and a disposable draarl_test_ MySQL DSN")
	}
	parsed, err := drivermysql.ParseDSN(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if err != nil || !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		b.Fatal("disposable draarl_test_ database required")
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 8, MaxIdleConns: 2, LogLevel: "silent"}); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = gormdb.Close() })
	if err := gormdb.AutoMigrate(); err != nil {
		b.Fatal(err)
	}
	db := gormdb.Get()
	owner := &gormdb.User{Name: fmt.Sprintf("bench-%d", time.Now().UnixNano()), Email: fmt.Sprintf("bench-%d@example.invalid", time.Now().UnixNano()), CallSign: "BENCH", Status: 1}
	if err := db.Create(owner).Error; err != nil {
		b.Fatal(err)
	}
	devices := make([]*gormdb.Device, 100)
	for i := range devices {
		devices[i] = &gormdb.Device{Name: fmt.Sprintf("bench-%03d", i), OwnerID: owner.ID, SSID: uint8(i + 1), Status: 1}
	}
	if err := db.Create(devices).Error; err != nil {
		b.Fatal(err)
	}
	filter := gormdb.DeviceListFilter{OwnerID: owner.ID}
	ctx := context.Background()
	b.Run("database_page_20", func(b *testing.B) {
		repo := gormdb.NewDeviceRepository().WithContext(ctx)
		b.ReportAllocs()
		for b.Loop() {
			items, count, err := repo.ListDevicesPaginated(filter, 20, 1)
			if err != nil || count != 100 || len(items) != 20 {
				b.Fatalf("list: count=%d error=%v", count, err)
			}
		}
	})
	b.Run("cached_page_20", func(b *testing.B) {
		c, err := NewDeviceCache(DeviceCacheConfig{LocalTTL: time.Hour, MaxSize: 100})
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := c.GetFilteredDeviceList(ctx, filter, 1, 20); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			items, count, err := c.GetFilteredDeviceList(ctx, filter, 1, 20)
			if err != nil || count != 100 || len(items) != 20 {
				b.Fatalf("cache: count=%d error=%v", count, err)
			}
		}
	})
}
