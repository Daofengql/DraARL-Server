package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/gormdb"
)

func TestRepositorySchedulePolicyMySQL(t *testing.T) {
	repo, owner, groups := setupRepositoryMySQL(t)
	ctx := context.Background()

	audioA := createReadyAudio(t, repo, owner.ID, groups.a.ID, "audio-a")
	audioB := createReadyAudio(t, repo, owner.ID, groups.b.ID, "audio-b")
	now := time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC)

	scheduleA := &model.BroadcastSchedule{
		GroupID: groups.a.ID, AudioID: audioA.ID, Name: "A daily", ScheduleType: model.ScheduleTypeDaily,
		Timezone: "Asia/Shanghai", LocalTime: "13:00:00", Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID,
	}
	if err := repo.SaveSchedule(ctx, scheduleA, now); err != nil {
		t.Fatalf("save schedule before interconnect: %v", err)
	}
	if scheduleA.NextRunAt == nil || scheduleA.SuspendedReason != "" {
		t.Fatalf("inactive interconnect changed schedule: %#v", scheduleA)
	}

	if err := repo.DB().Create(&gormdb.GroupLink{LinkGroupID: groups.virtual.ID, TargetGroupID: groups.a.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.DB().Create(&gormdb.GroupLink{LinkGroupID: groups.virtual.ID, TargetGroupID: groups.b.ID}).Error; err != nil {
		t.Fatal(err)
	}
	policy := &model.VirtualGroupBroadcastPolicy{VirtualGroupID: groups.virtual.ID, Mode: model.PolicyAllowSingleSource, AllowedSourceGroupID: &groups.b.ID, UpdatedBy: owner.ID}
	if err := repo.SavePolicy(ctx, policy); err != nil {
		t.Fatalf("save allow-single policy: %v", err)
	}
	if err := repo.DB().Model(&gormdb.Group{}).Where("id = ?", groups.virtual.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}

	scheduleA.Name = "A daily updated"
	if err := repo.SaveSchedule(ctx, scheduleA, now); err != nil {
		t.Fatalf("save suspended schedule: %v", err)
	}
	if !scheduleA.Enabled || scheduleA.NextRunAt != nil || scheduleA.SuspendedReason != model.SuspendReasonActiveVirtualGroup ||
		scheduleA.SuspendedByVirtualGroupID == nil || *scheduleA.SuspendedByVirtualGroupID != groups.virtual.ID {
		t.Fatalf("A schedule was not suspended: %#v", scheduleA)
	}

	scheduleB := &model.BroadcastSchedule{
		GroupID: groups.b.ID, AudioID: audioB.ID, Name: "B daily", ScheduleType: model.ScheduleTypeDaily,
		Timezone: "Asia/Shanghai", LocalTime: "13:00:00", Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID,
	}
	if err := repo.SaveSchedule(ctx, scheduleB, now); err != nil {
		t.Fatalf("save allowed schedule: %v", err)
	}
	if scheduleB.NextRunAt == nil || scheduleB.SuspendedReason != "" {
		t.Fatalf("allowed schedule was suspended: %#v", scheduleB)
	}

	disabled := &model.BroadcastSchedule{
		GroupID: groups.a.ID, AudioID: audioA.ID, Name: "disabled", ScheduleType: model.ScheduleTypeDaily,
		Timezone: "Asia/Shanghai", LocalTime: "13:00:00", Enabled: false, CreatedBy: owner.ID, UpdatedBy: owner.ID,
	}
	if err := repo.SaveSchedule(ctx, disabled, now); err != nil {
		t.Fatalf("save disabled schedule: %v", err)
	}
	if disabled.NextRunAt != nil || disabled.SuspendedReason != "" || disabled.SuspendedByVirtualGroupID != nil {
		t.Fatalf("disabled schedule gained runtime suspension: %#v", disabled)
	}

	stats, err := repo.ListPolicyMemberStats(ctx, groups.virtual.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].EnabledCount+stats[1].EnabledCount != 2 || stats[0].SuspendedCount+stats[1].SuspendedCount != 1 {
		t.Fatalf("unexpected member stats: %#v", stats)
	}
}

func TestRepositoryOwnershipAndDeleteProtectionMySQL(t *testing.T) {
	repo, owner, groups := setupRepositoryMySQL(t)
	ctx := context.Background()
	audioA := createReadyAudio(t, repo, owner.ID, groups.a.ID, "audio-a")
	audioB := createReadyAudio(t, repo, owner.ID, groups.b.ID, "audio-b")
	now := time.Now().UTC()

	crossGroup := &model.BroadcastSchedule{
		GroupID: groups.a.ID, AudioID: audioB.ID, Name: "wrong audio", ScheduleType: model.ScheduleTypeDaily,
		Timezone: "Asia/Shanghai", LocalTime: "12:00:00", Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID,
	}
	if err := repo.SaveSchedule(ctx, crossGroup, now); !errors.Is(err, ErrAudioGroupMismatch) {
		t.Fatalf("cross-group audio error = %v, want ErrAudioGroupMismatch", err)
	}

	schedule := &model.BroadcastSchedule{
		GroupID: groups.a.ID, AudioID: audioA.ID, Name: "in use", ScheduleType: model.ScheduleTypeDaily,
		Timezone: "Asia/Shanghai", LocalTime: "12:00:00", Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID,
	}
	if err := repo.SaveSchedule(ctx, schedule, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.DeleteAudio(ctx, groups.a.ID, audioA.ID); !errors.Is(err, ErrAudioInUse) {
		t.Fatalf("delete referenced audio error = %v, want ErrAudioInUse", err)
	}
	if err := repo.DeleteSchedule(ctx, groups.a.ID, schedule.ID); err != nil {
		t.Fatal(err)
	}
	original, playback, err := repo.DeleteAudio(ctx, groups.a.ID, audioA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if original == "" || playback == "" {
		t.Fatalf("delete did not return object keys: %q, %q", original, playback)
	}
}

type repositoryGroups struct {
	a       *gormdb.Group
	b       *gormdb.Group
	outside *gormdb.Group
	virtual *gormdb.Group
}

func setupRepositoryMySQL(t *testing.T) (*Repository, *gormdb.User, repositoryGroups) {
	t.Helper()
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_BROADCAST_REPOSITORY_E2E")), "true") {
		t.Skip("set DRAARL_BROADCAST_REPOSITORY_E2E=true and DRAARL_TEST_MYSQL_DSN to run repository E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("DRAARL_TEST_MYSQL_DSN is required")
	}
	if err := gormdb.Init(&gormdb.Config{DSN: dsn, MaxOpenConns: 10, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "error"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	if err := gormdb.AutoMigrate(); err != nil {
		t.Fatal(err)
	}

	stamp := time.Now().UnixNano()
	owner := &gormdb.User{Name: fmt.Sprintf("broadcast-owner-%d", stamp), Email: fmt.Sprintf("broadcast-%d@example.invalid", stamp), CallSign: fmt.Sprintf("B%07d", stamp%10_000_000), Roles: "admin", Status: 1}
	if err := gormdb.Get().Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	newGroup := func(name string, virtual bool) *gormdb.Group {
		status := 1
		if virtual {
			status = 0
		}
		group := &gormdb.Group{Name: fmt.Sprintf("%s-%d", name, stamp), Type: 1, OwerID: owner.ID, Status: status, IsVirtual: virtual}
		if err := gormdb.Get().Create(group).Error; err != nil {
			t.Fatal(err)
		}
		return group
	}
	groups := repositoryGroups{a: newGroup("A", false), b: newGroup("B", false), outside: newGroup("outside", false), virtual: newGroup("virtual", true)}
	return New(gormdb.Get()), owner, groups
}

func createReadyAudio(t *testing.T, repo *Repository, ownerID, groupID int, name string) *model.BroadcastAudio {
	t.Helper()
	audio := &model.BroadcastAudio{
		GroupID: groupID, Name: name, OriginalObjectKey: name + ".wav", PlaybackObjectKey: name + ".dabr",
		OriginalMIMEType: "audio/wav", OriginalSize: 10, PlaybackSize: 8, DurationMS: 1000,
		PacketCount: 8, SHA256: strings.Repeat("a", 64), Status: model.AudioStatusReady, CreatedBy: ownerID,
	}
	if err := repo.CreateAudio(context.Background(), audio); err != nil {
		t.Fatal(err)
	}
	return audio
}
