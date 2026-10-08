package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/middleware"
	appjwt "draarl/pkg/jwt"

	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

const groupPasswordE2EEnv = "DRAARL_GROUP_PASSWORD_E2E"

// TestGroupPasswordHTTPFlowMySQL exercises the group endpoints through JWT
// authentication. It deliberately signs test access tokens directly instead
// of using CAPTCHA/login flows, which keeps this security regression test fast
// and isolated from unrelated authentication behavior.
func TestGroupPasswordHTTPFlowMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(groupPasswordE2EEnv)), "true") {
		t.Skip("set " + groupPasswordE2EEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the group-password HTTP E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 10, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatalf("initialize MySQL: %v", err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	db := gormdb.Get()
	if err := db.AutoMigrate(&gormdb.User{}, &gormdb.Group{}, &gormdb.GroupMember{}, &gormdb.Device{}); err != nil {
		t.Fatalf("migrate group password E2E tables: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	owner := &gormdb.User{Name: "group-password-owner-" + suffix, Email: "owner-" + suffix + "@example.invalid", CallSign: "GP" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	member := &gormdb.User{Name: "group-password-member-" + suffix, Email: "member-" + suffix + "@example.invalid", CallSign: "GM" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	switcher := &gormdb.User{Name: "group-password-switcher-" + suffix, Email: "switcher-" + suffix + "@example.invalid", CallSign: "GS" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	rateUser := &gormdb.User{Name: "group-password-rate-" + suffix, Email: "rate-" + suffix + "@example.invalid", CallSign: "GR" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	users := []*gormdb.User{owner, member, switcher, rateUser}
	for _, user := range users {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		userIDs := []int{owner.ID, member.ID, switcher.ID, rateUser.ID}
		_ = db.Where("owner_id IN ?", userIDs).Delete(&gormdb.Device{}).Error
		_ = db.Where("user_id IN ?", userIDs).Delete(&gormdb.GroupMember{}).Error
		_ = db.Where("ower_id = ?", owner.ID).Delete(&gormdb.Group{}).Error
		_ = db.Where("id IN ?", userIDs).Delete(&gormdb.User{}).Error
	})

	middleware.InitDeviceRateLimiter()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	api.Use(middleware.AuthMiddleware(), middleware.RequireApproved())
	api.POST("/groups", CreateGroup)
	api.POST("/groups/:id/join", middleware.GroupJoinPasswordRateLimit(), JoinGroup)
	api.PUT("/devices/:id/group", ChangeDeviceGroup)

	issueToken := func(user *gormdb.User) string {
		t.Helper()
		token, err := appjwt.GenerateTokenForUser(user.ID, user.Name, []string{"user"}, user.SessionVersion)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	request := func(method, target, token string, payload any, wantStatus int, remoteAddr string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.RemoteAddr = remoteAddr
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != wantStatus {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, target, recorder.Code, wantStatus, recorder.Body.String())
		}
		return recorder
	}

	create := request(http.MethodPost, "/api/groups", issueToken(owner), CreateGroupRequest{
		Name: "bcrypt-group-" + suffix, Type: groupTypePrivate, Password: "private-password",
	}, http.StatusCreated, "192.0.2.10:10000")
	var created struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil || created.Data.ID <= 0 {
		t.Fatalf("decode create response: id=%d err=%v body=%s", created.Data.ID, err, create.Body.String())
	}
	var stored gormdb.Group
	if err := db.First(&stored, created.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Password == "private-password" || bcrypt.CompareHashAndPassword([]byte(stored.Password), []byte("private-password")) != nil {
		t.Fatalf("private group password was not stored as bcrypt: %q", stored.Password)
	}

	joinPath := fmt.Sprintf("/api/groups/%d/join", created.Data.ID)
	request(http.MethodPost, joinPath, issueToken(member), JoinGroupRequest{Password: "wrong-password"}, http.StatusUnauthorized, "192.0.2.11:10000")
	request(http.MethodPost, joinPath, issueToken(member), JoinGroupRequest{Password: "private-password"}, http.StatusOK, "192.0.2.11:10001")
	var membership gormdb.GroupMember
	if err := db.Where("group_id = ? AND user_id = ?", created.Data.ID, member.ID).First(&membership).Error; err != nil || !membership.IsVerified {
		t.Fatalf("verified membership missing: %#v err=%v", membership, err)
	}

	device := &gormdb.Device{Name: "group-password-device-" + suffix, OwnerID: switcher.ID, SSID: 1, Status: 1}
	if err := db.Create(device).Error; err != nil {
		t.Fatal(err)
	}
	changePath := fmt.Sprintf("/api/devices/%d/group", device.ID)
	request(http.MethodPut, changePath, issueToken(switcher), ChangeDeviceGroupRequest{DeviceID: device.ID, GroupID: &created.Data.ID, Password: "wrong-password"}, http.StatusUnauthorized, "192.0.2.20:10000")
	request(http.MethodPut, changePath, issueToken(switcher), ChangeDeviceGroupRequest{DeviceID: device.ID, GroupID: &created.Data.ID, Password: "private-password"}, http.StatusOK, "192.0.2.20:10001")
	if err := db.First(device, device.ID).Error; err != nil || device.GroupID != created.Data.ID {
		t.Fatalf("device did not enter bcrypt private group: group=%d err=%v", device.GroupID, err)
	}
	membership = gormdb.GroupMember{}
	if err := db.Where("group_id = ? AND user_id = ?", created.Data.ID, switcher.ID).First(&membership).Error; err != nil || !membership.IsVerified {
		t.Fatalf("device group change did not create verified membership: %#v err=%v", membership, err)
	}
	publicGroup := &gormdb.Group{Name: "public-group-" + suffix, Type: groupTypePublic, OwerID: owner.ID, Status: 1}
	if err := db.Create(publicGroup).Error; err != nil {
		t.Fatal(err)
	}
	request(http.MethodPut, changePath, issueToken(switcher), ChangeDeviceGroupRequest{DeviceID: device.ID, GroupID: &publicGroup.ID}, http.StatusOK, "192.0.2.20:10002")
	request(http.MethodPut, changePath, issueToken(switcher), ChangeDeviceGroupRequest{DeviceID: device.ID, GroupID: &created.Data.ID}, http.StatusOK, "192.0.2.20:10002")

	legacy := &gormdb.Group{Name: "legacy-group-" + suffix, Type: groupTypePrivate, Password: "legacy-password", OwerID: owner.ID, Status: 1}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	request(http.MethodPost, fmt.Sprintf("/api/groups/%d/join", legacy.ID), issueToken(member), JoinGroupRequest{Password: "legacy-password"}, http.StatusOK, "192.0.2.12:10000")
	if err := db.First(&legacy, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if legacy.Password == "legacy-password" || bcrypt.CompareHashAndPassword([]byte(legacy.Password), []byte("legacy-password")) != nil {
		t.Fatalf("legacy password was not upgraded after successful verification: %q", legacy.Password)
	}

	legacyDeviceGroup := &gormdb.Group{Name: "legacy-device-group-" + suffix, Type: groupTypePrivate, Password: "legacy-device-password", OwerID: owner.ID, Status: 1}
	if err := db.Create(legacyDeviceGroup).Error; err != nil {
		t.Fatal(err)
	}
	request(http.MethodPut, changePath, issueToken(switcher), ChangeDeviceGroupRequest{DeviceID: device.ID, GroupID: &legacyDeviceGroup.ID, Password: "legacy-device-password"}, http.StatusOK, "192.0.2.21:10000")
	if err := db.First(legacyDeviceGroup, legacyDeviceGroup.ID).Error; err != nil {
		t.Fatal(err)
	}
	if legacyDeviceGroup.Password == "legacy-device-password" || bcrypt.CompareHashAndPassword([]byte(legacyDeviceGroup.Password), []byte("legacy-device-password")) != nil {
		t.Fatalf("device group change did not upgrade legacy password: %q", legacyDeviceGroup.Password)
	}

	rateHash, err := bcrypt.GenerateFromPassword([]byte("rate-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	rateGroup := &gormdb.Group{Name: "rate-group-" + suffix, Type: groupTypePrivate, Password: string(rateHash), OwerID: owner.ID, Status: 1}
	if err := db.Create(rateGroup).Error; err != nil {
		t.Fatal(err)
	}
	rateDevice := &gormdb.Device{Name: "rate-device-" + suffix, OwnerID: rateUser.ID, SSID: 1, Status: 1}
	if err := db.Create(rateDevice).Error; err != nil {
		t.Fatal(err)
	}
	ratePath := fmt.Sprintf("/api/devices/%d/group", rateDevice.ID)
	for i := 0; i < 10; i++ {
		request(http.MethodPut, ratePath, issueToken(rateUser), ChangeDeviceGroupRequest{DeviceID: rateDevice.ID, GroupID: &rateGroup.ID, Password: "wrong-password"}, http.StatusUnauthorized, "192.0.2.22:10000")
	}
	request(http.MethodPut, ratePath, issueToken(rateUser), ChangeDeviceGroupRequest{DeviceID: rateDevice.ID, GroupID: &rateGroup.ID, Password: "wrong-password"}, http.StatusTooManyRequests, "192.0.2.22:10000")

	for i := 0; i < 7; i++ {
		request(http.MethodPost, joinPath, issueToken(member), JoinGroupRequest{Password: "wrong-password"}, http.StatusUnauthorized, "192.0.2.13:10000")
	}
	request(http.MethodPost, joinPath, issueToken(member), JoinGroupRequest{Password: "wrong-password"}, http.StatusTooManyRequests, "192.0.2.13:10000")
}
