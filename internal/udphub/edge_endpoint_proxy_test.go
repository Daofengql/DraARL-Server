package udphub

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"draarl/internal/config"
)

type edgeEndpointPacket struct {
	data       []byte
	remoteAddr *net.UDPAddr
	realAddr   *net.UDPAddr
}

func TestEdgeEndpointProxyV2SeparatesTransportAndClientAddresses(t *testing.T) {
	received := make(chan edgeEndpointPacket, 1)
	endpoint, err := NewEdgeEndpoint("127.0.0.1:0", "v2", func(data []byte, remoteAddr, realAddr *net.UDPAddr) {
		received <- edgeEndpointPacket{data: append([]byte(nil), data...), remoteAddr: cloneTestUDPAddr(remoteAddr), realAddr: cloneTestUDPAddr(realAddr)}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()

	proxy, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	clientAddr := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 27), Port: 23456}
	payload := []byte("ordinary DraARL packet")
	wire := encodeProxyV2UDP(t, clientAddr, endpoint.Addr().(*net.UDPAddr), payload)
	if _, err := proxy.WriteToUDP(wire, endpoint.Addr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}

	var packet edgeEndpointPacket
	select {
	case packet = <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("edge endpoint did not receive PROXY v2 datagram")
	}
	if !bytes.Equal(packet.data, payload) {
		t.Fatalf("payload=%q want=%q", packet.data, payload)
	}
	if !udpAddrTestEqual(packet.remoteAddr, proxy.LocalAddr().(*net.UDPAddr)) {
		t.Fatalf("transport address=%v want=%v", packet.remoteAddr, proxy.LocalAddr())
	}
	if !udpAddrTestEqual(packet.realAddr, clientAddr) {
		t.Fatalf("real address=%v want=%v", packet.realAddr, clientAddr)
	}

	// Replies must go back through the FRP transport address. Sending to the
	// address advertised in the PROXY header would bypass the proxy and fail.
	if err := endpoint.SendTo([]byte("reply"), packet.remoteAddr); err != nil {
		t.Fatal(err)
	}
	_ = proxy.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	n, _, err := proxy.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("FRP transport did not receive reply: %v", err)
	}
	if string(buf[:n]) != "reply" {
		t.Fatalf("reply=%q", buf[:n])
	}
}

func TestEdgeEndpointProxyV2SupportsIPv6ClientAddress(t *testing.T) {
	received := make(chan *net.UDPAddr, 1)
	endpoint, err := NewEdgeEndpoint("127.0.0.1:0", "v2", func(_ []byte, _, realAddr *net.UDPAddr) {
		received <- cloneTestUDPAddr(realAddr)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	proxy, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	clientAddr := &net.UDPAddr{IP: net.ParseIP("2001:db8::27"), Port: 34567}
	wire := encodeProxyV2UDP(t, clientAddr, &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 60050}, []byte("v6"))
	if _, err := proxy.WriteToUDP(wire, endpoint.Addr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	select {
	case realAddr := <-received:
		if !udpAddrTestEqual(realAddr, clientAddr) {
			t.Fatalf("real address=%v want=%v", realAddr, clientAddr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("edge endpoint did not parse IPv6 PROXY v2 address")
	}
}

func TestEdgeEndpointProxyV2KeepsUnwrappedDatagramsCompatible(t *testing.T) {
	received := make(chan edgeEndpointPacket, 1)
	endpoint, err := NewEdgeEndpoint("127.0.0.1:0", "v2", func(data []byte, remoteAddr, realAddr *net.UDPAddr) {
		received <- edgeEndpointPacket{data: append([]byte(nil), data...), remoteAddr: cloneTestUDPAddr(remoteAddr), realAddr: cloneTestUDPAddr(realAddr)}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	sender, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	if _, err := sender.WriteToUDP([]byte("type0-or-direct"), endpoint.Addr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	select {
	case packet := <-received:
		if string(packet.data) != "type0-or-direct" || !udpAddrTestEqual(packet.remoteAddr, packet.realAddr) {
			t.Fatalf("unexpected direct packet: %#v", packet)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("edge endpoint did not preserve unwrapped datagram")
	}
}

func TestEdgeEndpointProxyV2UsesEndpointLocalTrustedCIDRs(t *testing.T) {
	received := make(chan edgeEndpointPacket, 1)
	// The actual test sender is 127.0.0.1, which is intentionally outside the
	// configured proxy network. The header must therefore remain opaque data.
	endpoint, err := NewEdgeEndpoint("127.0.0.1:0", "v2", func(data []byte, remoteAddr, realAddr *net.UDPAddr) {
		received <- edgeEndpointPacket{data: append([]byte(nil), data...), remoteAddr: cloneTestUDPAddr(remoteAddr), realAddr: cloneTestUDPAddr(realAddr)}
	}, []string{"192.0.2.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	sender, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	clientAddr := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 27), Port: 23456}
	payload := []byte("untrusted-proxy-payload")
	wire := encodeProxyV2UDP(t, clientAddr, endpoint.Addr().(*net.UDPAddr), payload)
	if _, err := sender.WriteToUDP(wire, endpoint.Addr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	select {
	case packet := <-received:
		if !bytes.Equal(packet.data, wire) {
			t.Fatalf("untrusted source was unwrapped: data=%q", packet.data)
		}
		if !udpAddrTestEqual(packet.remoteAddr, packet.realAddr) {
			t.Fatalf("untrusted source changed real address: remote=%v real=%v", packet.remoteAddr, packet.realAddr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("edge endpoint did not receive untrusted PROXY datagram")
	}
}

func TestEdgeEndpointRejectsUnsupportedProxyProtocol(t *testing.T) {
	if _, err := NewEdgeEndpoint("127.0.0.1:0", "v1", func([]byte, *net.UDPAddr, *net.UDPAddr) {}); err == nil {
		t.Fatal("expected unsupported edge proxy protocol to be rejected")
	}
}

func TestEdgeEndpointRejectsInvalidProxyTrustedCIDR(t *testing.T) {
	endpoint, err := NewEdgeEndpoint("127.0.0.1:0", "v2", func([]byte, *net.UDPAddr, *net.UDPAddr) {}, []string{"not-a-cidr"})
	if endpoint != nil {
		_ = endpoint.Close()
		t.Fatal("invalid trusted CIDR returned a live edge endpoint")
	}
	if err == nil {
		t.Fatal("invalid edge proxy trusted CIDR was accepted")
	}
}

func TestEdgeEndpointRequiresProxyTrustedCIDRsForReleaseV2(t *testing.T) {
	previousRelease := config.IsReleaseBuild()
	config.SetReleaseBuild(true)
	t.Cleanup(func() {
		config.SetReleaseBuild(previousRelease)
	})

	if _, err := NewEdgeEndpoint("127.0.0.1:0", "v2", func([]byte, *net.UDPAddr, *net.UDPAddr) {}); err == nil || !strings.Contains(err.Error(), "trusted CIDRs") {
		t.Fatalf("expected release v2 trust-boundary validation error, got %v", err)
	}

	endpoint, err := NewEdgeEndpoint("127.0.0.1:0", "v2", func([]byte, *net.UDPAddr, *net.UDPAddr) {}, []string{"192.0.2.0/24"})
	if err != nil {
		t.Fatalf("valid release v2 endpoint rejected: %v", err)
	}
	endpoint.Close()
}

func TestEdgeEndpointFanoutDoesNotSynchronouslyFallbackWhenSenderStops(t *testing.T) {
	endpoint, err := NewEdgeEndpoint("127.0.0.1:0", "", func([]byte, *net.UDPAddr, *net.UDPAddr) {})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	receiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	target, ok := NewEdgeFanoutTarget(receiver.LocalAddr().(*net.UDPAddr), 2, "receiver", 1)
	if !ok {
		t.Fatal("failed to create fanout target")
	}
	plan := endpoint.PrepareFanout([]EdgeFanoutTarget{target})
	if plan == nil {
		t.Fatal("failed to prepare fanout plan")
	}
	endpoint.sender.stop()
	completed := make(chan EdgeFanoutResult, 1)
	if !endpoint.FanoutPlan([]byte("voice"), plan, 1, "source", 1, func(result EdgeFanoutResult) {
		completed <- result
	}) {
		t.Fatal("fanout should account a dropped frame after sender stop")
	}
	select {
	case result := <-completed:
		if result.Attempted != 0 || result.Sent != 0 || result.Dropped != 1 || result.Errors != 0 {
			t.Fatalf("unexpected dropped fanout result: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("fanout completion was not reported")
	}
	_ = receiver.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, _, err := receiver.ReadFromUDP(make([]byte, 32)); err == nil {
		t.Fatal("stopped sender synchronously wrote a fallback datagram")
	}
}

func encodeProxyV2UDP(t *testing.T, source, destination *net.UDPAddr, payload []byte) []byte {
	t.Helper()
	source4, destination4 := source.IP.To4(), destination.IP.To4()
	addressLength := ipv6AddrLen
	family := byte(afInet6 | protoDgram)
	if source4 != nil && destination4 != nil {
		addressLength = ipv4AddrLen
		family = afInet | protoDgram
	}
	wire := make([]byte, 16+addressLength+len(payload))
	copy(wire[:12], proxyProtocolV2Signature[:])
	wire[12] = proxyProtocolVersion2 | proxyCommandProxy
	wire[13] = family
	binary.BigEndian.PutUint16(wire[14:16], uint16(addressLength))
	if addressLength == ipv4AddrLen {
		copy(wire[16:20], source4)
		copy(wire[20:24], destination4)
		binary.BigEndian.PutUint16(wire[24:26], uint16(source.Port))
		binary.BigEndian.PutUint16(wire[26:28], uint16(destination.Port))
	} else {
		source16, destination16 := source.IP.To16(), destination.IP.To16()
		if source16 == nil || destination16 == nil {
			t.Fatal("invalid PROXY v2 test address")
		}
		copy(wire[16:32], source16)
		copy(wire[32:48], destination16)
		binary.BigEndian.PutUint16(wire[48:50], uint16(source.Port))
		binary.BigEndian.PutUint16(wire[50:52], uint16(destination.Port))
	}
	copy(wire[16+addressLength:], payload)
	return wire
}

func cloneTestUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	copyAddr := *addr
	copyAddr.IP = append(net.IP(nil), addr.IP...)
	return &copyAddr
}

func udpAddrTestEqual(a, b *net.UDPAddr) bool {
	return a != nil && b != nil && a.Port == b.Port && a.Zone == b.Zone && a.IP.Equal(b.IP)
}
