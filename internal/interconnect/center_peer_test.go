package interconnect

import (
	"bytes"
	"context"
	"draarl/internal/protocol"
	"fmt"
	"testing"
	"time"
)

func testCenterPeerLink() CenterPeerLink {
	return CenterPeerLink{LinkID: "bridge-001", LocalCenterID: "center-a", RemoteCenterID: "center-b", LocalGroupID: 12, RemoteGroupID: 38, InitiatorCenterID: "center-a", Direction: CenterLinkDirectionForward, Enabled: true, Accepted: true, ForwardAudio: true, VirtualDeviceName: "Server-B", CredentialEpoch: 1}
}
func mirrorPeer(l CenterPeerLink) CenterPeerLink {
	l.LocalCenterID, l.RemoteCenterID = l.RemoteCenterID, l.LocalCenterID
	l.LocalGroupID, l.RemoteGroupID = l.RemoteGroupID, l.LocalGroupID
	l.VirtualDeviceName = "Server-A"
	return l
}
func waitPeer(t *testing.T, check func() bool) {
	t.Helper()
	end := time.Now().Add(8 * time.Second)
	for time.Now().Before(end) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("peer state timeout")
}
func TestCenterPeerPolicyAndFreshness(t *testing.T) {
	l := testCenterPeerLink()
	if !l.AllowsOrigin("center-a") || l.AllowsOrigin("center-b") || l.AllowsOrigin("unknown") {
		t.Fatal("forward direction")
	}
	l.Direction = CenterLinkDirectionReverse
	if l.AllowsOrigin("center-a") || !l.AllowsOrigin("center-b") {
		t.Fatal("reverse direction")
	}
	l.Direction = CenterLinkDirectionBidirectional
	now := time.Now()
	f, err := NewCenterPeerFrame(l, CenterLinkMediaAudio, []byte{1}, 1, now)
	if err != nil || f.Validate(now, 0) != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*CenterPeerFrame){func(f *CenterPeerFrame) { f.CreatedAt = now.Add(-2 * time.Second).UnixMilli() }, func(f *CenterPeerFrame) { f.HopCount = 1 }, func(f *CenterPeerFrame) { f.MediaType = "text" }, func(f *CenterPeerFrame) { f.CredentialEpoch = 0 }, func(f *CenterPeerFrame) { f.Payload = make([]byte, 711) }} {
		bad := f
		mutate(&bad)
		if bad.Validate(now, 0) == nil {
			t.Fatal("invalid frame accepted")
		}
	}
	l.ForwardText = true
	if l.Validate() == nil {
		t.Fatal("unsupported media enabled")
	}
}

// Real TCP/TLS, authentication, reciprocal mapping hello and both data paths.
func TestCenterPeerTLSAudioDirectionsAndRevocation(t *testing.T) {
	for _, direction := range []string{CenterLinkDirectionForward, CenterLinkDirectionReverse, CenterLinkDirectionBidirectional} {
		t.Run(direction, func(t *testing.T) {
			l := testCenterPeerLink()
			l.Direction = direction
			bLink := mirrorPeer(l)
			tlsCfg, _, err := NewSelfSignedTLSConfig("localhost")
			if err != nil {
				t.Fatal(err)
			}
			receivedA := make(chan []byte, 16)
			receivedB := make(chan []byte, 16)
			token := "0123456789abcdef0123456789abcdef"
			b, err := StartCenterRuntime(CenterRuntimeConfig{ControlListen: "127.0.0.1:0", TLSConfig: tlsCfg, Authenticate: func(id, secret string) (NodeAuthentication, error) {
				return NodeAuthentication{Accepted: id == CenterPeerTransportID(l.LinkID) && secret == token, PeerLinkID: l.LinkID, CredentialEpoch: 1}, nil
			}, OnPeerAudio: func(_ CenterPeerLink, p []byte) bool { receivedB <- append([]byte(nil), p...); return true }})
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			a := NewCenterPeerManager(func(_ CenterPeerLink, p []byte) bool { receivedA <- append([]byte(nil), p...); return true })
			defer a.Close()
			if err := b.Peers.SetConfig(CenterPeerConfig{Link: bLink}); err != nil {
				t.Fatal(err)
			}
			ac := CenterPeerConfig{Link: l, RemoteAddress: b.Control.Addr().String(), TLSPinSHA256: CertificateFingerprint(tlsCfg), Credential: token}
			if err := a.SetConfig(ac); err != nil {
				t.Fatal(err)
			}
			waitPeer(t, func() bool { return a.Status(l.LinkID).Online && b.Peers.Status(l.LinkID).Online })
			// The peer cannot create any ordinary device or route projection.
			b.Gateway.mu.RLock()
			devices := len(b.Gateway.deviceSessions)
			b.Gateway.mu.RUnlock()
			if devices != 0 {
				t.Fatal("peer projected ordinary devices")
			}
			payload := []byte{1, 2, 3}
			wire := protocol.EncodeDraARLv1("private-user", "", 1, protocol.DraARLTypeOpus16K, 2, 123, "PRIVATE", payload)
			a.RelayGroup(l.LocalGroupID, wire)
			b.Peers.RelayGroup(bLink.LocalGroupID, wire)
			for _, tc := range []struct {
				ch   chan []byte
				want bool
			}{{receivedB, l.AllowsOrigin(l.LocalCenterID)}, {receivedA, bLink.AllowsOrigin(bLink.LocalCenterID)}} {
				if tc.want {
					select {
					case got := <-tc.ch:
						if !bytes.Equal(got, payload) {
							t.Fatal("identity header leaked")
						}
					case <-time.After(time.Second):
						t.Fatal("audio missing")
					}
				} else {
					select {
					case <-tc.ch:
						t.Fatal("wrong direction received")
					case <-time.After(80 * time.Millisecond):
					}
				}
			}
			// Reconnect, with a new session: pending old audio is never replayed.
			a.mu.Lock()
			oldSession := a.entries[l.LinkID].connection.session.SessionID
			a.entries[l.LinkID].connection.close()
			a.mu.Unlock()
			waitPeer(t, func() bool {
				a.mu.RLock()
				defer a.mu.RUnlock()
				c := a.entries[l.LinkID].connection
				return c != nil && c.ready.Load() && c.session.SessionID != oldSession
			})
			bLink.Accepted = false
			if err := b.Peers.SetConfig(CenterPeerConfig{Link: bLink}); err != nil {
				t.Fatal(err)
			}
			waitPeer(t, func() bool { return !a.Status(l.LinkID).Online && !b.Peers.Status(l.LinkID).Online })
		})
	}
}

func TestCenterPeerRejectsMismatchedHelloAndCredentials(t *testing.T) {
	l := testCenterPeerLink()
	bLink := mirrorPeer(l)
	bLink.RemoteGroupID++
	tlsCfg, _, _ := NewSelfSignedTLSConfig("localhost")
	token := "0123456789abcdef0123456789abcdef"
	b, err := StartCenterRuntime(CenterRuntimeConfig{ControlListen: "127.0.0.1:0", TLSConfig: tlsCfg, Authenticate: func(id, secret string) (NodeAuthentication, error) {
		return NodeAuthentication{Accepted: secret == token, PeerLinkID: l.LinkID, CredentialEpoch: 1}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Peers.SetConfig(CenterPeerConfig{Link: bLink})
	ac := CenterPeerConfig{Link: l, RemoteAddress: b.Control.Addr().String(), TLSPinSHA256: CertificateFingerprint(tlsCfg), Credential: token}
	a := NewCenterPeerManager()
	defer a.Close()
	a.SetConfig(ac)
	waitPeer(t, func() bool { return b.Peers.Status(l.LinkID).LastError != "" })
	if a.Status(l.LinkID).Online || b.Peers.Status(l.LinkID).Online {
		t.Fatal("mismatched mapping ready")
	}
	tc, _ := PeerTLSConfig(ac)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client, err := DialNode(ctx, NodeClientConfig{CenterAddr: ac.RemoteAddress, TLSConfig: tc, NodeID: CenterPeerTransportID(l.LinkID), Token: "wrong", Capabilities: NodeCapabilities{Features: NodeFeatureCenterPeer, RequiredFeatures: NodeFeatureCenterPeer}})
	if err == nil {
		client.Close()
		t.Fatal("wrong credential accepted")
	}
}

func TestCenterPeerRejectsReplayStaleAndOldSession(t *testing.T) {
	l := mirrorPeer(testCenterPeerLink())
	count := 0
	m := NewCenterPeerManager(func(_ CenterPeerLink, _ []byte) bool { count++; return true })
	defer m.Close()
	m.SetConfig(CenterPeerConfig{Link: l})
	c := &peerConnection{session: &NodeSession{SessionID: 99}, done: make(chan struct{}), queue: make(chan peerAudioJob, 8)}
	c.ready.Store(true)
	m.mu.Lock()
	e := m.entries[l.LinkID]
	e.connection = c
	m.mu.Unlock()
	// No network connection in this targeted validation test.
	defer func() { m.mu.Lock(); e.connection = nil; m.mu.Unlock() }()
	f, _ := NewCenterPeerFrame(mirrorPeer(l), CenterLinkMediaAudio, []byte{1}, 2, time.Now())
	f.MessageID = fmt.Sprintf("%016x:%016x", 99, 2)
	send := func(f CenterPeerFrame) {
		p, _ := EncodeJSON(f)
		env := NewEnvelope(SubtypeCenterPeerRelay, "", 99, f.Sequence, p)
		m.handle(e, c, env)
	}
	send(f)
	send(f)
	f.Sequence = 3
	f.MessageID = fmt.Sprintf("%016x:%016x", 99, 3)
	f.CreatedAt = time.Now().Add(-2 * time.Second).UnixMilli()
	send(f)
	f.CreatedAt = time.Now().UnixMilli()
	f.MessageID = fmt.Sprintf("%016x:%016x", 98, 3)
	send(f)
	f.MessageID = fmt.Sprintf("%016x:%016x", 99, 3)
	f.TargetGroupID++
	send(f)
	if count != 1 || m.Status(l.LinkID).DroppedPackets != 4 {
		t.Fatalf("accepted=%d status=%+v", count, m.Status(l.LinkID))
	}
}
