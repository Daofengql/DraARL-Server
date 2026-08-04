package udphub

import (
	"strings"
	"testing"
)

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

	recorder.RecordPacket(PhysicalCommRecordSourceKey(1), 1, 1, nil, nil, []byte{1, 2, 3})
	if got := recorder.buffer.GetActiveSessionCount(); got != 0 {
		t.Fatalf("disabled recorder created %d session(s)", got)
	}

	enabled := *disabled
	enabled.Enabled = true
	recorder.UpdateConfig(&enabled)
	recorder.RecordPacket(PhysicalCommRecordSourceKey(1), 1, 1, nil, nil, []byte{1, 2, 3})
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

	buffer.AppendPacket(sourceA, 0, 101, &groupID, &userID, []byte{1, 2, 3})
	buffer.AppendPacket(sourceB, 0, 101, &groupID, &userID, []byte{4, 5, 6})

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
