package udphub

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"
	"draarl/internal/groupaccess"
	"draarl/internal/protocol"
	jwtutil "draarl/pkg/jwt"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestProxiedModernGhostAuthenticationMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_EDGE_GHOST_E2E")), "true") {
		t.Skip("set DRAARL_EDGE_GHOST_E2E=true and DRAARL_TEST_MYSQL_DSN to run the edge ghost E2E")
	}
	parsed, err := drivermysql.ParseDSN(strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN")))
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
		t.Fatalf("migrate edge ghost tables: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	owner := &gormdb.User{Name: "edge-ghost-" + suffix, Email: "edge-ghost-" + suffix + "@example.invalid", CallSign: "EG" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	if err := db.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	firstGroup := &gormdb.Group{Name: "edge-rx-a-" + suffix, Type: groupaccess.TypePublic, OwerID: owner.ID, Status: 1}
	secondGroup := &gormdb.Group{Name: "edge-rx-b-" + suffix, Type: groupaccess.TypePublic, OwerID: owner.ID, Status: 1}
	for _, group := range []*gormdb.Group{firstGroup, secondGroup} {
		if err := db.Create(group).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := gormdb.NewUserRepository().UpsertUserDevicePreference(owner.ID, protocol.DraARLDevModelAndroid, firstGroup.ID); err != nil {
		t.Fatal(err)
	}
	refreshGroupCache()
	t.Cleanup(func() {
		var preferenceIDs []uint
		_ = db.Model(&gormdb.GhostClientPreference{}).Where("user_id = ?", owner.ID).Pluck("id", &preferenceIDs).Error
		if len(preferenceIDs) > 0 {
			_ = db.Where("preference_id IN ?", preferenceIDs).Delete(&gormdb.GhostClientSubscription{}).Error
		}
		_ = db.Where("user_id = ?", owner.ID).Delete(&gormdb.GhostClientPreference{}).Error
		_ = db.Where("user_id = ?", owner.ID).Delete(&gormdb.UserDevicePreference{}).Error
		_ = db.Delete(&gormdb.Group{}, []int{firstGroup.ID, secondGroup.ID}).Error
		_ = db.Delete(&gormdb.User{}, owner.ID).Error
		refreshGroupCache()
	})

	previousRegistry := ghostsession.Global
	previousManager := GlobalUDPGhostManager
	ghostsession.Global = ghostsession.NewRegistry(8, ghostsession.DefaultMaxSubscriptions)
	GlobalUDPGhostManager = newUDPGhostManager()
	t.Cleanup(func() {
		ghostsession.Global = previousRegistry
		GlobalUDPGhostManager = previousManager
	})
	if err := jwtutil.SetSecret("edge-ghost-e2e-secret-0123456789-abcdefghijklmnopqrstuvwxyz"); err != nil {
		t.Fatal(err)
	}
	token, err := jwtutil.GenerateToken(owner.Name, []string{"user"})
	if err != nil {
		t.Fatal(err)
	}

	encodeAuth := func(instanceID string) []byte {
		t.Helper()
		payload, marshalErr := json.Marshal(protocol.GhostAuthRequest{
			Version: protocol.GhostAuthPayloadVersion, Token: token, ClientInstanceID: instanceID,
			Capabilities: []string{"multi_receive_v1", "source_group_v1"},
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return protocol.EncodeDraARLv1(owner.Name, "", protocol.SSIDGhostAndroid, protocol.DraARLTypeJWTAuth, protocol.DraARLDevModelAndroid, 0, "", payload)
	}

	firstInstance, secondInstance := uuid.NewString(), uuid.NewString()
	if rejected := AuthenticateProxiedDevice("192.0.2.1", encodeAuth(firstInstance)); rejected.Success || rejected.Error != "node_ghost_multi_session_unsupported" {
		t.Fatalf("unnegotiated modern auth=%#v", rejected)
	}
	var applied ghostsession.Routing
	options := ProxiedDeviceAuthOptions{
		AllowGhostMultiSession: true, Endpoint: "edge-a/192.0.2.1:30001",
		Ghost: ProxiedGhostSessionHooks{ApplyRouting: func(_ string, _ int, _ byte, _ string, routing ghostsession.Routing) error {
			applied = routing
			return nil
		}},
	}
	first := AuthenticateProxiedDevice("192.0.2.1", encodeAuth(firstInstance), options)
	options.Endpoint = "edge-a/192.0.2.2:30002"
	second := AuthenticateProxiedDevice("192.0.2.2", encodeAuth(secondInstance), options)
	if !first.Success || !second.Success || first.GhostSessionID == second.GhostSessionID || first.SessionTag == second.SessionTag {
		t.Fatalf("modern proxied sessions did not coexist: first=%#v second=%#v", first, second)
	}
	if first.ClientInstanceID != firstInstance || second.ClientInstanceID != secondInstance || len(first.RxGroupIDs) != 1 || first.RxGroupIDs[0] != firstGroup.ID {
		t.Fatalf("unexpected proxied routing: first=%#v second=%#v", first, second)
	}
	if GlobalUDPGhostManager.GetSession(first.GhostSessionID) != nil || GlobalUDPGhostManager.GetSession(second.GhostSessionID) != nil {
		t.Fatal("edge ghosts were incorrectly registered in the center UDP manager")
	}
	responsePacket, err := protocol.NewDraARLv1Packet(nil, first.ResponsePacket)
	if err != nil {
		t.Fatal(err)
	}
	response, err := protocol.DecodeGhostAuthSuccessData(responsePacket.DATA)
	if err != nil || string(responsePacket.Username) != owner.Name || response.SessionID != first.GhostSessionID || response.SessionTag != first.SessionTag || protocol.ReservedUint32(responsePacket.Reserved) != first.SessionTag {
		t.Fatalf("invalid modern proxied auth response: response=%#v err=%v", response, err)
	}

	updated, err := ghostsession.Global.UpdateRouting(first.GhostSessionID, ghostsession.Routing{TxGroupID: secondGroup.ID, RxGroupIDs: []int{firstGroup.ID, secondGroup.ID}})
	if err != nil || updated.TxGroupID != secondGroup.ID || applied.TxGroupID != secondGroup.ID || len(applied.RxGroupIDs) != 2 {
		t.Fatalf("proxied routing controller was not updated: updated=%#v applied=%#v err=%v", updated.Routing(), applied, err)
	}
	if _, err := ConfirmProxiedGhostSession(first.GhostSessionID, "edge-b", owner.ID, protocol.SSIDGhostAndroid, protocol.DraARLDevModelAndroid, firstInstance); err == nil {
		t.Fatal("a different edge node confirmed another node's ghost session")
	}
	confirmed, err := ConfirmProxiedGhostSession(first.GhostSessionID, "edge-a", owner.ID, protocol.SSIDGhostAndroid, protocol.DraARLDevModelAndroid, firstInstance)
	if err != nil || confirmed.SessionTag != first.SessionTag {
		t.Fatalf("proxied ghost confirmation failed: session=%#v err=%v", confirmed, err)
	}
}
