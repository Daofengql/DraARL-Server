package interconnect

import (
	"testing"

	"draarl/internal/protocol"
)

func TestCenterGatewayRejectsUnauthorizedRelayUpstreamExactlyOnce(t *testing.T) {
	cluster := NewClusterManager(73)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	ownerSession := &NodeSession{NodeID: "edge-owner", SessionID: 101}
	gateway.OnConnect(ownerSession)
	grant := &DeviceGrant{
		DeviceID: 17, OwnerID: 23, Username: "alice", CallSign: "BG5SEC", SSID: 2,
		GroupID: 9, DomainID: 99,
	}
	if err := gateway.activateDeviceSession(ownerSession, grant); err != nil {
		t.Fatal(err)
	}

	accepted := 0
	gateway.SetAcceptedRelayHandler(func(AcceptedRelay) { accepted++ })
	inner := func(username, callSign string, ssid byte) []byte {
		return protocol.EncodeDraARLv1(username, "", ssid, protocol.DraARLTypeTextMessage, 2, 0, callSign, []byte("hello"))
	}
	frame := func(sessionEpoch uint64, packet []byte) Envelope {
		payload, err := (RelayFrame{
			SessionID: grant.SessionID, SessionEpoch: sessionEpoch, DomainID: grant.DomainID + 1000,
			InnerPacket: packet,
		}).MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		return NewEnvelope(SubtypeRelayUpstream, ownerSession.NodeID, ownerSession.SessionID, 1, payload)
	}

	valid := frame(grant.SessionEpoch, inner(grant.Username, grant.CallSign, grant.SSID))
	malformedInner := make([]byte, DraARLHeaderSize)
	gateway.OnDatagram(ownerSession, valid, nil)
	if accepted != 1 || ownerSession.DataMetrics.Snapshot().Drops != 0 {
		t.Fatalf("valid relay accepted=%d metrics=%#v", accepted, ownerSession.DataMetrics.Snapshot())
	}

	tests := []struct {
		name    string
		session *NodeSession
		env     Envelope
	}{
		{"stale device session epoch", ownerSession, frame(grant.SessionEpoch+1, inner(grant.Username, grant.CallSign, grant.SSID))},
		{"wrong control owner", &NodeSession{NodeID: ownerSession.NodeID, SessionID: ownerSession.SessionID + 1}, valid},
		{"wrong username", ownerSession, frame(grant.SessionEpoch, inner("mallory", grant.CallSign, grant.SSID))},
		{"wrong callsign", ownerSession, frame(grant.SessionEpoch, inner(grant.Username, "BG0BAD", grant.SSID))},
		{"wrong ssid", ownerSession, frame(grant.SessionEpoch, inner(grant.Username, grant.CallSign, grant.SSID+1))},
		{"malformed inner packet", ownerSession, frame(grant.SessionEpoch, malformedInner)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := test.session.DataMetrics.Snapshot()
			gateway.OnDatagram(test.session, test.env, nil)
			after := test.session.DataMetrics.Snapshot()
			if accepted != 1 {
				t.Fatalf("rejected relay reached the accepted handler: accepted=%d", accepted)
			}
			if after.Drops != before.Drops+1 || after.Errors != before.Errors {
				t.Fatalf("business rejection metrics before=%#v after=%#v", before, after)
			}
		})
	}

	before := ownerSession.DataMetrics.Snapshot()
	gateway.OnDatagram(ownerSession, NewEnvelope(SubtypeSpeakerLease, ownerSession.NodeID, ownerSession.SessionID, 2, nil), nil)
	after := ownerSession.DataMetrics.Snapshot()
	if after.Drops != before.Drops+1 || after.Errors != before.Errors {
		t.Fatalf("control subtype on data channel metrics before=%#v after=%#v", before, after)
	}
}
