package interconnect

import (
	"math"
	"testing"
	"time"
)

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

func TestNodeRateTrackerUsesCenterTimeAndResetsUnsafeDeltas(t *testing.T) {
	base := time.Unix(1000, 0)
	tracker := &nodeRateTracker{}
	tracker.observe("instance-a", MetricsSnapshot{}, MetricsSnapshot{}, MetricsSnapshot{}, base)
	tracker.observe("instance-a", MetricsSnapshot{InPackets: 50, InBytes: 5000, OutPackets: 20, OutBytes: 2000}, MetricsSnapshot{}, MetricsSnapshot{}, base.Add(5*time.Second))

	window := tracker.snapshot(true).Device
	assertRateClose(t, window.Current.InPPS, 10)
	assertRateClose(t, window.Current.OutPPS, 4)
	assertRateClose(t, window.Current.InBytesPerSecond, 1000)
	if window.SampleCount != 1 || window.Stale {
		t.Fatalf("unexpected active rate window: %#v", window)
	}

	tracker.observe("instance-a", MetricsSnapshot{InPackets: 150, InBytes: 15000, OutPackets: 30, OutBytes: 3000}, MetricsSnapshot{}, MetricsSnapshot{}, base.Add(10*time.Second))
	window = tracker.snapshot(true).Device
	assertRateClose(t, window.Current.InPPS, 20)
	assertRateClose(t, window.MinuteAverage.InPPS, 15)
	assertRateClose(t, window.MinutePeak.InPPS, 20)

	tracker.observe("instance-b", MetricsSnapshot{InPackets: 1}, MetricsSnapshot{}, MetricsSnapshot{}, base.Add(15*time.Second))
	if snapshot := tracker.snapshot(true); snapshot.Device.SampleCount != 0 || snapshot.ResetReason != "instance_changed" {
		t.Fatalf("instance reset produced a rate spike: %#v", snapshot)
	}
	tracker.observe("instance-b", MetricsSnapshot{}, MetricsSnapshot{}, MetricsSnapshot{}, base.Add(20*time.Second))
	if snapshot := tracker.snapshot(true); snapshot.Device.SampleCount != 0 || snapshot.ResetReason != "counter_reset" {
		t.Fatalf("counter reset produced a rate spike: %#v", snapshot)
	}
	tracker.observe("instance-b", MetricsSnapshot{InPackets: 10}, MetricsSnapshot{}, MetricsSnapshot{}, base.Add(90*time.Second))
	if snapshot := tracker.snapshot(true); snapshot.Device.SampleCount != 0 || snapshot.ResetReason != "sample_gap" {
		t.Fatalf("long sample gap produced a rate spike: %#v", snapshot)
	}
}

func TestOfflineRateWindowKeepsLastRateButReportsZeroCurrent(t *testing.T) {
	base := time.Unix(2000, 0)
	tracker := &nodeRateTracker{}
	tracker.observe("instance", MetricsSnapshot{}, MetricsSnapshot{}, MetricsSnapshot{}, base)
	tracker.observe("instance", MetricsSnapshot{InPackets: 25, InBytes: 2500}, MetricsSnapshot{}, MetricsSnapshot{}, base.Add(5*time.Second))
	window := tracker.snapshot(false).Device
	if !window.Stale || window.Current.InPPS != 0 {
		t.Fatalf("offline window must be stale with zero current rate: %#v", window)
	}
	assertRateClose(t, window.LastOnline.InPPS, 5)
}

func assertRateClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0001 {
		t.Fatalf("rate = %f, want %f", got, want)
	}
}
