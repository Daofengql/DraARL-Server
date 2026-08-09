package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWithSourceGroupIDCopiesPacketAndOnlyChangesReservedField(t *testing.T) {
	original := EncodeDraARLv1("alice", "", SSIDGhostWeb, DraARLTypeTextMessage, DraARLDevModelBrowser, 0, "BG7AAA", []byte("hello"))
	enriched, ok := WithSourceGroupID(original, 123456)
	if !ok {
		t.Fatal("valid packet was rejected")
	}
	if binary.BigEndian.Uint32(enriched[DraARLv1ReservedOffset:DraARLv1HeaderSize]) != 123456 {
		t.Fatalf("reserved=%v", enriched[DraARLv1ReservedOffset:DraARLv1HeaderSize])
	}
	if !bytes.Equal(original[:DraARLv1ReservedOffset], enriched[:DraARLv1ReservedOffset]) ||
		!bytes.Equal(original[DraARLv1HeaderSize:], enriched[DraARLv1HeaderSize:]) {
		t.Fatal("source metadata changed packet fields outside Reserved")
	}
	if !bytes.Equal(original[DraARLv1ReservedOffset:DraARLv1HeaderSize], []byte{0, 0, 0, 0}) {
		t.Fatal("input packet was mutated")
	}
}

func TestWithSourceGroupIDRejectsInvalidInput(t *testing.T) {
	if _, ok := WithSourceGroupID(make([]byte, DraARLv1HeaderSize-1), 1); ok {
		t.Fatal("short packet accepted")
	}
	if _, ok := WithSourceGroupID(make([]byte, DraARLv1HeaderSize), 0); ok {
		t.Fatal("zero group accepted")
	}
}
