package udphub

import (
	"sync"
	"sync/atomic"
)

// 异步录制队列：将 AppendPacket 移出语音转发热路径。
const commRecordQueueSize = 4096

type commRecordJob struct {
	sourceKey  string
	deviceID   int
	deviceSSID uint8
	groupID    *uint
	userID     *uint
	audioData  []byte
}

var (
	commRecordQueue    chan commRecordJob
	commRecordOnce     sync.Once
	commRecordStopOnce sync.Once
	commRecordStopCh   chan struct{}
	commRecordWg       sync.WaitGroup
	commRecordDrops    int64
	commRecordEnqueued int64
)

func ensureCommRecordWorker() {
	commRecordOnce.Do(func() {
		commRecordQueue = make(chan commRecordJob, commRecordQueueSize)
		commRecordStopCh = make(chan struct{})
		commRecordWg.Add(1)
		go commRecordWorker()
	})
}

func commRecordWorker() {
	defer commRecordWg.Done()
	for {
		select {
		case <-commRecordStopCh:
			// 排空剩余任务
			for {
				select {
				case job := <-commRecordQueue:
					if globalCommRecorder != nil {
						globalCommRecorder.RecordPacket(job.sourceKey, job.deviceID, job.deviceSSID, job.groupID, job.userID, job.audioData)
					}
				default:
					return
				}
			}
		case job, ok := <-commRecordQueue:
			if !ok {
				return
			}
			if globalCommRecorder != nil {
				globalCommRecorder.RecordPacket(job.sourceKey, job.deviceID, job.deviceSSID, job.groupID, job.userID, job.audioData)
			}
		}
	}
}

func enqueueCommRecord(sourceKey string, deviceID int, deviceSSID uint8, groupID *uint, userID *uint, audioData []byte) {
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

	job := commRecordJob{
		sourceKey:  normalizeCommRecordSourceKey(sourceKey, deviceID),
		deviceID:   deviceID,
		deviceSSID: deviceSSID,
		groupID:    gidPtr,
		userID:     uidPtr,
		audioData:  payload,
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
	commRecordStopOnce.Do(func() {
		if commRecordStopCh != nil {
			close(commRecordStopCh)
		}
	})
	commRecordWg.Wait()
}

// GetCommRecordQueueStats 监控统计。
func GetCommRecordQueueStats() map[string]int64 {
	qlen := int64(0)
	if commRecordQueue != nil {
		qlen = int64(len(commRecordQueue))
	}
	return map[string]int64{
		"enqueued": atomic.LoadInt64(&commRecordEnqueued),
		"drops":    atomic.LoadInt64(&commRecordDrops),
		"queued":   qlen,
	}
}
