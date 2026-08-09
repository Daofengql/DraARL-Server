package udphub

import (
	"net"
	"testing"
)

func TestEdgeFanoutPlanRejectsStaleGeneration(t *testing.T) {
	endpoint, err := NewEdgeEndpoint("127.0.0.1:0", "", func([]byte, *net.UDPAddr, *net.UDPAddr) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = endpoint.Close() })

	plan := endpoint.PrepareFanout([]EdgeFanoutTarget{{
		Addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, DeviceID: 1,
	}})
	if plan == nil || plan.Len() != 1 {
		t.Fatalf("prepared plan = %#v", plan)
	}
	endpoint.InvalidateFanoutPlans()
	if endpoint.FanoutPlan([]byte("stale"), plan, 0, "", 0, nil) {
		t.Fatal("stale edge fan-out plan was accepted")
	}
}
