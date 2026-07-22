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
type MetricsSnapshot struct {
	InPackets  uint64 `json:"in_packets"`
	InBytes    uint64 `json:"in_bytes"`
	OutPackets uint64 `json:"out_packets"`
	OutBytes   uint64 `json:"out_bytes"`
	Drops      uint64 `json:"drops"`
	Errors     uint64 `json:"errors"`
}

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
func (m *Metrics) AddOutBulk(packets, bytes uint64) {
	if packets == 0 {
		return
	}
	atomic.AddUint64(&m.outPackets, packets)
	atomic.AddUint64(&m.outBytes, bytes)
}
func (m *Metrics) AddErrorBulk(count uint64) {
	if count > 0 {
		atomic.AddUint64(&m.errors, count)
	}
}
func (m *Metrics) AddDropBulk(count uint64) {
	if count > 0 {
		atomic.AddUint64(&m.drops, count)
	}
}
func (m *Metrics) AddDrop()  { atomic.AddUint64(&m.drops, 1) }
func (m *Metrics) AddError() { atomic.AddUint64(&m.errors, 1) }
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{atomic.LoadUint64(&m.inPackets), atomic.LoadUint64(&m.inBytes), atomic.LoadUint64(&m.outPackets), atomic.LoadUint64(&m.outBytes), atomic.LoadUint64(&m.drops), atomic.LoadUint64(&m.errors)}
}

func AddMetricsSnapshots(values ...MetricsSnapshot) MetricsSnapshot {
	var total MetricsSnapshot
	for _, value := range values {
		total.InPackets += value.InPackets
		total.InBytes += value.InBytes
		total.OutPackets += value.OutPackets
		total.OutBytes += value.OutBytes
		total.Drops += value.Drops
		total.Errors += value.Errors
	}
	return total
}
