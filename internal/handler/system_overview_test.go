package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetSystemOverviewReturnsOperationalSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/system/overview", GetSystemOverview)
	req := httptest.NewRequest(http.MethodGet, "/system/overview", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var response struct {
		Code int                    `json:"code"`
		Data systemOverviewResponse `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != http.StatusOK || response.Data.CPUCores < 1 || response.Data.Goroutines < 1 || response.Data.UptimeSeconds < 0 {
		t.Fatalf("unexpected overview: %+v", response.Data)
	}
	if response.Data.SampledAt.IsZero() || strings.TrimSpace(response.Data.OS) == "" || strings.TrimSpace(response.Data.GoVersion) == "" {
		t.Fatalf("missing operational identity: %+v", response.Data)
	}
	encoded := res.Body.String()
	for _, forbidden := range []string{"JWT", "Password", "AESKey", "Authorization"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("response leaked sensitive field %q: %s", forbidden, encoded)
		}
	}
}

func TestReadCPUUsagePercentWarmsUpWithoutFabricatingValue(t *testing.T) {
	cpuSampleMu.Lock()
	previousCPUSample = cpuSample{}
	cpuSampleMu.Unlock()
	if value := readCPUUsagePercent(); value != nil {
		t.Fatalf("first sample=%v, want nil until a baseline exists", *value)
	}
}
