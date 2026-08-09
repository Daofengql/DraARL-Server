package websocket

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"

	drivermysql "github.com/go-sql-driver/mysql"
)

func TestSanitizePersistedRoutingFiltersRevokedGroupsMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_RADIO_SESSIONS_E2E")), "true") {
		t.Skip("set DRAARL_RADIO_SESSIONS_E2E=true and DRAARL_TEST_MYSQL_DSN to run the WebSocket routing E2E")
	}
	parsed, err := drivermysql.ParseDSN(strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 5, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	db := gormdb.Get()
	if err := db.AutoMigrate(&gormdb.User{}, &gormdb.Group{}, &gormdb.GroupMember{}); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := &gormdb.User{Name: "ws-route-" + suffix, Email: "ws-route-" + suffix + "@example.invalid", CallSign: "WR" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	owner := &gormdb.User{Name: "ws-owner-" + suffix, Email: "ws-owner-" + suffix + "@example.invalid", CallSign: "WO" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	for _, candidate := range []*gormdb.User{user, owner} {
		if err := db.Create(candidate).Error; err != nil {
			t.Fatal(err)
		}
	}
	public := &gormdb.Group{Name: "ws-public-" + suffix, Type: 1, OwerID: owner.ID, Status: 1}
	privateAllowed := &gormdb.Group{Name: "ws-private-allowed-" + suffix, Type: 2, OwerID: owner.ID, Status: 1}
	privateRevoked := &gormdb.Group{Name: "ws-private-revoked-" + suffix, Type: 2, OwerID: owner.ID, Status: 1}
	disabled := &gormdb.Group{Name: "ws-disabled-" + suffix, Type: 1, OwerID: owner.ID, Status: 0}
	groups := []*gormdb.Group{public, privateAllowed, privateRevoked, disabled}
	for _, group := range groups {
		if err := db.Create(group).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(disabled).Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&gormdb.GroupMember{GroupID: privateAllowed.ID, UserID: user.ID, IsVerified: true}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("user_id = ?", user.ID).Delete(&gormdb.GroupMember{}).Error
		ids := make([]int, len(groups))
		for i, group := range groups {
			ids[i] = group.ID
		}
		_ = db.Delete(&gormdb.Group{}, ids).Error
		_ = db.Delete(&gormdb.User{}, []int{user.ID, owner.ID}).Error
	})

	routing, changed, err := sanitizePersistedRouting(user, ghostsession.Routing{
		TxGroupID:  privateRevoked.ID,
		RxGroupIDs: []int{public.ID, privateAllowed.ID, privateRevoked.ID, disabled.ID},
	}, 999)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || routing.TxGroupID != 999 || len(routing.RxGroupIDs) != 3 ||
		!slices.Contains(routing.RxGroupIDs, public.ID) || !slices.Contains(routing.RxGroupIDs, privateAllowed.ID) || !slices.Contains(routing.RxGroupIDs, 999) {
		t.Fatalf("sanitized routing=%#v changed=%v", routing, changed)
	}
}
