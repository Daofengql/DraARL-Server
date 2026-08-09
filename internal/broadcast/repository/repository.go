package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/scheduler"
	"draarl/internal/gormdb"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNotFound               = errors.New("broadcast resource not found")
	ErrInvalidEntityGroup     = errors.New("broadcast target must be an entity group")
	ErrInvalidAudio           = errors.New("invalid broadcast audio")
	ErrAudioNotReady          = errors.New("broadcast audio is not ready")
	ErrAudioGroupMismatch     = errors.New("broadcast audio belongs to another group")
	ErrAudioInUse             = errors.New("broadcast audio is referenced by a schedule")
	ErrInvalidSchedule        = errors.New("invalid broadcast schedule")
	ErrInvalidPolicy          = errors.New("invalid virtual group broadcast policy")
	ErrPolicySourceNotMember  = errors.New("virtual group broadcast source is not a member")
	ErrVirtualGroupRequired   = errors.New("virtual group is required")
	ErrManualTriggerSuspended = errors.New("virtual group broadcast suspended")
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
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

func (r *Repository) GetPolicy(ctx context.Context, virtualGroupID int) (*model.VirtualGroupBroadcastPolicy, error) {
	var policy model.VirtualGroupBroadcastPolicy
	err := r.db.WithContext(ctx).Where("virtual_group_id = ?", virtualGroupID).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &policy, err
}

func (r *Repository) SavePolicy(ctx context.Context, policy *model.VirtualGroupBroadcastPolicy) error {
	if policy == nil || policy.VirtualGroupID <= 0 || policy.UpdatedBy <= 0 || !model.IsPolicyMode(policy.Mode) {
		return ErrInvalidPolicy
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group gormdb.Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, policy.VirtualGroupID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if !group.IsVirtual {
			return ErrVirtualGroupRequired
		}
		if policy.Mode == model.PolicySuspendAll {
			policy.AllowedSourceGroupID = nil
		} else {
			if policy.AllowedSourceGroupID == nil || *policy.AllowedSourceGroupID <= 0 {
				return ErrInvalidPolicy
			}
			var count int64
			if err := tx.Model(&gormdb.GroupLink{}).
				Where("link_group_id = ? AND target_group_id = ?", policy.VirtualGroupID, *policy.AllowedSourceGroupID).
				Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return ErrPolicySourceNotMember
			}
		}
		return tx.Save(policy).Error
	})
}

type MemberScheduleStats struct {
	GroupID        int    `json:"group_id"`
	GroupName      string `json:"group_name"`
	EnabledCount   int64  `json:"enabled_count"`
	SuspendedCount int64  `json:"suspended_count"`
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
	err := tx.Table("group_links gl").
		Select("vg.id AS virtual_group_id, COALESCE(p.mode, ?) AS mode, p.allowed_source_group_id", model.PolicySuspendAll).
		Joins("JOIN public_groups vg ON vg.id = gl.link_group_id AND vg.is_virtual = 1 AND vg.status = 1").
		Joins("LEFT JOIN virtual_group_broadcast_policies p ON p.virtual_group_id = vg.id").
		Where("gl.target_group_id = ?", groupID).Take(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &policy, err
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
