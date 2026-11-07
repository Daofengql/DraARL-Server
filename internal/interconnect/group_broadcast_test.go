package interconnect

import (
	"net"
	"testing"
	"time"

	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

func TestGroupBroadcastTargetsRequireExactGroupAndCapability(t *testing.T) {
	cluster := NewClusterManager(1)
	defer cluster.Close()
	for index, fixture := range []struct {
		node     string
		features uint64
		groupID  int
		rxGroups []int
	}{
		{"old-edge", NodeRequiredFeatures, 7, nil},
		{"linked-only", NodeSupportedFeatures, 8, nil},
		{"explicit-listener", NodeSupportedFeatures, 8, []int{7, 8}},
	} {
		cluster.OnConnect(&NodeSession{NodeID: fixture.node, SessionID: uint64(index + 1), Features: fixture.features})
		if err := cluster.UpsertNodeRoute(fixture.node, DeviceRoute{
			SessionID: uint64(100 + index), SessionEpoch: 1, GroupID: fixture.groupID, RxGroupIDs: fixture.rxGroups, DomainID: 99,
		}); err != nil {
			t.Fatal(err)
		}
		wantReceiver := fixture.node == "explicit-listener"
		if got := cluster.HasGroupBroadcastTarget(7, 99); got != wantReceiver {
			t.Fatalf("receiver after adding %s = %t, want %t", fixture.node, got, wantReceiver)
		}
	}
	cluster.mu.RLock()
	targets := cluster.groupBroadcastTargetsLocked(7, 99)
	cluster.mu.RUnlock()
	if len(targets) != 1 {
		t.Fatalf("group broadcast targets=%v", targets)
	}
	if _, ok := targets["explicit-listener"]; !ok {
		t.Fatalf("explicit A listener was excluded: %v", targets)
	}
}

func TestGroupBroadcastPlanFollowsGroupMoveWithinSameDomain(t *testing.T) {
	endpoint, err := udphub.NewEdgeEndpoint("127.0.0.1:0", func([]byte, *net.UDPAddr, *net.UDPAddr) {})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	target, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	grant := DeviceGrant{SessionID: 1, SessionEpoch: 1, DeviceID: 11, Username: "target", SSID: 1, GroupID: 7, DomainID: 99}
	gateway := &EdgeGateway{
		endpoint: endpoint, byIdentity: make(map[string]uint64),
		sessions: map[uint64]*edgeDeviceSession{1: {Grant: grant, Addr: target.LocalAddr().(*net.UDPAddr)}},
	}
	packet := protocol.EncodeDraARLv1("system-broadcast", "", 255, protocol.DraARLTypeOpus16K, 0, 0, "AUTO", []byte{0, 1, 1})
	packet, _ = protocol.WithSourceGroupID(packet, 7)
	env := NewEnvelope(SubtypeGroupBroadcastDownstream, "center", 1, 1, nil)
	frame := RelayFrame{DomainID: 99, InnerPacket: packet}
	gateway.fanoutDownstream(env, frame)
	buffer := make([]byte, 1400)
	_ = target.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := target.ReadFromUDP(buffer); err != nil {
		t.Fatalf("initial A receiver missed broadcast: %v", err)
	}
	projection := NewProjection(1)
	route := grant.Route()
	route.GroupID = 8
	projection.Devices[1] = route
	gateway.applyRoutes(projection)
	gateway.fanoutDownstream(env, frame)
	_ = target.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := target.ReadFromUDP(buffer); err == nil {
		t.Fatal("cached A broadcast still reached receiver after moving to B")
	}
	// An unscoped/malformed announcement must not fall back to domain fanout.
	frame.InnerPacket, _ = protocol.WithReservedUint32(packet, 0)
	gateway.fanoutDownstream(env, frame)
	_ = target.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := target.ReadFromUDP(buffer); err == nil {
		t.Fatal("announcement without a source group used domain fanout")
	}
}
