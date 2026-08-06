package handler

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	messageListRequests     atomic.Uint64
	messageDetailRequests   atomic.Uint64
	messageResponses2xx     atomic.Uint64
	messageResponses4xx     atomic.Uint64
	messageResponses5xx     atomic.Uint64
	messageScopeRejects     atomic.Uint64
	messageParameterRejects atomic.Uint64
	messageCursorRejects    atomic.Uint64
	messageQueryErrors      atomic.Uint64
	messageVisibleGroups    atomic.Uint64
	messageRowsReturned     atomic.Uint64
	messageAudioURLs        atomic.Uint64
	messageAudioURLFailures atomic.Uint64
	messageListQueryNanos   atomic.Uint64
	messageDetailQueryNanos atomic.Uint64
	messageMaxQueryNanos    atomic.Uint64
	messageRequestNanos     atomic.Uint64
	messageMaxRequestNanos  atomic.Uint64
	messageQueryBuckets     [10]atomic.Uint64
	messageRequestBuckets   [10]atomic.Uint64
)

var messageLatencyThresholds = [...]time.Duration{
	time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	5 * time.Second,
}

func beginMessageAPIRequest(c *gin.Context, detail bool) func() {
	if detail {
		messageDetailRequests.Add(1)
	} else {
		messageListRequests.Add(1)
	}
	started := time.Now()
	return func() {
		elapsed := nonZeroDurationNanos(time.Since(started))
		messageRequestNanos.Add(elapsed)
		updateMaxUint64(&messageMaxRequestNanos, elapsed)
		observeMessageLatency(&messageRequestBuckets, time.Duration(elapsed))
		status := c.Writer.Status()
		switch {
		case status >= http.StatusInternalServerError:
			messageResponses5xx.Add(1)
		case status >= http.StatusBadRequest:
			messageResponses4xx.Add(1)
		default:
			messageResponses2xx.Add(1)
		}
	}
}

func observeMessageQuery(detail bool, elapsed time.Duration) {
	nanos := nonZeroDurationNanos(elapsed)
	if detail {
		messageDetailQueryNanos.Add(nanos)
	} else {
		messageListQueryNanos.Add(nanos)
	}
	updateMaxUint64(&messageMaxQueryNanos, nanos)
	observeMessageLatency(&messageQueryBuckets, time.Duration(nanos))
}

func nonZeroDurationNanos(elapsed time.Duration) uint64 {
	if elapsed <= 0 {
		return 1
	}
	return uint64(elapsed.Nanoseconds())
}

func observeMessageLatency(buckets *[10]atomic.Uint64, elapsed time.Duration) {
	for index, threshold := range messageLatencyThresholds {
		if elapsed <= threshold {
			buckets[index].Add(1)
		}
	}
}

func updateMaxUint64(target *atomic.Uint64, candidate uint64) {
	for current := target.Load(); candidate > current; current = target.Load() {
		if target.CompareAndSwap(current, candidate) {
			return
		}
	}
}

func GetMessageAPIMetrics() map[string]uint64 {
	metrics := map[string]uint64{
		"list_requests":         messageListRequests.Load(),
		"detail_requests":       messageDetailRequests.Load(),
		"responses_2xx":         messageResponses2xx.Load(),
		"responses_4xx":         messageResponses4xx.Load(),
		"responses_5xx":         messageResponses5xx.Load(),
		"scope_rejects":         messageScopeRejects.Load(),
		"parameter_rejects":     messageParameterRejects.Load(),
		"cursor_rejects":        messageCursorRejects.Load(),
		"query_errors":          messageQueryErrors.Load(),
		"visible_groups":        messageVisibleGroups.Load(),
		"rows_returned":         messageRowsReturned.Load(),
		"audio_urls_authorized": messageAudioURLs.Load(),
		"audio_url_failures":    messageAudioURLFailures.Load(),
		"list_query_ns":         messageListQueryNanos.Load(),
		"detail_query_ns":       messageDetailQueryNanos.Load(),
		"max_query_ns":          messageMaxQueryNanos.Load(),
		"request_ns":            messageRequestNanos.Load(),
		"max_request_ns":        messageMaxRequestNanos.Load(),
	}
	labels := [...]string{"1ms", "5ms", "10ms", "25ms", "50ms", "100ms", "250ms", "500ms", "1s", "5s"}
	for index, label := range labels {
		metrics["query_le_"+label] = messageQueryBuckets[index].Load()
		metrics["request_le_"+label] = messageRequestBuckets[index].Load()
	}
	return metrics
}
