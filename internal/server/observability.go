package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"draarl/internal/config"
	legacydb "draarl/internal/db"
	"draarl/internal/gormdb"
	"draarl/internal/observability"
	"draarl/internal/udphub"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func databaseTimezoneHeader(cfg *config.Configuration) gin.HandlerFunc {
	zone := strings.TrimSpace(cfg.Database.Timezone)
	if zone == "" || strings.EqualFold(zone, "Local") {
		zone = time.Local.String()
		if zone == "Local" {
			zone = os.Getenv("TZ")
		}
		if zone == "" {
			zone = time.Now().Format("-07:00")
		}
	}
	return func(c *gin.Context) { c.Header("X-Database-Timezone", zone); c.Next() }
}

type requestMetric struct {
	count uint64
	nanos uint64
}

var httpMetrics = struct {
	sync.Mutex
	values map[string]requestMetric
}{values: map[string]requestMetric{}}

func requestObservability() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx, span := observability.Start(ctx, "http.request", attribute.String("http.request.method", c.Request.Method))
		defer span.End()
		c.Request = c.Request.WithContext(ctx)
		if id := span.SpanContext().TraceID(); id.IsValid() {
			c.Header("X-Trace-ID", id.String())
		}
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()
		span.SetName(c.Request.Method + " " + route)
		span.SetAttributes(attribute.String("http.route", route), attribute.Int("http.response.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
		method := c.Request.Method
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		default:
			method = "OTHER"
		}
		key := fmt.Sprintf("method=%q,route=%q,status=%q", method, route, strconv.Itoa(status))
		httpMetrics.Lock()
		value := httpMetrics.values[key]
		value.count++
		// #nosec G115 -- Monotonic elapsed request duration is nonnegative.
		value.nanos += uint64(time.Since(start))
		httpMetrics.values[key] = value
		httpMetrics.Unlock()
	}
}

func logRequest(c *gin.Context, duration time.Duration, path, clientIP string) {
	attrs := []any{"event", "http_request", "method", c.Request.Method, "path", path, "status", c.Writer.Status(), "duration_ms", duration.Milliseconds(), "client_ip", clientIP}
	if id := trace.SpanContextFromContext(c.Request.Context()).TraceID(); id.IsValid() {
		attrs = append(attrs, "trace_id", id.String())
	}
	if userID, ok := c.Get("user_id"); ok {
		attrs = append(attrs, "user_id", userID)
	}
	slog.InfoContext(c.Request.Context(), "HTTP request", attrs...)
}

func prometheusMetrics(c *gin.Context) {
	var out strings.Builder
	metric := func(name, help, typ string, value any) {
		fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s %s\n%s %v\n", name, help, name, typ, name, value)
	}
	stats := udphub.GetTotalStats()
	metric("draarl_devices_online", "Online devices across transports.", "gauge", stats.OnlineDevNumber)
	metric("draarl_udp_packets_total", "Received UDP datagrams.", "counter", udphub.GetUDPPipelineStats()["read"])
	metric("draarl_udp_dropped_total", "Dropped UDP datagrams.", "counter", udphub.GetUDPPipelineStats()["dropped"])
	metric("draarl_online_events_synced_total", "Persisted online state events.", "counter", udphub.GetOnlineEventMetrics()["synced"])
	metric("draarl_online_events_dropped_total", "Online events left to periodic reconciliation.", "counter", udphub.GetOnlineEventMetrics()["dropped"])
	metric("draarl_websocket_connections", "Active WebSocket sessions.", "gauge", udphub.GetWebSocketOnlineCount())
	cacheMetrics := cache.GetMetrics().Snapshot()
	metric("draarl_cache_hits_total", "Local cache hits.", "counter", cacheMetrics.L1Hits)
	metric("draarl_cache_misses_total", "Local cache misses.", "counter", cacheMetrics.L1Misses)
	queries, nanos, failures := observability.DatabaseMetrics()
	metric("draarl_db_query_duration_seconds_sum", "Database operation time in seconds.", "counter", float64(nanos)/1e9)
	metric("draarl_db_query_duration_seconds_count", "Database operations.", "counter", queries)
	metric("draarl_db_errors_total", "Database operation failures.", "counter", failures)
	if pool, ok := gormdb.PoolStats(); ok {
		metric("draarl_db_connections_open", "Open SQL connections.", "gauge", pool.OpenConnections)
		metric("draarl_db_connections_in_use", "Busy SQL connections.", "gauge", pool.InUse)
		metric("draarl_db_connections_idle", "Idle SQL connections.", "gauge", pool.Idle)
		metric("draarl_db_pool_wait_total", "SQL pool waits.", "counter", pool.WaitCount)
		metric("draarl_db_pool_wait_seconds_total", "SQL pool wait time.", "counter", pool.WaitDuration.Seconds())
	}
	if pool, ok := legacydb.PoolStats(); ok {
		metric("draarl_legacy_db_connections_open", "Open legacy SQL connections.", "gauge", pool.OpenConnections)
		metric("draarl_legacy_db_connections_in_use", "Busy legacy SQL connections.", "gauge", pool.InUse)
		metric("draarl_legacy_db_pool_wait_total", "Legacy SQL pool waits.", "counter", pool.WaitCount)
	}
	fmt.Fprintln(&out, "# TYPE draarl_http_requests_total counter\n# TYPE draarl_http_request_duration_seconds_sum counter\n# TYPE draarl_http_request_duration_seconds_count counter")
	httpMetrics.Lock()
	keys := make([]string, 0, len(httpMetrics.values))
	for key := range httpMetrics.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v := httpMetrics.values[key]
		fmt.Fprintf(&out, "draarl_http_requests_total{%s} %d\ndraarl_http_request_duration_seconds_sum{%s} %g\ndraarl_http_request_duration_seconds_count{%s} %d\n", key, v.count, key, float64(v.nanos)/1e9, key, v.count)
	}
	httpMetrics.Unlock()
	c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(out.String()))
}

type dependencyHealth struct{ redis *redis.Client }

func newDependencyHealth(cfg *config.Configuration) *dependencyHealth {
	return &dependencyHealth{redis: redis.NewClient(&redis.Options{Addr: cfg.RedisAddr(), Password: cfg.Redis.Password, DB: cfg.Redis.DB, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: -1, ContextTimeoutEnabled: true})}
}
func (h *dependencyHealth) ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	type result struct {
		name string
		err  error
	}
	results := make(chan result, 2)
	go func() { results <- result{"database", gormdb.PingContext(ctx)} }()
	go func() { results <- result{"redis", h.redis.Ping(ctx).Err()} }()
	deps := map[string]string{}
	status := http.StatusOK
	for range 2 {
		r := <-results
		if r.err != nil {
			deps[r.name] = "unavailable"
			status = http.StatusServiceUnavailable
		} else {
			deps[r.name] = "ok"
		}
	}
	state := "ok"
	if status != http.StatusOK {
		state = "degraded"
	}
	c.JSON(status, gin.H{"status": state, "dependencies": deps, "timestamp": time.Now().UTC().Format(time.RFC3339)})
}
