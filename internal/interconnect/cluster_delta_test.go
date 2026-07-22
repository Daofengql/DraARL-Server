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
