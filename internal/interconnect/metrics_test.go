package interconnect

import "testing"

func TestMetricsCountApplicationBytes(t *testing.T) {
	var m Metrics
	m.AddIn(90)
	m.AddOut(120)
	m.AddDrop()
	snap := m.Snapshot()
	if snap.InPackets != 1 || snap.InBytes != 90 || snap.OutPackets != 1 || snap.OutBytes != 120 || snap.Drops != 1 {
		t.Fatalf("unexpected metrics: %#v", snap)
	}
}
