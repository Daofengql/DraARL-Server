package media

import (
	"sync/atomic"
	"time"
)

type MetricsSnapshot struct {
	Running          bool   `json:"running"`
	QueueDepth       int    `json:"queue_depth"`
	QueueCapacity    int    `json:"queue_capacity"`
	Enqueued         uint64 `json:"enqueued"`
	Started          uint64 `json:"started"`
	Succeeded        uint64 `json:"succeeded"`
	Failed           uint64 `json:"failed"`
	Current          int64  `json:"current"`
	TranscodeSamples uint64 `json:"transcode_samples"`
	TranscodeTotalMS uint64 `json:"transcode_total_ms"`
	TranscodeMaxMS   uint64 `json:"transcode_max_ms"`
	TranscodeLastMS  uint64 `json:"transcode_last_ms"`
}

type processorMetrics struct {
	running         atomic.Bool
	enqueued        atomic.Uint64
	started         atomic.Uint64
	succeeded       atomic.Uint64
	failed          atomic.Uint64
	current         atomic.Int64
	durationSamples atomic.Uint64
	durationTotalMS atomic.Uint64
	durationMaxMS   atomic.Uint64
	durationLastMS  atomic.Uint64
}

func (m *processorMetrics) begin() time.Time {
	m.started.Add(1)
	m.current.Add(1)
	return time.Now()
}

func (m *processorMetrics) finish(startedAt time.Time, succeeded bool) {
	m.current.Add(-1)
	if succeeded {
		m.succeeded.Add(1)
	} else {
		m.failed.Add(1)
	}
	duration := time.Since(startedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	value := uint64(duration)
	m.durationSamples.Add(1)
	m.durationTotalMS.Add(value)
	m.durationLastMS.Store(value)
	for current := m.durationMaxMS.Load(); value > current && !m.durationMaxMS.CompareAndSwap(current, value); current = m.durationMaxMS.Load() {
	}
}

func (p *Processor) Metrics() MetricsSnapshot {
	if p == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		Running: p.metrics.running.Load(), QueueDepth: len(p.jobs), QueueCapacity: cap(p.jobs),
		Enqueued: p.metrics.enqueued.Load(), Started: p.metrics.started.Load(),
		Succeeded: p.metrics.succeeded.Load(), Failed: p.metrics.failed.Load(), Current: p.metrics.current.Load(),
		TranscodeSamples: p.metrics.durationSamples.Load(), TranscodeTotalMS: p.metrics.durationTotalMS.Load(),
		TranscodeMaxMS: p.metrics.durationMaxMS.Load(), TranscodeLastMS: p.metrics.durationLastMS.Load(),
	}
}
