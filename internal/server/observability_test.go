package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"draarl/internal/config"
	"github.com/gin-gonic/gin"
)

func TestReadinessReportsUnavailableDependencies(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.Redis.Host = "127.0.0.1"
	cfg.Redis.Port = 1
	h := newDependencyHealth(cfg)
	defer h.redis.Close()
	e := gin.New()
	e.GET("/readyz", h.ready)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"redis":"unavailable"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "127.0.0.1") || strings.Contains(w.Body.String(), "password") {
		t.Fatal("readiness exposed connection information")
	}
}

func TestMetricsUseRouteTemplates(t *testing.T) {
	e := gin.New()
	e.Use(requestObservability())
	e.GET("/devices/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	e.GET("/metrics", prometheusMetrics)
	for _, id := range []string{"17", "99"} {
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/devices/"+id, nil))
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(w.Body.String(), `route="/devices/:id"`) || strings.Contains(w.Body.String(), `route="/devices/17"`) {
		t.Fatalf("invalid route labels: %s", w.Body.String())
	}
	for _, name := range []string{"draarl_devices_online", "draarl_websocket_connections", "draarl_db_query_duration_seconds_count", "draarl_cache_hits_total"} {
		if !strings.Contains(w.Body.String(), name) {
			t.Errorf("missing %s", name)
		}
	}
}
