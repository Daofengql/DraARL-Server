package interconnect

import "testing"

func TestProjectionDeltaIsAtomicAndVersioned(t *testing.T) {
	p := NewProjection(9)
	route := DeviceRoute{SessionID: 11, DeviceID: 42, GroupID: 7, DomainID: 8}
	d := NewRouteDelta(9, 0, 1, []DeltaOperation{{Kind: "upsert", Route: &route}})
	if err := p.ApplyDelta(d); err != nil {
		t.Fatal(err)
	}
	if p.Version != 1 || p.Devices[11].DeviceID != 42 {
		t.Fatalf("delta not applied: %#v", p)
	}
	bad := NewRouteDelta(9, 0, 2, []DeltaOperation{{Kind: "remove", SessionID: 11}})
	if err := p.ApplyDelta(bad); err == nil {
		t.Fatal("stale delta applied")
	}
	if _, ok := p.Devices[11]; !ok {
		t.Fatal("failed delta mutated projection")
	}
}

func TestProjectionStoreSnapshotIsolation(t *testing.T) {
	store := NewProjectionStore(NewProjection(1))
	p := store.Snapshot()
	p.Devices[1] = DeviceRoute{SessionID: 1}
	if len(store.Snapshot().Devices) != 0 {
		t.Fatal("snapshot mutation leaked into store")
	}
}
