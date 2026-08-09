package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/scheduler"
	"draarl/internal/gormdb"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VirtualGroupMutation struct {
	MemberGroupIDs []int
	CancelGroupIDs []int
	CancelRunIDs   []uint
}

type VirtualGroupFields struct {
	Name   *string
	Note   *string
	Status *int
}

func (r *Repository) CreateVirtualGroup(ctx context.Context, group *gormdb.Group, targetGroupIDs []int, policy *model.VirtualGroupBroadcastPolicy, now time.Time) (*VirtualGroupMutation, error) {
	if group == nil || !group.IsVirtual || group.OwerID <= 0 || (group.Status != 0 && group.Status != 1) ||
		policy == nil || policy.UpdatedBy <= 0 || !model.IsPolicyMode(policy.Mode) {
		return nil, ErrInvalidPolicy
	}
	now = now.UTC()
	memberIDs := SortedUniqueGroupIDs(targetGroupIDs)
	mutation := &VirtualGroupMutation{MemberGroupIDs: memberIDs}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntityGroups(tx, memberIDs); err != nil {
			return err
		}
		if len(memberIDs) != 0 {
			var enabledCount int64
			if err := tx.Model(&gormdb.Group{}).Where("id IN ? AND status = 1", memberIDs).Count(&enabledCount).Error; err != nil {
				return err
			}
			if enabledCount != int64(len(memberIDs)) {
				return ErrInvalidEntityGroup
			}
			var occupied int64
			if err := tx.Model(&gormdb.GroupLink{}).Where("target_group_id IN ?", memberIDs).Count(&occupied).Error; err != nil {
				return err
			}
			if occupied != 0 {
				return gormdb.ErrTargetGroupAlreadyLinked
			}
		}
		if err := normalizeAndValidatePolicy(tx, policy, memberIDs); err != nil {
			return err
		}
		desiredStatus := group.Status
		if err := tx.Select("name", "type", "password", "ower_id", "note", "status", "is_virtual").Create(group).Error; err != nil {
			return err
		}
		if group.Status != desiredStatus {
			if err := tx.Model(group).Update("status", desiredStatus).Error; err != nil {
				return err
			}
			group.Status = desiredStatus
		}
		if len(memberIDs) != 0 {
			links := make([]gormdb.GroupLink, len(memberIDs))
			for index, targetGroupID := range memberIDs {
				links[index] = gormdb.GroupLink{LinkGroupID: group.ID, TargetGroupID: targetGroupID}
			}
			if err := tx.Create(&links).Error; err != nil {
				return err
			}
		}
		policy.VirtualGroupID = group.ID
		if err := tx.Create(policy).Error; err != nil {
			return err
		}
		if group.Status != 1 {
			return nil
		}
		if err := applyActiveVirtualGroupPolicy(tx, group.ID, memberIDs, policy, now); err != nil {
			return err
		}
		cancelRunIDs, err := cancelInterconnectRuns(tx, memberIDs, now)
		if err != nil {
			return err
		}
		mutation.CancelGroupIDs = memberIDs
		mutation.CancelRunIDs = cancelRunIDs
		return nil
	})
	return mutation, err
}

func (r *Repository) UpdateVirtualGroupPolicy(ctx context.Context, policy *model.VirtualGroupBroadcastPolicy, now time.Time) (*VirtualGroupMutation, error) {
	if policy == nil || policy.VirtualGroupID <= 0 || policy.UpdatedBy <= 0 || !model.IsPolicyMode(policy.Mode) {
		return nil, ErrInvalidPolicy
	}
	now = now.UTC()
	mutation := &VirtualGroupMutation{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		group, memberIDs, err := lockVirtualGroupMembers(tx, policy.VirtualGroupID, nil)
		if err != nil {
			return err
		}
		if err := normalizeAndValidatePolicy(tx, policy, memberIDs); err != nil {
			return err
		}
		if err := saveVirtualGroupPolicy(tx, policy); err != nil {
			return err
		}
		mutation.MemberGroupIDs = memberIDs
		if group.Status != 1 {
			return nil
		}
		if err := applyActiveVirtualGroupPolicy(tx, policy.VirtualGroupID, memberIDs, policy, now); err != nil {
			return err
		}
		cancelRunIDs, err := cancelInterconnectRuns(tx, memberIDs, now)
		if err != nil {
			return err
		}
		mutation.CancelGroupIDs = memberIDs
		mutation.CancelRunIDs = cancelRunIDs
		return nil
	})
	return mutation, err
}

func (r *Repository) SetVirtualGroupStatus(ctx context.Context, virtualGroupID, status int, now time.Time) (*VirtualGroupMutation, error) {
	return r.UpdateVirtualGroup(ctx, virtualGroupID, VirtualGroupFields{Status: &status}, now)
}

func (r *Repository) UpdateVirtualGroup(ctx context.Context, virtualGroupID int, fields VirtualGroupFields, now time.Time) (*VirtualGroupMutation, error) {
	if virtualGroupID <= 0 || (fields.Status != nil && *fields.Status != 0 && *fields.Status != 1) {
		return nil, ErrVirtualGroupRequired
	}
	now = now.UTC()
	mutation := &VirtualGroupMutation{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		group, memberIDs, err := lockVirtualGroupMembers(tx, virtualGroupID, nil)
		if err != nil {
			return err
		}
		mutation.MemberGroupIDs = memberIDs
		statusChanged := fields.Status != nil && group.Status != *fields.Status
		if statusChanged && *fields.Status == 1 {
			policy, err := loadPolicyForUpdate(tx, virtualGroupID)
			if err != nil {
				return err
			}
			if err := normalizeAndValidatePolicy(tx, policy, memberIDs); err != nil {
				return err
			}
			if err := applyActiveVirtualGroupPolicy(tx, virtualGroupID, memberIDs, policy, now); err != nil {
				return err
			}
			cancelRunIDs, err := cancelInterconnectRuns(tx, memberIDs, now)
			if err != nil {
				return err
			}
			mutation.CancelGroupIDs = memberIDs
			mutation.CancelRunIDs = cancelRunIDs
		}
		updates := make(map[string]any)
		if fields.Name != nil {
			updates["name"] = *fields.Name
			group.Name = *fields.Name
		}
		if fields.Note != nil {
			updates["note"] = *fields.Note
			group.Note = *fields.Note
		}
		if fields.Status != nil {
			updates["status"] = *fields.Status
			group.Status = *fields.Status
		}
		if len(updates) != 0 {
			if err := tx.Model(group).Updates(updates).Error; err != nil {
				return err
			}
		}
		if !statusChanged || *fields.Status == 1 {
			return nil
		}
		if err := restoreSchedulesForVirtualGroup(tx, virtualGroupID, memberIDs, now); err != nil {
			return err
		}
		cancelRunIDs, err := cancelInterconnectRuns(tx, memberIDs, now)
		if err != nil {
			return err
		}
		mutation.CancelGroupIDs = memberIDs
		mutation.CancelRunIDs = cancelRunIDs
		return nil
	})
	return mutation, err
}

func (r *Repository) AddVirtualGroupMember(ctx context.Context, virtualGroupID, targetGroupID int, now time.Time) (*VirtualGroupMutation, error) {
	if virtualGroupID <= 0 || targetGroupID <= 0 {
		return nil, ErrInvalidEntityGroup
	}
	now = now.UTC()
	mutation := &VirtualGroupMutation{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		group, memberIDs, err := lockVirtualGroupMembers(tx, virtualGroupID, []int{targetGroupID})
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&gormdb.GroupLink{}).Where("target_group_id = ?", targetGroupID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return gormdb.ErrTargetGroupAlreadyLinked
		}
		if err := tx.Create(&gormdb.GroupLink{LinkGroupID: virtualGroupID, TargetGroupID: targetGroupID}).Error; err != nil {
			return err
		}
		memberIDs = SortedUniqueGroupIDs(append(memberIDs, targetGroupID))
		mutation.MemberGroupIDs = memberIDs
		if group.Status != 1 {
			return nil
		}
		policy, err := loadPolicyForUpdate(tx, virtualGroupID)
		if err != nil {
			return err
		}
		if err := normalizeAndValidatePolicy(tx, policy, memberIDs); err != nil {
			return err
		}
		if err := applyActiveVirtualGroupPolicy(tx, virtualGroupID, memberIDs, policy, now); err != nil {
			return err
		}
		cancelRunIDs, err := cancelInterconnectRuns(tx, memberIDs, now)
		if err != nil {
			return err
		}
		mutation.CancelGroupIDs = memberIDs
		mutation.CancelRunIDs = cancelRunIDs
		return nil
	})
	return mutation, err
}

func (r *Repository) RemoveVirtualGroupMember(ctx context.Context, virtualGroupID, targetGroupID int, now time.Time) (*VirtualGroupMutation, error) {
	if virtualGroupID <= 0 || targetGroupID <= 0 {
		return nil, ErrInvalidEntityGroup
	}
	now = now.UTC()
	mutation := &VirtualGroupMutation{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		group, memberIDs, err := lockVirtualGroupMembers(tx, virtualGroupID, []int{targetGroupID})
		if err != nil {
			return err
		}
		if !containsGroupID(memberIDs, targetGroupID) {
			return ErrNotFound
		}
		policy, err := loadPolicyForUpdate(tx, virtualGroupID)
		if err != nil {
			return err
		}
		if policy.Mode == model.PolicyAllowSingleSource && policy.AllowedSourceGroupID != nil && *policy.AllowedSourceGroupID == targetGroupID {
			return ErrPolicySourceStillMember
		}
		mutation.CancelGroupIDs = nil
		if group.Status == 1 {
			cancelRunIDs, err := cancelInterconnectRuns(tx, memberIDs, now)
			if err != nil {
				return err
			}
			mutation.CancelGroupIDs = memberIDs
			mutation.CancelRunIDs = cancelRunIDs
		}
		if err := tx.Where("link_group_id = ? AND target_group_id = ?", virtualGroupID, targetGroupID).Delete(&gormdb.GroupLink{}).Error; err != nil {
			return err
		}
		if err := restoreSchedulesForVirtualGroup(tx, virtualGroupID, []int{targetGroupID}, now); err != nil {
			return err
		}
		mutation.MemberGroupIDs = removeGroupID(memberIDs, targetGroupID)
		return nil
	})
	return mutation, err
}

func lockVirtualGroupMembers(tx *gorm.DB, virtualGroupID int, additionalGroupIDs []int) (*gormdb.Group, []int, error) {
	var group gormdb.Group
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, virtualGroupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	if !group.IsVirtual {
		return nil, nil, ErrVirtualGroupRequired
	}
	var memberIDs []int
	if err := tx.Model(&gormdb.GroupLink{}).Where("link_group_id = ?", virtualGroupID).
		Order("target_group_id ASC").Pluck("target_group_id", &memberIDs).Error; err != nil {
		return nil, nil, err
	}
	if err := lockEntityGroups(tx, append(memberIDs, additionalGroupIDs...)); err != nil {
		return nil, nil, err
	}
	return &group, SortedUniqueGroupIDs(memberIDs), nil
}

func loadPolicyForUpdate(tx *gorm.DB, virtualGroupID int) (*model.VirtualGroupBroadcastPolicy, error) {
	var policy model.VirtualGroupBroadcastPolicy
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("virtual_group_id = ?", virtualGroupID).First(&policy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidPolicy
		}
		return nil, err
	}
	return &policy, nil
}

func saveVirtualGroupPolicy(tx *gorm.DB, policy *model.VirtualGroupBroadcastPolicy) error {
	var existing model.VirtualGroupBroadcastPolicy
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("virtual_group_id = ?", policy.VirtualGroupID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(policy).Error
	}
	if err != nil {
		return err
	}
	policy.CreatedAt = existing.CreatedAt
	return tx.Model(&existing).Updates(map[string]any{
		"mode": policy.Mode, "allowed_source_group_id": policy.AllowedSourceGroupID, "updated_by": policy.UpdatedBy,
	}).Error
}

func normalizeAndValidatePolicy(tx *gorm.DB, policy *model.VirtualGroupBroadcastPolicy, memberIDs []int) error {
	if policy == nil || !model.IsPolicyMode(policy.Mode) {
		return ErrInvalidPolicy
	}
	if policy.Mode == model.PolicySuspendAll {
		policy.AllowedSourceGroupID = nil
		return nil
	}
	if policy.AllowedSourceGroupID == nil || *policy.AllowedSourceGroupID <= 0 {
		return ErrInvalidPolicy
	}
	if !containsGroupID(memberIDs, *policy.AllowedSourceGroupID) {
		return ErrPolicySourceNotMember
	}
	return nil
}

func applyActiveVirtualGroupPolicy(tx *gorm.DB, virtualGroupID int, memberIDs []int, policy *model.VirtualGroupBroadcastPolicy, now time.Time) error {
	if len(memberIDs) == 0 {
		return nil
	}
	var schedules []model.BroadcastSchedule
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("group_id IN ?", memberIDs).
		Order("group_id ASC, id ASC").Find(&schedules).Error; err != nil {
		return err
	}
	for index := range schedules {
		schedule := &schedules[index]
		if !schedule.Enabled {
			continue
		}
		allowed := policy.Mode == model.PolicyAllowSingleSource && policy.AllowedSourceGroupID != nil && *policy.AllowedSourceGroupID == schedule.GroupID
		if !allowed {
			schedule.NextRunAt = nil
			schedule.SuspendedReason = model.SuspendReasonActiveVirtualGroup
			schedule.SuspendedByVirtualGroupID = &virtualGroupID
			suspendedAt := now
			schedule.SuspendedAt = &suspendedAt
			if err := tx.Save(schedule).Error; err != nil {
				return err
			}
			continue
		}
		if schedule.SuspendedByVirtualGroupID != nil && *schedule.SuspendedByVirtualGroupID == virtualGroupID {
			if err := restoreSchedule(tx, schedule, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func restoreSchedulesForVirtualGroup(tx *gorm.DB, virtualGroupID int, groupIDs []int, now time.Time) error {
	if len(groupIDs) == 0 {
		return nil
	}
	var schedules []model.BroadcastSchedule
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("group_id IN ? AND suspended_by_virtual_group_id = ?", groupIDs, virtualGroupID).
		Order("group_id ASC, id ASC").Find(&schedules).Error; err != nil {
		return err
	}
	for index := range schedules {
		if err := restoreSchedule(tx, &schedules[index], now); err != nil {
			return err
		}
	}
	return nil
}

func restoreSchedule(tx *gorm.DB, scheduleModel *model.BroadcastSchedule, now time.Time) error {
	scheduleModel.SuspendedReason = ""
	scheduleModel.SuspendedByVirtualGroupID = nil
	scheduleModel.SuspendedAt = nil
	if !scheduleModel.Enabled {
		scheduleModel.NextRunAt = nil
		return tx.Save(scheduleModel).Error
	}
	if scheduleModel.ScheduleType == model.ScheduleTypeOnce && scheduleModel.ScheduledAt != nil && !scheduleModel.ScheduledAt.After(now) {
		scheduledFor := scheduleModel.ScheduledAt.UTC()
		run := &model.BroadcastRun{
			ScheduleID: scheduleModel.ID, AudioID: scheduleModel.AudioID, SourceGroupID: scheduleModel.GroupID,
			ScheduledFor: scheduledFor, Status: model.RunStatusSkippedInterconnected,
			EndedAt: &now, ErrorCode: "virtual_group_broadcast_suspended",
			ErrorMessage: "one-time broadcast occurrence elapsed while interconnect policy was active",
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(run).Error; err != nil {
			return err
		}
		scheduleModel.Enabled = false
		scheduleModel.NextRunAt = nil
		return tx.Save(scheduleModel).Error
	}
	next, err := scheduler.NextOccurrence(scheduleModel, now)
	if err != nil || next == nil {
		if err == nil {
			err = errors.New("schedule has no future occurrence")
		}
		return fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	scheduleModel.NextRunAt = next
	return tx.Save(scheduleModel).Error
}

func cancelInterconnectRuns(tx *gorm.DB, groupIDs []int, now time.Time) ([]uint, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	var runs []model.BroadcastRun
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("source_group_id IN ? AND status IN ?", groupIDs, []string{model.RunStatusClaimed, model.RunStatusPlaying}).
		Order("id ASC").Find(&runs).Error; err != nil {
		return nil, err
	}
	cancelRunIDs := make([]uint, 0, len(runs))
	claimedRunIDs := make([]uint, 0, len(runs))
	for _, run := range runs {
		cancelRunIDs = append(cancelRunIDs, run.ID)
		if run.Status == model.RunStatusClaimed {
			claimedRunIDs = append(claimedRunIDs, run.ID)
		}
	}
	if len(claimedRunIDs) == 0 {
		return cancelRunIDs, nil
	}
	err := tx.Model(&model.BroadcastRun{}).
		Where("id IN ? AND status = ?", claimedRunIDs, model.RunStatusClaimed).
		Updates(map[string]any{
			"status": model.RunStatusCancelledInterconnectEnabled, "ended_at": now,
			"claimed_by": "", "lease_until": nil, "error_code": "interconnect_changed",
			"error_message": "broadcast execution stopped because interconnect topology or policy changed",
		}).Error
	return cancelRunIDs, err
}

func (r *Repository) FinalizeOrphanedInterconnectRuns(ctx context.Context, runIDs []uint, now time.Time) error {
	if len(runIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.BroadcastRun{}).
		Where("id IN ? AND status IN ?", runIDs, []string{model.RunStatusClaimed, model.RunStatusPlaying}).
		Updates(map[string]any{
			"status": model.RunStatusCancelledInterconnectEnabled, "ended_at": now.UTC(),
			"claimed_by": "", "lease_until": nil, "error_code": "interconnect_changed",
			"error_message": "broadcast execution stopped because interconnect topology or policy changed",
		}).Error
}

func containsGroupID(groupIDs []int, target int) bool {
	for _, groupID := range groupIDs {
		if groupID == target {
			return true
		}
	}
	return false
}

func removeGroupID(groupIDs []int, target int) []int {
	result := make([]int, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID != target {
			result = append(result, groupID)
		}
	}
	return result
}
