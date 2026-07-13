package protocol

import (
	"bytes"
	"testing"
)

func TestPrepareForwardPacketRewritesHeader(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03, 0x04}
	src := EncodeDraARLv1("user1", "secretpwd", 7, DraARLTypeOpus16K, 1, 12345, "", payload)

	out := PrepareForwardPacket(src, "user1", "BG7ABC", 7, DraARLTypeOpus16K, 1, 12345, payload)
	if out == nil {
		t.Fatal("expected non-nil forward packet")
	}
	if len(out) != len(src) {
		t.Fatalf("expected same length, got %d want %d", len(out), len(src))
	}

	if !bytes.Equal(out[38:48], make([]byte, 10)) {
		t.Fatalf("expected password cleared, got %q", out[38:48])
	}
	callsign := string(bytes.TrimRight(out[54:86], "\x00"))
	if callsign != "BG7ABC" {
		t.Fatalf("expected callsign BG7ABC, got %q", callsign)
	}
	if !bytes.Equal(out[DraARLv1HeaderSize:], payload) {
		t.Fatalf("payload mismatch")
	}
	username := string(bytes.TrimRight(out[6:38], "\x00"))
	if username != "user1" {
		t.Fatalf("expected username user1, got %q", username)
	}
	ReleaseForwardPacket(out)
}

func TestPrepareForwardPacketFallbackEncode(t *testing.T) {
	payload := []byte{9, 8, 7}
	out := PrepareForwardPacket([]byte("bad"), "u", "CS", 3, DraARLTypeTextMessage, 2, 1, payload)
	if len(out) != DraARLv1HeaderSize+len(payload) {
		t.Fatalf("unexpected length %d", len(out))
	}
	if out[48] != DraARLTypeTextMessage {
		t.Fatalf("unexpected type %d", out[48])
	}
	ReleaseForwardPacket(out)
}

func TestReleaseForwardPacketReuse(t *testing.T) {
	payload := []byte{1, 2, 3}
	src := EncodeDraARLv1("a", "pwd12345", 1, DraARLTypeOpus16K, 1, 1, "", payload)
	out1 := PrepareForwardPacket(src, "a", "CS1", 1, DraARLTypeOpus16K, 1, 1, payload)
	ReleaseForwardPacket(out1)
	out2 := PrepareForwardPacket(src, "a", "CS2", 1, DraARLTypeOpus16K, 1, 1, payload)
	if out2 == nil || len(out2) != len(src) {
		t.Fatalf("reuse failed")
	}
	cs := string(bytes.TrimRight(out2[54:86], "\x00"))
	if cs != "CS2" {
		t.Fatalf("expected CS2 got %q", cs)
	}
	ReleaseForwardPacket(out2)
}
