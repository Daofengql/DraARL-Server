package runtime

import (
	"sync/atomic"
	"time"

	"draarl/internal/broadcast/model"
)

type MetricsSnapshot struct {
	Scans                 uint64            `json:"scans"`
	ScanErrors            uint64            `json:"scan_errors"`
	DueRuns               uint64            `json:"due_runs"`
	RecoveredRuns         uint64            `json:"recovered_runs"`
	RunsByStatus          map[string]uint64 `json:"runs_by_status"`
	CurrentPlaying        int64             `json:"current_playing"`
	SentPackets           uint64            `json:"sent_packets"`
	DroppedPackets        uint64            `json:"dropped_packets"`
	ScheduleDelaySamples  uint64            `json:"schedule_delay_samples"`
	ScheduleDelayTotalMS  uint64            `json:"schedule_delay_total_ms"`
	ScheduleDelayMaxMS    uint64            `json:"schedule_delay_max_ms"`
	ScheduleDelayLastMS   uint64            `json:"schedule_delay_last_ms"`
	FinalizeErrors        uint64            `json:"finalize_errors"`
	LastScanAt            *time.Time        `json:"last_scan_at,omitempty"`
	LastSuccessfulScanAt  *time.Time        `json:"last_successful_scan_at,omitempty"`
	ConsecutiveScanErrors uint64            `json:"consecutive_scan_errors"`
}

type engineMetrics struct {
	scans                 atomic.Uint64
	scanErrors            atomic.Uint64
	dueRuns               atomic.Uint64
	recoveredRuns         atomic.Uint64
	succeeded             atomic.Uint64
	skippedRecentVoice    atomic.Uint64
	skippedDomainBusy     atomic.Uint64
	skippedInterconnected atomic.Uint64
	skippedNoReceiver     atomic.Uint64
	skippedSiteDisabled   atomic.Uint64
	cancelled             atomic.Uint64
	cancelledSiteDisabled atomic.Uint64
	cancelledInterconnect atomic.Uint64
	failed                atomic.Uint64
	currentPlaying        atomic.Int64
	sentPackets           atomic.Uint64
	droppedPackets        atomic.Uint64
	delaySamples          atomic.Uint64
	delayTotalMS          atomic.Uint64
	delayMaxMS            atomic.Uint64
	delayLastMS           atomic.Uint64
	finalizeErrors        atomic.Uint64
	lastScanUnixNano      atomic.Int64
	lastSuccessUnixNano   atomic.Int64
	consecutiveErrors     atomic.Uint64
}

func (m *engineMetrics) scanStarted(now time.Time) {
	m.scans.Add(1)
	m.lastScanUnixNano.Store(now.UnixNano())
}

func (m *engineMetrics) scanFinished(success bool, now time.Time) {
	if success {
		m.lastSuccessUnixNano.Store(now.UnixNano())
		m.consecutiveErrors.Store(0)
		return
	}
	m.scanErrors.Add(1)
	m.consecutiveErrors.Add(1)
}

func (m *engineMetrics) observeClaim(run model.BroadcastRun, recovered bool, now time.Time) {
	if recovered {
		m.recoveredRuns.Add(1)
	} else {
		m.dueRuns.Add(1)
	}
	delay := now.Sub(run.ScheduledFor).Milliseconds()
	if delay < 0 {
		delay = 0
	}
	value := uint64(delay)
	m.delaySamples.Add(1)
	m.delayTotalMS.Add(value)
	m.delayLastMS.Store(value)
	for current := m.delayMaxMS.Load(); value > current && !m.delayMaxMS.CompareAndSwap(current, value); current = m.delayMaxMS.Load() {
	}
}

func (m *engineMetrics) observeTerminal(status string, sentPackets, droppedPackets int) {
	switch status {
	case model.RunStatusSucceeded:
		m.succeeded.Add(1)
	case model.RunStatusSkippedRecentVoice:
		m.skippedRecentVoice.Add(1)
	case model.RunStatusSkippedDomainBusy:
		m.skippedDomainBusy.Add(1)
	case model.RunStatusSkippedInterconnected:
		m.skippedInterconnected.Add(1)
	case model.RunStatusSkippedNoReceiver:
		m.skippedNoReceiver.Add(1)
	case model.RunStatusSkippedSiteDisabled:
		m.skippedSiteDisabled.Add(1)
	case model.RunStatusCancelled:
		m.cancelled.Add(1)
	case model.RunStatusCancelledSiteDisabled:
		m.cancelledSiteDisabled.Add(1)
	case model.RunStatusCancelledInterconnectEnabled:
		m.cancelledInterconnect.Add(1)
	case model.RunStatusFailed:
		m.failed.Add(1)
	}
	if sentPackets > 0 {
		m.sentPackets.Add(uint64(sentPackets))
	}
	if droppedPackets > 0 {
		m.droppedPackets.Add(uint64(droppedPackets))
	}
}

func (m *engineMetrics) snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		Scans: m.scans.Load(), ScanErrors: m.scanErrors.Load(), DueRuns: m.dueRuns.Load(), RecoveredRuns: m.recoveredRuns.Load(),
		RunsByStatus: map[string]uint64{
			model.RunStatusSucceeded:                    m.succeeded.Load(),
			model.RunStatusSkippedRecentVoice:           m.skippedRecentVoice.Load(),
			model.RunStatusSkippedDomainBusy:            m.skippedDomainBusy.Load(),
			model.RunStatusSkippedInterconnected:        m.skippedInterconnected.Load(),
			model.RunStatusSkippedNoReceiver:            m.skippedNoReceiver.Load(),
			model.RunStatusSkippedSiteDisabled:          m.skippedSiteDisabled.Load(),
			model.RunStatusCancelled:                    m.cancelled.Load(),
			model.RunStatusCancelledSiteDisabled:        m.cancelledSiteDisabled.Load(),
			model.RunStatusCancelledInterconnectEnabled: m.cancelledInterconnect.Load(),
			model.RunStatusFailed:                       m.failed.Load(),
		},
		CurrentPlaying: m.currentPlaying.Load(), SentPackets: m.sentPackets.Load(), DroppedPackets: m.droppedPackets.Load(),
		ScheduleDelaySamples: m.delaySamples.Load(), ScheduleDelayTotalMS: m.delayTotalMS.Load(),
		ScheduleDelayMaxMS: m.delayMaxMS.Load(), ScheduleDelayLastMS: m.delayLastMS.Load(),
		FinalizeErrors: m.finalizeErrors.Load(), LastScanAt: atomicTime(m.lastScanUnixNano.Load()),
		LastSuccessfulScanAt: atomicTime(m.lastSuccessUnixNano.Load()), ConsecutiveScanErrors: m.consecutiveErrors.Load(),
	}
}

func atomicTime(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	result := time.Unix(0, value).UTC()
	return &result
}

type HealthSnapshot struct {
	Started               bool       `json:"started"`
	Stopped               bool       `json:"stopped"`
	OperationalEnabled    bool       `json:"operational_enabled"`
	Healthy               bool       `json:"healthy"`
	ActiveRuns            int        `json:"active_runs"`
	Capacity              int        `json:"capacity"`
	DueBacklog            int64      `json:"due_backlog"`
	LastScanAt            *time.Time `json:"last_scan_at,omitempty"`
	LastSuccessfulScanAt  *time.Time `json:"last_successful_scan_at,omitempty"`
	ConsecutiveScanErrors uint64     `json:"consecutive_scan_errors"`
}
