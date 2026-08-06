package handler

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"
	"draarl/internal/protocol"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

const radioSessionsE2EEnabledEnv = "DRAARL_RADIO_SESSIONS_E2E"

func TestRadioSessionRoutingMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(radioSessionsE2EEnabledEnv)), "true") {
		t.Skip("set " + radioSessionsE2EEnabledEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the radio session E2E")
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
	if err := db.AutoMigrate(&gormdb.User{}, &gormdb.Group{}, &gormdb.GroupMember{}, &gormdb.UserDevicePreference{}, &gormdb.GhostClientPreference{}, &gormdb.GhostClientSubscription{}); err != nil {
		t.Fatalf("migrate radio session tables: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	owner := &gormdb.User{Name: "radio-owner-" + suffix, Email: "radio-owner-" + suffix + "@example.invalid", CallSign: "RO" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	other := &gormdb.User{Name: "radio-other-" + suffix, Email: "radio-other-" + suffix + "@example.invalid", CallSign: "RX" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	for _, user := range []*gormdb.User{owner, other} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	publicA := &gormdb.Group{Name: "radio-public-a-" + suffix, Type: groupTypePublic, OwerID: owner.ID, Status: 1}
	publicB := &gormdb.Group{Name: "radio-public-b-" + suffix, Type: groupTypePublic, OwerID: other.ID, Status: 1}
	privateAllowed := &gormdb.Group{Name: "radio-private-allowed-" + suffix, Type: groupTypePrivate, OwerID: other.ID, Status: 1}
	privateForbidden := &gormdb.Group{Name: "radio-private-forbidden-" + suffix, Type: groupTypePrivate, OwerID: other.ID, Status: 1}
	groups := []*gormdb.Group{publicA, publicB, privateAllowed, privateForbidden}
	for _, group := range groups {
		if err := db.Create(group).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&gormdb.GroupMember{GroupID: privateAllowed.ID, UserID: owner.ID, IsVerified: true}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("user_id IN ?", []int{owner.ID, other.ID}).Delete(&gormdb.GroupMember{}).Error
		var preferenceIDs []uint
		_ = db.Model(&gormdb.GhostClientPreference{}).Where("user_id IN ?", []int{owner.ID, other.ID}).Pluck("id", &preferenceIDs).Error
		if len(preferenceIDs) > 0 {
			_ = db.Where("preference_id IN ?", preferenceIDs).Delete(&gormdb.GhostClientSubscription{}).Error
		}
		_ = db.Where("user_id IN ?", []int{owner.ID, other.ID}).Delete(&gormdb.GhostClientPreference{}).Error
		_ = db.Where("user_id IN ?", []int{owner.ID, other.ID}).Delete(&gormdb.UserDevicePreference{}).Error
		groupIDs := make([]int, len(groups))
		for i, group := range groups {
			groupIDs[i] = group.ID
		}
		_ = db.Delete(&gormdb.Group{}, groupIDs).Error
		_ = db.Delete(&gormdb.User{}, []int{owner.ID, other.ID}).Error
	})

	previousGlobal := ghostsession.Global
	ghostsession.Global = ghostsession.NewRegistry(8, ghostsession.DefaultMaxSubscriptions)
	t.Cleanup(func() { ghostsession.Global = previousGlobal })
	repository := gormdb.NewGhostClientPreferenceRepository()
	instanceID := uuid.NewString()
	if _, err := repository.GetOrCreate(owner.ID, protocol.DraARLDevModelBrowser, instanceID, publicA.ID); err != nil {
		t.Fatal(err)
	}
	rejectGroupID := 0
	disconnected := false
	session, err := ghostsession.Global.Register(ghostsession.Registration{
		ClientInstanceID: instanceID, OwnerID: owner.ID, Username: owner.Name, CallSign: owner.CallSign,
		DevModel: protocol.DraARLDevModelBrowser, SSID: protocol.SSIDGhostWeb, Transport: ghostsession.TransportWebSocket,
		Capabilities: []string{"multi_receive_v1", "source_group_v1"}, Routing: ghostsession.Routing{TxGroupID: publicA.ID, RxGroupIDs: []int{publicA.ID}},
	}, ghostsession.Controller{
		ApplyRouting: func(routing ghostsession.Routing) error {
			if routing.TxGroupID == rejectGroupID {
				return errors.New("runtime rejected routing")
			}
			return nil
		},
		Disconnect: func(string) { disconnected = true },
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := updateOwnedSessionRouting(owner, session.SessionID, ghostsession.Routing{TxGroupID: publicB.ID, RxGroupIDs: []int{privateAllowed.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TxGroupID != publicB.ID || len(updated.RxGroupIDs) != 2 {
		t.Fatalf("updated routing=%#v", updated.Routing())
	}
	persisted, err := repository.Get(owner.ID, protocol.DraARLDevModelBrowser, instanceID)
	if err != nil || persisted == nil || persisted.TxGroupID != publicB.ID || len(persisted.RxGroupIDs) != 2 {
		t.Fatalf("persisted routing=%#v err=%v", persisted, err)
	}

	if _, err := updateOwnedSessionRouting(other, session.SessionID, ghostsession.Routing{TxGroupID: publicA.ID, RxGroupIDs: []int{publicA.ID}}); !errors.Is(err, ghostsession.ErrSessionNotFound) {
		t.Fatalf("cross-owner update error=%v", err)
	}
	if _, err := updateOwnedSessionRouting(owner, session.SessionID, ghostsession.Routing{TxGroupID: privateForbidden.ID, RxGroupIDs: []int{privateForbidden.ID}}); !errors.Is(err, errRoutingGroupForbidden) {
		t.Fatalf("forbidden update error=%v", err)
	}

	rejectGroupID = publicA.ID
	if _, err := updateOwnedSessionRouting(owner, session.SessionID, ghostsession.Routing{TxGroupID: publicA.ID, RxGroupIDs: []int{publicA.ID}}); err == nil {
		t.Fatal("runtime projection failure was ignored")
	}
	persisted, err = repository.Get(owner.ID, protocol.DraARLDevModelBrowser, instanceID)
	if err != nil || persisted == nil || persisted.TxGroupID != publicB.ID || len(persisted.RxGroupIDs) != 2 {
		t.Fatalf("failed runtime update was not rolled back: %#v err=%v", persisted, err)
	}
	current, _ := ghostsession.Global.Get(session.SessionID)
	if current.TxGroupID != publicB.ID {
		t.Fatalf("registry changed on runtime failure: %#v", current.Routing())
	}

	if err := db.Where("group_id = ? AND user_id = ?", privateAllowed.ID, owner.ID).Delete(&gormdb.GroupMember{}).Error; err != nil {
		t.Fatal(err)
	}
	rejectGroupID = 0
	reconcileOwnerGhostSessions(owner.ID)
	current, _ = ghostsession.Global.Get(session.SessionID)
	if current.TxGroupID != publicB.ID || len(current.RxGroupIDs) != 1 || current.RxGroupIDs[0] != publicB.ID {
		t.Fatalf("revoked membership remained live: %#v", current.Routing())
	}
	persisted, err = repository.Get(owner.ID, protocol.DraARLDevModelBrowser, instanceID)
	if err != nil || persisted == nil || len(persisted.RxGroupIDs) != 1 || persisted.RxGroupIDs[0] != publicB.ID {
		t.Fatalf("revoked membership remained persisted: %#v err=%v", persisted, err)
	}

	tooMany := make([]int, ghostsession.DefaultMaxSubscriptions+1)
	for i := range tooMany {
		tooMany[i] = i + 1
	}
	if _, err := updateOwnedSessionRouting(owner, session.SessionID, ghostsession.Routing{TxGroupID: 1, RxGroupIDs: tooMany}); !errors.Is(err, ghostsession.ErrSubscriptionLimit) {
		t.Fatalf("subscription limit error=%v", err)
	}

	secondInstanceID := uuid.NewString()
	if _, err := repository.GetOrCreate(owner.ID, protocol.DraARLDevModelBrowser, secondInstanceID, publicA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ghostsession.Global.Register(ghostsession.Registration{
		ClientInstanceID: secondInstanceID, OwnerID: owner.ID, DevModel: protocol.DraARLDevModelBrowser,
		SSID: protocol.SSIDGhostWeb, Transport: ghostsession.TransportWebSocket,
		Capabilities: []string{ghostsession.CapabilityMultiReceiveV1, ghostsession.CapabilitySourceGroupV1},
		Routing:      ghostsession.Routing{TxGroupID: publicA.ID, RxGroupIDs: []int{publicA.ID}},
	}, ghostsession.Controller{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveOwnedPlatformSession(owner.ID, protocol.DraARLDevModelBrowser, ""); !errors.Is(err, errAmbiguousSession) {
		t.Fatalf("ambiguous session error=%v", err)
	}
	if err := ghostsession.Global.DisconnectOwned(owner.ID, session.SessionID, "test"); err != nil {
		t.Fatal(err)
	}
	if !disconnected {
		t.Fatal("session disconnect callback was not invoked")
	}
}
