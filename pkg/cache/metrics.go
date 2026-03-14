package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics 缓存监控指标
type Metrics struct {
	// L1 本地缓存统计
	L1Hits   int64 // L1 命中次数
	L1Misses int64 // L1 未命中次数

	// L2 Redis 缓存统计
	L2Hits   int64 // L2 命中次数
	L2Misses int64 // L2 未命中次数

	// 失效统计
	Invalidations int64 // 主动失效次数

	// 操作延迟统计（纳秒）
	GetLatencySum  int64 // Get 操作总延迟
	GetLatencyCnt  int64 // Get 操作次数
	SetLatencySum  int64 // Set 操作总延迟
	SetLatencyCnt  int64 // Set 操作次数
	DelLatencySum  int64 // Delete 操作总延迟
	DelLatencyCnt  int64 // Delete 操作次数

	// 错误统计
	Errors int64 // 错误次数

	// 启动时间
	StartTime time.Time
}

// 全局监控指标实例
var (
	globalMetrics     *Metrics
	globalMetricsOnce sync.Once
)

// GetMetrics 获取全局监控指标实例
func GetMetrics() *Metrics {
	globalMetricsOnce.Do(func() {
		globalMetrics = &Metrics{
			StartTime: time.Now(),
		}
	})
	return globalMetrics
}

// RecordL1Hit 记录 L1 命中
func (m *Metrics) RecordL1Hit() {
	atomic.AddInt64(&m.L1Hits, 1)
}

// RecordL1Miss 记录 L1 未命中
func (m *Metrics) RecordL1Miss() {
	atomic.AddInt64(&m.L1Misses, 1)
}

// RecordL2Hit 记录 L2 命中
func (m *Metrics) RecordL2Hit() {
	atomic.AddInt64(&m.L2Hits, 1)
}

// RecordL2Miss 记录 L2 未命中
func (m *Metrics) RecordL2Miss() {
	atomic.AddInt64(&m.L2Misses, 1)
}

// RecordInvalidation 记录主动失效
func (m *Metrics) RecordInvalidation() {
	atomic.AddInt64(&m.Invalidations, 1)
}

// RecordGetLatency 记录 Get 操作延迟
func (m *Metrics) RecordGetLatency(latency time.Duration) {
	atomic.AddInt64(&m.GetLatencySum, latency.Nanoseconds())
	atomic.AddInt64(&m.GetLatencyCnt, 1)
}

// RecordSetLatency 记录 Set 操作延迟
func (m *Metrics) RecordSetLatency(latency time.Duration) {
	atomic.AddInt64(&m.SetLatencySum, latency.Nanoseconds())
	atomic.AddInt64(&m.SetLatencyCnt, 1)
}

// RecordDelLatency 记录 Delete 操作延迟
func (m *Metrics) RecordDelLatency(latency time.Duration) {
	atomic.AddInt64(&m.DelLatencySum, latency.Nanoseconds())
	atomic.AddInt64(&m.DelLatencyCnt, 1)
}

// RecordError 记录错误
func (m *Metrics) RecordError() {
	atomic.AddInt64(&m.Errors, 1)
}

// Snapshot 获取指标快照
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		L1Hits:         atomic.LoadInt64(&m.L1Hits),
		L1Misses:       atomic.LoadInt64(&m.L1Misses),
		L2Hits:         atomic.LoadInt64(&m.L2Hits),
		L2Misses:       atomic.LoadInt64(&m.L2Misses),
		Invalidations:  atomic.LoadInt64(&m.Invalidations),
		GetLatencyAvg:  m.getAvgLatency(atomic.LoadInt64(&m.GetLatencySum), atomic.LoadInt64(&m.GetLatencyCnt)),
		SetLatencyAvg:  m.getAvgLatency(atomic.LoadInt64(&m.SetLatencySum), atomic.LoadInt64(&m.SetLatencyCnt)),
		DelLatencyAvg:  m.getAvgLatency(atomic.LoadInt64(&m.DelLatencySum), atomic.LoadInt64(&m.DelLatencyCnt)),
		Errors:         atomic.LoadInt64(&m.Errors),
		Uptime:         time.Since(m.StartTime),
		L1HitRate:      m.calcHitRate(atomic.LoadInt64(&m.L1Hits), atomic.LoadInt64(&m.L1Misses)),
		L2HitRate:      m.calcHitRate(atomic.LoadInt64(&m.L2Hits), atomic.LoadInt64(&m.L2Misses)),
		TotalRequests:  atomic.LoadInt64(&m.L1Hits) + atomic.LoadInt64(&m.L1Misses),
		TotalCacheHits: atomic.LoadInt64(&m.L1Hits) + atomic.LoadInt64(&m.L2Hits),
	}
}

func (m *Metrics) getAvgLatency(sum, cnt int64) time.Duration {
	if cnt == 0 {
		return 0
	}
	return time.Duration(sum / cnt)
}

func (m *Metrics) calcHitRate(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total) * 100
}

// MetricsSnapshot 指标快照（用于序列化输出）
type MetricsSnapshot struct {
	L1Hits         int64         `json:"l1_hits"`
	L1Misses       int64         `json:"l1_misses"`
	L1HitRate      float64       `json:"l1_hit_rate"` // 百分比
	L2Hits         int64         `json:"l2_hits"`
	L2Misses       int64         `json:"l2_misses"`
	L2HitRate      float64       `json:"l2_hit_rate"` // 百分比
	Invalidations  int64         `json:"invalidations"`
	GetLatencyAvg  time.Duration `json:"get_latency_avg"`
	SetLatencyAvg  time.Duration `json:"set_latency_avg"`
	DelLatencyAvg  time.Duration `json:"del_latency_avg"`
	Errors         int64         `json:"errors"`
	Uptime         time.Duration `json:"uptime"`
	TotalRequests  int64         `json:"total_requests"`
	TotalCacheHits int64         `json:"total_cache_hits"`
	OverallHitRate float64       `json:"overall_hit_rate"` // 总体命中率
}

// Reset 重置所有指标（慎用，仅用于测试）
func (m *Metrics) Reset() {
	atomic.StoreInt64(&m.L1Hits, 0)
	atomic.StoreInt64(&m.L1Misses, 0)
	atomic.StoreInt64(&m.L2Hits, 0)
	atomic.StoreInt64(&m.L2Misses, 0)
	atomic.StoreInt64(&m.Invalidations, 0)
	atomic.StoreInt64(&m.GetLatencySum, 0)
	atomic.StoreInt64(&m.GetLatencyCnt, 0)
	atomic.StoreInt64(&m.SetLatencySum, 0)
	atomic.StoreInt64(&m.SetLatencyCnt, 0)
	atomic.StoreInt64(&m.DelLatencySum, 0)
	atomic.StoreInt64(&m.DelLatencyCnt, 0)
	atomic.StoreInt64(&m.Errors, 0)
	m.StartTime = time.Now()
}

// MetricsHandler 缓存监控处理器（可用于 HTTP 接口）
type MetricsHandler struct {
	metrics *Metrics
}

// NewMetricsHandler 创建监控处理器
func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{
		metrics: GetMetrics(),
	}
}

// GetSnapshot 获取指标快照
func (h *MetricsHandler) GetSnapshot() MetricsSnapshot {
	snapshot := h.metrics.Snapshot()
	// 计算总体命中率
	if snapshot.TotalRequests > 0 {
		snapshot.OverallHitRate = float64(snapshot.TotalCacheHits) / float64(snapshot.TotalRequests) * 100
	}
	return snapshot
}

// GetMetrics 获取底层指标实例
func (h *MetricsHandler) GetMetrics() *Metrics {
	return h.metrics
}
