package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"draarl/internal/broadcast/identity"
	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/scheduler"
	"draarl/internal/gormdb"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNotFound                = errors.New("broadcast resource not found")
	ErrInvalidEntityGroup      = errors.New("broadcast target must be an entity group")
	ErrInvalidAudio            = errors.New("invalid broadcast audio")
	ErrAudioNotReady           = errors.New("broadcast audio is not ready")
	ErrAudioGroupMismatch      = errors.New("broadcast audio belongs to another group")
	ErrAudioInUse              = errors.New("broadcast audio is referenced by a schedule")
	ErrInvalidSchedule         = errors.New("invalid broadcast schedule")
	ErrInvalidPolicy           = errors.New("invalid virtual group broadcast policy")
	ErrPolicySourceNotMember   = errors.New("virtual group broadcast source is not a member")
	ErrPolicySourceStillMember = errors.New("virtual group broadcast source must be changed before removing the member")
	ErrVirtualGroupRequired    = errors.New("virtual group is required")
	ErrManualTriggerSuspended  = errors.New("virtual group broadcast suspended")
	ErrRunLeaseLost            = errors.New("broadcast run lease lost")
	ErrRunNotRunnable          = errors.New("broadcast run is not runnable")
)

type Repository struct {
	db             *gorm.DB
	operationalKey string
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db, operationalKey: OperationalEnabledKey}
}

func Default() *Repository {
	return New(gormdb.Get())
}

func (r *Repository) DB() *gorm.DB { return r.db }

func (r *Repository) ListAudios(ctx context.Context, groupID int) ([]model.BroadcastAudio, error) {
	var audios []model.BroadcastAudio
	err := r.db.WithContext(ctx).Where("group_id = ?", groupID).Order("id DESC").Find(&audios).Error
	return audios, err
}

func (r *Repository) AudioScheduleCounts(ctx context.Context, groupID int) (map[uint]int64, error) {
	type countRow struct {
		AudioID uint
		Count   int64
	}
	var rows []countRow
	err := r.db.WithContext(ctx).Model(&model.BroadcastSchedule{}).
		Select("audio_id, COUNT(*) AS count").
		Where("group_id = ?", groupID).
		Group("audio_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[uint]int64, len(rows))
	for _, row := range rows {
		counts[row.AudioID] = row.Count
	}
	return counts, nil
}

func (r *Repository) GetAudio(ctx context.Context, groupID int, audioID uint) (*model.BroadcastAudio, error) {
	var audio model.BroadcastAudio
	err := r.db.WithContext(ctx).Where("group_id = ? AND id = ?", groupID, audioID).First(&audio).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &audio, err
}

func (r *Repository) GetAudioByID(ctx context.Context, audioID uint) (*model.BroadcastAudio, error) {
	var audio model.BroadcastAudio
	err := r.db.WithContext(ctx).First(&audio, audioID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &audio, err
}

func (r *Repository) ListProcessingAudios(ctx context.Context, limit int) ([]model.BroadcastAudio, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	var audios []model.BroadcastAudio
	err := r.db.WithContext(ctx).Where("status = ?", model.AudioStatusProcessing).Order("id ASC").Limit(limit).Find(&audios).Error
	return audios, err
}

func (r *Repository) CreateAudio(ctx context.Context, audio *model.BroadcastAudio) error {
	if audio == nil || audio.GroupID <= 0 || audio.CreatedBy <= 0 || strings.TrimSpace(audio.Name) == "" {
		return ErrInvalidAudio
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntityGroup(tx, audio.GroupID); err != nil {
			return err
		}
		return tx.Create(audio).Error
	})
}

func (r *Repository) MarkAudioReady(ctx context.Context, audioID uint, playbackKey string, playbackSize int64, durationMS, packetCount int) error {
	result := r.db.WithContext(ctx).Model(&model.BroadcastAudio{}).
		Where("id = ? AND status = ?", audioID, model.AudioStatusProcessing).
		Updates(map[string]any{
			"playback_object_key": strings.TrimSpace(playbackKey),
			"playback_size":       playbackSize,
			"duration_ms":         durationMS,
			"packet_count":        packetCount,
			"status":              model.AudioStatusReady,
			"error_message":       "",
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) MarkAudioFailed(ctx context.Context, audioID uint, safeMessage string) error {
	result := r.db.WithContext(ctx).Model(&model.BroadcastAudio{}).Where("id = ?", audioID).Updates(map[string]any{
		"status":        model.AudioStatusFailed,
		"error_message": truncate(strings.TrimSpace(safeMessage), 500),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAudio returns the object keys that should be deleted after the
// metadata transaction commits.
func (r *Repository) DeleteAudio(ctx context.Context, groupID int, audioID uint) (string, string, error) {
	var originalKey, playbackKey string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var audio model.BroadcastAudio
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND id = ?", groupID, audioID).First(&audio).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		var count int64
		if err := tx.Model(&model.BroadcastSchedule{}).Where("audio_id = ?", audio.ID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrAudioInUse
		}
		if err := tx.Delete(&audio).Error; err != nil {
			return err
		}
		originalKey, playbackKey = audio.OriginalObjectKey, audio.PlaybackObjectKey
		return nil
	})
	return originalKey, playbackKey, err
}

func (r *Repository) ListSchedules(ctx context.Context, groupID int) ([]model.BroadcastSchedule, error) {
	var schedules []model.BroadcastSchedule
	err := r.db.WithContext(ctx).Where("group_id = ?", groupID).Order("id DESC").Find(&schedules).Error
	return schedules, err
}

func (r *Repository) GetSchedule(ctx context.Context, groupID int, scheduleID uint) (*model.BroadcastSchedule, error) {
	var schedule model.BroadcastSchedule
	err := r.db.WithContext(ctx).Where("group_id = ? AND id = ?", groupID, scheduleID).First(&schedule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &schedule, err
}

func (r *Repository) SaveSchedule(ctx context.Context, schedule *model.BroadcastSchedule, now time.Time) error {
	if schedule == nil || schedule.GroupID <= 0 || schedule.AudioID == 0 || schedule.CreatedBy <= 0 || schedule.UpdatedBy <= 0 {
		return ErrInvalidSchedule
	}
	if err := normalizeSchedule(schedule); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntityGroup(tx, schedule.GroupID); err != nil {
			return err
		}
		if err := validateAudioForSchedule(tx, schedule.GroupID, schedule.AudioID); err != nil {
			return err
		}
		if schedule.ID != 0 {
			var existing model.BroadcastSchedule
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("group_id = ? AND id = ?", schedule.GroupID, schedule.ID).First(&existing).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrNotFound
				}
				return err
			}
			schedule.CreatedBy = existing.CreatedBy
			schedule.CreatedAt = existing.CreatedAt
		}
		if err := applyScheduleRuntimeState(tx, schedule, now); err != nil {
			return err
		}
		return tx.Save(schedule).Error
	})
}

func (r *Repository) DeleteSchedule(ctx context.Context, groupID int, scheduleID uint) error {
	result := r.db.WithContext(ctx).Where("group_id = ? AND id = ?", groupID, scheduleID).Delete(&model.BroadcastSchedule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListRuns(ctx context.Context, groupID int, page, pageSize int) ([]model.BroadcastRun, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := r.db.WithContext(ctx).Model(&model.BroadcastRun{}).Where("source_group_id = ?", groupID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var runs []model.BroadcastRun
	err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&runs).Error
	return runs, total, err
}

func (r *Repository) GetRun(ctx context.Context, groupID int, runID uint) (*model.BroadcastRun, error) {
	var run model.BroadcastRun
	err := r.db.WithContext(ctx).Where("source_group_id = ? AND id = ?", groupID, runID).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &run, err
}

// ClaimDue atomically advances each due schedule and creates at most one run
// for its current theoretical occurrence. SKIP LOCKED permits multiple centre
// workers during rolling upgrades while the unique occurrence key remains the
// final duplicate-execution guard.
func (r *Repository) ClaimDue(ctx context.Context, now time.Time, claimedBy string, leaseDuration, recoveryWindow time.Duration, limit int) ([]model.BroadcastRun, error) {
	claimedBy = strings.TrimSpace(claimedBy)
	if claimedBy == "" || leaseDuration <= 0 || recoveryWindow < 0 || limit <= 0 {
		return nil, ErrInvalidSchedule
	}
	if limit > 100 {
		limit = 100
	}
	now = now.UTC()
	leaseUntil := now.Add(leaseDuration)
	claimed := make([]model.BroadcastRun, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		enabled, err := r.operationalEnabled(tx, true)
		if err != nil || !enabled {
			return err
		}
		// Discover candidates without locks, then take the shared coordination
		// locks in entity-group order before locking schedules. A candidate that
		// changes meanwhile is filtered by the second query and retried next scan.
		type dueCandidate struct {
			ID      uint
			GroupID int
		}
		var candidates []dueCandidate
		if err := tx.Model(&model.BroadcastSchedule{}).Select("id, group_id").
			Where("enabled = ? AND suspended_reason = '' AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
			Order("next_run_at ASC, id ASC").Limit(limit).Find(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		groupIDs := make([]int, 0, len(candidates))
		scheduleIDs := make([]uint, 0, len(candidates))
		for _, candidate := range candidates {
			groupIDs = append(groupIDs, candidate.GroupID)
			scheduleIDs = append(scheduleIDs, candidate.ID)
		}
		if err := lockEntityGroups(tx, groupIDs); err != nil {
			return err
		}
		var schedules []model.BroadcastSchedule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id IN ? AND enabled = ? AND suspended_reason = '' AND next_run_at IS NOT NULL AND next_run_at <= ?", scheduleIDs, true, now).
			Order("next_run_at ASC, id ASC").Limit(limit).Find(&schedules).Error; err != nil {
			return err
		}
		for index := range schedules {
			schedule := &schedules[index]
			scheduledFor := schedule.NextRunAt.UTC()
			if err := advanceClaimedSchedule(tx, schedule, now); err != nil {
				return err
			}
			run := model.BroadcastRun{
				ScheduleID: schedule.ID, AudioID: schedule.AudioID, SourceGroupID: schedule.GroupID,
				ScheduledFor: scheduledFor, Status: model.RunStatusClaimed,
				ClaimedBy: claimedBy, LeaseUntil: &leaseUntil,
			}
			if scheduledFor.Before(now.Add(-recoveryWindow)) {
				run.Status = model.RunStatusFailed
				run.ClaimedBy = ""
				run.LeaseUntil = nil
				run.EndedAt = &now
				run.ErrorCode = "recovery_window_expired"
				run.ErrorMessage = "scheduled occurrence exceeded the recovery window"
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&run)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 1 && run.Status == model.RunStatusClaimed {
				claimed = append(claimed, run)
			}
		}
		return nil
	})
	return claimed, err
}

func advanceClaimedSchedule(tx *gorm.DB, schedule *model.BroadcastSchedule, now time.Time) error {
	if schedule.ScheduleType == model.ScheduleTypeOnce {
		schedule.Enabled = false
		schedule.NextRunAt = nil
	} else {
		next, err := scheduler.NextOccurrence(schedule, now)
		if err != nil || next == nil {
			if err == nil {
				err = errors.New("schedule has no future occurrence")
			}
			return fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
		}
		schedule.NextRunAt = next
	}
	return tx.Model(&model.BroadcastSchedule{}).Where("id = ?", schedule.ID).Updates(map[string]any{
		"enabled": schedule.Enabled, "next_run_at": schedule.NextRunAt,
	}).Error
}

// RecoverExpiredRuns never replays a run that had entered playing. Claimed
// runs may be transferred only while their theoretical occurrence remains in
// the bounded recovery window.
func (r *Repository) RecoverExpiredRuns(ctx context.Context, now time.Time, claimedBy string, leaseDuration, recoveryWindow time.Duration, limit int) ([]model.BroadcastRun, error) {
	claimedBy = strings.TrimSpace(claimedBy)
	if claimedBy == "" || leaseDuration <= 0 || recoveryWindow < 0 || limit <= 0 {
		return nil, ErrInvalidSchedule
	}
	if limit > 100 {
		limit = 100
	}
	now = now.UTC()
	leaseUntil := now.Add(leaseDuration)
	recovered := make([]model.BroadcastRun, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		enabled, err := r.operationalEnabled(tx, true)
		if err != nil || !enabled {
			return err
		}
		if err := tx.Model(&model.BroadcastRun{}).
			Where("status = ? AND (lease_until IS NULL OR lease_until <= ?)", model.RunStatusPlaying, now).
			Updates(map[string]any{
				"status": model.RunStatusFailed, "ended_at": now, "claimed_by": "", "lease_until": nil,
				"error_code": "worker_lost_during_playback", "error_message": "playback worker lease expired",
			}).Error; err != nil {
			return err
		}
		var runs []model.BroadcastRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND (lease_until IS NULL OR lease_until <= ?)", model.RunStatusClaimed, now).
			Order("scheduled_for ASC, id ASC").Limit(limit).Find(&runs).Error; err != nil {
			return err
		}
		for index := range runs {
			run := &runs[index]
			if run.ScheduledFor.Before(now.Add(-recoveryWindow)) {
				if err := tx.Model(run).Updates(map[string]any{
					"status": model.RunStatusFailed, "ended_at": now, "claimed_by": "", "lease_until": nil,
					"error_code": "recovery_window_expired", "error_message": "claimed run exceeded the recovery window",
				}).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Model(run).Updates(map[string]any{"claimed_by": claimedBy, "lease_until": leaseUntil}).Error; err != nil {
				return err
			}
			run.ClaimedBy, run.LeaseUntil = claimedBy, &leaseUntil
			recovered = append(recovered, *run)
		}
		return nil
	})
	return recovered, err
}

// ClaimManualRun creates an immediate execution without advancing or otherwise
// changing the schedule's future occurrence. The schedule row serializes
// simultaneous manual requests so their millisecond occurrence keys remain
// unique even when they arrive together.
func (r *Repository) ClaimManualRun(ctx context.Context, groupID int, scheduleID uint, now time.Time, claimedBy string, leaseDuration time.Duration) (*model.BroadcastRun, string, error) {
	claimedBy = strings.TrimSpace(claimedBy)
	if groupID <= 0 || scheduleID == 0 || claimedBy == "" || leaseDuration <= 0 {
		return nil, "", ErrInvalidSchedule
	}
	now = now.UTC().Truncate(time.Millisecond)
	leaseUntil := now.Add(leaseDuration)
	var claimed model.BroadcastRun
	code := ""
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		enabled, stateErr := r.operationalEnabled(tx, true)
		if stateErr != nil {
			return stateErr
		}
		if !enabled {
			code = "site_broadcast_disabled"
			return nil
		}
		var group gormdb.Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, groupID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if group.Status != 1 || group.IsVirtual || (group.Type != 1 && group.Type != 2) {
			code = "group_unavailable"
			return nil
		}
		var schedule model.BroadcastSchedule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND group_id = ?", scheduleID, groupID).First(&schedule).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if !schedule.Enabled {
			code = "schedule_disabled"
			return nil
		}
		if schedule.SuspendedReason != "" {
			code = "virtual_group_broadcast_suspended"
			return nil
		}
		if err := validateAudioForSchedule(tx, groupID, schedule.AudioID); err != nil {
			if errors.Is(err, ErrAudioNotReady) || errors.Is(err, ErrAudioGroupMismatch) {
				code = "audio_unavailable"
				return nil
			}
			return err
		}
		policy, err := activePolicyForEntityGroup(tx, groupID)
		if err != nil {
			return err
		}
		if policy != nil && (policy.Mode != model.PolicyAllowSingleSource || policy.AllowedSourceGroupID == nil || *policy.AllowedSourceGroupID != groupID) {
			code = "virtual_group_broadcast_suspended"
			return nil
		}

		candidate := now
		for attempt := 0; attempt < 1000; attempt++ {
			var count int64
			if err := tx.Model(&model.BroadcastRun{}).Where("schedule_id = ? AND scheduled_for = ?", schedule.ID, candidate).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				claimed = model.BroadcastRun{
					ScheduleID: schedule.ID, AudioID: schedule.AudioID, SourceGroupID: groupID,
					ScheduledFor: candidate, Status: model.RunStatusClaimed,
					ClaimedBy: claimedBy, LeaseUntil: &leaseUntil,
				}
				return tx.Create(&claimed).Error
			}
			candidate = candidate.Add(time.Millisecond)
		}
		return errors.New("manual broadcast occurrence key exhausted")
	})
	if err != nil || code != "" {
		return nil, code, err
	}
	return &claimed, "", nil
}

func (r *Repository) LoadClaimedExecution(ctx context.Context, runID uint, claimedBy string, now time.Time) (*scheduler.RunExecution, string, error) {
	sourceGroupID, err := r.runSourceGroupID(ctx, runID)
	if err != nil {
		return nil, "", err
	}
	var execution scheduler.RunExecution
	code := ""
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		code, err = r.lockExecutionGuards(tx, sourceGroupID)
		if err != nil || code != "" {
			return err
		}
		execution, err = loadRunExecution(tx, runID)
		if err != nil {
			return err
		}
		code, err = r.validateExecutionEligibility(tx, &execution, claimedBy, now.UTC())
		return err
	})
	if err != nil {
		return nil, "", err
	}
	if code != "" {
		return nil, code, nil
	}
	return &execution, "", nil
}

func (r *Repository) runSourceGroupID(ctx context.Context, runID uint) (int, error) {
	var row struct {
		SourceGroupID int
	}
	result := r.db.WithContext(ctx).Model(&model.BroadcastRun{}).
		Select("source_group_id").Where("id = ?", runID).Limit(1).Scan(&row)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 || row.SourceGroupID <= 0 {
		return 0, ErrNotFound
	}
	return row.SourceGroupID, nil
}

func (r *Repository) lockExecutionGuards(tx *gorm.DB, sourceGroupID int) (string, error) {
	enabled, err := r.operationalEnabled(tx, true)
	if err != nil {
		return "", err
	}
	if !enabled {
		return "site_broadcast_disabled", nil
	}
	if err := lockEntityGroupsShared(tx, []int{sourceGroupID}); err != nil {
		if errors.Is(err, ErrNotFound) {
			return "group_unavailable", nil
		}
		return "", err
	}
	return "", nil
}

func loadRunExecution(tx *gorm.DB, runID uint) (scheduler.RunExecution, error) {
	var execution scheduler.RunExecution
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&execution.Run, runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return execution, ErrNotFound
		}
		return execution, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&execution.Schedule, execution.Run.ScheduleID).Error; err != nil {
		return execution, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&execution.Audio, execution.Run.AudioID).Error; err != nil {
		return execution, err
	}
	return execution, nil
}

func (r *Repository) validateExecutionEligibility(tx *gorm.DB, execution *scheduler.RunExecution, claimedBy string, now time.Time) (string, error) {
	if execution == nil || execution.Run.ClaimedBy != claimedBy || execution.Run.LeaseUntil == nil || !execution.Run.LeaseUntil.After(now) ||
		(execution.Run.Status != model.RunStatusClaimed && execution.Run.Status != model.RunStatusPlaying) {
		return "run_lease_lost", nil
	}
	emergencyStopped, err := r.emergencyStopsRun(tx, &execution.Run)
	if err != nil {
		return "", err
	}
	if emergencyStopped {
		return "emergency_stop", nil
	}
	schedule, audio := &execution.Schedule, &execution.Audio
	if !schedule.Enabled {
		return "schedule_disabled", nil
	}
	if schedule.SuspendedReason != "" {
		return "virtual_group_broadcast_suspended", nil
	}
	var group gormdb.Group
	if err := tx.First(&group, execution.Run.SourceGroupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "group_unavailable", nil
		}
		return "", err
	}
	if group.Status != 1 || group.IsVirtual || (group.Type != 1 && group.Type != 2) {
		return "group_unavailable", nil
	}
	if schedule.GroupID != execution.Run.SourceGroupID || schedule.AudioID != execution.Run.AudioID ||
		audio.GroupID != execution.Run.SourceGroupID || audio.Status != model.AudioStatusReady || strings.TrimSpace(audio.PlaybackObjectKey) == "" {
		return "audio_unavailable", nil
	}
	policy, err := activePolicyForEntityGroup(tx, execution.Run.SourceGroupID)
	if err != nil {
		return "", err
	}
	if policy != nil && (policy.Mode != model.PolicyAllowSingleSource || policy.AllowedSourceGroupID == nil || *policy.AllowedSourceGroupID != execution.Run.SourceGroupID) {
		return "virtual_group_broadcast_suspended", nil
	}
	return "", nil
}

func (r *Repository) MarkRunPlaying(ctx context.Context, runID uint, claimedBy, domainKey string, domainGroupIDs []int, now time.Time, leaseDuration time.Duration) (string, error) {
	now = now.UTC()
	sourceGroupID, err := r.runSourceGroupID(ctx, runID)
	if err != nil {
		return "", err
	}
	domainJSON, err := json.Marshal(SortedUniqueGroupIDs(domainGroupIDs))
	if err != nil {
		return "", err
	}
	code := ""
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var guardErr error
		code, guardErr = r.lockExecutionGuards(tx, sourceGroupID)
		if guardErr != nil || code != "" {
			return guardErr
		}
		execution, loadErr := loadRunExecution(tx, runID)
		if loadErr != nil {
			return loadErr
		}
		code, loadErr = r.validateExecutionEligibility(tx, &execution, claimedBy, now)
		if loadErr != nil || code != "" {
			return loadErr
		}
		if execution.Run.Status != model.RunStatusClaimed {
			code = "run_lease_lost"
			return nil
		}
		result := tx.Model(&model.BroadcastRun{}).
			Where("id = ? AND status = ? AND claimed_by = ?", runID, model.RunStatusClaimed, claimedBy).
			Updates(map[string]any{
				"status": model.RunStatusPlaying, "started_at": now, "lease_until": now.Add(leaseDuration),
				"domain_key": strings.TrimSpace(domainKey), "domain_group_ids": string(domainJSON),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			code = "run_lease_lost"
		}
		return nil
	})
	return code, err
}

// ValidateAndRenewRun is called before every paced packet. The transaction
// closes the validation/renewal window against schedule and group mutations.
func (r *Repository) ValidateAndRenewRun(ctx context.Context, runID uint, claimedBy string, now time.Time, leaseDuration time.Duration) (string, error) {
	now = now.UTC()
	sourceGroupID, err := r.runSourceGroupID(ctx, runID)
	if err != nil {
		return "", err
	}
	code := ""
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var guardErr error
		code, guardErr = r.lockExecutionGuards(tx, sourceGroupID)
		if guardErr != nil || code != "" {
			return guardErr
		}
		execution, err := loadRunExecution(tx, runID)
		if err != nil {
			return err
		}
		code, err = r.validateExecutionEligibility(tx, &execution, claimedBy, now)
		if err != nil || code != "" {
			return err
		}
		if execution.Run.Status != model.RunStatusPlaying {
			code = "run_lease_lost"
			return nil
		}
		result := tx.Model(&model.BroadcastRun{}).
			Where("id = ? AND status = ? AND claimed_by = ?", runID, model.RunStatusPlaying, claimedBy).
			Update("lease_until", now.Add(leaseDuration))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			code = "run_lease_lost"
		}
		return nil
	})
	return code, err
}

func (r *Repository) FinishRun(ctx context.Context, runID uint, claimedBy, status string, endedAt time.Time, playedDurationMS, sentPackets, droppedPackets int, lastVoiceAt *time.Time, domainKey string, domainGroupIDs []int, errorCode, errorMessage string) error {
	if runID == 0 || strings.TrimSpace(claimedBy) == "" || status == "" {
		return ErrRunLeaseLost
	}
	values := map[string]any{
		"status": status, "ended_at": endedAt.UTC(), "played_duration_ms": max(playedDurationMS, 0),
		"sent_packets": max(sentPackets, 0), "dropped_packets": max(droppedPackets, 0),
		"claimed_by": "", "lease_until": nil, "error_code": truncate(strings.TrimSpace(errorCode), 64),
		"error_message": truncate(strings.TrimSpace(errorMessage), 500), "last_voice_at": lastVoiceAt,
	}
	if strings.TrimSpace(domainKey) != "" {
		values["domain_key"] = strings.TrimSpace(domainKey)
	}
	if domainGroupIDs != nil {
		domainJSON, err := json.Marshal(SortedUniqueGroupIDs(domainGroupIDs))
		if err != nil {
			return err
		}
		values["domain_group_ids"] = string(domainJSON)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.BroadcastRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, runID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRunLeaseLost
			}
			return err
		}
		if run.ClaimedBy != claimedBy || (run.Status != model.RunStatusClaimed && run.Status != model.RunStatusPlaying) {
			return ErrRunLeaseLost
		}
		result := tx.Model(&model.BroadcastRun{}).
			Where("id = ? AND claimed_by = ? AND status IN ?", runID, claimedBy, []string{model.RunStatusClaimed, model.RunStatusPlaying}).
			Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRunLeaseLost
		}
		if sentPackets <= 0 {
			return nil
		}
		startTime := endedAt.UTC().Add(-time.Duration(max(playedDurationMS, 0)) * time.Millisecond)
		if run.StartedAt != nil && !run.StartedAt.IsZero() {
			startTime = run.StartedAt.UTC()
		}
		sourceGroupID := uint(run.SourceGroupID)
		deliveryGroupIDs := make([]uint, 0, len(domainGroupIDs))
		for _, groupID := range SortedUniqueGroupIDs(domainGroupIDs) {
			if groupID > 0 {
				deliveryGroupIDs = append(deliveryGroupIDs, uint(groupID))
			}
		}
		record := &gormdb.CommRecord{
			DeviceID: 0, DeviceSSID: identity.SSID, GroupID: &sourceGroupID, UserID: nil,
			StartTime: startTime, EndTime: endedAt.UTC(), DurationMs: max(playedDurationMS, 0),
			AudioPath: "", AudioSize: 0, Status: 2, MessageType: gormdb.CommMessageTypeVoice,
			SenderUsername: identity.Username, SenderCallSign: identity.CallSign,
			SenderNickname: identity.Nickname, SenderDevModel: 0, IsAutoBroadcast: true,
			DeliveryGroupIDs: deliveryGroupIDs,
		}
		return gormdb.CreateCommRecordsWithDeliveryGroups(tx, []*gormdb.CommRecord{record}, 1)
	})
}

func (r *Repository) GetPolicy(ctx context.Context, virtualGroupID int) (*model.VirtualGroupBroadcastPolicy, error) {
	var policy model.VirtualGroupBroadcastPolicy
	err := r.db.WithContext(ctx).Where("virtual_group_id = ?", virtualGroupID).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &policy, err
}

func (r *Repository) SavePolicy(ctx context.Context, policy *model.VirtualGroupBroadcastPolicy) error {
	_, err := r.UpdateVirtualGroupPolicy(ctx, policy, time.Now().UTC())
	return err
}

type MemberScheduleStats struct {
	GroupID        int    `json:"group_id"`
	GroupName      string `json:"group_name"`
	EnabledCount   int64  `json:"enabled_count"`
	SuspendedCount int64  `json:"suspended_count"`
}

type VirtualGroupPolicySummary struct {
	VirtualGroupID       int    `json:"virtual_group_id"`
	Mode                 string `json:"mode"`
	AllowedSourceGroupID *int   `json:"allowed_source_group_id,omitempty"`
	AllowedSourceName    string `json:"allowed_source_name,omitempty"`
}

func (r *Repository) ListVirtualGroupPolicySummaries(ctx context.Context, virtualGroupIDs []int) (map[int]VirtualGroupPolicySummary, error) {
	virtualGroupIDs = SortedUniqueGroupIDs(virtualGroupIDs)
	result := make(map[int]VirtualGroupPolicySummary, len(virtualGroupIDs))
	if len(virtualGroupIDs) == 0 {
		return result, nil
	}
	var rows []VirtualGroupPolicySummary
	err := r.db.WithContext(ctx).Table("public_groups vg").
		Select("vg.id AS virtual_group_id, COALESCE(p.mode, ?) AS mode, p.allowed_source_group_id, COALESCE(source.name, '') AS allowed_source_name", model.PolicySuspendAll).
		Joins("LEFT JOIN virtual_group_broadcast_policies p ON p.virtual_group_id = vg.id").
		Joins("LEFT JOIN public_groups source ON source.id = p.allowed_source_group_id").
		Where("vg.id IN ? AND vg.is_virtual = 1", virtualGroupIDs).Order("vg.id ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.VirtualGroupID] = row
	}
	return result, nil
}

func (r *Repository) EnabledScheduleCounts(ctx context.Context, groupIDs []int) (map[int]int64, error) {
	groupIDs = SortedUniqueGroupIDs(groupIDs)
	result := make(map[int]int64, len(groupIDs))
	if len(groupIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		GroupID int
		Count   int64
	}
	err := r.db.WithContext(ctx).Model(&model.BroadcastSchedule{}).
		Select("group_id, COUNT(*) AS count").Where("group_id IN ? AND enabled = 1", groupIDs).
		Group("group_id").Order("group_id ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.GroupID] = row.Count
	}
	return result, nil
}

func (r *Repository) ListPolicyMemberStats(ctx context.Context, virtualGroupID int) ([]MemberScheduleStats, error) {
	var stats []MemberScheduleStats
	err := r.db.WithContext(ctx).Table("group_links gl").
		Select(`g.id AS group_id, g.name AS group_name,
			COUNT(CASE WHEN s.enabled = 1 THEN 1 END) AS enabled_count,
			COUNT(CASE WHEN s.enabled = 1 AND s.suspended_reason <> '' THEN 1 END) AS suspended_count`).
		Joins("JOIN public_groups g ON g.id = gl.target_group_id").
		Joins("LEFT JOIN broadcast_schedules s ON s.group_id = g.id").
		Where("gl.link_group_id = ?", virtualGroupID).
		Group("g.id, g.name").Order("g.id ASC").Scan(&stats).Error
	return stats, err
}

type ActivePolicy struct {
	VirtualGroupID       int
	Mode                 string
	AllowedSourceGroupID *int
}

func (r *Repository) ActivePolicyForEntityGroup(ctx context.Context, groupID int) (*ActivePolicy, error) {
	return activePolicyForEntityGroup(r.db.WithContext(ctx), groupID)
}

func activePolicyForEntityGroup(tx *gorm.DB, groupID int) (*ActivePolicy, error) {
	var policy ActivePolicy
	result := tx.Table("group_links gl").
		Select("vg.id AS virtual_group_id, COALESCE(p.mode, ?) AS mode, p.allowed_source_group_id", model.PolicySuspendAll).
		Joins("JOIN public_groups vg ON vg.id = gl.link_group_id AND vg.is_virtual = 1 AND vg.status = 1").
		Joins("LEFT JOIN virtual_group_broadcast_policies p ON p.virtual_group_id = vg.id").
		Where("gl.target_group_id = ?", groupID).Limit(1).Find(&policy)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &policy, nil
}

func lockEntityGroup(tx *gorm.DB, groupID int) error {
	var group gormdb.Group
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, groupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if group.IsVirtual || (group.Type != 1 && group.Type != 2) {
		return ErrInvalidEntityGroup
	}
	return nil
}

func lockEntityGroups(tx *gorm.DB, groupIDs []int) error {
	return lockEntityGroupsWithStrength(tx, groupIDs, "UPDATE")
}

func lockEntityGroupsShared(tx *gorm.DB, groupIDs []int) error {
	return lockEntityGroupsWithStrength(tx, groupIDs, "SHARE")
}

func lockEntityGroupsWithStrength(tx *gorm.DB, groupIDs []int, strength string) error {
	groupIDs = SortedUniqueGroupIDs(groupIDs)
	if len(groupIDs) == 0 {
		return nil
	}
	var groups []gormdb.Group
	if err := tx.Clauses(clause.Locking{Strength: strength}).
		Where("id IN ?", groupIDs).Order("id ASC").Find(&groups).Error; err != nil {
		return err
	}
	if len(groups) != len(groupIDs) {
		return ErrNotFound
	}
	for index := range groups {
		if groups[index].IsVirtual || (groups[index].Type != 1 && groups[index].Type != 2) {
			return ErrInvalidEntityGroup
		}
	}
	return nil
}

func validateAudioForSchedule(tx *gorm.DB, groupID int, audioID uint) error {
	var audio model.BroadcastAudio
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&audio, audioID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if audio.GroupID != groupID {
		return ErrAudioGroupMismatch
	}
	if audio.Status != model.AudioStatusReady {
		return ErrAudioNotReady
	}
	return nil
}

func applyScheduleRuntimeState(tx *gorm.DB, schedule *model.BroadcastSchedule, now time.Time) error {
	if !schedule.Enabled {
		schedule.NextRunAt = nil
		schedule.SuspendedReason = ""
		schedule.SuspendedByVirtualGroupID = nil
		schedule.SuspendedAt = nil
		return nil
	}
	policy, err := activePolicyForEntityGroup(tx, schedule.GroupID)
	if err != nil {
		return err
	}
	if policy != nil && (policy.Mode != model.PolicyAllowSingleSource || policy.AllowedSourceGroupID == nil || *policy.AllowedSourceGroupID != schedule.GroupID) {
		schedule.NextRunAt = nil
		schedule.SuspendedReason = model.SuspendReasonActiveVirtualGroup
		schedule.SuspendedByVirtualGroupID = &policy.VirtualGroupID
		suspendedAt := now.UTC()
		schedule.SuspendedAt = &suspendedAt
		return nil
	}
	schedule.SuspendedReason = ""
	schedule.SuspendedByVirtualGroupID = nil
	schedule.SuspendedAt = nil
	next, err := scheduler.NextOccurrence(schedule, now)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	if next == nil {
		return fmt.Errorf("%w: schedule has no future occurrence", ErrInvalidSchedule)
	}
	schedule.NextRunAt = next
	return nil
}

func normalizeSchedule(schedule *model.BroadcastSchedule) error {
	schedule.Name = strings.TrimSpace(schedule.Name)
	schedule.ScheduleType = strings.ToLower(strings.TrimSpace(schedule.ScheduleType))
	schedule.Timezone = strings.TrimSpace(schedule.Timezone)
	schedule.LocalTime = strings.TrimSpace(schedule.LocalTime)
	if schedule.Name == "" || len(schedule.Name) > 255 || !model.IsScheduleType(schedule.ScheduleType) {
		return ErrInvalidSchedule
	}
	if schedule.Timezone == "" {
		schedule.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return fmt.Errorf("%w: invalid timezone", ErrInvalidSchedule)
	}
	switch schedule.ScheduleType {
	case model.ScheduleTypeOnce:
		if schedule.ScheduledAt == nil {
			return fmt.Errorf("%w: scheduled_at is required", ErrInvalidSchedule)
		}
		utc := schedule.ScheduledAt.UTC()
		schedule.ScheduledAt = &utc
		schedule.LocalTime = ""
		schedule.WeekdayMask = 0
	case model.ScheduleTypeDaily:
		if schedule.LocalTime == "" {
			return fmt.Errorf("%w: local_time is required", ErrInvalidSchedule)
		}
		schedule.ScheduledAt = nil
		schedule.WeekdayMask = 0
	case model.ScheduleTypeWeekly:
		if schedule.LocalTime == "" || schedule.WeekdayMask == 0 || schedule.WeekdayMask&0x80 != 0 {
			return fmt.Errorf("%w: local_time and weekday_mask are required", ErrInvalidSchedule)
		}
		schedule.ScheduledAt = nil
	}
	return nil
}

func SortedUniqueGroupIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Ints(result)
	return result
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
