package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestDeviceListBatchOwnersAndRequestCancellationMySQL(t *testing.T) {
	if os.Getenv("DRAARL_TODO_E2E") != "true" {
		t.Skip("set DRAARL_TODO_E2E=true and DRAARL_TEST_MYSQL_DSN")
	}
	parsed, err := drivermysql.ParseDSN(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if err != nil || !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatal("a disposable draarl_test_ database is required")
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 5, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	if err := gormdb.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := cache.InitManager(); err != nil {
		t.Fatal(err)
	}
	db := gormdb.Get()
	suffix := fmt.Sprint(time.Now().UnixNano())
	prefix := "todo-device-" + suffix
	ownerIDs := make([]int, 0, 6)
	t.Cleanup(func() {
		_ = db.Where("owner_id IN ?", ownerIDs).Delete(&gormdb.Device{}).Error
		_ = db.Where("id IN ?", ownerIDs).Delete(&gormdb.User{}).Error
	})
	for i := range 6 {
		owner := &gormdb.User{Name: fmt.Sprintf("todo-owner-%s-%d", suffix, i), Email: fmt.Sprintf("todo-%s-%d@example.invalid", suffix, i), CallSign: fmt.Sprintf("T%s%d", suffix[len(suffix)-6:], i), Status: 1, ApprovalStatus: 1}
		if err := db.Create(owner).Error; err != nil {
			t.Fatal(err)
		}
		ownerIDs = append(ownerIDs, owner.ID)
		device := &gormdb.Device{Name: fmt.Sprintf("%s-%d", prefix, i), OwnerID: owner.ID, SSID: 1, Status: 1}
		if err := db.Create(device).Error; err != nil {
			t.Fatal(err)
		}
		if err := cache.GetUserCache().InvalidateUser(context.Background(), owner.ID, owner.Name); err != nil {
			t.Fatal(err)
		}
	}
	ownerQueries := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:todo_owner_queries", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			ownerQueries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:todo_owner_queries") })
	request := func(ctx context.Context) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/devices?keyword="+prefix+"&limit=100", nil).WithContext(ctx)
		c.Set("user", &gormdb.User{ID: ownerIDs[0], Roles: "admin"})
		GetDevices(c)
		return recorder
	}
	response := request(context.Background())
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data struct {
			Items []DeviceInfo `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Items) != 6 || ownerQueries != 1 {
		t.Fatalf("cold list returned %d devices using %d owner queries; want 6 and 1", len(payload.Data.Items), ownerQueries)
	}
	for _, device := range payload.Data.Items {
		if device.OwnerID <= 0 || device.OwnerCallSign == "" || device.OwnerName == "" || device.GroupID != 0 {
			t.Fatalf("incomplete ungrouped device response: %+v", device)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response = request(ctx)
	if response.Code != http.StatusRequestTimeout || !strings.Contains(response.Body.String(), "request_canceled") {
		t.Fatalf("canceled query status=%d body=%s", response.Code, response.Body.String())
	}
	userRepo := gormdb.NewUserRepository().WithContext(ctx)
	deviceRepo := gormdb.NewDeviceRepository().WithContext(ctx)
	for name, operation := range map[string]func() error{
		"user by ID": func() error { _, err := userRepo.GetUserByID(ownerIDs[0]); return err },
		"user by name": func() error {
			_, err := userRepo.GetUserByName(fmt.Sprintf("todo-owner-%s-0", suffix))
			return err
		},
		"user update": func() error { return userRepo.UpdateUserDevicePassword(ownerIDs[0], "canceled-update") },
		"device transaction": func() error {
			return deviceRepo.UpdateDeviceEntry(payload.Data.Items[0].ID, "center", "udp", 1, true, time.Now())
		},
	} {
		if err := operation(); !errors.Is(err, context.Canceled) {
			t.Errorf("%s ignored canceled repository context: %v", name, err)
		}
	}
}
