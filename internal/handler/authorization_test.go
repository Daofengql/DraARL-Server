package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/pkg/crypto"
	appjwt "draarl/pkg/jwt"

	"github.com/gin-gonic/gin"
)

func TestCanViewGroup(t *testing.T) {
	user := &gormdb.User{ID: 10, Roles: "user"}
	admin := &gormdb.User{ID: 20, Roles: "admin"}
	publicGroup := &gormdb.Group{ID: 100, Type: groupTypePublic, OwerID: 30, Status: 1}
	privateGroup := &gormdb.Group{ID: 101, Type: groupTypePrivate, OwerID: 30, Status: 1}
	ownedPrivateGroup := &gormdb.Group{ID: 102, Type: groupTypePrivate, OwerID: user.ID, Status: 1}
	virtualGroup := &gormdb.Group{ID: 103, Type: groupTypePublic, IsVirtual: true, Status: 1}
	unsupportedGroup := &gormdb.Group{ID: 104, Type: 99, Status: 1}
	disabledGroup := &gormdb.Group{ID: 105, Type: groupTypePublic, Status: 0}

	tests := []struct {
		name             string
		actor            *gormdb.User
		group            *gormdb.Group
		isVerifiedMember bool
		want             bool
	}{
		{name: "public group is visible to user", actor: user, group: publicGroup, want: true},
		{name: "private group is hidden from non-member", actor: user, group: privateGroup, want: false},
		{name: "verified member can view private group", actor: user, group: privateGroup, isVerifiedMember: true, want: true},
		{name: "owner can view own private group", actor: user, group: ownedPrivateGroup, want: true},
		{name: "admin can view private group", actor: admin, group: privateGroup, want: true},
		{name: "virtual group is hidden from user", actor: user, group: virtualGroup, isVerifiedMember: true, want: false},
		{name: "virtual group is hidden from admin ordinary group access", actor: admin, group: virtualGroup, want: false},
		{name: "unsupported group type is hidden", actor: user, group: unsupportedGroup, isVerifiedMember: true, want: false},
		{name: "disabled group is hidden from user", actor: user, group: disabledGroup, want: false},
		{name: "disabled group is hidden from admin ordinary group access", actor: admin, group: disabledGroup, want: false},
		{name: "missing user is denied", actor: nil, group: publicGroup, want: false},
		{name: "missing group is denied", actor: user, group: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canViewGroup(tt.actor, tt.group, tt.isVerifiedMember); got != tt.want {
				t.Fatalf("canViewGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanViewOwnCommRecord(t *testing.T) {
	user := &gormdb.User{ID: 10, Roles: "user"}
	other := &gormdb.User{ID: 11, Roles: "user"}
	admin := &gormdb.User{ID: 20, Roles: "admin"}
	userID := uint(user.ID)
	otherUserID := uint(other.ID)

	tests := []struct {
		name   string
		actor  *gormdb.User
		record *CommRecordWithDetails
		want   bool
	}{
		{name: "sender snapshot owns ghost record", actor: user, record: &CommRecordWithDetails{DeviceID: 0, UserID: &userID}, want: true},
		{name: "other sender snapshot is denied", actor: user, record: &CommRecordWithDetails{DeviceID: 0, UserID: &otherUserID}, want: false},
		{name: "legacy physical record falls back to current owner", actor: user, record: &CommRecordWithDetails{DeviceID: 7, DeviceOwnerID: user.ID}, want: true},
		{name: "legacy physical record denies another owner", actor: other, record: &CommRecordWithDetails{DeviceID: 7, DeviceOwnerID: user.ID}, want: false},
		{name: "sender snapshot takes precedence over current device owner", actor: user, record: &CommRecordWithDetails{DeviceID: 7, DeviceOwnerID: user.ID, UserID: &otherUserID}, want: false},
		{name: "legacy ghost record without sender is denied", actor: user, record: &CommRecordWithDetails{DeviceID: 0}, want: false},
		{name: "admin may inspect any record", actor: admin, record: &CommRecordWithDetails{DeviceID: 0, UserID: &otherUserID}, want: true},
		{name: "missing actor is denied", actor: nil, record: &CommRecordWithDetails{DeviceID: 7, DeviceOwnerID: user.ID}, want: false},
		{name: "missing record is denied", actor: user, record: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canViewOwnCommRecord(tt.actor, tt.record); got != tt.want {
				t.Fatalf("canViewOwnCommRecord() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommRecordResponsePreservesUnknownSnapshotDeviceModel(t *testing.T) {
	response := toCommRecordResponse(CommRecordWithDetails{
		DeviceID: 7, SenderUsername: "sender-at-send-time", SenderDevModel: 0, CurrentDevModel: 105,
	})
	if response.DevModel != 0 {
		t.Fatalf("unknown sender device model changed to current model: %#v", response)
	}
}

func TestCanManageDevice(t *testing.T) {
	owner := &gormdb.User{ID: 10, Roles: "user"}
	otherUser := &gormdb.User{ID: 11, Roles: "user"}
	admin := &gormdb.User{ID: 12, Roles: "admin"}
	device := &gormdb.Device{ID: 20, OwnerID: owner.ID}

	if !canManageDevice(owner, device) {
		t.Fatal("device owner should be able to manage the device")
	}
	if canManageDevice(otherUser, device) {
		t.Fatal("another ordinary user must not manage the device")
	}
	if !canManageDevice(admin, device) {
		t.Fatal("admin should be able to manage any device")
	}
	if canManageDevice(nil, device) || canManageDevice(owner, nil) {
		t.Fatal("missing actor or device must be denied")
	}
}

func TestCanManageGroup(t *testing.T) {
	owner := &gormdb.User{ID: 10, Roles: "user"}
	otherUser := &gormdb.User{ID: 11, Roles: "user"}
	admin := &gormdb.User{ID: 12, Roles: "admin"}
	group := &gormdb.Group{ID: 20, OwerID: owner.ID}

	if !canManageGroup(owner, group) {
		t.Fatal("group owner should be able to manage the group")
	}
	if canManageGroup(otherUser, group) {
		t.Fatal("another ordinary user must not manage the group")
	}
	if !canManageGroup(admin, group) {
		t.Fatal("admin should be able to manage any group")
	}
	if canManageGroup(nil, group) || canManageGroup(owner, nil) {
		t.Fatal("missing actor or group must be denied")
	}
}

func TestCanAdminSwitchLogin(t *testing.T) {
	admin := &gormdb.User{ID: 1, Roles: "admin", Status: 1}
	otherAdmin := &gormdb.User{ID: 2, Roles: "admin", Status: 1}
	superAdminTarget := &gormdb.User{ID: 1, Roles: "admin", Status: 1}
	activeUser := &gormdb.User{ID: 10, Roles: "user", Status: 1}
	disabledUser := &gormdb.User{ID: 11, Roles: "user", Status: 0}
	ordinaryActor := &gormdb.User{ID: 12, Roles: "user", Status: 1}

	if !canAdminSwitchLogin(admin, activeUser) {
		t.Fatal("admin should be able to switch login to an active ordinary user")
	}
	if canAdminSwitchLogin(ordinaryActor, activeUser) {
		t.Fatal("ordinary user must never switch another user's login")
	}
	if !canAdminSwitchLogin(admin, otherAdmin) {
		t.Fatal("admin should be able to switch login to another non-super-admin account")
	}
	if canAdminSwitchLogin(otherAdmin, superAdminTarget) {
		t.Fatal("switching login to the super administrator must be denied")
	}
	if canAdminSwitchLogin(admin, disabledUser) {
		t.Fatal("switching login to a disabled user must be denied")
	}
	if canAdminSwitchLogin(admin, admin) {
		t.Fatal("switching login to the current account must be denied")
	}
	if canAdminSwitchLogin(nil, activeUser) || canAdminSwitchLogin(admin, nil) {
		t.Fatal("missing actor or target must be denied")
	}
}

func TestAdminSwitchLoginRejectsOrdinaryUserBeforeTargetLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/auth/switch-login/:id", func(c *gin.Context) {
		c.Set("user", &gormdb.User{ID: 12, Name: "ordinary", Roles: "user", Status: 1})
		AdminSwitchLogin(c)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/switch-login/20", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("ordinary user switch-login status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if !strings.Contains(recorder.Body.String(), "仅管理员") {
		t.Fatalf("ordinary user rejection message = %s", recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestIssuedSwitchLoginTokenUsesTargetIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/auth/switch-login/20", nil)
	target := &gormdb.User{ID: 20, Name: "target-user", Roles: "user", Status: 1}

	issued, err := issueAuthTokens(context, target)
	if err != nil {
		t.Fatalf("issue target auth tokens: %v", err)
	}
	claims, err := appjwt.ParseToken(issued.AccessToken)
	if err != nil {
		t.Fatalf("parse issued access token: %v", err)
	}
	if claims.Username != target.Name {
		t.Fatalf("issued username = %q, want %q", claims.Username, target.Name)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "user" {
		t.Fatalf("issued roles = %#v, want target user role only", claims.Roles)
	}
	if issued.RefreshToken == "" {
		t.Fatal("switch login must issue a target refresh token")
	}

	hasRefreshCookie := false
	hasWSCookie := false
	for _, cookie := range recorder.Result().Cookies() {
		switch cookie.Name {
		case refreshTokenCookieName:
			hasRefreshCookie = cookie.Value != ""
		case wsTokenCookieName:
			hasWSCookie = cookie.Value != ""
		}
	}
	if !hasRefreshCookie || !hasWSCookie {
		t.Fatalf("target session cookies missing: refresh=%v ws=%v", hasRefreshCookie, hasWSCookie)
	}
}

func TestGroupJSONNeverIncludesPassword(t *testing.T) {
	encoded, err := json.Marshal(&gormdb.Group{
		ID:       100,
		Name:     "private",
		Type:     groupTypePrivate,
		Password: "secret123",
	})
	if err != nil {
		t.Fatalf("marshal group: %v", err)
	}
	if strings.Contains(string(encoded), "secret123") || strings.Contains(string(encoded), "\"password\"") {
		t.Fatalf("group JSON leaked password: %s", encoded)
	}
	if strings.Contains(string(encoded), "\"callsign\"") {
		t.Fatalf("group JSON still exposes removed group callsign: %s", encoded)
	}
}

func TestRuntimeGroupJSONNeverIncludesPassword(t *testing.T) {
	encoded, err := json.Marshal(&models.Group{
		ID:       100,
		Name:     "runtime-private",
		Type:     groupTypePrivate,
		Password: "runtime-secret",
	})
	if err != nil {
		t.Fatalf("marshal runtime group: %v", err)
	}
	if strings.Contains(string(encoded), "runtime-secret") || strings.Contains(string(encoded), "\"password\"") {
		t.Fatalf("runtime group JSON leaked password: %s", encoded)
	}
	if strings.Contains(string(encoded), "\"callsign\"") {
		t.Fatalf("runtime group JSON still exposes removed group callsign: %s", encoded)
	}
}

func TestCanUseGroupAsDeviceDefault(t *testing.T) {
	user := &gormdb.User{ID: 10, Roles: "user"}
	admin := &gormdb.User{ID: 20, Roles: "admin"}
	publicGroup := &gormdb.Group{ID: 100, Type: groupTypePublic, Status: 1}
	privateGroup := &gormdb.Group{ID: 101, Type: groupTypePrivate, Status: 1, OwerID: 30}
	disabledGroup := &gormdb.Group{ID: 102, Type: groupTypePublic, Status: 0}
	virtualGroup := &gormdb.Group{ID: 103, Type: groupTypePublic, Status: 1, IsVirtual: true}

	if !canUseGroupAsDeviceDefault(user, publicGroup, false) {
		t.Fatal("public enabled group should be a valid default")
	}
	if canUseGroupAsDeviceDefault(user, privateGroup, false) {
		t.Fatal("unjoined private group must not be a user default")
	}
	if !canUseGroupAsDeviceDefault(user, privateGroup, true) {
		t.Fatal("verified private group should be a valid user default")
	}
	if !canUseGroupAsDeviceDefault(admin, privateGroup, false) {
		t.Fatal("admin should be able to select any enabled entity group")
	}
	if canUseGroupAsDeviceDefault(user, disabledGroup, false) {
		t.Fatal("disabled group must not be a default")
	}
	if canUseGroupAsDeviceDefault(admin, virtualGroup, false) {
		t.Fatal("virtual group must not be a device default")
	}
}

func TestCanManageGroupDeviceCommControl(t *testing.T) {
	owner := &gormdb.User{ID: 10, Roles: "user"}
	admin := &gormdb.User{ID: 20, Roles: "admin"}
	member := &gormdb.User{ID: 30, Roles: "user"}
	group := &gormdb.Group{ID: 100, Type: groupTypePublic, OwerID: owner.ID}
	device := &gormdb.Device{ID: 7, GroupID: group.ID, OwnerID: member.ID}

	if !canManageGroupDeviceCommControl(owner, group, device) {
		t.Fatal("group owner should manage communication control for a current group device")
	}
	if !canManageGroupDeviceCommControl(admin, group, device) {
		t.Fatal("admin should manage communication control for a current group device")
	}
	if canManageGroupDeviceCommControl(member, group, device) {
		t.Fatal("ordinary group member must not manage another device through the group endpoint")
	}

	moved := *device
	moved.GroupID = 101
	if canManageGroupDeviceCommControl(owner, group, &moved) {
		t.Fatal("group owner must lose control after the device leaves the group")
	}

	virtual := *group
	virtual.IsVirtual = true
	if canManageGroupDeviceCommControl(admin, &virtual, device) {
		t.Fatal("virtual groups must not expose entity-device communication control")
	}
}

func TestUpdateGroupRequestDistinguishesOmittedAndEmptyFields(t *testing.T) {
	var omitted UpdateGroupRequest
	if err := json.Unmarshal([]byte(`{}`), &omitted); err != nil {
		t.Fatalf("unmarshal omitted request: %v", err)
	}
	if omitted.Note != nil {
		t.Fatal("omitted optional text fields must remain nil")
	}

	var cleared UpdateGroupRequest
	if err := json.Unmarshal([]byte(`{"note":""}`), &cleared); err != nil {
		t.Fatalf("unmarshal explicit empty request: %v", err)
	}
	if cleared.Note == nil || *cleared.Note != "" {
		t.Fatal("explicit empty note must be represented as a present empty value")
	}
}

func TestUpdateGroupDeviceCommControlRequestPreservesPartialUpdates(t *testing.T) {
	var request UpdateGroupDeviceCommControlRequest
	if err := json.Unmarshal([]byte(`{"disable_send":true}`), &request); err != nil {
		t.Fatalf("unmarshal communication control request: %v", err)
	}
	if request.DisableSend == nil || !*request.DisableSend {
		t.Fatal("explicit disable_send must be preserved")
	}
	if request.DisableRecv != nil {
		t.Fatal("omitted disable_recv must remain nil")
	}
}

func TestRevealDevicePasswordReadsExistingAESValueWithoutRepository(t *testing.T) {
	const (
		key      = "0123456789abcdef0123456789abcdef"
		password = "A1b2C3d4"
	)
	if err := crypto.InitAES(key); err != nil {
		t.Fatalf("initialize AES: %v", err)
	}
	encrypted, err := crypto.Encrypt(password)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}

	got, isNew, err := revealDevicePassword(nil, &gormdb.User{DevicePassword: encrypted})
	if err != nil {
		t.Fatalf("reveal password: %v", err)
	}
	if got != password {
		t.Fatalf("revealed password = %q, want %q", got, password)
	}
	if isNew {
		t.Fatal("existing AES password must not be replaced")
	}
}
