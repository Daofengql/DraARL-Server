package models

import (
	"net"
	"sync"
	"testing"
	"time"
)

func TestDeviceRuntimeSnapshotConcurrentAccess(t *testing.T) {
	device := &Device{
		ID:       42,
		SSID:     7,
		CallSign: "BG7TEST",
		UDPAddr:  &net.UDPAddr{IP: net.ParseIP("192.0.2.10"), Port: 41000},
		DeviceParm: map[string]string{
			"profile": "default",
		},
		GhostRxGroupIDs: []int{1, 2},
	}

	const (
		writers = 8
		readers = 8
		loops   = 2000
	)
	var wg sync.WaitGroup
	wg.Add(writers + readers)
	for i := 0; i < writers; i++ {
		go func(index int) {
			defer wg.Done()
			for n := 0; n < loops; n++ {
				seenAt := time.Unix(int64(index*loops+n), 0)
				device.UpdateRuntime(func(current *Device) {
					current.CallSign = "BG7TEST"
					current.Priority = index + n
					current.LastPacketTime = seenAt
					current.Traffic++
					current.DeviceParm = map[string]string{"profile": "default"}
					current.GhostRxGroupIDs = []int{1, 2}
					current.UDPAddr = &net.UDPAddr{IP: net.ParseIP("192.0.2.10"), Port: 41000}
				})
				if got := device.GetCallSignSSID(); got != "BG7TEST-\a" {
					t.Errorf("unexpected callsign SSID: %q", got)
					return
				}
			}
		}(i)
	}
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for n := 0; n < loops; n++ {
				state := device.RuntimeSnapshot()
				if state.ID != 42 || state.SSID != 7 || state.CallSign != "BG7TEST" || state.Priority < 0 {
					t.Errorf("inconsistent runtime snapshot: %+v", state)
					return
				}
				state.GhostRxGroupIDs[0] = 99
				state.DeviceParm["profile"] = "mutated"
			}
		}()
	}
	wg.Wait()

	state := device.RuntimeSnapshot()
	if state.Traffic != int64(writers*loops) {
		t.Fatalf("traffic updates lost: got=%d want=%d", state.Traffic, writers*loops)
	}
	if state.GhostRxGroupIDs[0] != 1 || state.DeviceParm["profile"] != "default" {
		t.Fatalf("snapshot returned aliased mutable data: %+v", state)
	}
}
