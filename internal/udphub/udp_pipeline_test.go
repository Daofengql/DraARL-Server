package udphub

import (
	"net"
	"testing"

	"draarl/internal/protocol"
)

func TestUDPDatagramShardFollowsDeviceIdentityAcrossSourceChanges(t *testing.T) {
	packet := protocol.EncodeDraARLv1("alice", "secret", 7, protocol.DraARLTypeOpus16K, 2, 123456, "", []byte{1})
	firstAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10001}
	secondAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 20002}

	first := udpDatagramShard(packet, firstAddr, 16)
	second := udpDatagramShard(packet, secondAddr, 16)
	if first != second {
		t.Fatalf("same device moved shards after source change: %d != %d", first, second)
	}

	packet[50] = 8
	if other := udpDatagramShard(packet, firstAddr, 16); other == first {
		t.Logf("different SSID hashed to the same shard (%d); allowed but uncommon", first)
	}
}
