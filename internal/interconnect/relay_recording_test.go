package interconnect

import (
	"bytes"
	"net"
	"testing"
	"time"

	"draarl/internal/protocol"
)

func TestAcceptedRemoteVoiceIsRecordedExactlyOnce(t *testing.T) {
	cluster := NewClusterManager(71)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	session := &NodeSession{NodeID: "edge-record", SessionID: 10, KeyEpoch: 1, Key: []byte("recording-session-key-123456789")}
	gateway.OnConnect(session)
	grant := &DeviceGrant{
		DeviceID: 17, OwnerID: 23, Username: "alice", CallSign: "BG5REC", SSID: 2,
		GroupID: 9, DomainID: 99,
	}
	if err := gateway.activateDeviceSession(session, grant); err != nil {
		t.Fatal(err)
	}
	lease := gateway.speaker.Claim(session.NodeID, session.SessionID, SpeakerLeaseControl{
		Action: SpeakerLeaseActionClaim, RequestID: 1, SessionID: grant.SessionID,
		SessionEpoch: grant.SessionEpoch, DomainID: grant.DomainID,
	}, time.Now())
	if lease.Action != SpeakerLeaseActionGrant || lease.LeaseID == 0 {
		t.Fatalf("speaker lease=%#v", lease)
	}
	recorded := make(chan AcceptedRelay, 2)
	gateway.SetAcceptedRelayHandler(func(relay AcceptedRelay) {
		relay.Payload = append([]byte(nil), relay.Payload...)
		recorded <- relay
	})
	bridge, err := NewNodeDatagramBridge(func(nodeID string, sessionID uint64) *NodeSession {
		if nodeID == session.NodeID && sessionID == session.SessionID {
			return session
		}
		return nil
	}, gateway.OnDatagram, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32000}
	session.BindDataAddr(addr)
	audio := []byte{1, 2, 3, 4}
	inner := protocol.EncodeDraARLv1(grant.Username, "", grant.SSID, protocol.DraARLTypeOpus16K, 2, 0, grant.CallSign, audio)
	marshal := func(messageID, leaseID uint64) []byte {
		t.Helper()
		payload, marshalErr := (RelayFrame{
			SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch, DomainID: grant.DomainID,
			SpeakerLeaseID: leaseID, InnerPacket: inner,
		}).MarshalBinary()
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		env := NewEnvelope(SubtypeRelayUpstream, session.NodeID, session.SessionID, messageID, payload)
		env.KeyEpoch = session.KeyEpoch
		wire, marshalErr := env.Marshal(session.Key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return wire
	}
	wire := marshal(1, lease.LeaseID)
	if !bridge.Handle(wire, addr) {
		t.Fatal("valid upstream voice was not accepted")
	}
	if bridge.Handle(wire, addr) {
		t.Fatal("replayed upstream voice was accepted twice")
	}
	select {
	case relay := <-recorded:
		if relay.DeviceID != grant.DeviceID || relay.OwnerID != grant.OwnerID || relay.SSID != grant.SSID ||
			relay.GroupID != grant.GroupID || relay.Type != protocol.DraARLTypeOpus16K || !bytes.Equal(relay.Payload, audio) {
			t.Fatalf("recorded relay=%#v", relay)
		}
	case <-time.After(time.Second):
		t.Fatal("accepted upstream voice was not recorded")
	}
	select {
	case relay := <-recorded:
		t.Fatalf("replayed upstream voice was recorded again: %#v", relay)
	case <-time.After(50 * time.Millisecond):
	}

	if !bridge.Handle(marshal(2, lease.LeaseID+1), addr) {
		t.Fatal("invalid-lease Type 0 packet was not consumed")
	}
	select {
	case relay := <-recorded:
		t.Fatalf("voice rejected by Speaker Lease was recorded: %#v", relay)
	case <-time.After(50 * time.Millisecond):
	}
}
