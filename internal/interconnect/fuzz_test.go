package interconnect

import (
	"bytes"
	"encoding/json"
	"testing"

	"draarl/internal/protocol"
)

func FuzzEnvelopeUnmarshal(f *testing.F) {
	key := []byte("0123456789abcdef0123456789abcdef")
	env := NewEnvelope(SubtypeRelayUpstream, "edge-fuzz", 7, 11, []byte("payload"))
	env.KeyEpoch = 2
	valid, err := env.Marshal(key)
	if err != nil {
		f.Fatal(err)
	}
	control, err := env.MarshalControl(key)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(control)
	f.Add([]byte("DraA"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, controlFrame := range []bool{false, true} {
			var decoded Envelope
			var decodeErr error
			if controlFrame {
				decoded, decodeErr = UnmarshalControl(data, key)
			} else {
				decoded, decodeErr = Unmarshal(data, key)
			}
			if decodeErr != nil {
				continue
			}
			decoded.AuthTag = nil
			var wire []byte
			if controlFrame {
				wire, decodeErr = decoded.MarshalControl(key)
			} else {
				wire, decodeErr = decoded.Marshal(key)
			}
			if decodeErr != nil || len(wire) == 0 {
				t.Fatalf("decoded envelope did not re-encode: %v", decodeErr)
			}
		}
	})
}

func FuzzControlFrameDecode(f *testing.F) {
	valid, err := marshalControlMessage(ControlMessage{Kind: controlHeartbeat, NodeID: "edge-fuzz", MessageID: 9})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		message, _, err := readControlMessageSizeLimit(bytes.NewReader(data), controlMaxFrame)
		if err != nil {
			return
		}
		wire, err := marshalControlMessage(message)
		if err != nil {
			t.Fatalf("decoded control message did not re-encode: %v", err)
		}
		if _, _, err := readControlMessageSizeLimit(bytes.NewReader(wire), controlMaxFrame); err != nil {
			t.Fatalf("canonical control message did not decode: %v", err)
		}
	})
}

func FuzzSnapshotAssembler(f *testing.F) {
	p := NewProjection(7)
	p.Version = 2
	p.Devices[1] = DeviceRoute{SessionID: 1, DeviceID: 1, DomainID: 3, SessionEpoch: 1}
	begin, chunks, err := SplitProjection(9, p)
	if err != nil {
		f.Fatal(err)
	}
	seed, err := json.Marshal(struct {
		Begin  SnapshotBegin   `json:"begin"`
		Chunks []SnapshotChunk `json:"chunks"`
		Commit SnapshotCommit  `json:"commit"`
	}{begin, chunks, SnapshotCommit{SnapshotID: begin.SnapshotID}})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{"begin":{"snapshot_id":1,"cluster_epoch":1,"chunks":10000,"total_bytes":1}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Begin  SnapshotBegin   `json:"begin"`
			Chunks []SnapshotChunk `json:"chunks"`
			Commit SnapshotCommit  `json:"commit"`
		}
		if json.Unmarshal(data, &input) != nil {
			return
		}
		assembler, err := NewSnapshotAssembler(input.Begin)
		if err != nil {
			return
		}
		for _, chunk := range input.Chunks {
			_ = assembler.Add(chunk)
		}
		_, _ = assembler.Commit(input.Commit)
	})
}

func FuzzRelayFrameUnmarshal(f *testing.F) {
	inner := protocol.EncodeDraARLv1("alice", "", 1, protocol.DraARLTypeTextMessage, 2, 7, "BG5ABC", []byte("hello"))
	wire, err := (RelayFrame{SessionID: 1, SessionEpoch: 2, DomainID: 3, RequiredProjectionVersion: 4, InnerPacket: inner}).MarshalBinary()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(wire)
	f.Add([]byte("short"))
	f.Fuzz(func(t *testing.T, data []byte) {
		frame, err := UnmarshalRelayFrame(data)
		if err != nil {
			return
		}
		if _, err := frame.MarshalBinary(); err != nil {
			t.Fatalf("decoded relay frame did not re-encode: %v", err)
		}
		_ = protocol.ValidateRelayInnerPacket(frame.InnerPacket)
	})
}
