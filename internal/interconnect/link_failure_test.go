package interconnect

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"
)

func TestUDPWriterFailureDoesNotBreakTLSControlSession(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	messages := make(chan ControlMessage, 1)
	server, err := NewNodeServer(NodeServerConfig{
		ListenAddr: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(nodeID, token string) bool { return nodeID == "edge-split" && token == "test-token" },
		OnMessage:     func(_ *NodeSession, message ControlMessage) { messages <- message },
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
	client, err := DialNode(ctx, NodeClientConfig{
		CenterAddr: server.Addr().String(),
		TLSConfig:  &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13},
		NodeID:     "edge-split", Token: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	peer, err := NewNodeDatagramPeer("127.0.0.1:60050", client.Session, nil)
	if err != nil {
		t.Fatal(err)
	}
	peer.SetWriter(func(*net.UDPAddr, []byte) error { return errors.New("injected UDP outage") })
	if err := peer.Send(NewEnvelope(SubtypeRelayUpstream, "edge-split", client.Session.SessionID, 1, []byte("voice"))); err == nil {
		t.Fatal("injected UDP writer failure was ignored")
	}
	if metrics := peer.Metrics.Snapshot(); metrics.Errors != 1 || metrics.OutPackets != 0 {
		t.Fatalf("UDP failure metrics=%#v", metrics)
	}
	if err := client.Send(ControlMessage{Kind: "control_probe"}); err != nil {
		t.Fatalf("UDP failure broke the TLS control session: %v", err)
	}
	select {
	case message := <-messages:
		if message.Kind != "control_probe" {
			t.Fatalf("unexpected TLS control message: %#v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("TLS control session stopped after the UDP failure")
	}
	peer.SetWriter(func(*net.UDPAddr, []byte) error { return nil })
	if err := peer.Send(NewEnvelope(SubtypeRelayUpstream, "edge-split", client.Session.SessionID, 2, []byte("voice"))); err != nil {
		t.Fatalf("UDP data path did not recover independently: %v", err)
	}
	if metrics := peer.Metrics.Snapshot(); metrics.Errors != 1 || metrics.OutPackets != 1 {
		t.Fatalf("UDP recovery metrics=%#v", metrics)
	}
	if current, ok := server.Session("edge-split"); !ok || current.SessionID != client.Session.SessionID {
		t.Fatal("UDP outage replaced or removed the authenticated TLS session")
	}
}

func TestTLSReplacementAndDisconnectImmediatelyRevokeOldUDPIdentity(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewNodeServer(NodeServerConfig{
		ListenAddr: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(nodeID, token string) bool { return nodeID == "edge-rekey" && token == "test-token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	delivered := make(chan uint64, 4)
	bridge, err := NewNodeDatagramBridge(server.SessionByID, func(_ *NodeSession, env Envelope, _ *net.UDPAddr) {
		delivered <- env.MessageID
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	clientTLS := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	dial := func() *NodeClient {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		client, dialErr := DialNode(ctx, NodeClientConfig{
			CenterAddr: server.Addr().String(), TLSConfig: clientTLS.Clone(), NodeID: "edge-rekey", Token: "test-token",
		})
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		return client
	}
	marshal := func(session *NodeSession, messageID uint64, keyEpoch uint32) []byte {
		t.Helper()
		env := NewEnvelope(SubtypeRelayUpstream, session.NodeID, session.SessionID, messageID, []byte("voice"))
		env.KeyEpoch = keyEpoch
		wire, marshalErr := env.Marshal(session.Key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return wire
	}
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32000}

	first := dial()
	defer first.Close()
	firstServerSession, ok := server.Session("edge-rekey")
	if !ok {
		t.Fatal("first TLS session was not installed")
	}
	firstServerSession.BindDataAddr(addr)
	if !bridge.Handle(marshal(first.Session, 1, first.Session.KeyEpoch), addr) {
		t.Fatal("active first UDP identity was rejected")
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("active first UDP identity was not delivered")
	}
	oldWire := marshal(first.Session, 2, first.Session.KeyEpoch)

	second := dial()
	defer second.Close()
	secondServerSession, ok := server.Session("edge-rekey")
	if !ok || secondServerSession.SessionID != second.Session.SessionID || second.Session.SessionID == first.Session.SessionID {
		t.Fatal("same-node TLS reconnect did not atomically replace the session")
	}
	secondServerSession.BindDataAddr(addr)
	if bridge.Handle(oldWire, addr) {
		t.Fatal("old UDP session retained Type 0 authentication after TLS replacement")
	}
	if snapshot := bridge.ProtectionSnapshot(); snapshot.UnauthenticatedType0 != 1 {
		t.Fatalf("old session rejection metrics=%#v", snapshot)
	}

	staleEpoch := marshal(second.Session, 3, second.Session.KeyEpoch+1)
	if bridge.Handle(staleEpoch, addr) {
		t.Fatal("wrong KeyEpoch retained Type 0 authentication")
	}
	if snapshot := bridge.ProtectionSnapshot(); snapshot.InvalidType0 != 1 {
		t.Fatalf("stale key epoch rejection metrics=%#v", snapshot)
	}
	if !bridge.Handle(marshal(second.Session, 4, second.Session.KeyEpoch), addr) {
		t.Fatal("replacement UDP identity was rejected")
	}
	select {
	case messageID := <-delivered:
		if messageID != 4 {
			t.Fatalf("unexpected replacement UDP message %d", messageID)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement UDP identity was not delivered")
	}

	afterDisconnect := marshal(second.Session, 5, second.Session.KeyEpoch)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	waitForCondition(t, 3*time.Second, func() bool {
		_, connected := server.Session("edge-rekey")
		return !connected
	}, "server retained the TLS session after disconnect")
	if bridge.Handle(afterDisconnect, addr) {
		t.Fatal("UDP identity remained authenticated after its TLS session disconnected")
	}
}
