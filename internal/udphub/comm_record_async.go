package udphub

import (
	"sync"
	"sync/atomic"
)

// 异步录制队列：将 AppendPacket 移出语音转发热路径。
const commRecordQueueSize = 4096

type commRecordJob struct {
	sourceKey        string
	deviceID         int
	deviceSSID       uint8
	groupID          *uint
	userID           *uint
	sender           CommSenderSnapshot
	deliveryGroupIDs []uint
	audioData        []byte
}

var (
	commRecordMu       sync.Mutex
	commRecordQueue    chan commRecordJob
	commRecordStopCh   chan struct{}
	commRecordRunning  bool
	commRecordWg       sync.WaitGroup
	commRecordDrops    int64
	commRecordEnqueued int64
)

func ensureCommRecordWorker() {
	commRecordMu.Lock()
	if commRecordRunning {
		commRecordMu.Unlock()
		return
	}
	queue := make(chan commRecordJob, commRecordQueueSize)
	stopCh := make(chan struct{})
	commRecordQueue = queue
	commRecordStopCh = stopCh
	commRecordRunning = true
	commRecordWg.Add(1)
	commRecordMu.Unlock()
	go commRecordWorker(queue, stopCh)
}

func commRecordWorker(queue <-chan commRecordJob, stopCh <-chan struct{}) {
	defer commRecordWg.Done()
	for {
		select {
		case <-stopCh:
			// 排空剩余任务
			for {
				select {
				case job := <-queue:
					if globalCommRecorder != nil {
						globalCommRecorder.RecordPacket(job.sourceKey, job.deviceID, job.deviceSSID, job.groupID, job.userID, job.sender, job.deliveryGroupIDs, job.audioData)
					}
				default:
					return
				}
			}
		case job, ok := <-queue:
			if !ok {
				return
			}
			if globalCommRecorder != nil {
				globalCommRecorder.RecordPacket(job.sourceKey, job.deviceID, job.deviceSSID, job.groupID, job.userID, job.sender, job.deliveryGroupIDs, job.audioData)
			}
		}
	}
}

func enqueueCommRecord(sourceKey string, deviceID int, deviceSSID uint8, groupID *uint, userID *uint, sender CommSenderSnapshot, deliveryGroupIDs []uint, audioData []byte) {
	if globalCommRecorder == nil || !globalCommRecorder.canRecord() {
		return
	}
	ensureCommRecordWorker()

	// 拷贝音频，避免调用方复用 buffer
	payload := make([]byte, len(audioData))
	copy(payload, audioData)

	// groupID/userID 也做值拷贝，避免调用方栈变量被覆盖
	var gidPtr *uint
	var uidPtr *uint
	if groupID != nil {
		g := *groupID
		gidPtr = &g
	}
	if userID != nil {
		u := *userID
		uidPtr = &u
	}
	deliveryGroups := append([]uint(nil), deliveryGroupIDs...)

	job := commRecordJob{
		sourceKey:        normalizeCommRecordSourceKey(sourceKey, deviceID),
		deviceID:         deviceID,
		deviceSSID:       deviceSSID,
		groupID:          gidPtr,
		userID:           uidPtr,
		sender:           sender,
		deliveryGroupIDs: deliveryGroups,
		audioData:        payload,
	}

	commRecordMu.Lock()
	defer commRecordMu.Unlock()
	if !commRecordRunning || commRecordQueue == nil {
		atomic.AddInt64(&commRecordDrops, 1)
		return
	}
	select {
	case commRecordQueue <- job:
		atomic.AddInt64(&commRecordEnqueued, 1)
	default:
		// 队列满：丢弃录制，保证转发不阻塞
		atomic.AddInt64(&commRecordDrops, 1)
	}
}

// stopCommRecordWorker 在录制器停止时调用，排空队列。
func stopCommRecordWorker() {
	commRecordMu.Lock()
	if !commRecordRunning || commRecordStopCh == nil {
		commRecordMu.Unlock()
		return
	}
	commRecordRunning = false
	stopCh := commRecordStopCh
	close(stopCh)
	commRecordMu.Unlock()
	commRecordWg.Wait()
	commRecordMu.Lock()
	if commRecordStopCh == stopCh {
		commRecordQueue = nil
		commRecordStopCh = nil
	}
	commRecordMu.Unlock()
}

// GetCommRecordQueueStats 监控统计。
func GetCommRecordQueueStats() map[string]int64 {
	qlen := int64(0)
	commRecordMu.Lock()
	if commRecordQueue != nil && commRecordRunning {
		qlen = int64(len(commRecordQueue))
	}
	commRecordMu.Unlock()
	return map[string]int64{
		"enqueued": atomic.LoadInt64(&commRecordEnqueued),
		"drops":    atomic.LoadInt64(&commRecordDrops),
		"queued":   qlen,
	}
}
