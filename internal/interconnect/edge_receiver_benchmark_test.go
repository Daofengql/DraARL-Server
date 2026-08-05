package interconnect

import (
	"fmt"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

func benchmarkEdgeGateway(sessionCount, domainCount int) *EdgeGateway {
	gateway := &EdgeGateway{
		sessions:   make(map[uint64]*edgeDeviceSession, sessionCount),
		byIdentity: make(map[string]uint64, sessionCount),
	}
	for i := 0; i < sessionCount; i++ {
		sessionID := uint64(i + 1)
		domainID := uint64(i%domainCount + 1)
		username := fmt.Sprintf("bench-%05d", i)
		gateway.sessions[sessionID] = &edgeDeviceSession{
			Grant: DeviceGrant{
				SessionID: sessionID,
				DeviceID:  i + 1,
				Username:  username,
				SSID:      byte(i%99 + 1),
				DomainID:  domainID,
			},
			Addr: &net.UDPAddr{
				IP:   net.IPv4(127, byte(i>>16), byte(i>>8), byte(i)),
				Port: 10000 + i%50000,
			},
			LastSeen: time.Now(),
		}
		gateway.byIdentity[fmt.Sprintf("%s-%d", username, byte(i%99+1))] = sessionID
	}
	return gateway
}

func benchmarkCollectEdgeTargets(gateway *EdgeGateway, sourceSession, domainID uint64) []udphub.EdgeFanoutTarget {
	gateway.mu.RLock()
	targets := make([]udphub.EdgeFanoutTarget, 0, len(gateway.sessions))
	for id, session := range gateway.sessions {
		if id != sourceSession && session.Grant.DomainID == domainID && !session.Grant.DisableRecv && session.Addr != nil {
			targets = append(targets, udphub.EdgeFanoutTarget{
				Addr:     cloneUDPAddr(session.Addr),
				DeviceID: session.Grant.DeviceID,
				Username: session.Grant.Username,
				SSID:     session.Grant.SSID,
			})
		}
	}
	gateway.mu.RUnlock()
	return targets
}

func BenchmarkEdgeReceiverCollection(b *testing.B) {
	for _, sessionCount := range []int{10000, 20000} {
		gateway := benchmarkEdgeGateway(sessionCount, 5)
		endpoint, err := udphub.NewEdgeEndpoint("127.0.0.1:0", "", func([]byte, *net.UDPAddr, *net.UDPAddr) {})
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { _ = endpoint.Close() })
		gateway.endpoint = endpoint
		if plan := gateway.receiverPlan(1); plan == nil || plan.Len() == 0 {
			b.Fatal("receiver plan returned no targets")
		}
		name := fmt.Sprintf("sessions_%d", sessionCount)

		b.Run(name+"/locked_scan", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				targets := benchmarkCollectEdgeTargets(gateway, 1, 1)
				if len(targets) == 0 {
					b.Fatal("receiver collection returned no targets")
				}
			}
		})

		b.Run(name+"/cached_plan", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				plan := gateway.receiverPlan(1)
				if plan == nil || plan.Len() == 0 {
					b.Fatal("receiver snapshot returned no targets")
				}
			}
		})

		b.Run(name+"/snapshot_rebuild", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				gateway.mu.Lock()
				gateway.invalidateReceiverPlansLocked()
				gateway.mu.Unlock()
				plan := gateway.receiverPlan(1)
				if plan == nil || plan.Len() == 0 {
					b.Fatal("receiver snapshot rebuild returned no targets")
				}
			}
		})

		b.Run(name+"/locked_scan_with_churn", func(b *testing.B) {
			var changes atomic.Uint64
			stop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				session := gateway.sessions[uint64(sessionCount)]
				for {
					select {
					case <-stop:
						return
					default:
					}
					gateway.mu.Lock()
					session.Grant.DisableRecv = !session.Grant.DisableRecv
					gateway.mu.Unlock()
					changes.Add(1)
				}
			}()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = benchmarkCollectEdgeTargets(gateway, 1, 1)
			}
			b.StopTimer()
			close(stop)
			<-done
			b.ReportMetric(float64(changes.Load())/b.Elapsed().Seconds(), "changes/s")
		})
	}
}

func benchmarkModernGhostGateway(sessionCount, domainCount, receiveDomains int) (*EdgeGateway, map[uint64]int) {
	gateway := &EdgeGateway{
		sessions:   make(map[uint64]*edgeDeviceSession, sessionCount),
		byIdentity: make(map[string]uint64, sessionCount), bySessionTag: make(map[uint32]uint64, sessionCount),
	}
	expectedByDomain := make(map[uint64]int, domainCount)
	for i := 0; i < sessionCount; i++ {
		sessionID := uint64(i + 1)
		domains := make([]uint64, 0, receiveDomains)
		groups := make([]int, 0, receiveDomains)
		for offset := 0; offset < receiveDomains; offset++ {
			domainID := uint64((i+offset)%domainCount + 1)
			domains = append(domains, domainID)
			groups = append(groups, int(domainID))
			expectedByDomain[domainID]++
		}
		username := fmt.Sprintf("ghost-%04d", i)
		grant := modernEdgeGhost(
			i+1, username, fmt.Sprintf("ghost-session-%04d", i),
			fmt.Sprintf("%08x-0000-4000-8000-%012x", i+1, i+1), uint32(i+1),
			groups[0], domains[0], groups, domains,
		)
		grant.SessionID = sessionID
		gateway.sessions[sessionID] = &edgeDeviceSession{
			Grant: *grant,
			Addr: &net.UDPAddr{
				IP: net.IPv4(127, byte(i>>16), byte(i>>8), byte(i)), Port: 10000 + i,
			},
			LastSeen: time.Now(),
		}
		gateway.byIdentity[edgeSessionIdentity(*grant)] = sessionID
		gateway.bySessionTag[grant.SessionTag] = sessionID
	}
	return gateway, expectedByDomain
}

func TestEdgeReceiverPlanSupportsThousandModernGhostSessions(t *testing.T) {
	const (
		sessionCount   = 1000
		domainCount    = 32
		receiveDomains = 4
	)
	gateway, expected := benchmarkModernGhostGateway(sessionCount, domainCount, receiveDomains)
	endpoint, err := udphub.NewEdgeEndpoint("127.0.0.1:0", "", func([]byte, *net.UDPAddr, *net.UDPAddr) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = endpoint.Close() })
	gateway.endpoint = endpoint

	for domainID := uint64(1); domainID <= domainCount; domainID++ {
		plan := gateway.receiverPlan(domainID)
		if plan == nil || plan.Len() != expected[domainID] {
			t.Fatalf("domain %d receiver count=%d want=%d", domainID, plan.Len(), expected[domainID])
		}
	}
	stats := gateway.ReceiverCacheSnapshot()
	if stats.Rebuilds != 1 || stats.Hits < domainCount-1 || stats.MaxEntries == 0 {
		t.Fatalf("unexpected thousand-session receiver cache stats: %#v", stats)
	}

	gateway.mu.Lock()
	changed := gateway.sessions[sessionCount]
	changed.Grant.RxDomainIDs = []uint64{domainCount}
	changed.Grant.RxGroupIDs = []int{domainCount}
	gateway.invalidateReceiverPlansLocked()
	gateway.mu.Unlock()
	plan := gateway.receiverPlan(domainCount)
	if plan == nil || !slices.Equal(changed.Grant.RxDomainIDs, []uint64{domainCount}) {
		t.Fatal("receiver cache did not rebuild after a high-scale routing change")
	}
}

func BenchmarkModernGhostMultiReceiveFanout(b *testing.B) {
	const (
		sessionCount   = 1000
		domainCount    = 32
		receiveDomains = 4
	)
	gateway, expected := benchmarkModernGhostGateway(sessionCount, domainCount, receiveDomains)
	endpoint, err := udphub.NewEdgeEndpoint("127.0.0.1:0", "", func([]byte, *net.UDPAddr, *net.UDPAddr) {})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = endpoint.Close() })
	gateway.endpoint = endpoint

	b.Run("cache_build", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(float64(sessionCount), "sessions")
		b.ReportMetric(float64(receiveDomains), "rx_domains/session")
		for i := 0; i < b.N; i++ {
			gateway.mu.Lock()
			gateway.invalidateReceiverPlansLocked()
			gateway.mu.Unlock()
			plan := gateway.receiverPlan(1)
			if plan == nil || plan.Len() != expected[1] {
				b.Fatalf("receiver plan len=%d want=%d", plan.Len(), expected[1])
			}
		}
	})

	plan := gateway.receiverPlan(1)
	b.Run("cache_hit", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(float64(plan.Len()), "targets/op")
		for i := 0; i < b.N; i++ {
			if got := gateway.receiverPlan(1); got != plan {
				b.Fatal("unchanged receiver plan was rebuilt")
			}
		}
	})

	payload := protocol.EncodeDraARLv1(
		"ghost-0000", "", protocol.SSIDGhostAndroid, protocol.DraARLTypeTextMessage,
		protocol.DraARLDevModelAndroid, 0, "BG5TEST", []byte("benchmark multi receive"),
	)
	b.Run("fanout_complete", func(b *testing.B) {
		completed := make(chan udphub.EdgeFanoutResult, 1)
		onComplete := func(result udphub.EdgeFanoutResult) { completed <- result }
		b.ReportAllocs()
		b.ReportMetric(float64(plan.Len()), "targets/op")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !endpoint.FanoutSessionPlan(payload, plan, 1, 1, onComplete) {
				b.Fatal("fan-out plan was rejected")
			}
			result := <-completed
			if result.Attempted != int64(plan.Len()-1) || result.Errors != 0 {
				b.Fatalf("fan-out result=%#v plan=%d", result, plan.Len())
			}
		}
	})
}
