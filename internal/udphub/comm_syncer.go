package udphub

import (
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
	"nrllink/internal/gormdb"
)

// DBRecord 待写入数据库的记录
type dbRecord struct {
	Session   *AudioSession
	AudioPath string
	AudioSize int64
	Error     error
}

// CommSyncer 通信记录数据库同步器
type CommSyncer struct {
	db         *gorm.DB
	pending    []*dbRecord
	resultChan chan *UploadResult
	mu         sync.Mutex
	running    bool
}

// NewCommSyncer 创建数据库同步器
func NewCommSyncer(resultChan chan *UploadResult) *CommSyncer {
	return &CommSyncer{
		db:         gormdb.Get(),
		pending:    make([]*dbRecord, 0),
		resultChan: resultChan,
	}
}

// Start 启动监听上传结果
func (cs *CommSyncer) Start() {
	if cs == nil {
		return
	}

	cs.running = true
	go cs.listenResults()
}

// listenResults 监听上传结果
func (cs *CommSyncer) listenResults() {
	for result := range cs.resultChan {
		if !cs.running {
			return
		}

		cs.mu.Lock()
		cs.pending = append(cs.pending, &dbRecord{
			Session:   result.Session,
			AudioPath: result.AudioPath,
			AudioSize: result.AudioSize,
			Error:     result.Error,
		})
		cs.mu.Unlock()
	}
}

// SyncToDatabase 同步到数据库（由定时器调用）
func (cs *CommSyncer) SyncToDatabase() {
	if cs == nil {
		return
	}

	cs.mu.Lock()
	if len(cs.pending) == 0 {
		cs.mu.Unlock()
		return
	}

	// 取出待写入记录
	batch := cs.pending
	cs.pending = make([]*dbRecord, 0)
	cs.mu.Unlock()

	log.Printf("[COMM_SYNCER] 开始同步 %d 条通信记录到数据库", len(batch))

	// 批量写入数据库
	records := make([]gormdb.CommRecord, 0, len(batch))
	for _, item := range batch {
		status := 2 // 已完成
		if item.Error != nil {
			status = 3 // 上传失败
		}

		// 计算时长
		durationMs := int(item.Session.LastPacketTime.Sub(item.Session.StartTime).Milliseconds())
		endTime := item.Session.StartTime.Add(time.Duration(durationMs) * time.Millisecond)

		// 处理设备ID（幽灵设备使用负数ID）
		deviceID := uint(0)
		isGhost := item.Session.IsGhost
		if item.Session.DeviceID > 0 {
			deviceID = uint(item.Session.DeviceID)
		}

		records = append(records, gormdb.CommRecord{
			DeviceID:   deviceID,
			DeviceSSID: item.Session.DeviceSSID,
			GroupID:    item.Session.GroupID,
			UserID:     item.Session.UserID,
			IsGhost:    isGhost,
			StartTime:  item.Session.StartTime,
			EndTime:    endTime,
			DurationMs: durationMs,
			AudioPath:  item.AudioPath,
			AudioSize:  item.AudioSize,
			Status:     status,
		})
	}

	// 批量插入
	if cs.db != nil && len(records) > 0 {
		if err := cs.db.CreateInBatches(records, 100).Error; err != nil {
			log.Printf("[COMM_SYNCER] 批量写入数据库失败: %v", err)
		} else {
			log.Printf("[COMM_SYNCER] 成功写入 %d 条通信记录", len(records))
		}
	}
}

// GetPendingCount 获取待写入数量（用于监控）
func (cs *CommSyncer) GetPendingCount() int {
	if cs == nil {
		return 0
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.pending)
}

// Stop 停止同步器
func (cs *CommSyncer) Stop() {
	if cs != nil {
		cs.running = false
		// 处理剩余数据
		cs.SyncToDatabase()
	}
}
