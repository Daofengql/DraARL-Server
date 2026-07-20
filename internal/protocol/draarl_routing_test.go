package protocol

import (
	"bytes"
	"net"
	"testing"
)

func TestDecodeRoutingOnlyMaterializesRequiredStrings(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60050}
	voice := EncodeDraARLv1("alice", "secret", 7, DraARLTypeOpus16K, 2, 123456, "BG7TEST", []byte{1, 2, 3})
	packet, err := NewDraARLv1RoutingPacket(addr, voice)
	if err != nil {
		t.Fatalf("decode voice: %v", err)
	}
	if packet.Username != "alice" || packet.SSID != 7 || packet.DMRID != 123456 {
		t.Fatalf("routing fields = %#v", packet)
	}
	if packet.DevicePassword != "" || packet.CallSign != "" {
		t.Fatalf("voice materialized cold strings: password=%q callsign=%q", packet.DevicePassword, packet.CallSign)
	}
	ReleaseDraARLv1RoutingPacket(packet)

	heartbeat := EncodeDraARLv1("alice", "secret", 7, DraARLTypeHeartbeat, 2, 123456, "", nil)
	packet, err = NewDraARLv1RoutingPacket(addr, heartbeat)
	if err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	defer ReleaseDraARLv1RoutingPacket(packet)
	if packet.DevicePassword != "secret" {
		t.Fatalf("heartbeat password = %q", packet.DevicePassword)
	}
}

func TestRoutingDecodeAndForwardStillClearsSensitiveHeader(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60050}
	source := EncodeDraARLv1("alice", "secret", 7, DraARLTypeOpus16K, 2, 123456, "CLIENT", []byte{1, 2, 3})
	packet, err := NewDraARLv1RoutingPacket(addr, source)
	if err != nil {
		t.Fatalf("routing decode: %v", err)
	}
	defer ReleaseDraARLv1RoutingPacket(packet)

	forwarded := PrepareForwardPacket(source, packet.Username, "BG7SERVER", packet.SSID, packet.Type, packet.DevModel, packet.DMRID, packet.DATA)
	defer ReleaseForwardPacket(forwarded)
	if !bytes.Equal(forwarded[38:48], make([]byte, 10)) {
		t.Fatalf("forwarded password bytes = %q", forwarded[38:48])
	}
	var decoded DraARLv1Packet
	if err := decoded.Decode(forwarded); err != nil {
		t.Fatalf("decode forwarded packet: %v", err)
	}
	if decoded.DevicePassword != "" || decoded.Username != "alice" || decoded.CallSign != "BG7SERVER" {
		t.Fatalf("forwarded header = password:%q username:%q callsign:%q", decoded.DevicePassword, decoded.Username, decoded.CallSign)
	}
}

var benchmarkPacketSink *DraARLv1Packet

func BenchmarkDecodeVoiceFull(b *testing.B) {
	data := EncodeDraARLv1("benchmark-user", "secret", 7, DraARLTypeOpus16K, 2, 123456, "BG7BENCH", make([]byte, 320))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		packet := &DraARLv1Packet{}
		if err := packet.Decode(data); err != nil {
			b.Fatal(err)
		}
		benchmarkPacketSink = packet
	}
}

func BenchmarkDecodeVoiceRouting(b *testing.B) {
	data := EncodeDraARLv1("benchmark-user", "secret", 7, DraARLTypeOpus16K, 2, 123456, "BG7BENCH", make([]byte, 320))
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60050}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		packet, err := NewDraARLv1RoutingPacket(addr, data)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkPacketSink = packet
		ReleaseDraARLv1RoutingPacket(packet)
	}
}
