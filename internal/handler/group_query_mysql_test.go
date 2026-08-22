package handler

import (
	"bytes"
	"encoding/json"
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

func TestGroupQueryPaginationAndDeviceStatsMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_GROUP_QUERY_E2E")), "true") {
		t.Skip("set DRAARL_GROUP_QUERY_E2E=true and DRAARL_TEST_MYSQL_DSN to run the group query E2E")
	}
	parsed, err := drivermysql.ParseDSN(strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 10, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	db := gormdb.Get()
	if err := db.AutoMigrate(&gormdb.User{}, &gormdb.Group{}, &gormdb.Device{}, &gormdb.GroupMember{}); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	viewer := &gormdb.User{Name: "group-query-viewer-" + suffix, Email: "group-query-viewer-" + suffix + "@example.invalid", CallSign: "GQ" + suffix[len(suffix)-8:], Status: 1, ApprovalStatus: 1}
	owner := &gormdb.User{Name: "group-query-owner-" + suffix, Email: "group-query-owner-" + suffix + "@example.invalid", CallSign: "GO" + suffix[len(suffix)-8:], Status: 1, ApprovalStatus: 1}
	if err := db.Create([]*gormdb.User{viewer, owner}).Error; err != nil {
		t.Fatal(err)
	}
	active := &gormdb.Group{Name: "group-query-active-" + suffix, Type: groupTypePublic, OwerID: owner.ID, Status: 1}
	disabled := &gormdb.Group{Name: "group-query-disabled-" + suffix, Type: groupTypePublic, OwerID: owner.ID, Status: 0}
	if err := db.Create([]*gormdb.Group{active, disabled}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(disabled).Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	deviceRows := []*gormdb.Device{
		{Name: "group-query-online-" + suffix, OwnerID: owner.ID, SSID: 1, GroupID: active.ID, Status: 1, ISOnline: true},
		{Name: "group-query-offline-" + suffix, OwnerID: owner.ID, SSID: 2, GroupID: active.ID, Status: 1, ISOnline: false},
	}
	if err := db.Create(deviceRows).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Delete(&gormdb.Device{}, []int{deviceRows[0].ID, deviceRows[1].ID}).Error
		_ = db.Delete(&gormdb.Group{}, []int{active.ID, disabled.ID}).Error
		_ = db.Delete(&gormdb.User{}, []int{viewer.ID, owner.ID}).Error
	})

	response := performGroupQueryRequest(t, GetGroups, http.MethodGet, "/groups?page=1&page_size=20", nil, viewer)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var list struct {
		Data struct {
			Items []struct {
				ID          int `json:"id"`
				OnlineCount int `json:"online_count"`
				TotalCount  int `json:"total_count"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	foundActive, foundDisabled := false, false
	for _, item := range list.Data.Items {
		switch item.ID {
		case active.ID:
			foundActive = true
			if item.OnlineCount != 1 || item.TotalCount != 2 {
				t.Fatalf("device stats=(%d,%d), want=(1,2)", item.OnlineCount, item.TotalCount)
			}
		case disabled.ID:
			foundDisabled = true
		}
	}
	if !foundActive || foundDisabled {
		t.Fatalf("active=%v disabled=%v body=%s", foundActive, foundDisabled, response.Body.String())
	}

	searchBody := []byte(`{"keyword":"group-query-active-"}`)
	search := performGroupQueryRequest(t, SearchGroups, http.MethodPost, "/groups/search", searchBody, viewer)
	if search.Code != http.StatusOK || !bytes.Contains(search.Body.Bytes(), []byte(strconv.Itoa(active.ID))) {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}

	maxInt := int(^uint(0) >> 1)
	overflow := performGroupQueryRequest(t, SearchGroups, http.MethodPost, "/groups/search", []byte(`{"keyword":"group-query","page":`+strconv.Itoa(maxInt)+`}`), viewer)
	if overflow.Code != http.StatusBadRequest {
		t.Fatalf("overflow page status=%d body=%s", overflow.Code, overflow.Body.String())
	}
}

func performGroupQueryRequest(t *testing.T, handler gin.HandlerFunc, method, path string, body []byte, user *gormdb.User) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	context.Set("user", user)
	handler(context)
	return recorder
}
