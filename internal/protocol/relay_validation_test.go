package protocol

import "testing"

func TestValidateRelayInnerPacketAndIdentity(t *testing.T) {
	valid := EncodeDraARLv1("alice", "", 1, DraARLTypeOpus16K, DraARLDevModelESP32NoRadio, 7, "BG5ABC", []byte{1, 2, 3})
	if err := ValidateRelayInnerPacket(valid); err != nil {
		t.Fatal(err)
	}
	if !RelayInnerIdentityMatches(valid, "alice", "BG5ABC", 1) {
		t.Fatal("valid relay identity did not match")
	}
	if RelayInnerIdentityMatches(valid, "bob", "BG5ABC", 1) || RelayInnerIdentityMatches(valid, "alice", "BG5ABC", 2) || RelayInnerIdentityMatches(valid, "alice", "BG5ABD", 1) {
		t.Fatal("mismatched relay identity was accepted")
	}

	emptyCallSign := append([]byte(nil), valid...)
	clear(emptyCallSign[54:86])
	if !RelayInnerIdentityMatches(emptyCallSign, "alice", "BG5ABC", 1) {
		t.Fatal("empty compatibility callsign was rejected")
	}
}

func TestValidateRelayInnerPacketRejectsCredentialsAndUnsupportedTypes(t *testing.T) {
	credential := EncodeDraARLv1("alice", "secret", 1, DraARLTypeTextMessage, DraARLDevModelESP32NoRadio, 0, "BG5ABC", []byte("hello"))
	if err := ValidateRelayInnerPacket(credential); err == nil {
		t.Fatal("relay packet containing a device credential was accepted")
	}
	config := EncodeDraARLv1("alice", "", 1, DraARLTypeConfig, DraARLDevModelESP32NoRadio, 0, "BG5ABC", []byte{1})
	if err := ValidateRelayInnerPacket(config); err == nil {
		t.Fatal("non-realtime relay packet was accepted")
	}
	malformed := append([]byte(nil), credential...)
	malformed[4], malformed[5] = 0, 1
	if err := ValidateRelayInnerPacket(malformed); err == nil {
		t.Fatal("malformed relay length was accepted")
	}
}

func TestRelayInnerValidationDoesNotAllocate(t *testing.T) {
	wire := EncodeDraARLv1("alice", "", 1, DraARLTypeOpus16K, DraARLDevModelESP32NoRadio, 7, "BG5ABC", []byte{1, 2, 3})
	allocations := testing.AllocsPerRun(1000, func() {
		if ValidateRelayInnerPacket(wire) != nil || !RelayInnerIdentityMatches(wire, "alice", "BG5ABC", 1) {
			t.Fatal("relay validation failed")
		}
	})
	if allocations != 0 {
		t.Fatalf("relay validation allocations=%f want=0", allocations)
	}
}
