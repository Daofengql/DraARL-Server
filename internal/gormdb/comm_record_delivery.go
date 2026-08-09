package gormdb

import (
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultCommRecordBatchSize = 100

// CreateCommRecordsWithDeliveryGroups writes communication records and their
// send-time delivery snapshots atomically.
func CreateCommRecordsWithDeliveryGroups(db *gorm.DB, records []*CommRecord, batchSize int) error {
	if db == nil || len(records) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = defaultCommRecordBatchSize
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(records, batchSize).Error; err != nil {
			return fmt.Errorf("create communication records: %w", err)
		}

		deliveryGroups := make([]CommRecordDeliveryGroup, 0, len(records))
		for _, record := range records {
			if record == nil || record.ID == 0 {
				continue
			}
			messageType := record.MessageType
			for _, groupID := range normalizeDeliveryGroupIDs(record.DeliveryGroupIDs, record.GroupID) {
				deliveryGroups = append(deliveryGroups, CommRecordDeliveryGroup{
					RecordID: record.ID, GroupID: groupID, StartTime: record.StartTime,
					MessageType: &messageType,
				})
			}
		}
		if len(deliveryGroups) == 0 {
			return nil
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(deliveryGroups, batchSize).Error; err != nil {
			return fmt.Errorf("create communication delivery snapshots: %w", err)
		}
		return nil
	})
}

func normalizeDeliveryGroupIDs(groupIDs []uint, sourceGroupID *uint) []uint {
	seen := make(map[uint]struct{}, len(groupIDs)+1)
	result := make([]uint, 0, len(groupIDs)+1)
	for _, groupID := range groupIDs {
		if groupID == 0 {
			continue
		}
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		result = append(result, groupID)
	}
	if len(result) == 0 && sourceGroupID != nil && *sourceGroupID > 0 {
		result = append(result, *sourceGroupID)
	}
	return result
}

// BackfillCommRecordDeliveryGroups adds a source-group snapshot to records
// created before the delivery snapshot table existed. It deliberately does not
// infer historical virtual interconnect membership.
func BackfillCommRecordDeliveryGroups(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	const batchSize = 1000
	var orphanCount int64
	if err := db.Table("comm_records cr").
		Joins("LEFT JOIN public_groups source_group ON source_group.id = cr.group_id").
		Where("cr.group_id IS NOT NULL AND source_group.id IS NULL").
		Count(&orphanCount).Error; err != nil {
		return fmt.Errorf("count communication records with missing source groups: %w", err)
	}
	if orphanCount > 0 {
		log.Printf("[Migration Warning] 跳过 %d 条来源群组已不存在的通信记录投递快照", orphanCount)
	}

	var afterID uint
	for {
		var rows []struct {
			ID          uint
			GroupID     uint
			StartTime   time.Time
			MessageType uint8
		}
		err := db.Table("comm_records cr").
			Select("cr.id, cr.group_id, cr.start_time, cr.message_type").
			Joins("INNER JOIN public_groups source_group ON source_group.id = cr.group_id").
			Joins("LEFT JOIN comm_record_delivery_groups dg ON dg.record_id = cr.id AND dg.group_id = cr.group_id").
			Where("cr.id > ? AND cr.group_id IS NOT NULL AND dg.record_id IS NULL", afterID).
			Order("cr.id ASC").
			Limit(batchSize).
			Scan(&rows).Error
		if err != nil {
			return fmt.Errorf("list communication records missing delivery snapshots: %w", err)
		}
		if len(rows) == 0 {
			break
		}

		snapshots := make([]CommRecordDeliveryGroup, 0, len(rows))
		for _, row := range rows {
			if row.ID != 0 && row.GroupID != 0 {
				messageType := row.MessageType
				snapshots = append(snapshots, CommRecordDeliveryGroup{
					RecordID: row.ID, GroupID: row.GroupID, StartTime: row.StartTime,
					MessageType: &messageType,
				})
			}
		}
		if len(snapshots) > 0 {
			if err := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(snapshots, batchSize).Error; err != nil {
				return fmt.Errorf("backfill communication delivery snapshots: %w", err)
			}
		}
		afterID = rows[len(rows)-1].ID
	}
	return backfillCommRecordDeliveryGroupTimes(db, batchSize)
}

func backfillCommRecordDeliveryGroupTimes(db *gorm.DB, batchSize int) error {
	var afterRecordID, afterGroupID uint
	for {
		var rows []struct {
			RecordID    uint
			GroupID     uint
			StartTime   time.Time
			MessageType uint8
		}
		err := db.Table("comm_record_delivery_groups dg").
			Select("dg.record_id, dg.group_id, cr.start_time, cr.message_type").
			Joins("INNER JOIN comm_records cr ON cr.id = dg.record_id").
			Where("dg.start_time IS NULL OR dg.message_type IS NULL").
			Where("dg.record_id > ? OR (dg.record_id = ? AND dg.group_id > ?)", afterRecordID, afterRecordID, afterGroupID).
			Order("dg.record_id ASC, dg.group_id ASC").
			Limit(batchSize).
			Scan(&rows).Error
		if err != nil {
			return fmt.Errorf("list communication delivery snapshots missing start time: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}

		snapshots := make([]CommRecordDeliveryGroup, 0, len(rows))
		for _, row := range rows {
			messageType := row.MessageType
			snapshots = append(snapshots, CommRecordDeliveryGroup{
				RecordID: row.RecordID, GroupID: row.GroupID, StartTime: row.StartTime,
				MessageType: &messageType,
			})
		}
		if err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "record_id"}, {Name: "group_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"start_time", "message_type"}),
		}).CreateInBatches(snapshots, batchSize).Error; err != nil {
			return fmt.Errorf("backfill communication delivery snapshot start times: %w", err)
		}
		last := rows[len(rows)-1]
		afterRecordID, afterGroupID = last.RecordID, last.GroupID
	}
}
