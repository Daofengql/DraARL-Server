package udphub

import (
	"log"
	"sync"
	"time"

	"draarl/internal/gormdb"
	"gorm.io/gorm"
)

// DBRecord 待写入数据库的记录
type dbRecord struct {
	Session   *AudioSession
	AudioPath string
	AudioSize int64
	Error     error
}

// CommSyncer 通信记录数据库同步器
// maxPendingCommRecords 待写入通信记录上限，防止长期 DB 故障时内存无界增长。
const maxPendingCommRecords = 20000

type CommSyncer struct {
	db           *gorm.DB
	pending      []*dbRecord
	resultChan   chan *UploadResult
	stopChan     chan struct{}
	listenerDone chan struct{}
	mu           sync.Mutex
	running      bool
	stopClosed   bool
	droppedCount int
}

// NewCommSyncer 创建数据库同步器
func NewCommSyncer(resultChan chan *UploadResult) *CommSyncer {
	return &CommSyncer{
		db:         gormdb.Get(),
		pending:    make([]*dbRecord, 0),
		resultChan: resultChan,
		stopChan:   make(chan struct{}),
	}
}

// Start 启动监听上传结果
func (cs *CommSyncer) Start() {
	if cs == nil {
		return
	}

	cs.mu.Lock()
	if cs.running {
		cs.mu.Unlock()
		return
	}
	if cs.stopChan == nil || cs.stopClosed {
		cs.stopChan = make(chan struct{})
		cs.stopClosed = false
	}
	cs.running = true
	stopChan := cs.stopChan
	listenerDone := make(chan struct{})
	cs.listenerDone = listenerDone
	cs.mu.Unlock()
	go func() {
		defer close(listenerDone)
		cs.listenResults(stopChan)
	}()
}

// listenResults 监听上传结果
func (cs *CommSyncer) listenResults(stopChan <-chan struct{}) {
	for {
		select {
		case <-stopChan:
			return
		case result, ok := <-cs.resultChan:
			if !ok {
				return
			}
			cs.mu.Lock()
			if !cs.running {
				cs.mu.Unlock()
				return
			}

			// 【有界修复】长期 DB/存储故障时 pending 无界增长会 OOM；
			// 超过上限先丢弃最旧 10% 为新记录腾位。
			if len(cs.pending) >= maxPendingCommRecords {
				drop := len(cs.pending) / 10
				if drop < 1 {
					drop = 1
				}
				cs.pending = append(cs.pending[:0], cs.pending[drop:]...)
				cs.droppedCount += drop
				log.Printf("[COMM_SYNCER] 待写入记录超限，丢弃最旧 %d 条（累计 %d）", drop, cs.droppedCount)
			}
			cs.pending = append(cs.pending, &dbRecord{
				Session:   result.Session,
				AudioPath: result.AudioPath,
				AudioSize: result.AudioSize,
				Error:     result.Error,
			})
			cs.mu.Unlock()
		}
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
	records := make([]*gormdb.CommRecord, 0, len(batch))
	for _, item := range batch {
		status := 2 // 已完成
		if item.Error != nil {
			status = 3 // 上传失败
		}

		// 录音时长按实际 Opus 帧数估算，避免大包/弱网场景下被墙钟时间严重低估。
		durationMs := estimateSessionDurationMs(item.Session)
		endTime := item.Session.StartTime.Add(time.Duration(durationMs) * time.Millisecond)

		// 处理设备ID（幽灵设备持久化为0）
		deviceID := uint(0)
		if item.Session.DeviceID > 0 {
			deviceID = uint(item.Session.DeviceID)
		}

		records = append(records, &gormdb.CommRecord{
			SourceType: item.Session.Sender.SourceType, SourceCenterID: item.Session.Sender.SourceCenterID, LinkID: item.Session.Sender.LinkID, VirtualDeviceID: item.Session.Sender.VirtualDeviceID,
			DeviceID:         deviceID,
			DeviceSSID:       item.Session.DeviceSSID,
			GroupID:          item.Session.GroupID,
			UserID:           item.Session.UserID,
			StartTime:        item.Session.StartTime,
			EndTime:          endTime,
			DurationMs:       durationMs,
			AudioPath:        item.AudioPath,
			AudioSize:        item.AudioSize,
			Status:           status,
			MessageType:      gormdb.CommMessageTypeVoice,
			SenderUsername:   item.Session.Sender.Username,
			SenderCallSign:   item.Session.Sender.CallSign,
			SenderNickname:   item.Session.Sender.Nickname,
			SenderDevModel:   item.Session.Sender.DevModel,
			DeliveryGroupIDs: append([]uint(nil), item.Session.DeliveryGroupIDs...),
		})
	}

	// 批量插入通信记录及其发送时投递快照
	if cs.db != nil && len(records) > 0 {
		if err := gormdb.CreateCommRecordsWithDeliveryGroups(cs.db, records, 100); err != nil {
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
		cs.mu.Lock()
		cs.running = false
		listenerDone := cs.listenerDone
		if cs.stopChan != nil && !cs.stopClosed {
			close(cs.stopChan)
			cs.stopClosed = true
		}
		cs.mu.Unlock()
		if listenerDone != nil {
			<-listenerDone
		}
		// 处理剩余数据
		cs.SyncToDatabase()
	}
}
