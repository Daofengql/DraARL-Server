package interconnect

import (
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
	env := NewEnvelope(SubtypeRelayUpstream, edgeSession.NodeID, edgeSession.SessionID, 1, []byte("voice"))
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
	if !session.AcceptMessage(42, now.Add(31*time.Second)) {
		t.Fatal("expired replay entry was not evicted")
	}
}
