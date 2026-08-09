package interconnect

import "time"

const (
	rateWindowDuration = time.Minute
	rateMaxSampleGap   = 30 * time.Second
)

type MetricRate struct {
	InPPS             float64 `json:"in_pps"`
	OutPPS            float64 `json:"out_pps"`
	InBytesPerSecond  float64 `json:"in_bytes_per_second"`
	OutBytesPerSecond float64 `json:"out_bytes_per_second"`
	DropsPerSecond    float64 `json:"drops_per_second"`
	ErrorsPerSecond   float64 `json:"errors_per_second"`
}

type RateWindow struct {
	Current       MetricRate `json:"current"`
	MinuteAverage MetricRate `json:"minute_average"`
	MinutePeak    MetricRate `json:"minute_peak"`
	LastOnline    MetricRate `json:"last_online"`
	SampleCount   int        `json:"sample_count"`
	Stale         bool       `json:"stale"`
}

type NodeTrafficRates struct {
	Device             RateWindow `json:"device"`
	EdgeInterconnect   RateWindow `json:"edge_interconnect"`
	CenterInterconnect RateWindow `json:"center_interconnect"`
	ResetReason        string     `json:"reset_reason,omitempty"`
}

type timedMetricRate struct {
	at       time.Time
	duration time.Duration
	rate     MetricRate
}

type metricRateHistory struct {
	current MetricRate
	history []timedMetricRate
}

type nodeRateTracker struct {
	initialized bool
	instanceID  string
	previousAt  time.Time
	device      MetricsSnapshot
	edge        MetricsSnapshot
	center      MetricsSnapshot
	deviceRate  metricRateHistory
	edgeRate    metricRateHistory
	centerRate  metricRateHistory
	resetReason string
}

func (t *nodeRateTracker) observe(instanceID string, device, edge, center MetricsSnapshot, at time.Time) {
	if !t.initialized {
		t.reset(instanceID, device, edge, center, at, "initial_sample")
		return
	}
	if instanceID == "" || instanceID != t.instanceID {
		t.reset(instanceID, device, edge, center, at, "instance_changed")
		return
	}
	if !at.After(t.previousAt) {
		t.reset(instanceID, device, edge, center, at, "sample_time_reset")
		return
	}
	duration := at.Sub(t.previousAt)
	if duration > rateMaxSampleGap {
		t.reset(instanceID, device, edge, center, at, "sample_gap")
		return
	}
	if countersRegressed(t.device, device) || countersRegressed(t.edge, edge) || countersRegressed(t.center, center) {
		t.reset(instanceID, device, edge, center, at, "counter_reset")
		return
	}
	t.deviceRate.append(rateBetween(t.device, device, duration), at, duration)
	t.edgeRate.append(rateBetween(t.edge, edge, duration), at, duration)
	t.centerRate.append(rateBetween(t.center, center, duration), at, duration)
	t.device, t.edge, t.center, t.previousAt = device, edge, center, at
	t.resetReason = ""
}

func (t *nodeRateTracker) reset(instanceID string, device, edge, center MetricsSnapshot, at time.Time, reason string) {
	t.initialized = true
	t.instanceID = instanceID
	t.previousAt = at
	t.device, t.edge, t.center = device, edge, center
	t.deviceRate, t.edgeRate, t.centerRate = metricRateHistory{}, metricRateHistory{}, metricRateHistory{}
	t.resetReason = reason
}

func (h *metricRateHistory) append(rate MetricRate, at time.Time, duration time.Duration) {
	h.current = rate
	h.history = append(h.history, timedMetricRate{at: at, duration: duration, rate: rate})
	cutoff := at.Add(-rateWindowDuration)
	first := 0
	for first < len(h.history) && h.history[first].at.Before(cutoff) {
		first++
	}
	if first > 0 {
		copy(h.history, h.history[first:])
		h.history = h.history[:len(h.history)-first]
	}
}

func (h *metricRateHistory) snapshot(online bool) RateWindow {
	window := RateWindow{Current: h.current, LastOnline: h.current, SampleCount: len(h.history), Stale: !online}
	var totalSeconds float64
	for _, sample := range h.history {
		seconds := sample.duration.Seconds()
		if seconds <= 0 {
			continue
		}
		totalSeconds += seconds
		window.MinuteAverage.addWeighted(sample.rate, seconds)
		window.MinutePeak.max(sample.rate)
	}
	if totalSeconds > 0 {
		window.MinuteAverage.scale(1 / totalSeconds)
	}
	if !online {
		window.Current = MetricRate{}
	}
	return window
}

func (t *nodeRateTracker) snapshot(online bool) NodeTrafficRates {
	if t == nil {
		return NodeTrafficRates{Device: RateWindow{Stale: !online}, EdgeInterconnect: RateWindow{Stale: !online}, CenterInterconnect: RateWindow{Stale: !online}}
	}
	return NodeTrafficRates{
		Device:             t.deviceRate.snapshot(online),
		EdgeInterconnect:   t.edgeRate.snapshot(online),
		CenterInterconnect: t.centerRate.snapshot(online),
		ResetReason:        t.resetReason,
	}
}

func countersRegressed(previous, current MetricsSnapshot) bool {
	return current.InPackets < previous.InPackets || current.InBytes < previous.InBytes ||
		current.OutPackets < previous.OutPackets || current.OutBytes < previous.OutBytes ||
		current.Drops < previous.Drops || current.Errors < previous.Errors
}

func rateBetween(previous, current MetricsSnapshot, duration time.Duration) MetricRate {
	seconds := duration.Seconds()
	if seconds <= 0 {
		return MetricRate{}
	}
	return MetricRate{
		InPPS:             float64(current.InPackets-previous.InPackets) / seconds,
		OutPPS:            float64(current.OutPackets-previous.OutPackets) / seconds,
		InBytesPerSecond:  float64(current.InBytes-previous.InBytes) / seconds,
		OutBytesPerSecond: float64(current.OutBytes-previous.OutBytes) / seconds,
		DropsPerSecond:    float64(current.Drops-previous.Drops) / seconds,
		ErrorsPerSecond:   float64(current.Errors-previous.Errors) / seconds,
	}
}

func (r *MetricRate) addWeighted(value MetricRate, weight float64) {
	r.InPPS += value.InPPS * weight
	r.OutPPS += value.OutPPS * weight
	r.InBytesPerSecond += value.InBytesPerSecond * weight
	r.OutBytesPerSecond += value.OutBytesPerSecond * weight
	r.DropsPerSecond += value.DropsPerSecond * weight
	r.ErrorsPerSecond += value.ErrorsPerSecond * weight
}

func (r *MetricRate) scale(factor float64) {
	r.InPPS *= factor
	r.OutPPS *= factor
	r.InBytesPerSecond *= factor
	r.OutBytesPerSecond *= factor
	r.DropsPerSecond *= factor
	r.ErrorsPerSecond *= factor
}

func (r *MetricRate) max(value MetricRate) {
	r.InPPS = max(r.InPPS, value.InPPS)
	r.OutPPS = max(r.OutPPS, value.OutPPS)
	r.InBytesPerSecond = max(r.InBytesPerSecond, value.InBytesPerSecond)
	r.OutBytesPerSecond = max(r.OutBytesPerSecond, value.OutBytesPerSecond)
	r.DropsPerSecond = max(r.DropsPerSecond, value.DropsPerSecond)
	r.ErrorsPerSecond = max(r.ErrorsPerSecond, value.ErrorsPerSecond)
}
