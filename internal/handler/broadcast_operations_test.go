package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	broadcastruntime "draarl/internal/broadcast/runtime"
	"draarl/internal/config"
	"draarl/internal/gormdb"

	"github.com/gin-gonic/gin"
)

func TestBroadcastHealthReportsDeploymentDisabledWithoutScheduler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldConfig := config.Config
	config.Config = &config.Configuration{}
	t.Cleanup(func() { config.Config = oldConfig })
	_ = broadcastruntime.Stop(nil)

	admin := &gormdb.User{ID: 1, Roles: "admin"}
	result := performBroadcastOperationRequest(admin, http.MethodGet, "/broadcast/health", nil, GetBroadcastHealth)
	if result.Code != http.StatusOK || result.Header().Get("Cache-Control") != "no-store, max-age=0" {
		t.Fatalf("status=%d headers=%v body=%s", result.Code, result.Header(), result.Body.String())
	}
	var response struct {
		Data struct {
			DeploymentEnabled  bool `json:"deployment_enabled"`
			SchedulerAvailable bool `json:"scheduler_available"`
			Scheduler          struct {
				Healthy bool `json:"healthy"`
			} `json:"scheduler"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.DeploymentEnabled || response.Data.SchedulerAvailable || !response.Data.Scheduler.Healthy {
		t.Fatalf("health response=%s", result.Body.String())
	}
}

func TestBroadcastOperationsRequireAdminAndRunningScheduler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldConfig := config.Config
	config.Config = &config.Configuration{}
	t.Cleanup(func() { config.Config = oldConfig })
	_ = broadcastruntime.Stop(nil)

	nonAdmin := &gormdb.User{ID: 2, Roles: "user"}
	forbidden := performBroadcastOperationRequest(nonAdmin, http.MethodPost, "/broadcast/emergency-stop", nil, EmergencyStopBroadcasts)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}

	admin := &gormdb.User{ID: 1, Roles: "admin"}
	unavailable := performBroadcastOperationRequest(admin, http.MethodPut, "/broadcast/runtime", []byte(`{"enabled":false}`), UpdateBroadcastOperationalState)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("runtime status=%d body=%s", unavailable.Code, unavailable.Body.String())
	}
}

func TestGenericSiteConfigCannotBypassBroadcastRuntimeCoordination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("user", &gormdb.User{ID: 1, Roles: "admin"})
	context.Request = httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(
		`{"key":" Broadcast.Runtime_Enabled ","value":"false","category":"broadcast"}`,
	))
	context.Request.Header.Set("Content-Type", "application/json")
	(&SiteConfigHandler{}).UpdateConfig(context)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func performBroadcastOperationRequest(user *gormdb.User, method, path string, body []byte, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("user", user)
	context.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	if len(body) > 0 {
		context.Request.Header.Set("Content-Type", "application/json")
	}
	handler(context)
	return recorder
}
