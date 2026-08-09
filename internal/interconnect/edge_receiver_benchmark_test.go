package interconnect

import (
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

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
