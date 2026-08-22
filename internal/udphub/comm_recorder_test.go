package udphub

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCommSenderSnapshotNormalizesNicknameAtSendTime(t *testing.T) {
	snapshot := (CommSenderSnapshot{Username: "alice", CallSign: "BG7OLD"}).normalized()
	if snapshot.Nickname != "BG7OLD" {
		t.Fatalf("normalized nickname=%q", snapshot.Nickname)
	}
}

func TestCommRecorderCanEnableAfterDisabledStart(t *testing.T) {
	disabled := &CommSettingsConfig{
		Enabled: false, RetentionDays: 30, MinDurationMs: 0,
		MaxDurationSec: 300, BatchUploadSec: 10,
	}
	resultChan := make(chan *UploadResult, 1)
	buffer := NewCommBuffer(disabled)
	recorder := &CommRecorder{
		buffer: buffer, uploader: NewCommUploader(disabled, resultChan),
		syncer: &CommSyncer{pending: make([]*dbRecord, 0), resultChan: resultChan},
		config: disabled, stopChan: make(chan struct{}),
	}
	buffer.SetOnSessionEnd(recorder.uploader.AddToQueue)
	recorder.Start()
	t.Cleanup(recorder.Stop)

	recorder.RecordPacket(PhysicalCommRecordSourceKey(1), 1, 1, nil, nil, CommSenderSnapshot{}, nil, []byte{1, 2, 3})
	if got := recorder.buffer.GetActiveSessionCount(); got != 0 {
		t.Fatalf("disabled recorder created %d session(s)", got)
	}

	enabled := *disabled
	enabled.Enabled = true
	recorder.UpdateConfig(&enabled)
	recorder.RecordPacket(PhysicalCommRecordSourceKey(1), 1, 1, nil, nil, CommSenderSnapshot{}, nil, []byte{1, 2, 3})
	if got := recorder.buffer.GetActiveSessionCount(); got != 1 {
		t.Fatalf("recorder did not activate after config reload: sessions=%d", got)
	}

	stats := recorder.GetStats()
	if stats["enabled"] != true || stats["running"] != true {
		t.Fatalf("unexpected recorder stats after enable: %#v", stats)
	}
}

func TestCommBufferSeparatesGhostRecordingSources(t *testing.T) {
	buffer := NewCommBuffer(&CommSettingsConfig{Enabled: true, MinDurationMs: 0})
	groupID := uint(7)
	userID := uint(42)
	sourceA := GhostCommRecordSourceKey("udp", int(userID), 101, "10.0.0.1:20001")
	sourceB := GhostCommRecordSourceKey("udp", int(userID), 101, "10.0.0.2:20002")

	buffer.AppendPacket(sourceA, 0, 101, &groupID, &userID, CommSenderSnapshot{}, []uint{groupID}, []byte{1, 2, 3})
	buffer.AppendPacket(sourceB, 0, 101, &groupID, &userID, CommSenderSnapshot{}, []uint{groupID}, []byte{4, 5, 6})

	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	if len(buffer.sessions) != 2 {
		t.Fatalf("ghost sources shared a recording buffer: sessions=%d", len(buffer.sessions))
	}
	first := buffer.sessions[sourceA]
	second := buffer.sessions[sourceB]
	if first == nil || second == nil || first == second {
		t.Fatalf("ghost source sessions were not independently indexed: first=%p second=%p", first, second)
	}
	if first.DeviceID != 0 || second.DeviceID != 0 {
		t.Fatalf("ghost persistence device IDs changed: first=%d second=%d", first.DeviceID, second.DeviceID)
	}
	if first.SessionID == second.SessionID {
		t.Fatalf("ghost source sessions received the same object ID: %q", first.SessionID)
	}
	for _, sessionID := range []string{first.SessionID, second.SessionID} {
		if strings.Contains(sessionID, "10.0.0") || strings.ContainsAny(sessionID, ":/") {
			t.Fatalf("audio session ID leaked an endpoint or unsafe path character: %q", sessionID)
		}
	}
}

func TestCommUploaderPublishResultDoesNotBlockOnFullChannel(t *testing.T) {
	resultChan := make(chan *UploadResult, 1)
	resultChan <- &UploadResult{}
	uploader := NewCommUploader(&CommSettingsConfig{}, resultChan)
	result := &UploadResult{Session: &AudioSession{SessionID: "blocked-result"}}

	started := time.Now()
	if uploader.publishResult(result) {
		t.Fatal("publishResult reported success while result channel was full")
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("publishResult blocked too long on a full channel: %s", elapsed)
	}

	uploader.Stop()
	started = time.Now()
	if uploader.publishResult(result) {
		t.Fatal("publishResult reported success after uploader cancellation")
	}
	if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
		t.Fatalf("cancelled publishResult did not return promptly: %s", elapsed)
	}
}

func TestCommUploaderBoundsPendingQueueAndRetainsNewestSessions(t *testing.T) {
	uploader := NewCommUploader(&CommSettingsConfig{}, nil)
	total := maxPendingUploadSessions + 17
	sessions := make([]*AudioSession, total)
	for i := range sessions {
		sessions[i] = &AudioSession{SessionID: fmt.Sprintf("session-%d", i)}
		uploader.AddToQueue(sessions[i])
	}

	if got := uploader.GetPendingCount(); got != maxPendingUploadSessions {
		t.Fatalf("pending uploads=%d want=%d", got, maxPendingUploadSessions)
	}
	if got := uploader.GetDroppedCount(); got != 17 {
		t.Fatalf("dropped uploads=%d want=17", got)
	}

	uploader.mu.Lock()
	defer uploader.mu.Unlock()
	if got := uploader.pendingQueue[0].Session.SessionID; got != "session-17" {
		t.Fatalf("oldest retained session=%q want=session-17", got)
	}
	if got := uploader.pendingQueue[len(uploader.pendingQueue)-1].Session.SessionID; got != "session-4112" {
		t.Fatalf("newest retained session=%q want=session-4112", got)
	}
}

func TestCommSyncerStopWakesResultListener(t *testing.T) {
	cs := &CommSyncer{
		pending:    make([]*dbRecord, 0),
		resultChan: make(chan *UploadResult),
	}
	cs.Start()

	stopped := make(chan struct{})
	go func() {
		cs.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("CommSyncer.Stop did not wake the result listener")
	}

	// A stopped syncer can be started again without reusing a closed stop
	// channel or creating a second listener for the same result stream.
	cs.Start()
	cs.Stop()
}

func TestCommBufferConfigUpdatesAreSynchronizedAndCopied(t *testing.T) {
	config := &CommSettingsConfig{Enabled: false, MinDurationMs: 0}
	buffer := NewCommBuffer(config)
	config.Enabled = true // external mutation must not change the buffer copy
	buffer.AppendPacket("source", 1, 1, nil, nil, CommSenderSnapshot{}, nil, []byte{1})
	if got := buffer.GetActiveSessionCount(); got != 0 {
		t.Fatalf("external config mutation enabled recording: sessions=%d", got)
	}

	buffer.UpdateConfig(&CommSettingsConfig{Enabled: true, MinDurationMs: 0})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if worker%2 == 0 {
					buffer.UpdateConfig(&CommSettingsConfig{Enabled: true, MinDurationMs: 0})
				} else {
					buffer.AppendPacket("source", 1, 1, nil, nil, CommSenderSnapshot{}, nil, []byte{1, 2, 3})
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestCommRecordWorkerCanRestartAfterStop(t *testing.T) {
	ensureCommRecordWorker()
	commRecordMu.Lock()
	firstQueue := commRecordQueue
	firstRunning := commRecordRunning
	commRecordMu.Unlock()
	if !firstRunning || firstQueue == nil {
		t.Fatal("record worker did not start")
	}
	stopCommRecordWorker()

	ensureCommRecordWorker()
	commRecordMu.Lock()
	secondQueue := commRecordQueue
	secondRunning := commRecordRunning
	commRecordMu.Unlock()
	if !secondRunning || secondQueue == nil || secondQueue == firstQueue {
		t.Fatal("record worker did not restart with a fresh queue")
	}
	stopCommRecordWorker()
}
