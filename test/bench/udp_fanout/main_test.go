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

func TestServerIndexForClientPlacements(t *testing.T) {
	tests := []struct {
		name      string
		placement string
		want      []int
	}{
		{name: "local", placement: "local", want: []int{0, 0, 0, 0, 0, 0, 0, 0}},
		{name: "distributed", placement: "distributed", want: []int{0, 0, 1, 1, 2, 2, 0, 0}},
		{name: "cross", placement: "cross", want: []int{0, 0, 1, 1, 2, 2, 1, 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := make([]int, len(test.want))
			for i := range got {
				got[i] = serverIndexForClient(i, 2, 3, test.placement)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("placement=%s got=%v want=%v", test.placement, got, test.want)
			}
		})
	}
}

func TestResolveServerAddrsAndProcessTargets(t *testing.T) {
	servers, err := resolveServerAddrs("ignored:1", "127.0.0.1:62051,127.0.0.1:62052")
	if err != nil || len(servers) != 2 || servers[0].Port != 62051 || servers[1].Port != 62052 {
		t.Fatalf("servers=%v err=%v", servers, err)
	}
	if _, err := resolveServerAddrs("", "127.0.0.1:62051,127.0.0.1:62051"); err == nil {
		t.Fatal("duplicate server address was accepted")
	}
	targets, err := parseProcessTargets(0, "center=101,edge-a=102")
	if err != nil || !reflect.DeepEqual(targets, []processTarget{{name: "center", pid: 101}, {name: "edge_a", pid: 102}}) {
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
