package gormdb

import (
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
)

type MessageRecord struct {
	ID              uint      `gorm:"column:id"`
	DeviceID        uint      `gorm:"column:device_id"`
	DeviceSSID      uint8     `gorm:"column:device_ssid"`
	SourceGroupID   uint      `gorm:"column:source_group_id"`
	SourceGroupName string    `gorm:"column:source_group_name"`
	UserID          *uint     `gorm:"column:user_id"`
	StartTime       time.Time `gorm:"column:start_time"`
	EndTime         time.Time `gorm:"column:end_time"`
	DurationMs      int       `gorm:"column:duration_ms"`
	AudioPath       string    `gorm:"column:audio_path"`
	AudioSize       int64     `gorm:"column:audio_size"`
	Status          int       `gorm:"column:status"`
	MessageType     uint8     `gorm:"column:message_type"`
	TextContent     string    `gorm:"column:text_content"`
	SenderUsername  string    `gorm:"column:sender_username"`
	SenderCallSign  string    `gorm:"column:sender_callsign"`
	SenderNickname  string    `gorm:"column:sender_nickname"`
	SenderDevModel  int       `gorm:"column:sender_dev_model"`
	CurrentUsername string    `gorm:"column:current_username"`
	CurrentCallSign string    `gorm:"column:current_callsign"`
	CurrentNickname string    `gorm:"column:current_nickname"`
	CurrentDevModel int       `gorm:"column:current_dev_model"`
}

type MessageQuery struct {
	GroupIDs   []int
	Type       *uint8
	BeforeTime *time.Time
	BeforeID   uint
	Limit      int
}

type MessageRepository struct {
	db *gorm.DB
}

func NewMessageRepository() *MessageRepository {
	return &MessageRepository{db: Get()}
}

func (r *MessageRepository) baseQuery(groupIDs []int) *gorm.DB {
	return r.messageQuery("comm_record_delivery_groups dg", groupIDs)
}

func (r *MessageRepository) listQuery(groupID int, messageType *uint8) *gorm.DB {
	indexName := "idx_delivery_group_cursor"
	if messageType != nil {
		indexName = "idx_delivery_group_type_cursor"
	}
	return r.messageQuery("comm_record_delivery_groups dg FORCE INDEX ("+indexName+")", []int{groupID})
}

func (r *MessageRepository) messageQuery(table string, groupIDs []int) *gorm.DB {
	return r.db.Table(table).
		Select(`
			cr.id, cr.device_id, cr.device_ssid, cr.group_id AS source_group_id,
			COALESCE(cr.user_id, d.owner_id) AS user_id, cr.start_time, cr.end_time, cr.duration_ms,
			cr.audio_path, cr.audio_size, cr.status, cr.message_type, cr.text_content,
			cr.sender_username, cr.sender_callsign, cr.sender_nickname, cr.sender_dev_model,
			g.name AS source_group_name,
			u.name AS current_username, u.callsign AS current_callsign, u.nickname AS current_nickname,
			CASE WHEN cr.device_id = 0 THEN cr.device_ssid ELSE COALESCE(d.dev_model, 0) END AS current_dev_model
		`).
		Joins("INNER JOIN comm_records cr ON cr.id = dg.record_id").
		Joins("LEFT JOIN devices d ON cr.device_id = d.id").
		Joins("LEFT JOIN users u ON u.id = COALESCE(cr.user_id, d.owner_id)").
		Joins("LEFT JOIN public_groups g ON cr.group_id = g.id").
		Where("cr.status = ? AND dg.group_id IN ?", 2, groupIDs)
}

func (r *MessageRepository) List(query MessageQuery) ([]MessageRecord, bool, error) {
	if len(query.GroupIDs) == 0 {
		return []MessageRecord{}, false, nil
	}
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	groupIDs := uniquePositiveGroupIDs(query.GroupIDs)
	pageCapacity := len(groupIDs)
	if query.BeforeTime != nil {
		pageCapacity *= 2
	}
	pages := make([][]MessageRecord, 0, pageCapacity)
	for _, groupID := range groupIDs {
		newPageQuery := func() *gorm.DB {
			db := r.listQuery(groupID, query.Type)
			if query.Type != nil {
				db = db.Where("dg.message_type = ? AND cr.message_type = ?", *query.Type, *query.Type)
			}
			return db
		}
		queries := []*gorm.DB{newPageQuery()}
		if query.BeforeTime != nil {
			queries = []*gorm.DB{
				newPageQuery().Where("dg.start_time = ? AND dg.record_id < ?", *query.BeforeTime, query.BeforeID),
				newPageQuery().Where("dg.start_time < ?", *query.BeforeTime),
			}
		}

		for _, pageQuery := range queries {
			var records []MessageRecord
			if err := pageQuery.Order("dg.start_time DESC").Order("dg.record_id DESC").Limit(limit + 1).Scan(&records).Error; err != nil {
				return nil, false, err
			}
			pages = append(pages, records)
		}
	}
	return mergeMessagePages(pages, limit)
}

func uniquePositiveGroupIDs(groupIDs []int) []int {
	seen := make(map[int]struct{}, len(groupIDs))
	result := make([]int, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		result = append(result, groupID)
	}
	return result
}

func mergeMessagePages(pages [][]MessageRecord, limit int) ([]MessageRecord, bool, error) {
	records := make([]MessageRecord, 0, len(pages)*min(limit+1, 101))
	seen := make(map[uint]struct{}, len(records))
	for _, page := range pages {
		for _, record := range page {
			if record.ID == 0 {
				continue
			}
			if _, exists := seen[record.ID]; exists {
				continue
			}
			seen[record.ID] = struct{}{}
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].StartTime.Equal(records[j].StartTime) {
			return records[i].ID > records[j].ID
		}
		return records[i].StartTime.After(records[j].StartTime)
	})
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	return records, hasMore, nil
}

func (r *MessageRepository) GetByID(messageID uint, groupIDs []int) (*MessageRecord, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	var record MessageRecord
	err := r.baseQuery(groupIDs).Where("cr.id = ?", messageID).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}
