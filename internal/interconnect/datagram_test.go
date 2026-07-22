package interconnect

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNodeDatagramAuthenticatesSession(t *testing.T) {
	session := &NodeSession{NodeID: "edge-a", SessionID: 12, KeyEpoch: 1, Key: []byte("datagram-key"), RemoteAddr: "127.0.0.1:1"}
	received := make(chan Envelope, 1)
	server, err := NewNodeDatagramServer(NodeDatagramServerConfig{ListenAddr: "127.0.0.1:0", Sessions: func() []*NodeSession { return []*NodeSession{session} }, OnDatagram: func(_ *NodeSession, e Envelope, _ *net.UDPAddr) { received <- e }, MaxAge: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := DialNodeDatagram(context.Background(), server.Addr().String(), session)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	env := NewEnvelope(SubtypeRelayUpstream, session.NodeID, session.SessionID, 1, []byte("voice"))
	if err := client.Send(env); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if string(got.Payload) != "voice" {
			t.Fatalf("payload=%q", got.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("datagram not received")
	}
}
