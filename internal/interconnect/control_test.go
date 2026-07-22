package interconnect

import (
	"context"
	"crypto/tls"
	"testing"
	"time"
)

func TestTLSControlPlaneAuthenticatesAndCarriesMessages(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan ControlMessage, 1)
	server, err := NewNodeServer(NodeServerConfig{
		ListenAddr: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(nodeID, token string) bool { return nodeID == "edge-a" && token == "secret" },
		OnMessage:     func(_ *NodeSession, msg ControlMessage) { received <- msg },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}, NodeID: "edge-a", Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.Session == nil || client.Session.SessionID == 0 || len(client.Session.Key) != 32 {
		t.Fatalf("invalid authenticated session: %#v", client.Session)
	}
	if err := client.Send(ControlMessage{Kind: controlHeartbeat, MessageID: 9}); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-received:
		if msg.Kind != controlHeartbeat || msg.MessageID != 9 {
			t.Fatalf("unexpected message: %#v", msg)
		}
	case <-ctx.Done():
		t.Fatal("control message not delivered")
	}
}

func TestTLSControlPlaneRejectsInvalidToken(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewNodeServer(NodeServerConfig{ListenAddr: "127.0.0.1:0", TLSConfig: serverTLS, ValidateToken: func(_, _ string) bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}, NodeID: "edge-a", Token: "wrong"})
	if err == nil {
		client.Close()
		t.Fatal("invalid token was accepted")
	}
}

func TestTLSControlPlaneRejectsReservedCenterNodeID(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	validatorCalled := false
	server, err := NewNodeServer(NodeServerConfig{
		ListenAddr: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(_, _ string) bool { validatorCalled = true; return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}, NodeID: CenterLocalNodeID, Token: "secret"})
	if err == nil {
		client.Close()
		t.Fatal("reserved centre node ID was accepted")
	}
	if validatorCalled {
		t.Fatal("reserved centre node ID reached the external token validator")
	}
}

func TestTLSControlPlaneReplacesSameNodeSessionWithoutDeletingNewSession(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	connected := make(chan *NodeSession, 2)
	disconnected := make(chan *NodeSession, 2)
	server, err := NewNodeServer(NodeServerConfig{
		ListenAddr: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(nodeID, token string) bool { return nodeID == "edge-a" && token == "secret" },
		OnConnect:     func(session *NodeSession) { connected <- session },
		OnDisconnect:  func(session *NodeSession, _ error) { disconnected <- session },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	clientTLS := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: clientTLS, NodeID: "edge-a", Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	<-connected
	second, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: clientTLS.Clone(), NodeID: "edge-a", Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	secondSession := <-connected
	select {
	case oldSession := <-disconnected:
		if oldSession.SessionID == secondSession.SessionID {
			t.Fatal("replacement session was disconnected")
		}
	case <-ctx.Done():
		t.Fatal("old session did not disconnect after replacement")
	}
	current, ok := server.Session("edge-a")
	if !ok || current.SessionID != second.Session.SessionID {
		t.Fatalf("current session was lost during replacement: %#v", current)
	}
}

func TestTLSControlMetricsCountSerializedApplicationFrames(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan struct{}, 1)
	server, err := NewNodeServer(NodeServerConfig{
		ListenAddr: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(_, _ string) bool { return true },
		OnMessage:     func(_ *NodeSession, _ ControlMessage) { received <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}, NodeID: "edge-metrics", Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	msg := ControlMessage{Kind: controlHeartbeat, MessageID: 123, Payload: []byte(`{"sample":true}`)}
	wire, err := marshalControlMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	clientBefore := client.Session.ControlMetrics.Snapshot()
	if err := client.Send(msg); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("control message was not received")
	}
	if got := client.Session.ControlMetrics.Snapshot(); got.OutPackets-clientBefore.OutPackets != 1 || got.OutBytes-clientBefore.OutBytes != uint64(len(wire)) {
		t.Fatalf("client control metric delta = %#v - %#v, want one %d-byte frame", got, clientBefore, len(wire))
	}
	session, ok := server.Session("edge-metrics")
	if !ok {
		t.Fatal("server session missing")
	}
	serverMetrics := session.ControlMetrics.Snapshot()
	if serverMetrics.InPackets != 2 || serverMetrics.InBytes <= uint64(len(wire)) {
		t.Fatalf("server control metrics = %#v, want handshake plus one %d-byte frame", serverMetrics, len(wire))
	}
}

func TestTLSControlPlaneReservesNodeCapacityBeforeAuthSuccess(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewNodeServer(NodeServerConfig{
		ListenAddr: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken:  func(_, token string) bool { return token == "secret" },
		ResourceLimits: ResourceLimits{MaxNodes: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	clientTLS := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: clientTLS, NodeID: "edge-a", Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: clientTLS.Clone(), NodeID: "edge-b", Token: "secret"}); err == nil {
		second.Close()
		t.Fatal("second NodeID exceeded MaxNodes")
	}
	if snapshot := server.ProtectionSnapshot(); snapshot.ActiveNodes != 1 || snapshot.MaxNodesRejected != 1 {
		t.Fatalf("capacity snapshot=%#v", snapshot)
	}
	replacement, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: clientTLS.Clone(), NodeID: "edge-a", Token: "secret"})
	if err != nil {
		t.Fatalf("same NodeID replacement consumed extra capacity: %v", err)
	}
	defer replacement.Close()
	if snapshot := server.ProtectionSnapshot(); snapshot.ActiveNodes != 1 {
		t.Fatalf("replacement active nodes=%d", snapshot.ActiveNodes)
	}
}

func TestTLSControlPlaneLimitsAuthenticationAttemptsPerIP(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewNodeServer(NodeServerConfig{
		ListenAddr: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken:  func(_, token string) bool { return token == "secret" },
		ResourceLimits: ResourceLimits{AuthAttemptsPerMinutePerIP: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	clientTLS := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if client, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: clientTLS, NodeID: "edge-a", Token: "wrong"}); err == nil {
		client.Close()
		t.Fatal("invalid credential was accepted")
	}
	if client, err := DialNode(ctx, NodeClientConfig{CenterAddr: server.Addr().String(), TLSConfig: clientTLS.Clone(), NodeID: "edge-a", Token: "secret"}); err == nil {
		client.Close()
		t.Fatal("authentication rate limit was bypassed")
	}
	snapshot := server.ProtectionSnapshot()
	if snapshot.AuthFailed != 1 || snapshot.AuthRateRejected != 1 {
		t.Fatalf("authentication snapshot=%#v", snapshot)
	}
}
