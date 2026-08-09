package handler

import (
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

	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
)

const commRecordsE2EEnabledEnv = "DRAARL_COMM_RECORDS_E2E"

func TestCommRecordsAuthorizationHTTPE2E(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(commRecordsE2EEnabledEnv)), "true") {
		t.Skip("set " + commRecordsE2EEnabledEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the HTTP E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("DRAARL_TEST_MYSQL_DSN is required")
	}
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
	if err := db.AutoMigrate(&gormdb.User{}, &gormdb.Group{}, &gormdb.Device{}, &gormdb.GroupMember{}, &gormdb.GroupLink{}, &gormdb.CommRecord{}); err != nil {
		t.Fatalf("migrate communication record E2E tables: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	newUser := func(prefix, role string) *gormdb.User {
		return &gormdb.User{
			Name: prefix + "-" + suffix, Email: prefix + "-" + suffix + "@example.invalid",
			CallSign: strings.ToUpper(prefix) + suffix[len(suffix)-6:], Roles: role,
			Status: 1, ApprovalStatus: 1,
		}
	}
	owner := newUser("record-owner", "user")
	other := newUser("record-other", "user")
	admin := newUser("record-admin", "admin")
	for _, user := range []*gormdb.User{owner, other, admin} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}

	publicGroup := &gormdb.Group{Name: "record-public-" + suffix, Type: groupTypePublic, OwerID: owner.ID, Status: 1}
	privateGroup := &gormdb.Group{Name: "record-private-" + suffix, Type: groupTypePrivate, OwerID: other.ID, Status: 1}
	for _, group := range []*gormdb.Group{publicGroup, privateGroup} {
		if err := db.Create(group).Error; err != nil {
			t.Fatal(err)
		}
	}
	device := &gormdb.Device{Name: "record-device-" + suffix, SSID: 1, OwnerID: owner.ID, GroupID: publicGroup.ID, Status: 1}
	if err := db.Create(device).Error; err != nil {
		t.Fatal(err)
	}

	publicGroupID := uint(publicGroup.ID)
	privateGroupID := uint(privateGroup.ID)
	ownerID := uint(owner.ID)
	otherID := uint(other.ID)
	now := time.Now().UTC().Truncate(time.Millisecond)
	records := []*gormdb.CommRecord{
		{DeviceID: 0, DeviceSSID: 101, GroupID: &publicGroupID, UserID: &ownerID, StartTime: now, EndTime: now, AudioPath: "text:owner", Status: 2},
		{DeviceID: 0, DeviceSSID: 101, GroupID: &publicGroupID, UserID: &otherID, StartTime: now.Add(time.Millisecond), EndTime: now.Add(time.Millisecond), AudioPath: "text:other", Status: 2},
		{DeviceID: uint(device.ID), DeviceSSID: device.SSID, GroupID: &publicGroupID, UserID: nil, StartTime: now.Add(2 * time.Millisecond), EndTime: now.Add(2 * time.Millisecond), AudioPath: "text:legacy", Status: 2},
		{DeviceID: 0, DeviceSSID: 101, GroupID: &privateGroupID, UserID: &otherID, StartTime: now.Add(3 * time.Millisecond), EndTime: now.Add(3 * time.Millisecond), AudioPath: "text:private", Status: 2},
		{DeviceID: 0, DeviceSSID: 255, GroupID: &publicGroupID, UserID: nil, StartTime: now.Add(4 * time.Millisecond), EndTime: now.Add(124 * time.Millisecond), DurationMs: 120, AudioPath: "", AudioSize: 0, Status: 2, MessageType: gormdb.CommMessageTypeVoice, SenderUsername: "system-broadcast", SenderCallSign: "AUTO", SenderNickname: "自动播报", IsAutoBroadcast: true},
	}
	for _, record := range records {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = db.Where("group_id IN ?", []int{publicGroup.ID, privateGroup.ID}).Delete(&gormdb.CommRecord{}).Error
		_ = db.Where("group_id IN ?", []int{publicGroup.ID, privateGroup.ID}).Delete(&gormdb.GroupMember{}).Error
		_ = db.Delete(&gormdb.Device{}, device.ID).Error
		_ = db.Delete(&gormdb.Group{}, []int{publicGroup.ID, privateGroup.ID}).Error
		_ = db.Delete(&gormdb.User{}, []int{owner.ID, other.ID, admin.ID}).Error
	})

	assertStatus := func(name string, actor *gormdb.User, pattern, path string, handler gin.HandlerFunc, want int) []byte {
		t.Helper()
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.GET(pattern, func(c *gin.Context) {
			c.Set("user", actor)
			handler(c)
		})
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("%s status=%d want=%d body=%s", name, response.Code, want, response.Body.String())
		}
		return response.Body.Bytes()
	}

	detailPattern := "/api/comm-records/:id"
	assertStatus("owner detail", owner, detailPattern, fmt.Sprintf("/api/comm-records/%d", records[0].ID), GetCommRecord, http.StatusOK)
	assertStatus("other sender detail", owner, detailPattern, fmt.Sprintf("/api/comm-records/%d", records[1].ID), GetCommRecord, http.StatusForbidden)
	assertStatus("legacy physical owner detail", owner, detailPattern, fmt.Sprintf("/api/comm-records/%d", records[2].ID), GetCommRecord, http.StatusOK)
	assertStatus("admin detail", admin, detailPattern, fmt.Sprintf("/api/comm-records/%d", records[1].ID), GetCommRecord, http.StatusOK)
	autoBody := assertStatus("automatic broadcast detail", admin, detailPattern, fmt.Sprintf("/api/comm-records/%d", records[4].ID), GetCommRecord, http.StatusOK)
	var autoEnvelope struct {
		Data CommRecordResponse `json:"data"`
	}
	if err := json.Unmarshal(autoBody, &autoEnvelope); err != nil || !autoEnvelope.Data.IsAutoBroadcast || autoEnvelope.Data.AudioPath != "" || autoEnvelope.Data.AudioSize != 0 {
		t.Fatalf("automatic broadcast flag or empty-audio semantics missing: err=%v body=%s", err, autoBody)
	}
	assertStatus("missing detail", owner, detailPattern, "/api/comm-records/4294967295", GetCommRecord, http.StatusNotFound)

	listPattern := "/api/comm-records"
	body := assertStatus("own communication records", owner, listPattern, "/api/comm-records", GetCommRecords, http.StatusOK)
	var listEnvelope struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &listEnvelope); err != nil {
		t.Fatalf("decode own records response: %v", err)
	}
	if listEnvelope.Data.Total != 2 {
		t.Fatalf("own record total=%d want=2 body=%s", listEnvelope.Data.Total, body)
	}

	body = assertStatus("own public group records", owner, listPattern, fmt.Sprintf("/api/comm-records?group_id=%d", publicGroup.ID), GetCommRecords, http.StatusOK)
	if err := json.Unmarshal(body, &listEnvelope); err != nil {
		t.Fatalf("decode public group response: %v", err)
	}
	if listEnvelope.Data.Total != 2 {
		t.Fatalf("own public group total=%d want=2 body=%s", listEnvelope.Data.Total, body)
	}
	body = assertStatus("own private group records", owner, listPattern, fmt.Sprintf("/api/comm-records?group_id=%d", privateGroup.ID), GetCommRecords, http.StatusOK)
	if err := json.Unmarshal(body, &listEnvelope); err != nil {
		t.Fatalf("decode private group response: %v", err)
	}
	if listEnvelope.Data.Total != 0 {
		t.Fatalf("own private group total=%d want=0 body=%s", listEnvelope.Data.Total, body)
	}
	body = assertStatus("admin public group audit", admin, listPattern, fmt.Sprintf("/api/comm-records?admin_mode=true&group_id=%d", publicGroup.ID), GetCommRecords, http.StatusOK)
	if err := json.Unmarshal(body, &listEnvelope); err != nil {
		t.Fatalf("decode admin public group response: %v", err)
	}
	if listEnvelope.Data.Total != 4 {
		t.Fatalf("admin public group total=%d want=4 body=%s", listEnvelope.Data.Total, body)
	}
	assertStatus("invalid group id", owner, listPattern, "/api/comm-records?group_id=invalid", GetCommRecords, http.StatusBadRequest)
	assertStatus("missing group", owner, listPattern, "/api/comm-records?group_id=2147483647", GetCommRecords, http.StatusNotFound)
}
