package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
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

func TestVirtualGroupCoordinationMySQL(t *testing.T) {
	repo, owner, groups := setupRepositoryMySQL(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC)
	audioA := createReadyAudio(t, repo, owner.ID, groups.a.ID, "coord-a")
	audioB := createReadyAudio(t, repo, owner.ID, groups.b.ID, "coord-b")
	newDaily := func(groupID int, audioID uint, name string, enabled bool) *model.BroadcastSchedule {
		schedule := &model.BroadcastSchedule{
			GroupID: groupID, AudioID: audioID, Name: name, ScheduleType: model.ScheduleTypeDaily,
			Timezone: "UTC", LocalTime: "12:00:00", Enabled: enabled, CreatedBy: owner.ID, UpdatedBy: owner.ID,
		}
		if err := repo.SaveSchedule(ctx, schedule, now); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
		return schedule
	}
	dailyA := newDaily(groups.a.ID, audioA.ID, "daily A", true)
	disabledA := newDaily(groups.a.ID, audioA.ID, "disabled A", false)
	dailyB := newDaily(groups.b.ID, audioB.ID, "daily B", true)
	onceAt := now.Add(30 * time.Minute)
	onceA := &model.BroadcastSchedule{
		GroupID: groups.a.ID, AudioID: audioA.ID, Name: "once A", ScheduleType: model.ScheduleTypeOnce,
		Timezone: "UTC", ScheduledAt: &onceAt, Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID,
	}
	if err := repo.SaveSchedule(ctx, onceA, now); err != nil {
		t.Fatal(err)
	}

	for _, groupID := range []int{groups.a.ID, groups.b.ID} {
		mutation, err := repo.AddVirtualGroupMember(ctx, groups.virtual.ID, groupID, now)
		if err != nil {
			t.Fatalf("add closed member %d: %v", groupID, err)
		}
		if len(mutation.CancelGroupIDs) != 0 {
			t.Fatalf("closed interconnect requested cancellation: %#v", mutation)
		}
	}
	assertScheduleState(t, repo, dailyA.ID, true, "", true)
	assertScheduleState(t, repo, dailyB.ID, true, "", true)

	policy := &model.VirtualGroupBroadcastPolicy{
		VirtualGroupID: groups.virtual.ID, Mode: model.PolicyAllowSingleSource,
		AllowedSourceGroupID: &groups.b.ID, UpdatedBy: owner.ID,
	}
	if mutation, err := repo.UpdateVirtualGroupPolicy(ctx, policy, now); err != nil || len(mutation.CancelGroupIDs) != 0 {
		t.Fatalf("save closed policy mutation=%#v err=%v", mutation, err)
	}
	lease := now.Add(time.Minute)
	preEnableRuns := []*model.BroadcastRun{
		{ScheduleID: dailyA.ID, AudioID: audioA.ID, SourceGroupID: groups.a.ID, ScheduledFor: now.Add(-2 * time.Second), Status: model.RunStatusClaimed, ClaimedBy: "old-a", LeaseUntil: &lease},
		{ScheduleID: dailyB.ID, AudioID: audioB.ID, SourceGroupID: groups.b.ID, ScheduledFor: now.Add(-time.Second), Status: model.RunStatusPlaying, ClaimedBy: "old-b", LeaseUntil: &lease},
	}
	if err := repo.DB().Create(preEnableRuns).Error; err != nil {
		t.Fatal(err)
	}
	mutation, err := repo.SetVirtualGroupStatus(ctx, groups.virtual.ID, 1, now)
	if err != nil {
		t.Fatalf("enable virtual group: %v", err)
	}
	if len(mutation.CancelGroupIDs) != 2 {
		t.Fatalf("enable cancellation groups=%v", mutation.CancelGroupIDs)
	}
	assertScheduleState(t, repo, dailyA.ID, true, model.SuspendReasonActiveVirtualGroup, false)
	assertScheduleState(t, repo, onceA.ID, true, model.SuspendReasonActiveVirtualGroup, false)
	assertScheduleState(t, repo, dailyB.ID, true, "", true)
	assertScheduleState(t, repo, disabledA.ID, false, "", false)
	for _, run := range preEnableRuns {
		var stored model.BroadcastRun
		if err := repo.DB().First(&stored, run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Status != model.RunStatusCancelledInterconnectEnabled || stored.ErrorCode != "interconnect_changed" || stored.LeaseUntil != nil || stored.ClaimedBy != "" {
			t.Fatalf("run not cancelled by enable: %#v", stored)
		}
	}

	activeCreated := newDaily(groups.a.ID, audioA.ID, "created while active", true)
	assertScheduleState(t, repo, activeCreated.ID, true, model.SuspendReasonActiveVirtualGroup, false)
	policy = &model.VirtualGroupBroadcastPolicy{
		VirtualGroupID: groups.virtual.ID, Mode: model.PolicyAllowSingleSource,
		AllowedSourceGroupID: &groups.a.ID, UpdatedBy: owner.ID,
	}
	if _, err := repo.UpdateVirtualGroupPolicy(ctx, policy, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("switch allowed source: %v", err)
	}
	assertScheduleState(t, repo, dailyA.ID, true, "", true)
	assertScheduleState(t, repo, dailyB.ID, true, model.SuspendReasonActiveVirtualGroup, false)
	if _, err := repo.RemoveVirtualGroupMember(ctx, groups.virtual.ID, groups.a.ID, now); !errors.Is(err, ErrPolicySourceStillMember) {
		t.Fatalf("remove selected source error=%v", err)
	}

	policy = &model.VirtualGroupBroadcastPolicy{VirtualGroupID: groups.virtual.ID, Mode: model.PolicySuspendAll, UpdatedBy: owner.ID}
	if _, err := repo.UpdateVirtualGroupPolicy(ctx, policy, now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RemoveVirtualGroupMember(ctx, groups.virtual.ID, groups.a.ID, now.Add(20*time.Minute)); err != nil {
		t.Fatalf("remove non-selected source: %v", err)
	}
	assertScheduleState(t, repo, dailyA.ID, true, "", true)
	assertScheduleState(t, repo, dailyB.ID, true, model.SuspendReasonActiveVirtualGroup, false)
	if _, err := repo.AddVirtualGroupMember(ctx, groups.virtual.ID, groups.a.ID, now.Add(25*time.Minute)); err != nil {
		t.Fatalf("re-add active member: %v", err)
	}
	assertScheduleState(t, repo, dailyA.ID, true, model.SuspendReasonActiveVirtualGroup, false)

	closedAt := now.Add(time.Hour)
	if _, err := repo.SetVirtualGroupStatus(ctx, groups.virtual.ID, 0, closedAt); err != nil {
		t.Fatalf("disable virtual group: %v", err)
	}
	assertScheduleState(t, repo, dailyA.ID, true, "", true)
	assertScheduleState(t, repo, dailyB.ID, true, "", true)
	assertScheduleState(t, repo, onceA.ID, false, "", false)
	var skipped model.BroadcastRun
	if err := repo.DB().Where("schedule_id = ? AND scheduled_for = ?", onceA.ID, onceAt).First(&skipped).Error; err != nil {
		t.Fatalf("load elapsed one-time result: %v", err)
	}
	if skipped.Status != model.RunStatusSkippedInterconnected || skipped.ErrorCode != "virtual_group_broadcast_suspended" {
		t.Fatalf("unexpected elapsed one-time result: %#v", skipped)
	}
}

func assertScheduleState(t *testing.T, repo *Repository, scheduleID uint, enabled bool, suspendedReason string, hasNext bool) {
	t.Helper()
	var schedule model.BroadcastSchedule
	if err := repo.DB().First(&schedule, scheduleID).Error; err != nil {
		t.Fatal(err)
	}
	if schedule.Enabled != enabled || schedule.SuspendedReason != suspendedReason || (schedule.NextRunAt != nil) != hasNext {
		t.Fatalf("schedule %d state enabled=%t suspended=%q next=%v", scheduleID, schedule.Enabled, schedule.SuspendedReason, schedule.NextRunAt)
	}
	if suspendedReason == "" && (schedule.SuspendedByVirtualGroupID != nil || schedule.SuspendedAt != nil) {
		t.Fatalf("schedule %d retained suspension metadata: %#v", scheduleID, schedule)
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

func TestRepositorySupportsMultipleSchedulesWithSameAndDifferentAudioMySQL(t *testing.T) {
	repo, owner, groups := setupRepositoryMySQL(t)
	ctx := context.Background()
	audioA := createReadyAudio(t, repo, owner.ID, groups.a.ID, "shared-audio")
	audioB := createReadyAudio(t, repo, owner.ID, groups.a.ID, "alternate-audio")
	now := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	schedules := []*model.BroadcastSchedule{
		{GroupID: groups.a.ID, AudioID: audioA.ID, Name: "morning shared", ScheduleType: model.ScheduleTypeDaily, Timezone: "Asia/Shanghai", LocalTime: "08:00:00", Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID},
		{GroupID: groups.a.ID, AudioID: audioA.ID, Name: "evening shared", ScheduleType: model.ScheduleTypeDaily, Timezone: "Asia/Shanghai", LocalTime: "20:00:00", Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID},
		{GroupID: groups.a.ID, AudioID: audioB.ID, Name: "weekly alternate", ScheduleType: model.ScheduleTypeWeekly, Timezone: "Asia/Shanghai", LocalTime: "12:30:00", WeekdayMask: 1 << uint(time.Monday), Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID},
	}
	for _, schedule := range schedules {
		if err := repo.SaveSchedule(ctx, schedule, now); err != nil {
			t.Fatalf("save %q: %v", schedule.Name, err)
		}
		if schedule.ID == 0 || schedule.NextRunAt == nil {
			t.Fatalf("schedule was not independently persisted: %#v", schedule)
		}
	}
	stored, err := repo.ListSchedules(ctx, groups.a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("stored schedules=%d, want 3", len(stored))
	}
	counts := map[uint]int{}
	for _, schedule := range stored {
		counts[schedule.AudioID]++
	}
	if counts[audioA.ID] != 2 || counts[audioB.ID] != 1 {
		t.Fatalf("audio schedule references=%v, want shared=2 alternate=1", counts)
	}
}

func TestRepositoryClaimsAndAdvancesIndependentSchedulesMySQL(t *testing.T) {
	repo, owner, groups := setupRepositoryMySQL(t)
	ctx := context.Background()
	audioA := createReadyAudio(t, repo, owner.ID, groups.a.ID, "claim-shared")
	audioB := createReadyAudio(t, repo, owner.ID, groups.a.ID, "claim-alternate")
	now := time.Date(2000, 8, 9, 5, 0, 0, 0, time.UTC)
	schedules := make([]*model.BroadcastSchedule, 0, 12)
	for index := 0; index < 12; index++ {
		audioID := audioA.ID
		if index%3 == 0 {
			audioID = audioB.ID
		}
		schedule := &model.BroadcastSchedule{
			GroupID: groups.a.ID, AudioID: audioID, Name: fmt.Sprintf("claim-%d", index),
			ScheduleType: model.ScheduleTypeDaily, Timezone: "Asia/Shanghai", LocalTime: "13:30:00",
			Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID,
		}
		if err := repo.SaveSchedule(ctx, schedule, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		due := now.Add(-time.Second)
		if err := repo.DB().Model(schedule).Update("next_run_at", due).Error; err != nil {
			t.Fatal(err)
		}
		schedules = append(schedules, schedule)
	}

	var wg sync.WaitGroup
	claimedCh := make(chan []model.BroadcastRun, 8)
	errCh := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			claimed, err := repo.ClaimDue(ctx, now, fmt.Sprintf("worker-%d", worker), 5*time.Second, 10*time.Second, 12)
			if err != nil {
				errCh <- err
				return
			}
			claimedCh <- claimed
		}(worker)
	}
	wg.Wait()
	close(claimedCh)
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	claimedBySchedule := make(map[uint]model.BroadcastRun)
	for batch := range claimedCh {
		for _, run := range batch {
			if _, duplicate := claimedBySchedule[run.ScheduleID]; duplicate {
				t.Fatalf("schedule %d was claimed twice", run.ScheduleID)
			}
			claimedBySchedule[run.ScheduleID] = run
		}
	}
	if len(claimedBySchedule) != len(schedules) {
		t.Fatalf("claimed schedules=%d want=%d", len(claimedBySchedule), len(schedules))
	}
	for _, schedule := range schedules {
		var stored model.BroadcastSchedule
		if err := repo.DB().First(&stored, schedule.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.NextRunAt == nil || !stored.NextRunAt.After(now) || !stored.Enabled {
			t.Fatalf("schedule was not advanced to the future: %#v", stored)
		}
		if run := claimedBySchedule[schedule.ID]; run.AudioID != schedule.AudioID || !run.ScheduledFor.Equal(now.Add(-time.Second)) {
			t.Fatalf("independent run mismatch: %#v", run)
		}
	}
	if duplicate, err := repo.ClaimDue(ctx, now, "late-worker", 5*time.Second, 10*time.Second, 100); err != nil || len(duplicate) != 0 {
		t.Fatalf("second claim=%d err=%v", len(duplicate), err)
	}
}

func TestRepositoryRecoveryWindowAndRunLeaseMySQL(t *testing.T) {
	repo, owner, groups := setupRepositoryMySQL(t)
	ctx := context.Background()
	audio := createReadyAudio(t, repo, owner.ID, groups.a.ID, "recovery-audio")
	now := time.Date(2001, 8, 9, 6, 0, 0, 0, time.UTC)

	onceAt := now.Add(time.Hour)
	once := &model.BroadcastSchedule{
		GroupID: groups.a.ID, AudioID: audio.ID, Name: "stale-once", ScheduleType: model.ScheduleTypeOnce,
		Timezone: "UTC", ScheduledAt: &onceAt, Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID,
	}
	if err := repo.SaveSchedule(ctx, once, now); err != nil {
		t.Fatal(err)
	}
	staleAt := now.Add(-11 * time.Second)
	if err := repo.DB().Model(once).Update("next_run_at", staleAt).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimDue(ctx, now, "worker-a", 5*time.Second, 10*time.Second, 10)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("stale once claim=%d err=%v", len(claimed), err)
	}
	var storedOnce model.BroadcastSchedule
	if err := repo.DB().First(&storedOnce, once.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOnce.Enabled || storedOnce.NextRunAt != nil {
		t.Fatalf("stale once schedule remained active: %#v", storedOnce)
	}
	var staleRun model.BroadcastRun
	if err := repo.DB().Where("schedule_id = ? AND scheduled_for = ?", once.ID, staleAt).First(&staleRun).Error; err != nil {
		t.Fatal(err)
	}
	if staleRun.Status != model.RunStatusFailed || staleRun.ErrorCode != "recovery_window_expired" {
		t.Fatalf("stale run=%#v", staleRun)
	}

	daily := &model.BroadcastSchedule{
		GroupID: groups.a.ID, AudioID: audio.ID, Name: "recover-daily", ScheduleType: model.ScheduleTypeDaily,
		Timezone: "UTC", LocalTime: "07:00:00", Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID,
	}
	if err := repo.SaveSchedule(ctx, daily, now); err != nil {
		t.Fatal(err)
	}
	expiredLease := now.Add(-time.Second)
	runs := []model.BroadcastRun{
		{ScheduleID: daily.ID, AudioID: audio.ID, SourceGroupID: groups.a.ID, ScheduledFor: now.Add(-5 * time.Second), Status: model.RunStatusClaimed, ClaimedBy: "dead-a", LeaseUntil: &expiredLease},
		{ScheduleID: daily.ID, AudioID: audio.ID, SourceGroupID: groups.a.ID, ScheduledFor: now.Add(-20 * time.Second), Status: model.RunStatusClaimed, ClaimedBy: "dead-b", LeaseUntil: &expiredLease},
		{ScheduleID: daily.ID, AudioID: audio.ID, SourceGroupID: groups.a.ID, ScheduledFor: now.Add(-4 * time.Second), Status: model.RunStatusPlaying, ClaimedBy: "dead-c", LeaseUntil: &expiredLease},
	}
	if err := repo.DB().Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := repo.RecoverExpiredRuns(ctx, now, "worker-b", 5*time.Second, 10*time.Second, 10)
	if err != nil || len(recovered) != 1 || recovered[0].ID != runs[0].ID {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	var staleClaim, crashedPlaying model.BroadcastRun
	if err := repo.DB().First(&staleClaim, runs[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.DB().First(&crashedPlaying, runs[2].ID).Error; err != nil {
		t.Fatal(err)
	}
	if staleClaim.Status != model.RunStatusFailed || staleClaim.ErrorCode != "recovery_window_expired" ||
		crashedPlaying.Status != model.RunStatusFailed || crashedPlaying.ErrorCode != "worker_lost_during_playback" {
		t.Fatalf("recovery terminal states stale=%#v playing=%#v", staleClaim, crashedPlaying)
	}

	execution, code, err := repo.LoadClaimedExecution(ctx, runs[0].ID, "worker-b", now)
	if err != nil || code != "" || execution.Audio.ID != audio.ID {
		t.Fatalf("load execution=%#v code=%q err=%v", execution, code, err)
	}
	if code, err := repo.MarkRunPlaying(ctx, runs[0].ID, "worker-b", "1,2", []int{groups.b.ID, groups.a.ID, groups.a.ID}, now, 5*time.Second); err != nil || code != "" {
		t.Fatalf("mark playing code=%q err=%v", code, err)
	}
	if code, err := repo.ValidateAndRenewRun(ctx, runs[0].ID, "worker-b", now.Add(time.Second), 5*time.Second); err != nil || code != "" {
		t.Fatalf("validate active run code=%q err=%v", code, err)
	}
	if err := repo.DB().Model(daily).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if code, err := repo.ValidateAndRenewRun(ctx, runs[0].ID, "worker-b", now.Add(2*time.Second), 5*time.Second); err != nil || code != "schedule_disabled" {
		t.Fatalf("validate disabled schedule code=%q err=%v", code, err)
	}
	if err := repo.FinishRun(ctx, runs[0].ID, "worker-b", model.RunStatusCancelled, now.Add(2*time.Second), 120, 1, 0, nil, "1,2", []int{groups.a.ID, groups.b.ID}, "schedule_disabled", "schedule disabled during playback"); err != nil {
		t.Fatal(err)
	}
	var finished model.BroadcastRun
	if err := repo.DB().First(&finished, runs[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if finished.Status != model.RunStatusCancelled || finished.PlayedDurationMS != 120 || finished.SentPackets != 1 || finished.ClaimedBy != "" || finished.LeaseUntil != nil || len(finished.DomainGroupIDs) != 2 || finished.DomainGroupIDs[0] != groups.a.ID {
		t.Fatalf("finished run=%#v", finished)
	}
}

func TestRepositoryManualClaimsAreIndependentAndDoNotAdvanceScheduleMySQL(t *testing.T) {
	repo, owner, groups := setupRepositoryMySQL(t)
	ctx := context.Background()
	audio := createReadyAudio(t, repo, owner.ID, groups.a.ID, "manual-audio")
	now := time.Date(2002, 8, 9, 7, 0, 0, 0, time.UTC)
	schedule := &model.BroadcastSchedule{
		GroupID: groups.a.ID, AudioID: audio.ID, Name: "manual-daily", ScheduleType: model.ScheduleTypeDaily,
		Timezone: "Asia/Shanghai", LocalTime: "18:00:00", Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID,
	}
	if err := repo.SaveSchedule(ctx, schedule, now); err != nil {
		t.Fatal(err)
	}
	originalNext := *schedule.NextRunAt

	var wg sync.WaitGroup
	runs := make(chan *model.BroadcastRun, 10)
	errs := make(chan error, 10)
	for index := 0; index < 10; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			run, code, err := repo.ClaimManualRun(ctx, groups.a.ID, schedule.ID, now, fmt.Sprintf("manual-%d", index), 5*time.Second)
			if err != nil {
				errs <- err
				return
			}
			if code != "" {
				errs <- fmt.Errorf("manual code %s", code)
				return
			}
			runs <- run
		}(index)
	}
	wg.Wait()
	close(runs)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	occurrences := make(map[int64]struct{})
	for run := range runs {
		occurrences[run.ScheduledFor.UnixMilli()] = struct{}{}
	}
	if len(occurrences) != 10 {
		t.Fatalf("manual occurrence keys=%d want=10", len(occurrences))
	}
	var stored model.BroadcastSchedule
	if err := repo.DB().First(&stored, schedule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.NextRunAt == nil || !stored.NextRunAt.Equal(originalNext) {
		t.Fatalf("manual trigger advanced schedule: before=%v after=%v", originalNext, stored.NextRunAt)
	}

	if err := repo.DB().Create(&gormdb.GroupLink{LinkGroupID: groups.virtual.ID, TargetGroupID: groups.a.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePolicy(ctx, &model.VirtualGroupBroadcastPolicy{VirtualGroupID: groups.virtual.ID, Mode: model.PolicySuspendAll, UpdatedBy: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DB().Model(&gormdb.Group{}).Where("id = ?", groups.virtual.ID).Update("status", 1).Error; err != nil {
		t.Fatal(err)
	}
	if run, code, err := repo.ClaimManualRun(ctx, groups.a.ID, schedule.ID, now.Add(time.Second), "manual-blocked", 5*time.Second); err != nil || run != nil || code != "virtual_group_broadcast_suspended" {
		t.Fatalf("suspended manual run=%#v code=%q err=%v", run, code, err)
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
		if virtual {
			if err := gormdb.Get().Model(group).Update("status", 0).Error; err != nil {
				t.Fatal(err)
			}
			group.Status = 0
		}
		return group
	}
	groups := repositoryGroups{a: newGroup("A", false), b: newGroup("B", false), outside: newGroup("outside", false), virtual: newGroup("virtual", true)}
	t.Cleanup(func() {
		groupIDs := []int{groups.a.ID, groups.b.ID, groups.outside.ID, groups.virtual.ID}
		_ = gormdb.Get().Where("source_group_id IN ?", groupIDs).Delete(&model.BroadcastRun{}).Error
		_ = gormdb.Get().Where("group_id IN ?", groupIDs).Delete(&model.BroadcastSchedule{}).Error
		_ = gormdb.Get().Where("group_id IN ?", groupIDs).Delete(&model.BroadcastAudio{}).Error
		_ = gormdb.Get().Where("virtual_group_id = ?", groups.virtual.ID).Delete(&model.VirtualGroupBroadcastPolicy{}).Error
		_ = gormdb.Get().Where("link_group_id = ? OR target_group_id IN ?", groups.virtual.ID, groupIDs).Delete(&gormdb.GroupLink{}).Error
		_ = gormdb.Get().Delete(&gormdb.Group{}, groupIDs).Error
		_ = gormdb.Get().Delete(&gormdb.User{}, owner.ID).Error
	})
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
