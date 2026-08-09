package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/scheduler"
	"draarl/internal/gormdb"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	OperationalEnabledKey     = "broadcast.runtime_enabled"
	operationalConfigCategory = "broadcast"
	operationalConfigDesc     = "Automatic broadcast runtime switch"
)

type PersistedMetrics struct {
	TotalRuns      int64            `json:"total_runs"`
	RunsByStatus   map[string]int64 `json:"runs_by_status"`
	SentPackets    int64            `json:"sent_packets"`
	DroppedPackets int64            `json:"dropped_packets"`
	LastEndedAt    *time.Time       `json:"last_ended_at,omitempty"`
}

type emergencyFence struct {
	RunID uint      `json:"run_id"`
	At    time.Time `json:"at"`
}

// EnsureOperationalEnabled creates the persistent runtime switch once without
// overwriting an administrator's existing choice.
func (r *Repository) EnsureOperationalEnabled(ctx context.Context) (bool, error) {
	configRow := gormdb.SiteConfig{
		Key: r.runtimeStateKey(), Value: "true", Category: operationalConfigCategory,
		Description: operationalConfigDesc,
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&configRow).Error; err != nil {
		return false, err
	}
	return r.OperationalEnabled(ctx)
}

func (r *Repository) OperationalEnabled(ctx context.Context) (bool, error) {
	return r.operationalEnabled(r.db.WithContext(ctx), false)
}

// SetOperationalEnabled serializes the runtime switch with schedule claiming.
// Re-enabling advances every overdue schedule so disabled time never causes a
// delayed burst of automatic transmissions.
func (r *Repository) SetOperationalEnabled(ctx context.Context, enabled bool, now time.Time) error {
	now = now.UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := r.operationalEnabledForUpdate(tx)
		if err != nil {
			return err
		}
		if current == enabled {
			return nil
		}
		if enabled {
			if err := skipSchedulesMissedWhileDisabled(tx, operationTime(now)); err != nil {
				return err
			}
		}
		value := strconv.FormatBool(enabled)
		result := tx.Model(&gormdb.SiteConfig{}).Where("config_key = ?", r.runtimeStateKey()).Updates(map[string]any{
			"config_value": value, "category": operationalConfigCategory, "description": operationalConfigDesc,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return tx.Create(&gormdb.SiteConfig{
				Key: r.runtimeStateKey(), Value: value, Category: operationalConfigCategory,
				Description: operationalConfigDesc,
			}).Error
		}
		return nil
	})
}

func (r *Repository) operationalEnabled(tx *gorm.DB, lock bool) (bool, error) {
	query := tx.Where("config_key = ?", r.runtimeStateKey())
	if lock {
		// FOR UPDATE is supported by MySQL 5.7 and newer MariaDB releases.
		// The stronger lock keeps the runtime switch serialized without relying
		// on the MySQL 8-only FOR SHARE syntax.
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var configRow gormdb.SiteConfig
	err := query.First(&configRow).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	value := strings.TrimSpace(configRow.Value)
	enabled, parseErr := strconv.ParseBool(value)
	if parseErr != nil {
		return false, parseErr
	}
	return enabled, nil
}

func (r *Repository) operationalEnabledForUpdate(tx *gorm.DB) (bool, error) {
	var configRow gormdb.SiteConfig
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key = ?", r.runtimeStateKey()).First(&configRow).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	enabled, parseErr := strconv.ParseBool(strings.TrimSpace(configRow.Value))
	if parseErr != nil {
		return false, parseErr
	}
	return enabled, nil
}

func (r *Repository) runtimeStateKey() string {
	if r != nil && strings.TrimSpace(r.operationalKey) != "" {
		return strings.TrimSpace(r.operationalKey)
	}
	return OperationalEnabledKey
}

func (r *Repository) emergencyFenceKey() string {
	return r.runtimeStateKey() + ".emergency_fence"
}

// FenceEmergencyStop blocks every run that existed before this transaction,
// including runs owned by another scheduler instance. Claim transactions share
// the operational row lock, so runs created after the fence remain eligible.
func (r *Repository) FenceEmergencyStop(ctx context.Context, now time.Time) error {
	now = now.UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := r.operationalEnabledForUpdate(tx); err != nil {
			return err
		}
		var latest model.BroadcastRun
		result := tx.Order("id DESC").Limit(1).Find(&latest)
		if result.Error != nil {
			return result.Error
		}
		fenceAt := operationTime(now)
		if latest.CreatedAt.After(fenceAt) {
			fenceAt = latest.CreatedAt.UTC()
		}
		encoded, err := json.Marshal(emergencyFence{RunID: latest.ID, At: fenceAt})
		if err != nil {
			return err
		}
		row := gormdb.SiteConfig{
			Key: r.emergencyFenceKey(), Value: string(encoded), Category: operationalConfigCategory,
			Description: "Automatic broadcast emergency stop fence",
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "config_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"config_value", "category", "description", "update_time"}),
		}).Create(&row).Error
	})
}

func operationTime(requested time.Time) time.Time {
	current := time.Now().UTC()
	if requested.After(current) {
		return requested.UTC()
	}
	return current
}

func (r *Repository) emergencyStopsRun(tx *gorm.DB, run *model.BroadcastRun) (bool, error) {
	if run == nil || run.ID == 0 {
		return false, nil
	}
	var row gormdb.SiteConfig
	result := tx.Where("config_key = ?", r.emergencyFenceKey()).Limit(1).Find(&row)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	var fence emergencyFence
	if err := json.Unmarshal([]byte(row.Value), &fence); err != nil {
		return false, err
	}
	return fence.RunID >= run.ID && !run.CreatedAt.After(fence.At), nil
}

func skipSchedulesMissedWhileDisabled(tx *gorm.DB, now time.Time) error {
	var schedules []model.BroadcastSchedule
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("enabled = ? AND suspended_reason = '' AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC, id ASC").Find(&schedules).Error; err != nil {
		return err
	}
	for index := range schedules {
		scheduleModel := &schedules[index]
		scheduledFor := scheduleModel.NextRunAt.UTC()
		run := model.BroadcastRun{
			ScheduleID: scheduleModel.ID, AudioID: scheduleModel.AudioID, SourceGroupID: scheduleModel.GroupID,
			ScheduledFor: scheduledFor, Status: model.RunStatusSkippedSiteDisabled,
			EndedAt: &now, ErrorCode: "site_broadcast_disabled",
			ErrorMessage: "scheduled occurrence was skipped while site automatic broadcasts were disabled",
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&run).Error; err != nil {
			return err
		}
		if scheduleModel.ScheduleType == model.ScheduleTypeOnce {
			if err := tx.Model(scheduleModel).Updates(map[string]any{"enabled": false, "next_run_at": nil}).Error; err != nil {
				return err
			}
			continue
		}
		next, err := scheduler.NextOccurrence(scheduleModel, now)
		if err != nil || next == nil {
			return ErrInvalidSchedule
		}
		if err := tx.Model(scheduleModel).Update("next_run_at", next).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) DueBacklog(ctx context.Context, now time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.BroadcastSchedule{}).
		Where("enabled = ? AND suspended_reason = '' AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now.UTC()).
		Count(&count).Error
	return count, err
}

func (r *Repository) PersistedMetrics(ctx context.Context) (PersistedMetrics, error) {
	result := PersistedMetrics{RunsByStatus: make(map[string]int64)}
	type statusRow struct {
		Status string
		Count  int64
	}
	var statuses []statusRow
	if err := r.db.WithContext(ctx).Model(&model.BroadcastRun{}).
		Select("status, COUNT(*) AS count").Group("status").Scan(&statuses).Error; err != nil {
		return result, err
	}
	for _, row := range statuses {
		result.RunsByStatus[row.Status] = row.Count
		result.TotalRuns += row.Count
	}
	type packetTotals struct {
		SentPackets    int64
		DroppedPackets int64
		LastEndedAt    *time.Time
	}
	var totals packetTotals
	if err := r.db.WithContext(ctx).Model(&model.BroadcastRun{}).
		Select("COALESCE(SUM(sent_packets), 0) AS sent_packets, COALESCE(SUM(dropped_packets), 0) AS dropped_packets, MAX(ended_at) AS last_ended_at").
		Scan(&totals).Error; err != nil {
		return result, err
	}
	result.SentPackets = totals.SentPackets
	result.DroppedPackets = totals.DroppedPackets
	result.LastEndedAt = totals.LastEndedAt
	return result, nil
}
