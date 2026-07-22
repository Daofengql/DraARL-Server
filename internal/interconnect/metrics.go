package interconnect

import "sync/atomic"

// Metrics are monotonic application-layer counters.  The centre computes rates
// from heartbeat deltas, so a process restart is represented by InstanceID.
type Metrics struct {
	inPackets  uint64
	inBytes    uint64
	outPackets uint64
	outBytes   uint64
	drops      uint64
	errors     uint64
}
type MetricsSnapshot struct{ InPackets, InBytes, OutPackets, OutBytes, Drops, Errors uint64 }

func (m *Metrics) AddIn(n int) {
	if n >= 0 {
		atomic.AddUint64(&m.inPackets, 1)
		atomic.AddUint64(&m.inBytes, uint64(n))
	}
}
func (m *Metrics) AddOut(n int) {
	if n >= 0 {
		atomic.AddUint64(&m.outPackets, 1)
		atomic.AddUint64(&m.outBytes, uint64(n))
	}
}
func (m *Metrics) AddDrop()  { atomic.AddUint64(&m.drops, 1) }
func (m *Metrics) AddError() { atomic.AddUint64(&m.errors, 1) }
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{atomic.LoadUint64(&m.inPackets), atomic.LoadUint64(&m.inBytes), atomic.LoadUint64(&m.outPackets), atomic.LoadUint64(&m.outBytes), atomic.LoadUint64(&m.drops), atomic.LoadUint64(&m.errors)}
}
