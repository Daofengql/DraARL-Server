package interconnect

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net"
	"testing"
	"time"
)

func TestNodeDatagramBridgeUsesOwnerWriterWithoutListening(t *testing.T) {
	centerSession := &NodeSession{NodeID: "edge-a", SessionID: 12, KeyEpoch: 1, Key: []byte("datagram-key-123")}
	edgeSession := &NodeSession{NodeID: "edge-a", SessionID: 12, KeyEpoch: 1, Key: []byte("datagram-key-123")}
	received := make(chan Envelope, 1)
	bridge, err := NewNodeDatagramBridge(
		func(nodeID string, sessionID uint64) *NodeSession {
			if nodeID == centerSession.NodeID && sessionID == centerSession.SessionID {
				return centerSession
			}
			return nil
		},
		func(_ *NodeSession, env Envelope, _ *net.UDPAddr) { received <- env },
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()

	peer, err := NewNodeDatagramPeer("127.0.0.1:60050", edgeSession, nil)
	if err != nil {
		t.Fatal(err)
	}
	peer.SetWriter(func(_ *net.UDPAddr, wire []byte) error {
		if !bridge.Handle(wire, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32000}) {
			t.Fatal("valid Type 0 packet was not handled")
		}
		return nil
	})
	challenge, err := centerSession.IssueDataBindChallenge(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.ProveDataBind(challenge, 1); err != nil {
		t.Fatal(err)
	}
	env := NewEnvelope(SubtypeRelayUpstream, edgeSession.NodeID, edgeSession.SessionID, 2, []byte("voice"))
	if err := peer.Send(env); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if string(got.Payload) != "voice" {
			t.Fatalf("payload=%q", got.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("bridge did not deliver datagram")
	}
	edgeMetrics := peer.Metrics.Snapshot()
	centerMetrics := centerSession.DataMetrics.Snapshot()
	if edgeMetrics.OutPackets != 2 || centerMetrics.InPackets != 2 || edgeMetrics.OutBytes != centerMetrics.InBytes ||
		edgeMetrics.Drops != 0 || edgeMetrics.Errors != 0 || centerMetrics.Drops != 0 || centerMetrics.Errors != 0 {
		t.Fatalf("edge/center datagram metrics disagree: edge=%#v center=%#v", edgeMetrics, centerMetrics)
	}
}

func TestNodeDatagramBridgeMetricsAttributeAuthenticatedDropsExactly(t *testing.T) {
	key := []byte("datagram-key-123")
	session := &NodeSession{NodeID: "edge-metrics", SessionID: 41, KeyEpoch: 2, Key: key}
	bound := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32100}
	other := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32101}
	session.BindDataAddr(bound)
	delivered := make(chan struct{}, 1)
	bridge, err := NewNodeDatagramBridge(
		func(nodeID string, sessionID uint64) *NodeSession {
			if nodeID == session.NodeID && sessionID == session.SessionID {
				return session
			}
			return nil
		},
		func(_ *NodeSession, _ Envelope, _ *net.UDPAddr) { delivered <- struct{}{} },
		50*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	marshal := func(messageID uint64, sentAt time.Time, payload string) []byte {
		t.Helper()
		env := NewEnvelope(SubtypeRelayUpstream, session.NodeID, session.SessionID, messageID, []byte(payload))
		env.KeyEpoch = session.KeyEpoch
		env.SentAtMillis = sentAt.UnixMilli()
		wire, marshalErr := env.Marshal(key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return wire
	}
	now := time.Now()
	accepted := marshal(1, now, "accepted")
	if !bridge.Handle(accepted, bound) {
		t.Fatal("valid datagram was not consumed")
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("valid datagram was not delivered")
	}
	if bridge.Handle(accepted, bound) {
		t.Fatal("replayed datagram received a rate-limit exemption")
	}
	expired := marshal(2, now.Add(-time.Second), "expired")
	if bridge.Handle(expired, bound) {
		t.Fatal("expired datagram received a rate-limit exemption")
	}
	unbound := marshal(3, time.Now(), "unbound")
	if !bridge.Handle(unbound, other) {
		t.Fatal("authenticated unbound datagram was not consumed")
	}

	metrics := session.DataMetrics.Snapshot()
	wantBytes := uint64(len(accepted)*2 + len(expired) + len(unbound))
	if metrics.InPackets != 4 || metrics.InBytes != wantBytes || metrics.OutPackets != 0 || metrics.OutBytes != 0 || metrics.Drops != 3 || metrics.Errors != 0 {
		t.Fatalf("datagram metrics=%#v want in=4/%d drops=3", metrics, wantBytes)
	}
	protection := session.ProtectionSnapshot()
	if protection.ReplayDrops != 1 || protection.ExpiredDrops != 1 || protection.UnboundAddressDrops != 1 ||
		protection.DataHardLimitDrops != 0 || protection.DataQueueDrops != 0 || protection.DataStaleDrops != 0 || protection.InvalidAuthTags != 0 || protection.IdentityRejects != 0 {
		t.Fatalf("drop cause attribution=%#v", protection)
	}
	bridgeMetrics := bridge.ProtectionSnapshot()
	if bridgeMetrics.InvalidType0 != 2 || bridgeMetrics.UnauthenticatedType0 != 0 || bridgeMetrics.GlobalQueueDrops != 0 {
		t.Fatalf("bridge protection metrics=%#v", bridgeMetrics)
	}
}

func TestNodeDatagramPeerMetricsAttributeAuthenticatedDropsExactly(t *testing.T) {
	key := []byte("datagram-key-123")
	session := &NodeSession{NodeID: "edge-peer", SessionID: 61, KeyEpoch: 4, Key: key}
	delivered := make(chan struct{}, 1)
	peer, err := NewNodeDatagramPeer("127.0.0.1:60050", session, func(Envelope) { delivered <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	marshal := func(messageID uint64, sentAt time.Time, payload string) []byte {
		t.Helper()
		env := NewEnvelope(SubtypeRelayDownstream, "center", session.SessionID, messageID, []byte(payload))
		env.KeyEpoch = session.KeyEpoch
		env.SentAtMillis = sentAt.UnixMilli()
		wire, marshalErr := env.Marshal(key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return wire
	}
	now := time.Now()
	accepted := marshal(1, now, "accepted")
	if !peer.Handle(accepted, nil) {
		t.Fatal("valid center datagram was not handled")
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("valid center datagram was not delivered")
	}
	if peer.Handle(accepted, nil) {
		t.Fatal("replayed center datagram was accepted")
	}
	expired := marshal(2, now.Add(-3*time.Second), "expired")
	if peer.Handle(expired, nil) {
		t.Fatal("expired center datagram was accepted")
	}
	metrics := peer.Metrics.Snapshot()
	wantBytes := uint64(len(accepted)*2 + len(expired))
	if metrics != (MetricsSnapshot{InPackets: 3, InBytes: wantBytes, Drops: 2}) {
		t.Fatalf("edge peer metrics=%#v want in=3/%d drops=2", metrics, wantBytes)
	}
	protection := session.ProtectionSnapshot()
	if protection.ReplayDrops != 1 || protection.ExpiredDrops != 1 || protection.IdentityRejects != 0 || protection.InvalidAuthTags != 0 {
		t.Fatalf("edge peer drop attribution=%#v", protection)
	}
}

func TestNodeDatagramBridgeRejectsForgedSessionEpochAndHopCount(t *testing.T) {
	key := []byte("datagram-key-123")
	session := &NodeSession{NodeID: "edge-forge", SessionID: 71, KeyEpoch: 5, Key: key}
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32400}
	session.BindDataAddr(addr)
	delivered := make(chan struct{}, 1)
	bridge, err := NewNodeDatagramBridge(
		func(nodeID string, sessionID uint64) *NodeSession {
			if nodeID == session.NodeID && sessionID == session.SessionID {
				return session
			}
			return nil
		},
		func(*NodeSession, Envelope, *net.UDPAddr) { delivered <- struct{}{} },
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	marshal := func(sessionID uint64, keyEpoch uint32, messageID uint64) []byte {
		t.Helper()
		env := NewEnvelope(SubtypeRelayUpstream, session.NodeID, sessionID, messageID, []byte("forged"))
		env.KeyEpoch = keyEpoch
		wire, marshalErr := env.Marshal(key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return wire
	}
	if bridge.Handle(marshal(session.SessionID+1, session.KeyEpoch, 1), addr) {
		t.Fatal("forged node session ID was accepted")
	}
	if bridge.Handle(marshal(session.SessionID, session.KeyEpoch-1, 2), addr) {
		t.Fatal("old key epoch was accepted")
	}
	hopWire := marshal(session.SessionID, session.KeyEpoch, 3)
	hopWire[DraARLHeaderSize+44] = 3
	tagStart := len(hopWire) - NodeAuthTagSize
	tag := hmac.New(sha256.New, key)
	_, _ = tag.Write(hopWire[:tagStart])
	copy(hopWire[tagStart:], tag.Sum(nil))
	if bridge.Handle(hopWire, addr) {
		t.Fatal("hop count above the protocol limit was accepted")
	}
	select {
	case <-delivered:
		t.Fatal("forged datagram reached the business handler")
	case <-time.After(20 * time.Millisecond):
	}
	if got := bridge.ProtectionSnapshot(); got.UnauthenticatedType0 != 1 || got.InvalidType0 != 2 || got.GlobalQueueDrops != 0 {
		t.Fatalf("forged datagram bridge counters=%#v", got)
	}
	if got := session.ProtectionSnapshot(); got.IdentityRejects != 1 || got.InvalidAuthTags != 1 || got.ReplayDrops != 0 || got.ExpiredDrops != 0 {
		t.Fatalf("forged datagram attribution=%#v", got)
	}
	if got := session.DataMetrics.Snapshot(); got != (MetricsSnapshot{}) {
		t.Fatalf("rejected pre-identity traffic polluted application metrics: %#v", got)
	}
}

func TestNodeDatagramSendSocketErrorOnlyIncrementsError(t *testing.T) {
	session := &NodeSession{NodeID: "edge-send", SessionID: 52, KeyEpoch: 3, Key: []byte("datagram-key-123")}
	session.BindDataAddr(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32200})
	bridge, err := NewNodeDatagramBridge(
		func(string, uint64) *NodeSession { return session },
		func(*NodeSession, Envelope, *net.UDPAddr) {},
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	bridge.SetWriter(func(*net.UDPAddr, []byte) error { return errors.New("socket write failed") })
	failed := NewEnvelope(SubtypeRelayDownstream, "center", session.SessionID, 1, []byte("failed"))
	if err := bridge.Send(session, failed); err == nil {
		t.Fatal("socket write error was hidden")
	}
	if got := session.DataMetrics.Snapshot(); got != (MetricsSnapshot{Errors: 1}) {
		t.Fatalf("failed send metrics=%#v", got)
	}

	var sentBytes int
	bridge.SetWriter(func(_ *net.UDPAddr, wire []byte) error {
		sentBytes = len(wire)
		return nil
	})
	sent := NewEnvelope(SubtypeRelayDownstream, "center", session.SessionID, 2, []byte("sent"))
	if err := bridge.Send(session, sent); err != nil {
		t.Fatal(err)
	}
	if got := session.DataMetrics.Snapshot(); got != (MetricsSnapshot{OutPackets: 1, OutBytes: uint64(sentBytes), Errors: 1}) {
		t.Fatalf("successful send metrics=%#v sent_bytes=%d", got, sentBytes)
	}
}

func TestNodeDatagramBridgeDoesNotSilentlyRebindAuthenticatedTraffic(t *testing.T) {
	key := []byte("datagram-key-123")
	session := &NodeSession{NodeID: "edge-a", SessionID: 12, KeyEpoch: 1, Key: key}
	delivered := make(chan struct{}, 1)
	bridge, err := NewNodeDatagramBridge(
		func(nodeID string, sessionID uint64) *NodeSession {
			if nodeID == session.NodeID && sessionID == session.SessionID {
				return session
			}
			return nil
		},
		func(_ *NodeSession, _ Envelope, _ *net.UDPAddr) { delivered <- struct{}{} },
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()

	bound := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32000}
	other := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32001}
	session.BindDataAddr(bound)
	env := NewEnvelope(SubtypeRelayUpstream, session.NodeID, session.SessionID, 1, []byte("voice"))
	env.KeyEpoch = session.KeyEpoch
	wire, err := env.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !bridge.Handle(wire, other) {
		t.Fatal("authenticated Type 0 packet was not consumed")
	}
	select {
	case <-delivered:
		t.Fatal("packet from an unbound address was delivered")
	case <-time.After(20 * time.Millisecond):
	}
	if !session.DataAddrMatches(bound) || session.DataAddrMatches(other) {
		t.Fatal("ordinary authenticated traffic changed the UDP binding")
	}
	if got := session.ProtectionSnapshot().UnboundAddressDrops; got != 1 {
		t.Fatalf("unbound address drops=%d", got)
	}
}

func TestNodeDatagramBridgeRebindRequiresFreshControlChallenge(t *testing.T) {
	key := []byte("datagram-key-123")
	session := &NodeSession{NodeID: "edge-a", SessionID: 12, KeyEpoch: 1, Key: key}
	bridge, err := NewNodeDatagramBridge(
		func(nodeID string, sessionID uint64) *NodeSession {
			if nodeID == session.NodeID && sessionID == session.SessionID {
				return session
			}
			return nil
		},
		func(_ *NodeSession, _ Envelope, _ *net.UDPAddr) {},
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	oldAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32000}
	newAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32001}
	session.BindDataAddr(oldAddr)
	challenge, err := session.IssueDataBindChallenge(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := EncodeJSON(NodeDataBind{Action: NodeDataBindProof, Challenge: challenge})
	proof := NewEnvelope(SubtypeNodeDataBind, session.NodeID, session.SessionID, 1, payload)
	proof.KeyEpoch = session.KeyEpoch
	wire, err := proof.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !bridge.Handle(wire, newAddr) || !session.DataAddrMatches(newAddr) {
		t.Fatal("fresh challenge did not authorize UDP rebinding")
	}

	replayed := NewEnvelope(SubtypeNodeDataBind, session.NodeID, session.SessionID, 2, payload)
	replayed.KeyEpoch = session.KeyEpoch
	replayWire, err := replayed.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !bridge.Handle(replayWire, oldAddr) {
		t.Fatal("replayed proof was not consumed")
	}
	if !session.DataAddrMatches(newAddr) || session.DataAddrMatches(oldAddr) {
		t.Fatal("a consumed challenge was reused to rebind the session")
	}
	if got := session.ProtectionSnapshot().DataBindRejects; got != 1 {
		t.Fatalf("data bind rejects=%d", got)
	}
}

func TestNodeSessionRejectsReplayMessageID(t *testing.T) {
	session := &NodeSession{NodeID: "edge-a", SessionID: 1, Key: []byte("0123456789012345")}
	now := time.Now()
	if !session.AcceptMessage(42, now) {
		t.Fatal("first message was rejected")
	}
	if session.AcceptMessage(42, now.Add(time.Millisecond)) {
		t.Fatal("duplicate message was accepted")
	}
	if session.AcceptMessage(42, now.Add(31*time.Second)) {
		t.Fatal("message ID replay became valid again inside one node session")
	}
	if !session.AcceptMessage(44, now.Add(31*time.Second)) || !session.AcceptMessage(43, now.Add(31*time.Second)) {
		t.Fatal("new or out-of-order message inside the replay window was rejected")
	}
	if session.AcceptMessage(43, now.Add(31*time.Second)) {
		t.Fatal("out-of-order message replay was accepted")
	}
}

func TestDownstreamWaitsForRequiredProjectionVersion(t *testing.T) {
	gateway, err := NewEdgeGateway("127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	link := newEdgeControlLink(nil, nil)
	link.markReady()
	gateway.control.Store(link)
	receiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	now := time.Now()
	gateway.sessions[2] = &edgeDeviceSession{Grant: DeviceGrant{SessionID: 2, SessionEpoch: 1, Username: "bob", SSID: 1, DomainID: 9}, Addr: receiver.LocalAddr().(*net.UDPAddr), LastSeen: now}
	gateway.byIdentity["bob-1"] = 2
	projection := NewProjection(7)
	projection.Version = 1
	projection.Devices[2] = gateway.sessions[2].Grant.Route()
	if err := gateway.projection.Replace(projection); err != nil {
		t.Fatal(err)
	}
	env := NewEnvelope(SubtypeRelayDownstream, "center", 1, 1, nil)
	env.ClusterEpoch, env.ProjectionVersion = 7, 2
	frame := RelayFrame{SessionID: 99, SessionEpoch: 3, DomainID: 9, RequiredProjectionVersion: 2, InnerPacket: []byte("voice")}
	gateway.deliverDownstream(env, frame)
	_ = receiver.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, _, err := receiver.ReadFromUDP(make([]byte, 32)); err == nil {
		t.Fatal("downstream frame bypassed the projection barrier")
	}
	projection.Version = 2
	if err := gateway.projection.Replace(projection); err != nil {
		t.Fatal(err)
	}
	gateway.drainDownstream(time.Now())
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 32)
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil || string(buf[:n]) != "voice" {
		t.Fatalf("eligible downstream was not released: data=%q err=%v", buf[:n], err)
	}
}

func TestDownstreamFreshnessUsesLocalReceiptTimeAcrossClockSkew(t *testing.T) {
	gateway, err := NewEdgeGateway("127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	link := newEdgeControlLink(nil, nil)
	link.markReady()
	gateway.control.Store(link)
	receiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()

	now := time.Now()
	gateway.sessions[2] = &edgeDeviceSession{Grant: DeviceGrant{SessionID: 2, SessionEpoch: 1, Username: "bob", SSID: 1, DomainID: 9}, Addr: receiver.LocalAddr().(*net.UDPAddr), LastSeen: now}
	gateway.byIdentity["bob-1"] = 2
	projection := NewProjection(7)
	projection.Version = 2
	projection.Devices[2] = gateway.sessions[2].Grant.Route()
	if err := gateway.projection.Replace(projection); err != nil {
		t.Fatal(err)
	}

	env := NewEnvelope(SubtypeRelayDownstream, "center", 1, 1, nil)
	env.ClusterEpoch, env.ProjectionVersion = 7, 2
	env.SentAtMillis = now.Add(700 * time.Millisecond).UnixMilli()
	env.receivedAt = now
	frame := RelayFrame{SessionID: 99, SessionEpoch: 3, DomainID: 9, RequiredProjectionVersion: 2, InnerPacket: []byte("voice")}
	gateway.deliverDownstream(env, frame)

	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 32)
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil || string(buf[:n]) != "voice" {
		t.Fatalf("fresh local frame was rejected because of remote clock skew: data=%q err=%v", buf[:n], err)
	}
}

func TestDownstreamProjectionWaitStillExpiresByLocalAge(t *testing.T) {
	gateway, err := NewEdgeGateway("127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	link := newEdgeControlLink(nil, nil)
	link.markReady()
	gateway.control.Store(link)
	receiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()

	now := time.Now()
	gateway.sessions[2] = &edgeDeviceSession{Grant: DeviceGrant{SessionID: 2, SessionEpoch: 1, Username: "bob", SSID: 1, DomainID: 9}, Addr: receiver.LocalAddr().(*net.UDPAddr), LastSeen: now}
	gateway.byIdentity["bob-1"] = 2
	projection := NewProjection(7)
	projection.Version = 1
	projection.Devices[2] = gateway.sessions[2].Grant.Route()
	if err := gateway.projection.Replace(projection); err != nil {
		t.Fatal(err)
	}

	env := NewEnvelope(SubtypeRelayDownstream, "center", 1, 1, nil)
	env.ClusterEpoch, env.ProjectionVersion = 7, 2
	env.receivedAt = now.Add(-gateway.downstreamMaxAge - time.Millisecond)
	frame := RelayFrame{SessionID: 99, SessionEpoch: 3, DomainID: 9, RequiredProjectionVersion: 2, InnerPacket: []byte("voice")}
	gateway.deliverDownstream(env, frame)
	projection.Version = 2
	if err := gateway.projection.Replace(projection); err != nil {
		t.Fatal(err)
	}
	gateway.drainDownstream(now)

	_ = receiver.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, _, err := receiver.ReadFromUDP(make([]byte, 32)); err == nil {
		t.Fatal("locally stale downstream frame was delivered")
	}
}
