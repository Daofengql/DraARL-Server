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
