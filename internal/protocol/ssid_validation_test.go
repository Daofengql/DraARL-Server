package protocol

import (
	"encoding/binary"
	"math"
	"net"
	"testing"
)

func TestIsValidNormalSSID(t *testing.T) {
	validCases := []byte{1, 99, 106, 254}
	for _, ssid := range validCases {
		if !IsValidNormalSSID(ssid) {
			t.Fatalf("expected ssid=%d to be valid", ssid)
		}
	}

	invalidCases := []byte{0, 100, 101, 102, 103, 104, 105, 255}
	for _, ssid := range invalidCases {
		if IsValidNormalSSID(ssid) {
			t.Fatalf("expected ssid=%d to be invalid", ssid)
		}
	}
}

func TestEncodeHeartbeatRejectResponse(t *testing.T) {
	req := &DraARLv1Packet{
		Username: "test-user",
		SSID:     120,
		Type:     DraARLTypeHeartbeat,
		DevModel: DraARLDevModelESP32Radio,
		DMRID:    123456,
	}

	resp := EncodeHeartbeatRejectResponse(req, HeartbeatStatusDeviceConflictOnline, "device_conflict_online")
	packet, err := NewDraARLv1Packet(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10000}, resp)
	if err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if packet.Type != DraARLTypeHeartbeat {
		t.Fatalf("expected heartbeat packet, got %d", packet.Type)
	}
	if packet.SSID != req.SSID {
		t.Fatalf("expected ssid %d, got %d", req.SSID, packet.SSID)
	}
	if len(packet.DATA) == 0 || packet.DATA[0] != HeartbeatStatusDeviceConflictOnline {
		t.Fatalf("expected reject status %d, got %v", HeartbeatStatusDeviceConflictOnline, packet.DATA)
	}
}

func TestExtractHeartbeatMAC(t *testing.T) {
	data := make([]byte, HeartbeatGPSPayloadSize)
	binary.BigEndian.PutUint64(data[0:8], math.Float64bits(22.3))
	binary.BigEndian.PutUint64(data[8:16], math.Float64bits(113.9))
	binary.BigEndian.PutUint64(data[16:24], math.Float64bits(15))
	data = append(data, []byte("aa:bb:cc:dd:ee:ff")...)

	if got := ExtractHeartbeatMAC(data); got != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("expected normalized mac, got %q", got)
	}
}
