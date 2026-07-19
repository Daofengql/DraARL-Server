package main

import (
	"fmt"
	"testing"

	"draarl/internal/protocol"
)

func TestIdentitySpaceSupportsMaximumClientCount(t *testing.T) {
	seen := make(map[string]struct{}, 9000)
	for i := 0; i < 9000; i++ {
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

func TestUsersForClients(t *testing.T) {
	tests := map[int]int{
		1:    1,
		248:  1,
		249:  2,
		9000: 37,
	}
	for clients, want := range tests {
		if got := usersForClients(clients); got != want {
			t.Fatalf("usersForClients(%d)=%d, want %d", clients, got, want)
		}
	}
}
