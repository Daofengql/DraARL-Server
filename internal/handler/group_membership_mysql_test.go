package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/internal/protocol"
	"draarl/internal/udphub"

	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestRemoveGroupMemberHTTPE2E(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(radioSessionsE2EEnabledEnv)), "true") {
		t.Skip("set " + radioSessionsE2EEnabledEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the membership E2E")
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
	if err := db.AutoMigrate(
		&gormdb.User{}, &gormdb.Group{}, &gormdb.Device{}, &gormdb.GroupMember{}, &gormdb.GroupLink{}, &gormdb.CommRecord{},
		&gormdb.UserDevicePreference{}, &gormdb.GhostClientPreference{}, &gormdb.GhostClientSubscription{},
	); err != nil {
		t.Fatalf("migrate group membership E2E tables: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	userSequence := 0
	newUser := func(prefix, role string) *gormdb.User {
		userSequence++
		return &gormdb.User{
			Name: prefix + "-" + suffix, Email: prefix + "-" + suffix + "@example.invalid",
			CallSign: fmt.Sprintf("GM%d%s", userSequence, suffix[len(suffix)-8:]), Roles: role, Status: 1, ApprovalStatus: 1,
		}
	}
	owner := newUser("membership-owner", "user")
	member := newUser("membership-member", "user")
	admin := newUser("membership-admin", "admin")
	adminTarget := newUser("membership-admin-target", "user")
	outsider := newUser("membership-outsider", "user")
	users := []*gormdb.User{owner, member, admin, adminTarget, outsider}
	for _, user := range users {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}

	privateGroup := &gormdb.Group{Name: "membership-private-" + suffix, Type: groupTypePrivate, OwerID: owner.ID, Status: 1}
	publicGroup := &gormdb.Group{Name: "membership-public-" + suffix, Type: groupTypePublic, OwerID: owner.ID, Status: 1}
	adminGroup := &gormdb.Group{Name: "membership-admin-private-" + suffix, Type: groupTypePrivate, OwerID: owner.ID, Status: 1}
	groups := []*gormdb.Group{privateGroup, publicGroup, adminGroup}
	for _, group := range groups {
		if err := db.Create(group).Error; err != nil {
			t.Fatal(err)
		}
	}
	var defaultGroup gormdb.Group
	defaultCreated := false
	if err := db.First(&defaultGroup, models.GroupIDPublicMin).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		defaultGroup = gormdb.Group{ID: models.GroupIDPublicMin, Name: "membership-default-" + suffix, Type: groupTypePublic, OwerID: owner.ID, Status: 1}
		if err := db.Create(&defaultGroup).Error; err != nil {
			t.Fatalf("create default group: %v", err)
		}
		defaultCreated = true
	} else if err != nil {
		t.Fatalf("load default group: %v", err)
	}

	memberships := []*gormdb.GroupMember{
		{GroupID: privateGroup.ID, UserID: member.ID, IsVerified: true},
		{GroupID: adminGroup.ID, UserID: adminTarget.ID, IsVerified: true},
	}
	if err := db.Create(memberships).Error; err != nil {
		t.Fatal(err)
	}
	device := &gormdb.Device{
		Name: "membership-device-" + suffix, OwnerID: member.ID, SSID: 1,
		DevModel: 2, GroupID: privateGroup.ID, Status: 1,
	}
	if err := db.Create(device).Error; err != nil {
		t.Fatal(err)
	}
	devicePreference := &gormdb.UserDevicePreference{UserID: member.ID, DevModel: 0, LastGroupID: privateGroup.ID}
	if err := db.Create(devicePreference).Error; err != nil {
		t.Fatal(err)
	}

	preferenceRepository := gormdb.NewGhostClientPreferenceRepository()
	instanceID := uuid.NewString()
	if _, err := preferenceRepository.GetOrCreate(member.ID, protocol.DraARLDevModelBrowser, instanceID, privateGroup.ID); err != nil {
		t.Fatal(err)
	}
	if err := preferenceRepository.ReplaceRouting(member.ID, protocol.DraARLDevModelBrowser, instanceID, privateGroup.ID, []int{privateGroup.ID, publicGroup.ID}); err != nil {
		t.Fatal(err)
	}

	previousRegistry := ghostsession.Global
	ghostsession.Global = ghostsession.NewRegistry(8, ghostsession.DefaultMaxSubscriptions)
	t.Cleanup(func() { ghostsession.Global = previousRegistry })
	var appliedRouting ghostsession.Routing
	session, err := ghostsession.Global.Register(ghostsession.Registration{
		ClientInstanceID: instanceID, OwnerID: member.ID, Username: member.Name, CallSign: member.CallSign,
		DevModel: protocol.DraARLDevModelBrowser, SSID: protocol.SSIDGhostWeb, Transport: ghostsession.TransportWebSocket,
		Capabilities: []string{ghostsession.CapabilityMultiReceiveV1, ghostsession.CapabilitySourceGroupV1},
		Routing:      ghostsession.Routing{TxGroupID: privateGroup.ID, RxGroupIDs: []int{privateGroup.ID, publicGroup.ID}},
	}, ghostsession.Controller{ApplyRouting: func(routing ghostsession.Routing) error {
		appliedRouting = routing
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		udphub.RemoveRuntimeDevice(member.ID, byte(device.SSID))
		var preferenceIDs []uint
		_ = db.Model(&gormdb.GhostClientPreference{}).Where("user_id IN ?", userIDs(users)).Pluck("id", &preferenceIDs).Error
		if len(preferenceIDs) > 0 {
			_ = db.Where("preference_id IN ?", preferenceIDs).Delete(&gormdb.GhostClientSubscription{}).Error
		}
		_ = db.Where("user_id IN ?", userIDs(users)).Delete(&gormdb.GhostClientPreference{}).Error
		_ = db.Where("user_id IN ?", userIDs(users)).Delete(&gormdb.UserDevicePreference{}).Error
		_ = db.Where("group_id IN ?", groupIDs(groups)).Delete(&gormdb.CommRecord{}).Error
		_ = db.Where("link_group_id IN ? OR target_group_id IN ?", groupIDs(groups), groupIDs(groups)).Delete(&gormdb.GroupLink{}).Error
		_ = db.Where("group_id IN ?", groupIDs(groups)).Delete(&gormdb.GroupMember{}).Error
		_ = db.Delete(&gormdb.Device{}, device.ID).Error
		_ = db.Delete(&gormdb.Group{}, groupIDs(groups)).Error
		if defaultCreated {
			_ = db.Delete(&gormdb.Group{}, defaultGroup.ID).Error
		}
		_ = db.Delete(&gormdb.User{}, userIDs(users)).Error
	})

	udphub.RefreshGroupCache()
	runtimeDevice := udphub.GetDeviceByID(device.ID)
	if runtimeDevice == nil || runtimeDevice.GroupID != privateGroup.ID {
		t.Fatalf("initial runtime device=%#v", runtimeDevice)
	}

	request := func(name, method, pattern, path string, actor *gormdb.User, handler gin.HandlerFunc, want int) []byte {
		t.Helper()
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.Handle(method, pattern, func(c *gin.Context) {
			c.Set("user", actor)
			handler(c)
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != want {
			t.Fatalf("%s status=%d want=%d body=%s", name, response.Code, want, response.Body.String())
		}
		return response.Body.Bytes()
	}
	removePattern := "/api/groups/:id/members/:userId"
	removePath := func(groupID, userID int) string {
		return fmt.Sprintf("/api/groups/%d/members/%d", groupID, userID)
	}
	memberListBody := request(
		"member list", http.MethodGet, "/api/groups/:id/members",
		fmt.Sprintf("/api/groups/%d/members", privateGroup.ID), owner, GetGroupMembers, http.StatusOK,
	)
	var memberList struct {
		Data struct {
			Items []GroupMemberInfo `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(memberListBody, &memberList); err != nil {
		t.Fatal(err)
	}
	if len(memberList.Data.Items) != 1 || memberList.Data.Items[0].UserID != member.ID ||
		memberList.Data.Items[0].Username != member.Name || memberList.Data.Items[0].CallSign != member.CallSign ||
		memberList.Data.Items[0].DeviceCount != 1 {
		t.Fatalf("member list=%s", memberListBody)
	}

	request("non-owner", http.MethodDelete, removePattern, removePath(privateGroup.ID, member.ID), outsider, RemoveGroupMember, http.StatusForbidden)
	request("remove owner", http.MethodDelete, removePattern, removePath(privateGroup.ID, owner.ID), owner, RemoveGroupMember, http.StatusBadRequest)
	request("missing member", http.MethodDelete, removePattern, removePath(privateGroup.ID, outsider.ID), owner, RemoveGroupMember, http.StatusNotFound)
	request("admin", http.MethodDelete, removePattern, removePath(adminGroup.ID, adminTarget.ID), admin, RemoveGroupMember, http.StatusOK)
	request("owner", http.MethodDelete, removePattern, removePath(privateGroup.ID, member.ID), owner, RemoveGroupMember, http.StatusOK)

	var membershipCount int64
	if err := db.Model(&gormdb.GroupMember{}).Where("group_id = ? AND user_id = ?", privateGroup.ID, member.ID).Count(&membershipCount).Error; err != nil || membershipCount != 0 {
		t.Fatalf("membership count=%d err=%v", membershipCount, err)
	}
	var persistedDevice gormdb.Device
	if err := db.First(&persistedDevice, device.ID).Error; err != nil || persistedDevice.GroupID != models.GroupIDPublicMin {
		t.Fatalf("persisted device group=%d err=%v", persistedDevice.GroupID, err)
	}
	runtimeDevice = udphub.GetDeviceByID(device.ID)
	if runtimeDevice == nil || runtimeDevice.GroupID != models.GroupIDPublicMin {
		t.Fatalf("runtime device=%#v", runtimeDevice)
	}
	var persistedDevicePreference gormdb.UserDevicePreference
	if err := db.First(&persistedDevicePreference, devicePreference.ID).Error; err != nil || persistedDevicePreference.LastGroupID != 0 {
		t.Fatalf("device preference group=%d err=%v", persistedDevicePreference.LastGroupID, err)
	}

	currentSession, exists := ghostsession.Global.Get(session.SessionID)
	if !exists || currentSession.TxGroupID != models.GroupIDPublicMin || containsGroup(currentSession.RxGroupIDs, privateGroup.ID) ||
		!containsGroup(currentSession.RxGroupIDs, models.GroupIDPublicMin) || !containsGroup(currentSession.RxGroupIDs, publicGroup.ID) {
		t.Fatalf("reconciled session=%#v exists=%v", currentSession.Routing(), exists)
	}
	if appliedRouting.TxGroupID != models.GroupIDPublicMin || containsGroup(appliedRouting.RxGroupIDs, privateGroup.ID) {
		t.Fatalf("runtime projection=%#v", appliedRouting)
	}
	persistedRouting, err := preferenceRepository.Get(member.ID, protocol.DraARLDevModelBrowser, instanceID)
	if err != nil || persistedRouting == nil || persistedRouting.TxGroupID != models.GroupIDPublicMin ||
		containsGroup(persistedRouting.RxGroupIDs, privateGroup.ID) || !containsGroup(persistedRouting.RxGroupIDs, publicGroup.ID) {
		t.Fatalf("persisted ghost routing=%#v err=%v", persistedRouting, err)
	}
	if _, err := updateOwnedSessionRouting(member, session.SessionID, ghostsession.Routing{
		TxGroupID: privateGroup.ID, RxGroupIDs: []int{privateGroup.ID},
	}); !errors.Is(err, errRoutingGroupForbidden) {
		t.Fatalf("revoked transmit/subscription error=%v", err)
	}
	request(
		"removed member history", http.MethodGet, "/api/groups/:id/messages",
		fmt.Sprintf("/api/groups/%d/messages", privateGroup.ID), member, GetGroupMessages, http.StatusForbidden,
	)
}

func userIDs(users []*gormdb.User) []int {
	ids := make([]int, len(users))
	for i, user := range users {
		ids[i] = user.ID
	}
	return ids
}

func groupIDs(groups []*gormdb.Group) []int {
	ids := make([]int, len(groups))
	for i, group := range groups {
		ids[i] = group.ID
	}
	return ids
}

func containsGroup(groupIDs []int, target int) bool {
	for _, groupID := range groupIDs {
		if groupID == target {
			return true
		}
	}
	return false
}
