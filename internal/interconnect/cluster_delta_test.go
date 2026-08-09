package interconnect

import "testing"

func TestNodeRouteVersionsAreIndependent(t *testing.T) {
	m := NewClusterManager(7)
	m.nodeProjection["a"] = NewProjection(7)
	m.nodeProjection["b"] = NewProjection(7)
	if err := m.SetNodeRoute("a", DeviceRoute{SessionID: 1, DomainID: 4, SessionEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetNodeRoute("a", DeviceRoute{SessionID: 1, DomainID: 5, SessionEpoch: 2}); err != nil {
		t.Fatal(err)
	}
	if got := m.nodeProjection["a"].Version; got != 2 {
		t.Fatalf("node a version=%d", got)
	}
	if got := m.nodeProjection["b"].Version; got != 0 {
		t.Fatalf("node b version advanced unexpectedly: %d", got)
	}
}

func TestRouteAckMustMatchPendingMessageAndVersion(t *testing.T) {
	m := NewClusterManager(7)
	defer m.Close()
	m.pendingControl["a"] = map[uint64]*pendingControl{11: {version: 3}}
	if m.MarkRouteAck("a", RouteAck{ClusterEpoch: 7, ProjectionVersion: 4, AckForMessageID: 11}) {
		t.Fatal("mismatched projection version was accepted")
	}
	if m.MarkRouteAck("a", RouteAck{ClusterEpoch: 8, ProjectionVersion: 3, AckForMessageID: 11}) {
		t.Fatal("wrong cluster epoch was accepted")
	}
	if !m.MarkRouteAck("a", RouteAck{ClusterEpoch: 7, ProjectionVersion: 3, AckForMessageID: 11}) {
		t.Fatal("matching ACK was rejected")
	}
	if version, messageID := m.RouteAck("a"); version != 3 || messageID != 11 {
		t.Fatalf("ack state version=%d message=%d", version, messageID)
	}
	if m.PendingControl("a") != 0 {
		t.Fatal("matching ACK did not clear pending control")
	}
}
