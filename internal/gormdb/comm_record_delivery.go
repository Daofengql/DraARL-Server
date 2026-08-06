package gormdb

import (
	"fmt"

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
			for _, groupID := range normalizeDeliveryGroupIDs(record.DeliveryGroupIDs, record.GroupID) {
				deliveryGroups = append(deliveryGroups, CommRecordDeliveryGroup{
					RecordID: record.ID,
					GroupID:  groupID,
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
	var afterID uint
	for {
		var rows []struct {
			ID      uint
			GroupID uint
		}
		err := db.Table("comm_records cr").
			Select("cr.id, cr.group_id").
			Joins("LEFT JOIN comm_record_delivery_groups dg ON dg.record_id = cr.id AND dg.group_id = cr.group_id").
			Where("cr.id > ? AND cr.group_id IS NOT NULL AND dg.record_id IS NULL", afterID).
			Order("cr.id ASC").
			Limit(batchSize).
			Scan(&rows).Error
		if err != nil {
			return fmt.Errorf("list communication records missing delivery snapshots: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}

		snapshots := make([]CommRecordDeliveryGroup, 0, len(rows))
		for _, row := range rows {
			if row.ID != 0 && row.GroupID != 0 {
				snapshots = append(snapshots, CommRecordDeliveryGroup{RecordID: row.ID, GroupID: row.GroupID})
			}
		}
		if len(snapshots) > 0 {
			if err := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(snapshots, batchSize).Error; err != nil {
				return fmt.Errorf("backfill communication delivery snapshots: %w", err)
			}
		}
		afterID = rows[len(rows)-1].ID
	}
}
