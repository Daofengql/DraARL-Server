package interconnect

import (
	"bytes"
	"net"
	"testing"
	"time"

	"draarl/internal/protocol"
)

func TestHTTPPeerUDPAdmissionEnvelope(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	linkA := CenterPeerLink{LinkID: "invite-1", LocalCenterID: "center-a", RemoteCenterID: "center-b", LocalGroupID: 12, RemoteGroupID: 38, InitiatorCenterID: "center-a", Direction: CenterLinkDirectionBidirectional, Enabled: true, Accepted: true, ForwardAudio: true, VirtualDeviceName: "Server-B", CredentialEpoch: 1}
	linkB := linkA
	linkB.LocalCenterID, linkB.RemoteCenterID = linkB.RemoteCenterID, linkB.LocalCenterID
	linkB.LocalGroupID, linkB.RemoteGroupID = linkB.RemoteGroupID, linkB.LocalGroupID
	linkB.VirtualDeviceName = "Server-A"
	received := make(chan []byte, 1)
	a := NewCenterHTTPPeerManager(func(_ CenterPeerLink, p []byte) bool { received <- p; return true })
	b := NewCenterHTTPPeerManager(nil)
	addrA := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 26060}
	addrB := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 26061}
	a.SetWriter(func(addr *net.UDPAddr, data []byte) error { b.Handle(data, addrA); return nil })
	b.SetWriter(func(addr *net.UDPAddr, data []byte) error { a.Handle(data, addrB); return nil })
	if err := a.RegisterSession(CenterHTTPPeerSession{Link: linkA, SessionID: "0123456789abcdef0123456789abcdef", SessionKey: key, RemoteUDP: addrB.String(), LocalSendAudio: true, LocalReceiveAudio: true, RemoteSendAudio: true, RemoteReceiveAudio: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterSession(CenterHTTPPeerSession{Link: linkB, SessionID: "0123456789abcdef0123456789abcdef", SessionKey: key, RemoteUDP: addrA.String(), LocalSendAudio: true, LocalReceiveAudio: true, RemoteSendAudio: true, RemoteReceiveAudio: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.Bind(linkB.LinkID); err != nil {
		t.Fatal(err)
	}
	if !a.Status(linkA.LinkID).Online || !b.Status(linkB.LinkID).Online {
		t.Fatal("authenticated UDP hello did not establish")
	}
	inner := protocol.EncodeDraARLv1("local", "", 1, protocol.DraARLTypeOpus16K, 2, 0, "CALL", []byte{1, 2, 3})
	if err := b.RelayGroup(linkB.LocalGroupID, inner); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if !bytes.Equal(got, []byte{1, 2, 3}) {
			t.Fatalf("payload leaked header: %x", got)
		}
	case <-time.After(time.Second):
		t.Fatal("audio not delivered")
	}
	// A replayed datagram is rejected by the per-session sequence window.
	if a.Handle([]byte("CPUD"), addrB) {
		t.Fatal("malformed datagram accepted")
	}
}

func TestHTTPPeerRejectsForgedHelloBeforeRebinding(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	linkA := CenterPeerLink{LinkID: "invite-auth", LocalCenterID: "center-a", RemoteCenterID: "center-b", LocalGroupID: 12, RemoteGroupID: 38, InitiatorCenterID: "center-a", Direction: CenterLinkDirectionBidirectional, Enabled: true, Accepted: true, ForwardAudio: true, VirtualDeviceName: "Server-B", CredentialEpoch: 1}
	linkB := linkA
	linkB.LocalCenterID, linkB.RemoteCenterID = linkB.RemoteCenterID, linkB.LocalCenterID
	linkB.LocalGroupID, linkB.RemoteGroupID = linkB.RemoteGroupID, linkB.LocalGroupID
	addrA := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 26160}
	addrB := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 26161}
	a := NewCenterHTTPPeerManager(nil)
	b := NewCenterHTTPPeerManager(nil)
	var hello []byte
	b.SetWriter(func(_ *net.UDPAddr, data []byte) error {
		hello = append([]byte(nil), data...)
		return nil
	})
	if err := a.RegisterSession(CenterHTTPPeerSession{Link: linkA, SessionID: "abcdefabcdefabcdefabcdefabcdefab", SessionKey: key}); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterSession(CenterHTTPPeerSession{Link: linkB, SessionID: "abcdefabcdefabcdefabcdefabcdefab", SessionKey: key, RemoteUDP: addrA.String()}); err != nil {
		t.Fatal(err)
	}
	if err := b.Bind(linkB.LinkID); err != nil {
		t.Fatal(err)
	}
	if len(hello) == 0 {
		t.Fatal("peer did not emit hello")
	}
	forged := append([]byte(nil), hello...)
	forged[len(forged)-1] ^= 0x80
	if !a.Handle(forged, addrB) {
		t.Fatal("CPUD packet should be consumed by the peer handler")
	}
	if a.Status(linkA.LinkID).Online {
		t.Fatal("forged hello rebound the peer endpoint")
	}

	a.SetWriter(func(addr *net.UDPAddr, data []byte) error {
		b.Handle(data, addrA)
		return nil
	})
	if !a.Handle(hello, addrB) || !a.Status(linkA.LinkID).Online {
		t.Fatal("authenticated hello did not establish the peer")
	}
}
