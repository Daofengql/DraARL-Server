package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestDraARLv1PhysicalPacketLayoutCompatibility(t *testing.T) {
	if DraARLv1HeaderSize != 90 || DraARLv1ReservedOffset != 86 {
		t.Fatalf("UDP packet layout changed: header=%d reserved_offset=%d", DraARLv1HeaderSize, DraARLv1ReservedOffset)
	}
	payload := []byte{0x11, 0x22, 0x33}
	wire := EncodeDraARLv1("physical-user", "devicepass", 7, DraARLTypeHeartbeat, DraARLDevModelESP32NoRadio, 0x123456, "BG7TEST", payload)
	if len(wire) != 93 || binary.BigEndian.Uint16(wire[4:6]) != 93 {
		t.Fatalf("physical UDP packet length changed: bytes=%d header=%d", len(wire), binary.BigEndian.Uint16(wire[4:6]))
	}
	if string(wire[0:4]) != DraARLVersion || wire[48] != DraARLTypeHeartbeat || wire[49] != DraARLDevModelESP32NoRadio || wire[50] != 7 {
		t.Fatalf("physical UDP fixed header fields changed: %v", wire[:54])
	}
	if !bytes.Equal(wire[51:54], []byte{0x12, 0x34, 0x56}) ||
		!bytes.Equal(wire[DraARLv1ReservedOffset:DraARLv1HeaderSize], []byte{0, 0, 0, 0}) ||
		!bytes.Equal(wire[DraARLv1HeaderSize:], payload) {
		t.Fatalf("physical UDP packet offsets changed: %v", wire)
	}
	decoded, err := NewDraARLv1Packet(nil, wire)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Username != "physical-user" || decoded.DevicePassword != "devicepass" || decoded.CallSign != "BG7TEST" || decoded.DMRID != 0x123456 {
		t.Fatalf("physical UDP round trip changed: %#v", decoded)
	}
}
