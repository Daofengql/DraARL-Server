package udphub

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
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

func TestEdgeEndpointRejectsUnsupportedProxyProtocol(t *testing.T) {
	if _, err := NewEdgeEndpoint("127.0.0.1:0", "v1", func([]byte, *net.UDPAddr, *net.UDPAddr) {}); err == nil {
		t.Fatal("expected unsupported edge proxy protocol to be rejected")
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
