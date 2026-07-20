package udphub

import (
	"net"
	"testing"
)

func TestFanoutSenderCreatesRequestedWriterCount(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Skipf("IPv4 UDP listener unavailable: %v", err)
	}
	defer conn.Close()
	sender := newFanoutSender(conn, 4, 8)
	defer sender.stop()
	if len(sender.writers) != 4 {
		t.Fatalf("writer count = %d, want 4", len(sender.writers))
	}
}
