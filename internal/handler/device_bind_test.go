package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSubmitDeviceConfigRejectsInvalidSSIDRanges(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []int{0, 100, 105, 236, 255}
	for _, ssid := range testCases {
		t.Run(strconv.Itoa(ssid), func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{
				"device_mac": "AA:BB:CC:DD:EE:FF",
				"ssid":       ssid,
			})

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/device/submit-config", bytes.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			SubmitDeviceConfig(ctx)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", recorder.Code)
			}
			if !bytes.Contains(recorder.Body.Bytes(), []byte("1-99")) || !bytes.Contains(recorder.Body.Bytes(), []byte("106-235")) {
				t.Fatalf("expected SSID range error message, got %s", recorder.Body.String())
			}
		})
	}
}

func TestSubmitDeviceConfigAcceptsDualNormalSSIDRangesBeforeMACValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []int{1, 99, 106, 235}
	for _, ssid := range testCases {
		t.Run(strconv.Itoa(ssid), func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{
				"device_mac": "invalid-mac",
				"ssid":       ssid,
			})

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/device/submit-config", bytes.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			SubmitDeviceConfig(ctx)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", recorder.Code)
			}
			if !bytes.Contains(recorder.Body.Bytes(), []byte("MAC 地址格式错误")) {
				t.Fatalf("expected MAC validation error, got %s", recorder.Body.String())
			}
		})
	}
}
