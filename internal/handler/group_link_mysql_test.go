package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/repository"
	"draarl/internal/gormdb"

	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
)

func TestVirtualGroupBroadcastPolicyHTTPE2E(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(broadcastAPIE2EEnabledEnv)), "true") {
		t.Skip("set " + broadcastAPIE2EEnabledEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the virtual-group API E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("virtual-group E2E requires a draarl_test_* database: db=%q err=%v", parsed.DBName, err)
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 10, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	db := gormdb.Get()
	if err := db.AutoMigrate(
		&gormdb.User{}, &gormdb.Group{}, &gormdb.GroupMember{}, &gormdb.GroupLink{}, &gormdb.Device{},
		&gormdb.UserDevicePreference{}, &gormdb.GhostClientPreference{}, &gormdb.GhostClientSubscription{},
		&model.BroadcastAudio{}, &model.BroadcastSchedule{}, &model.VirtualGroupBroadcastPolicy{}, &model.BroadcastRun{},
	); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	admin := &gormdb.User{Name: "vg-admin-" + suffix, Email: "vg-admin-" + suffix + "@example.invalid", CallSign: "VG" + suffix[len(suffix)-6:], Roles: "admin", Status: 1, ApprovalStatus: 1}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	newGroup := func(label string) *gormdb.Group {
		group := &gormdb.Group{Name: "vg-" + label + "-" + suffix, Type: groupTypePublic, OwerID: admin.ID, Status: 1}
		if err := db.Create(group).Error; err != nil {
			t.Fatal(err)
		}
		return group
	}
	groupA, groupB, groupC := newGroup("a"), newGroup("b"), newGroup("c")
	groupIDs := []int{groupA.ID, groupB.ID, groupC.ID}
	var virtualGroupID int
	t.Cleanup(func() {
		_ = db.Where("source_group_id IN ?", groupIDs).Delete(&model.BroadcastRun{}).Error
		_ = db.Where("group_id IN ?", groupIDs).Delete(&model.BroadcastSchedule{}).Error
		_ = db.Where("group_id IN ?", groupIDs).Delete(&model.BroadcastAudio{}).Error
		if virtualGroupID > 0 {
			_ = db.Where("virtual_group_id = ?", virtualGroupID).Delete(&model.VirtualGroupBroadcastPolicy{}).Error
			_ = db.Where("link_group_id = ?", virtualGroupID).Delete(&gormdb.GroupLink{}).Error
			_ = db.Delete(&gormdb.Group{}, virtualGroupID).Error
		}
		_ = db.Delete(&gormdb.Group{}, groupIDs).Error
		_ = db.Delete(&gormdb.User{}, admin.ID).Error
	})

	repo := repository.Default()
	newSchedule := func(group *gormdb.Group, label string) *model.BroadcastSchedule {
		audio := &model.BroadcastAudio{
			GroupID: group.ID, Name: label, OriginalObjectKey: label + ".wav", PlaybackObjectKey: label + ".dabr",
			OriginalMIMEType: "audio/wav", OriginalSize: 10, PlaybackSize: 10, DurationMS: 1000,
			PacketCount: 8, SHA256: strings.Repeat("a", 64), Status: model.AudioStatusReady, CreatedBy: admin.ID,
		}
		if err := repo.CreateAudio(context.Background(), audio); err != nil {
			t.Fatal(err)
		}
		schedule := &model.BroadcastSchedule{
			GroupID: group.ID, AudioID: audio.ID, Name: label, ScheduleType: model.ScheduleTypeDaily,
			Timezone: "Asia/Shanghai", LocalTime: "20:00:00", Enabled: true, CreatedBy: admin.ID, UpdatedBy: admin.ID,
		}
		if err := repo.SaveSchedule(context.Background(), schedule, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		return schedule
	}
	scheduleA, scheduleB, scheduleC := newSchedule(groupA, "vg-audio-a"), newSchedule(groupB, "vg-audio-b"), newSchedule(groupC, "vg-audio-c")

	createPayload := []byte(fmt.Sprintf(`{"name":"互联点名","status":0,"target_group_ids":[%d,%d],"broadcast_policy":{"mode":"allow_single_source","allowed_source_group_id":%d}}`, groupA.ID, groupB.ID, groupB.ID))
	created := performVirtualGroupHandlerRequest(t, admin, http.MethodPost, "/group-links", "/group-links", createPayload, CreateVirtualGroup)
	requireBroadcastStatus(t, created, http.StatusCreated)
	var createdEnvelope struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body, &createdEnvelope); err != nil || createdEnvelope.Data.ID == 0 {
		t.Fatalf("decode create response: err=%v body=%s", err, created.Body)
	}
	virtualGroupID = createdEnvelope.Data.ID
	assertHTTPBroadcastSchedule(t, repo, scheduleA.ID, "", true)
	assertHTTPBroadcastSchedule(t, repo, scheduleB.ID, "", true)

	statusPath := fmt.Sprintf("/group-links/%d", virtualGroupID)
	enabled := performVirtualGroupHandlerRequest(t, admin, http.MethodPut, "/group-links/:id", statusPath, []byte(`{"status":1}`), UpdateVirtualGroup)
	requireBroadcastStatus(t, enabled, http.StatusOK)
	assertHTTPBroadcastSchedule(t, repo, scheduleA.ID, model.SuspendReasonActiveVirtualGroup, false)
	assertHTTPBroadcastSchedule(t, repo, scheduleB.ID, "", true)

	detail := performVirtualGroupHandlerRequest(t, admin, http.MethodGet, "/group-links/:id", statusPath, nil, GetVirtualGroup)
	requireBroadcastStatus(t, detail, http.StatusOK)
	if !bytes.Contains(detail.Body, []byte(`"mode":"allow_single_source"`)) || !bytes.Contains(detail.Body, []byte(`"broadcast_members"`)) {
		t.Fatalf("detail omitted broadcast policy state: %s", detail.Body)
	}

	addPath := fmt.Sprintf("/group-links/%d/targets", virtualGroupID)
	added := performVirtualGroupHandlerRequest(t, admin, http.MethodPost, "/group-links/:id/targets", addPath, []byte(fmt.Sprintf(`{"target_group_id":%d}`, groupC.ID)), AddGroupLinkTarget)
	requireBroadcastStatus(t, added, http.StatusOK)
	assertHTTPBroadcastSchedule(t, repo, scheduleC.ID, model.SuspendReasonActiveVirtualGroup, false)

	policyPath := fmt.Sprintf("/group-links/%d/broadcast-policy", virtualGroupID)
	allowA := performVirtualGroupHandlerRequest(t, admin, http.MethodPut, "/group-links/:id/broadcast-policy", policyPath, []byte(fmt.Sprintf(`{"mode":"allow_single_source","allowed_source_group_id":%d}`, groupA.ID)), UpdateVirtualGroupBroadcastPolicy)
	requireBroadcastStatus(t, allowA, http.StatusOK)
	assertHTTPBroadcastSchedule(t, repo, scheduleA.ID, "", true)
	assertHTTPBroadcastSchedule(t, repo, scheduleB.ID, model.SuspendReasonActiveVirtualGroup, false)

	removeAPath := fmt.Sprintf("/group-links/%d/targets/%d", virtualGroupID, groupA.ID)
	removeSelected := performVirtualGroupHandlerRequest(t, admin, http.MethodDelete, "/group-links/:id/targets/:targetId", removeAPath, nil, RemoveGroupLinkTarget)
	requireBroadcastStatus(t, removeSelected, http.StatusConflict)
	if !bytes.Contains(removeSelected.Body, []byte(`"error_code":"virtual_broadcast_source_must_change"`)) {
		t.Fatalf("unexpected selected-source removal response: %s", removeSelected.Body)
	}

	suspendAll := performVirtualGroupHandlerRequest(t, admin, http.MethodPut, "/group-links/:id/broadcast-policy", policyPath, []byte(`{"mode":"suspend_all"}`), UpdateVirtualGroupBroadcastPolicy)
	requireBroadcastStatus(t, suspendAll, http.StatusOK)
	removed := performVirtualGroupHandlerRequest(t, admin, http.MethodDelete, "/group-links/:id/targets/:targetId", removeAPath, nil, RemoveGroupLinkTarget)
	requireBroadcastStatus(t, removed, http.StatusOK)
	assertHTTPBroadcastSchedule(t, repo, scheduleA.ID, "", true)

	disabled := performVirtualGroupHandlerRequest(t, admin, http.MethodPut, "/group-links/:id", statusPath, []byte(`{"status":0}`), UpdateVirtualGroup)
	requireBroadcastStatus(t, disabled, http.StatusOK)
	assertHTTPBroadcastSchedule(t, repo, scheduleB.ID, "", true)
	assertHTTPBroadcastSchedule(t, repo, scheduleC.ID, "", true)
	repeatedDisabled := performVirtualGroupHandlerRequest(t, admin, http.MethodPut, "/group-links/:id", statusPath, []byte(`{"status":0}`), UpdateVirtualGroup)
	requireBroadcastStatus(t, repeatedDisabled, http.StatusOK)
	reenabled := performVirtualGroupHandlerRequest(t, admin, http.MethodPut, "/group-links/:id", statusPath, []byte(`{"status":1}`), UpdateVirtualGroup)
	requireBroadcastStatus(t, reenabled, http.StatusOK)
	assertHTTPBroadcastSchedule(t, repo, scheduleB.ID, model.SuspendReasonActiveVirtualGroup, false)
	deleted := performVirtualGroupHandlerRequest(t, admin, http.MethodDelete, "/group-links/:id", statusPath, nil, DeleteVirtualGroup)
	requireBroadcastStatus(t, deleted, http.StatusOK)
	assertHTTPBroadcastSchedule(t, repo, scheduleB.ID, "", true)
	assertHTTPBroadcastSchedule(t, repo, scheduleC.ID, "", true)
	var virtualCount int64
	if err := db.Model(&gormdb.Group{}).Where("id = ?", virtualGroupID).Count(&virtualCount).Error; err != nil || virtualCount != 0 {
		t.Fatalf("deleted virtual group remained: count=%d err=%v", virtualCount, err)
	}
	virtualGroupID = 0
}

func performVirtualGroupHandlerRequest(t *testing.T, actor *gormdb.User, method, pattern, requestPath string, body []byte, handler gin.HandlerFunc) broadcastHTTPResult {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, pattern, func(c *gin.Context) {
		c.Set("user", actor)
		c.Set("username", actor.Name)
		handler(c)
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, requestPath, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(response, request)
	return broadcastHTTPResult{Status: response.Code, Body: response.Body.Bytes()}
}

func assertHTTPBroadcastSchedule(t *testing.T, repo *repository.Repository, scheduleID uint, suspendedReason string, hasNext bool) {
	t.Helper()
	var schedule model.BroadcastSchedule
	if err := repo.DB().First(&schedule, scheduleID).Error; err != nil {
		t.Fatal(err)
	}
	if !schedule.Enabled || schedule.SuspendedReason != suspendedReason || (schedule.NextRunAt != nil) != hasNext {
		t.Fatalf("schedule %d state enabled=%t suspended=%q next=%v", schedule.ID, schedule.Enabled, schedule.SuspendedReason, schedule.NextRunAt)
	}
}
