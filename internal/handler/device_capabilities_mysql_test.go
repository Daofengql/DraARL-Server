package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"draarl/internal/gormdb"
	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
)

func TestDeviceConfigCapabilitiesAndStaleSaveMySQL(t *testing.T) {
	if os.Getenv("DRAARL_DEVICE_CONFIG_E2E") != "true" {
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
	owner := &gormdb.User{Name: "caps-api", Email: "caps-api@example.invalid", Status: 1}
	if err := db.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	device := &gormdb.Device{Name: "caps-api-1", OwnerID: owner.ID, SSID: 1, DevModel: 2, Status: 1}
	if err := db.Create(device).Error; err != nil {
		t.Fatal(err)
	}
	repo := gormdb.NewDeviceConfigRepository()
	t.Cleanup(func() {
		_ = repo.DeleteDeviceConfigs(device.ID)
		_ = db.Delete(device).Error
		_ = db.Delete(owner).Error
	})
	if err := repo.SetDeviceConfigs(device.ID, map[string]string{"rx_freq": "438500000", "adc_volume": "80", "rf_guard_enabled": "1"}); err != nil {
		t.Fatal(err)
	}
	request := func(method, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(method, "/api/devices/"+strconv.Itoa(device.ID)+"/config", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(device.ID)}}
		c.Set("user", owner)
		handler(c)
		return response
	}
	response := request(http.MethodGet, "", GetDeviceConfig)
	var payload struct {
		Model int               `json:"dev_model"`
		Keys  []string          `json:"supported_keys"`
		Data  map[string]string `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || payload.Model != 2 || len(payload.Keys) != 5 || len(payload.Data) != 1 || payload.Data["adc_volume"] != "80" {
		t.Fatalf("configuration metadata does not match current hardware: %d %s", response.Code, response.Body.String())
	}
	for _, handler := range []gin.HandlerFunc{UpdateDeviceConfig, AdminUpdateDeviceConfig} {
		response := request(http.MethodPut, `{"rx_freq":"439500000","adc_volume":"90"}`, handler)
		if response.Code != http.StatusConflict {
			t.Fatalf("stale save was not rejected: %d %s", response.Code, response.Body.String())
		}
	}
	stored, err := repo.GetDeviceConfigs(device.ID)
	if err != nil || stored["rx_freq"] != "438500000" || stored["adc_volume"] != "80" {
		t.Fatalf("rejected save changed parameters: %v %v", stored, err)
	}
}
