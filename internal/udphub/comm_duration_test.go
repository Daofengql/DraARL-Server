package udphub

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestOpusPacketSamples(t *testing.T) {
	for _, tc := range []struct {
		name    string
		packet  []byte
		samples int
	}{
		{"silk 10ms", []byte{0x00, 0x01}, 480},
		{"silk 20ms", []byte{0x08, 0x01}, 960},
		{"silk 40ms", []byte{0x10, 0x01}, 1920},
		{"silk 60ms", []byte{0x18, 0x01}, 2880},
		{"hybrid 10ms", []byte{0x60, 0x01}, 480},
		{"hybrid 20ms", []byte{0x68, 0x01}, 960},
		{"celt 2.5ms", []byte{0x80, 0x01}, 120},
		{"celt 5ms", []byte{0x88, 0x01}, 240},
		{"celt 10ms", []byte{0x90, 0x01}, 480},
		{"celt 20ms", []byte{0x98, 0x01}, 960},
		{"two equal frames", []byte{0x19, 0x01, 0x02}, 5760},
		{"two unequal frames", []byte{0x1a, 0x01, 0x01, 0x02}, 5760},
		{"multiple frames", []byte{0x83, 48, 0x01}, 5760},
		{"too long", []byte{0x1b, 3, 0x01}, 0},
		{"zero frames", []byte{0x83, 0}, 0},
		{"missing frame count", []byte{0x83}, 0},
		{"empty packet", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := opusPacketSamples(tc.packet); got != tc.samples {
				t.Fatalf("samples=%d, want %d", got, tc.samples)
			}
		})
	}
}

func mergedRecordingFrames(frames ...[]byte) []byte {
	var data []byte
	for _, frame := range frames {
		data = binary.BigEndian.AppendUint16(data, uint16(len(frame)))
		data = append(data, frame...)
	}
	return data
}

func TestRecordingThresholdUsesAudioDuration(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		packet                         []byte
		packets, durationMs, threshold int
		keep                           bool
	}{
		{"20ms packets below threshold", []byte{0x08, 1}, 10, 200, 500, false},
		{"20ms packets at threshold", []byte{0x08, 1}, 25, 500, 500, true},
		{"60ms packets below threshold", []byte{0x18, 1}, 8, 480, 500, false},
		{"60ms packets above threshold", []byte{0x18, 1}, 9, 540, 500, true},
		{"merged packets below threshold", mergedRecordingFrames([]byte{0x18, 1}, []byte{0x18, 2}), 4, 480, 500, false},
		{"merged packets at threshold", mergedRecordingFrames([]byte{0x18, 1}, []byte{0x18, 2}), 5, 600, 600, true},
		{"fractional frames accumulate", []byte{0x80, 1}, 200, 500, 500, true},
		{"zero threshold", []byte{0x08, 1}, 1, 20, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffer := NewCommBuffer(&CommSettingsConfig{Enabled: true, MinDurationMs: tc.threshold})
			ended := make(chan *AudioSession, 1)
			buffer.SetOnSessionEnd(func(session *AudioSession) { ended <- session })
			for range tc.packets {
				buffer.AppendPacket("source", 1, 1, nil, nil, CommSenderSnapshot{}, nil, tc.packet)
			}
			session := buffer.sessions["source"]
			if got := estimateSessionDurationMs(session); got != tc.durationMs {
				t.Fatalf("duration=%d, want %d", got, tc.durationMs)
			}
			// Even a long network wait must not inflate the saved audio duration.
			session.StartTime = time.Now().Add(-time.Minute)
			session.LastPacketTime = time.Now().Add(-time.Second)
			buffer.CheckTimeout()
			select {
			case saved := <-ended:
				if !tc.keep || estimateSessionDurationMs(saved) != tc.durationMs {
					t.Fatalf("unexpected saved session: duration=%d, keep=%v", estimateSessionDurationMs(saved), tc.keep)
				}
			case <-time.After(100 * time.Millisecond):
				if tc.keep {
					t.Fatal("eligible session was discarded")
				}
			}
		})
	}
}

func TestRecordingThresholdHotReloadAndAudioLimit(t *testing.T) {
	buffer := NewCommBuffer(&CommSettingsConfig{Enabled: true, MinDurationMs: 2000})
	ended := make(chan *AudioSession, 2)
	buffer.SetOnSessionEnd(func(session *AudioSession) { ended <- session })
	for range 25 {
		buffer.AppendPacket("source", 1, 1, nil, nil, CommSenderSnapshot{}, nil, []byte{0x08, 1})
	}
	buffer.UpdateConfig(&CommSettingsConfig{Enabled: true, MinDurationMs: 1000, MaxDurationSec: 1})
	for range 25 {
		buffer.AppendPacket("source", 1, 1, nil, nil, CommSenderSnapshot{}, nil, []byte{0x08, 1})
	}
	select {
	case saved := <-ended:
		if estimateSessionDurationMs(saved) != 1000 {
			t.Fatalf("duration=%d", estimateSessionDurationMs(saved))
		}
	case <-time.After(time.Second):
		t.Fatal("audio maximum did not finalize a burst delivered faster than real time")
	}
	buffer.AppendPacket("source", 1, 1, nil, nil, CommSenderSnapshot{}, nil, []byte{0x08, 1})
	buffer.UpdateConfig(&CommSettingsConfig{Enabled: false})
	if buffer.GetActiveSessionCount() != 0 {
		t.Fatal("disabled recording retained an active session")
	}
	buffer.AppendPacket("empty", 1, 1, nil, nil, CommSenderSnapshot{}, nil, nil)
	if buffer.GetActiveSessionCount() != 0 {
		t.Fatal("empty data created a recording")
	}
}
