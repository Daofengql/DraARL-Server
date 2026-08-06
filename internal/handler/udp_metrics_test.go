package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetUDPMetricsReturnsUncachedLayeredSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/udp/metrics", nil)

	GetUDPMetrics(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("Cache-Control = %q", got)
	}
	var response struct {
		Code int                       `json:"code"`
		Data map[string]map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("response code = %d", response.Code)
	}
	for _, layer := range []string{"fanout", "ingress", "receiver_cache", "recording", "socket", "ghost_sessions", "ghost_packets", "message_api"} {
		if _, ok := response.Data[layer]; !ok {
			t.Fatalf("missing metrics layer %q", layer)
		}
	}
}
