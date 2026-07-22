package interconnect

import (
	"bytes"
	"testing"
	"time"
)

func TestEnvelopeRoundTripAndAuthentication(t *testing.T) {
	key := []byte("test-node-session-key")
	original := NewEnvelope(SubtypeRelayUpstream, "edge-fuzhou", 11, 22, []byte{1, 2, 3})
	original.ClusterEpoch, original.ProjectionVersion, original.KeyEpoch = 7, 9, 2
	wire, err := original.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Unmarshal(wire, key)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SourceNodeID != original.SourceNodeID || decoded.Subtype != original.Subtype || decoded.NodeSessionID != original.NodeSessionID || !bytes.Equal(decoded.Payload, original.Payload) {
		t.Fatalf("decoded envelope mismatch: %#v", decoded)
	}
	wire[len(wire)-1] ^= 1
	if _, err := Unmarshal(wire, key); err == nil {
		t.Fatal("tampered packet was accepted")
	}
}

func TestEnvelopeRejectsOversizeAndOldFrame(t *testing.T) {
	e := NewEnvelope(SubtypeRelayDownstream, "edge", 1, 1, make([]byte, NodeMaxDatagramSize))
	if _, err := e.Marshal([]byte("key")); err == nil {
		t.Fatal("oversize envelope accepted")
	}
	e = NewEnvelope(SubtypeRelayDownstream, "edge", 1, 1, nil)
	e.SentAtMillis = time.Now().Add(-time.Second).UnixMilli()
	wire, err := e.Marshal([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Unmarshal(wire, []byte("key"))
	if err != nil || !decoded.Expired(time.Now(), 100*time.Millisecond) {
		t.Fatalf("expected expired packet, decoded=%v err=%v", decoded, err)
	}
}
