package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"draarl/internal/protocol"
)

func TestIdentitySpaceSupportsMaximumClientCount(t *testing.T) {
	seen := make(map[string]struct{}, maxBenchClients)
	for i := 0; i < maxBenchClients; i++ {
		identity := identityForIndex(i)
		if !protocol.IsValidNormalSSID(identity.ssid) {
			t.Fatalf("client %d received reserved SSID %d", i, identity.ssid)
		}
		if identity.ip == nil || !identity.ip.IsLoopback() {
			t.Fatalf("client %d received non-loopback IP %v", i, identity.ip)
		}
		if protocol.NormalizeMAC(identity.mac) != identity.mac {
			t.Fatalf("client %d received invalid MAC %q", i, identity.mac)
		}
		key := fmt.Sprintf("%s:%d", identity.username, identity.ssid)
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate device identity %s", key)
		}
		seen[key] = struct{}{}
	}
}

func TestParseProcessTargets(t *testing.T) {
	targets, err := parseProcessTargets(0, "site=101,worker-a=102")
	if err != nil || !reflect.DeepEqual(targets, []processTarget{{name: "site", pid: 101}, {name: "worker_a", pid: 102}}) {
		t.Fatalf("targets=%v err=%v", targets, err)
	}
}

func TestParseOptionsIntervalValidationHelperAssumptions(t *testing.T) {
	for _, interval := range []string{"120ms", "60ms", "10ms"} {
		value, err := time.ParseDuration(interval)
		if err != nil || value < 10*time.Millisecond {
			t.Fatalf("test interval %q is invalid", interval)
		}
	}
}

func TestUsersForClients(t *testing.T) {
	tests := map[int]int{
		1:               1,
		248:             1,
		249:             2,
		9000:            37,
		maxBenchClients: 81,
	}
	for clients, want := range tests {
		if got := usersForClients(clients); got != want {
			t.Fatalf("usersForClients(%d)=%d, want %d", clients, got, want)
		}
	}
}
