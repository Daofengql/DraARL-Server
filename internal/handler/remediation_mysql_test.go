package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	authstore "draarl/internal/auth"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/middleware"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	appjwt "draarl/pkg/jwt"
	ws "draarl/pkg/websocket"
	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

// This regression suite uses a disposable, explicitly named MySQL database.
// It exercises HTTP authentication and the same claims in WS/UDP authentication.
func TestRemediationAuthenticationAndLogbookMySQL(t *testing.T) {
	if os.Getenv("DRAARL_REMEDIATION_E2E") != "true" {
		t.Skip("set DRAARL_REMEDIATION_E2E=true and DRAARL_TEST_MYSQL_DSN")
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
	db := gormdb.Get()
	if err := gormdb.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := gormdb.AutoMigrate(); err != nil {
		t.Fatal("repeat migration:", err)
	}
	if err := gormdb.ValidateDefaultPublicGroup(); err != nil {
		t.Fatal(err)
	}
	if err := appjwt.SetSecret(strings.Repeat("remediation", 4)); err != nil {
		t.Fatal(err)
	}
	authstore.CloseRefreshTokenStore()
	t.Cleanup(authstore.CloseRefreshTokenStore)
	if path := os.Getenv("DRAARL_TEST_CONFIG"); path != "" {
		previous := config.Config
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { config.Config = previous })
		cfg.Redis.Prefix = "draarl-remediation-regression-20261003"
		if err := authstore.InitRefreshTokenStore(cfg); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.InitManager(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.GetManager().ClearAll(context.Background()) })
	suffix := fmt.Sprint(time.Now().UnixNano())
	user := &gormdb.User{Name: "original-" + suffix, Email: "original-" + suffix + "@example.invalid", CallSign: "O" + suffix[len(suffix)-8:], Status: 1, ApprovalStatus: 1, Roles: "user"}
	admin := &gormdb.User{Name: "admin-" + suffix, Email: "admin-" + suffix + "@example.invalid", CallSign: "A" + suffix[len(suffix)-8:], Status: 1, ApprovalStatus: 1, Roles: "admin"}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("RemediationInitial2026!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	admin.Password, user.Password = string(hashedPassword), string(hashedPassword)
	// Reserve ID 1 for the administrator on a fresh database, matching runtime.
	for _, u := range []*gormdb.User{admin, user} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	oldName := user.Name
	t.Cleanup(func() { _ = db.Delete(&gormdb.User{}, []int{user.ID, admin.ID}).Error })
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	_ = engine.SetTrustedProxies(nil)
	engine.POST("/api/auth/refresh", RefreshToken)
	protected := engine.Group("/api", middleware.AuthMiddleware())
	protected.GET("/me", func(c *gin.Context) { c.JSON(200, gin.H{"id": c.GetInt("user_id"), "name": c.GetString("username")}) })
	protected.PUT("/users/:id/status", middleware.RequireAdmin(), UpdateUserStatus)
	protected.PUT("/users/:id", middleware.RequireAdmin(), UpdateUser)
	protected.PUT("/users/:id/password", middleware.RequireAdmin(), UpdateUserPassword)
	protected.PUT("/logbooks/:id", UpdateLogbook)
	protected.PUT("/admin/logbooks/:id", middleware.RequireAdmin(), AdminUpdateLogbook)
	protected.PUT("/groups/:id", UpdateGroup)
	protected.DELETE("/groups/:id", DeleteGroup)
	request := func(method, path, token string, payload any) *httptest.ResponseRecorder {
		var body []byte
		if payload != nil {
			body, err = json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		engine.ServeHTTP(out, req)
		return out
	}
	tokenFor := func(u *gormdb.User) string {
		token, err := appjwt.GenerateTokenForUser(u.ID, u.Name, u.GetRoles(), u.SessionVersion)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	adminToken := tokenFor(admin)
	for _, mutation := range []struct {
		method string
		body   any
	}{
		{"PUT", map[string]int{"status": 0}},
		{"PUT", map[string]int{"type": 2}},
		{"DELETE", nil},
	} {
		if got := request(mutation.method, "/api/groups/999", adminToken, mutation.body); got.Code != 400 {
			t.Fatalf("default group contract violated: %s", got.Body)
		}
	}
	out := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(out)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/login", nil)
	issued, err := issueAuthTokens(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := authstore.GetRefreshTokenStore().GetByTokenHash(hashRefreshToken(issued.RefreshToken))
	if err != nil || stored == nil || stored.SessionVersion != user.SessionVersion {
		t.Fatal("refresh token did not retain session version")
	}
	if got := request("GET", "/api/me", issued.AccessToken, nil); got.Code != 200 {
		t.Fatal("initial access", got.Code)
	}
	for _, status := range []int{0, 1} {
		got := request("PUT", fmt.Sprintf("/api/users/%d/status", user.ID), adminToken, map[string]int{"status": status})
		if got.Code != 200 {
			t.Fatalf("status mutation failed: %s", got.Body)
		}
	}
	if got := request("GET", "/api/me", issued.AccessToken, nil); got.Code != 401 {
		t.Fatalf("old access survived disable/enable: %d", got.Code)
	}
	if got := request("POST", "/api/auth/refresh", "", map[string]string{"refresh_token": issued.RefreshToken}); got.Code != 401 {
		t.Fatalf("old refresh survived disable/enable: %d", got.Code)
	}
	repo := gormdb.NewUserRepository()
	user, err = repo.GetUserByID(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeRename := tokenFor(user)
	if _, err := cache.GetUserCache().GetUserByID(context.Background(), user.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate a second process changing the account without invalidating this
	// process's local profile cache. HTTP auth must read authoritative state.
	if err := repo.UpdateUserStatus(user.ID, 0); err != nil {
		t.Fatal(err)
	}
	if got := request("GET", "/api/me", beforeRename, nil); got.Code != 403 {
		t.Fatalf("stale local cache failed to reject disabled user: status=%d body=%s", got.Code, got.Body)
	}
	if err := repo.UpdateUserStatus(user.ID, 1); err != nil {
		t.Fatal(err)
	}
	if got := request("GET", "/api/me", beforeRename, nil); got.Code != 401 {
		t.Fatalf("stale local cache accepted revoked session: status=%d body=%s", got.Code, got.Body)
	}
	user, err = repo.GetUserByID(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeRename = tokenFor(user)
	if got := request("GET", "/api/me", beforeRename, nil); got.Code != 200 {
		t.Fatal("new session failed after re-enable")
	}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/login", nil)
	fresh, err := issueAuthTokens(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if got := request("POST", "/api/auth/refresh", "", map[string]string{"refresh_token": fresh.RefreshToken}); got.Code != 200 {
		t.Fatalf("fresh refresh failed after re-enable: %s", got.Body)
	}
	if got := request("PUT", fmt.Sprintf("/api/users/%d", user.ID), adminToken, map[string]string{"username": "renamed-" + suffix}); got.Code != 200 {
		t.Fatalf("rename failed: %s", got.Body)
	}
	user, err = repo.GetUserByID(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	replacement := &gormdb.User{Name: oldName, Email: "replacement-" + suffix + "@example.invalid", CallSign: "R" + suffix[len(suffix)-8:], Status: 1, ApprovalStatus: 1, Roles: "user"}
	if err := db.Create(replacement).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Delete(replacement).Error })
	if got := request("GET", "/api/me", beforeRename, nil); got.Code != 401 {
		t.Fatalf("old token survived rename: %d", got.Code)
	}
	if ws.AuthenticateJWT(beforeRename).Success || udphub.AuthenticateJWT(beforeRename).Success {
		t.Fatal("WS or UDP accepted revoked identity")
	}
	// Even a signed display name matching the replacement must resolve only by ID.
	nameStale, err := appjwt.GenerateTokenForUser(user.ID, oldName, user.GetRoles(), user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	got := request("GET", "/api/me", nameStale, nil)
	var identity struct {
		ID   int
		Name string
	}
	_ = json.Unmarshal(got.Body.Bytes(), &identity)
	if got.Code != 200 || identity.ID != user.ID || identity.Name != user.Name {
		t.Fatalf("identity drift: status=%d body=%s", got.Code, got.Body)
	}
	if result := ws.AuthenticateJWT(nameStale); !result.Success || result.UserID != user.ID {
		t.Fatal("WS identity not bound to ID")
	}
	if result := udphub.AuthenticateJWT(nameStale); !result.Success || result.User.ID != user.ID {
		t.Fatal("UDP identity not bound to ID")
	}
	preference, err := gormdb.NewGhostClientPreferenceRepository().GetOrCreate(user.ID, 105, "12345678-1234-4234-8234-123456789abc", 999)
	if err != nil || preference.TxGroupID != 999 {
		t.Fatalf("fresh preference failed: %v", err)
	}
	power := 50
	logbook := &gormdb.Logbook{UserID: user.ID, MyCallSign: user.CallSign, CallSign: "BG7TEST", TimeUTC: time.Now(), Mode: "FM", TxFrequency: 438.5, RxFrequency: 438.5, Notes: "remove", MyQTH: "keep", TheirRST: "59", TheirPower: &power, MyPower: &power}
	if err := db.Create(logbook).Error; err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api/logbooks/", "/api/admin/logbooks/"} {
		if err := db.Model(logbook).Updates(map[string]any{"notes": "remove", "their_rst": "59", "their_power": 50}).Error; err != nil {
			t.Fatal(err)
		}
		token := nameStale
		if strings.Contains(route, "/admin/") {
			token = adminToken
		}
		got := request("PUT", fmt.Sprintf("%s%d", route, logbook.ID), token, map[string]any{"notes": "", "their_rst": "", "their_power": nil, "cq_zone": 0})
		if got.Code != 200 {
			t.Fatalf("logbook update failed: %s", got.Body)
		}
		var saved gormdb.Logbook
		if err := db.First(&saved, logbook.ID).Error; err != nil {
			t.Fatal(err)
		}
		if saved.Notes != "" || saved.TheirRST != "" || saved.TheirPower != nil || saved.MyPower == nil || *saved.MyPower != 50 || saved.MyQTH != "keep" {
			t.Fatalf("clear/omission semantics failed: %#v", saved)
		}
		if got := request("PUT", fmt.Sprintf("%s%d", route, logbook.ID), token, map[string]any{"time_utc": "invalid"}); got.Code != 400 {
			t.Fatal("invalid time accepted")
		}
	}
	if got := request("PUT", fmt.Sprintf("/api/users/%d/password", admin.ID), adminToken, map[string]string{"new_password": "RemediationChanged2026!"}); got.Code != 400 {
		t.Fatal("administrator changed own password without current password")
	}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/login", nil)
	beforePassword, err := issueAuthTokens(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if got := request("PUT", fmt.Sprintf("/api/users/%d/password", user.ID), adminToken, map[string]string{"new_password": "RemediationChanged2026!"}); got.Code != 200 {
		t.Fatalf("password mutation failed: %s", got.Body)
	}
	if got := request("GET", "/api/me", nameStale, nil); got.Code != 401 {
		t.Fatal("old access survived password change")
	}
	if got := request("POST", "/api/auth/refresh", "", map[string]string{"refresh_token": beforePassword.RefreshToken}); got.Code != 401 {
		t.Fatal("old refresh survived password change")
	}
}
