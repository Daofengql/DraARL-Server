package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/gormdb"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestFinishRunSurvivesInnoDBDeadlockMySQL(t *testing.T) {
	repo, owner, groups := setupRepositoryMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UTC()
	audio := createReadyAudio(t, repo, owner.ID, groups.a.ID, "deadlock-audio")
	schedule := &model.BroadcastSchedule{GroupID: groups.a.ID, AudioID: audio.ID, Name: "deadlock", ScheduleType: model.ScheduleTypeDaily, Timezone: "UTC", LocalTime: "18:00:00", Enabled: true, CreatedBy: owner.ID, UpdatedBy: owner.ID}
	if err := repo.SaveSchedule(ctx, schedule, now); err != nil {
		t.Fatal(err)
	}
	run := &model.BroadcastRun{ScheduleID: schedule.ID, AudioID: audio.ID, SourceGroupID: groups.a.ID, ScheduledFor: now, Status: model.RunStatusPlaying, StartedAt: &now, ClaimedBy: "deadlock-worker"}
	if err := repo.DB().Create(run).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.DB().Exec("CREATE TABLE broadcast_deadlock_probe (id INT PRIMARY KEY, value INT) ENGINE=InnoDB").Error; err != nil {
		t.Fatal(err)
	}
	const callback = "test:finalization_deadlock"
	t.Cleanup(func() {
		_ = repo.DB().Callback().Query().Remove(callback)
		_ = repo.DB().Exec("DROP TABLE broadcast_deadlock_probe").Error
	})
	for id := 1; id <= 20; id++ {
		if err := repo.DB().Exec("INSERT INTO broadcast_deadlock_probe VALUES (?, 0)", id).Error; err != nil {
			t.Fatal(err)
		}
	}
	competitor := repo.DB().WithContext(ctx).Begin()
	defer competitor.Rollback()
	// Give the competing transaction a larger rollback cost so InnoDB chooses
	// finalization as the victim when the two row locks form a cycle.
	if err := competitor.Exec("UPDATE broadcast_deadlock_probe SET value = value + 1").Error; err != nil {
		t.Fatal(err)
	}
	locked := make(chan struct{})
	deadlock := make(chan error, 1)
	var once sync.Once
	if err := repo.DB().Callback().Query().After("gorm:after_query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "broadcast_runs" {
			return
		}
		once.Do(func() {
			close(locked)
			err := tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE broadcast_deadlock_probe SET value = value + 1 WHERE id = 1").Error
			deadlock <- err
			if err != nil {
				tx.AddError(err)
			}
		})
	}); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		finished <- repo.FinishRun(ctx, run.ID, "deadlock-worker", model.RunStatusSucceeded, now.Add(time.Second), 1000, 1, 0, nil, "deadlock", []int{groups.a.ID}, "deadlock.raw", 20, "", "")
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := competitor.Exec("UPDATE broadcast_runs SET sent_packets = sent_packets WHERE id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := competitor.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	var mysqlError *drivermysql.MySQLError
	if err := <-deadlock; !errors.As(err, &mysqlError) || mysqlError.Number != 1213 {
		t.Fatalf("expected a real InnoDB deadlock, got %v", err)
	}
	stored, err := repo.GetRun(ctx, groups.a.ID, run.ID)
	if err != nil || stored.Status != model.RunStatusSucceeded {
		t.Fatalf("terminal state not persisted: run=%+v err=%v", stored, err)
	}
	var count int64
	if err := repo.DB().Model(&gormdb.CommRecord{}).Where("group_id = ? AND is_auto_broadcast = ?", groups.a.ID, true).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("finalization must create exactly one recording: count=%d err=%v", count, err)
	}
}
