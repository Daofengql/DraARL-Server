package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/middleware"
	"draarl/internal/udphub"
	"draarl/pkg/crypto"
	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
)

func TestDynamicBindingRetriesMySQL(t *testing.T) {
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
	if err := crypto.InitAES("0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	password, err := crypto.Encrypt("test-device-password")
	if err != nil {
		t.Fatal(err)
	}
	db := gormdb.Get()
	owner := &gormdb.User{Name: "binding-owner", Email: "binding-owner@example.invalid", CallSign: "TEST", Status: 1, ApprovalStatus: 1, DevicePassword: password}
	other := &gormdb.User{Name: "binding-other", Email: "binding-other@example.invalid", CallSign: "OTHER", Status: 1, ApprovalStatus: 1, DevicePassword: password}
	for _, user := range []*gormdb.User{owner, other} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	udphub.InitPendingDeviceManager()
	manager := udphub.GetPendingDeviceManager()
	t.Cleanup(manager.Stop)
	middleware.InitDeviceRateLimiter()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/request-code", middleware.RequestCodeRateLimit(), RequestCode)
	router.POST("/confirm-bind", middleware.ConfirmBindRateLimit(), ConfirmBind)
	authenticated := router.Group("", func(c *gin.Context) { c.Set("username", c.GetHeader("Test-User")); c.Next() })
	authenticated.POST("/bind", middleware.BindRateLimit(), BindDevice)
	authenticated.POST("/submit-config", middleware.SubmitConfigRateLimit(), SubmitDeviceConfig)
	request := func(path, user, body string, want int) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Test-User", user)
		req.RemoteAddr = "192.0.2.1:12345"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s status = %d, want %d", path, response.Code, want)
		}
		var payload struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return response, payload.Data
	}
	const mac = "AA:BB:CC:DD:EE:FF"
	const deviceBody = `{"mac":"` + mac + `"}`
	_, initial := request("/request-code", "", deviceBody, 200)
	_, repeated := request("/request-code", "", deviceBody, 200)
	if initial["dynamic_code"] != repeated["dynamic_code"] {
		t.Fatal("retry replaced a valid code")
	}
	bindBody, _ := json.Marshal(map[string]any{"dynamic_code": initial["dynamic_code"]})
	for i := 0; i < 2; i++ {
		request("/bind", owner.Name, string(bindBody), 200)
	}
	request("/bind", other.Name, string(bindBody), 400)
	_, bound := request("/request-code", "", deviceBody, 200)
	if bound["bound"] != true {
		t.Fatal("accepted binding was not resumed")
	}
	const configBody = `{"device_mac":"` + mac + `","ssid":1}`
	request("/submit-config", other.Name, configBody, 403)
	for i := 0; i < 2; i++ {
		request("/submit-config", owner.Name, configBody, 200)
	}
	for i := 0; i < 3; i++ {
		_, data := request("/confirm-bind", "", deviceBody, 200)
		if data["status"] != "ready" || data["username"] != owner.Name {
			t.Fatal("retry lost queued configuration")
		}
	}
	rejected, data := request("/confirm-bind", "", deviceBody, 429)
	seconds, err := strconv.Atoi(rejected.Header().Get("Retry-After"))
	if err != nil || seconds < 1 || seconds > 5 || data["retry_after"] != float64(seconds) {
		t.Fatal("invalid retry deadline")
	}
	time.Sleep(time.Duration(seconds)*time.Second + 50*time.Millisecond)
	_, recovered := request("/confirm-bind", "", deviceBody, 200)
	if recovered["status"] != "ready" {
		t.Fatal("rate limited client could not resume")
	}
}
