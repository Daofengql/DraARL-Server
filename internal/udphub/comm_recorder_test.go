package udphub

import "testing"

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

	recorder.RecordPacket(1, 1, nil, nil, []byte{1, 2, 3})
	if got := recorder.buffer.GetActiveSessionCount(); got != 0 {
		t.Fatalf("disabled recorder created %d session(s)", got)
	}

	enabled := *disabled
	enabled.Enabled = true
	recorder.UpdateConfig(&enabled)
	recorder.RecordPacket(1, 1, nil, nil, []byte{1, 2, 3})
	if got := recorder.buffer.GetActiveSessionCount(); got != 1 {
		t.Fatalf("recorder did not activate after config reload: sessions=%d", got)
	}

	stats := recorder.GetStats()
	if stats["enabled"] != true || stats["running"] != true {
		t.Fatalf("unexpected recorder stats after enable: %#v", stats)
	}
}
