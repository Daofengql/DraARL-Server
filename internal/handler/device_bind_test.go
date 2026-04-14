package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSubmitDeviceConfigRejectsInvalidSSIDRanges(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []int{0, 100, 101, 102, 103, 104, 105, 255}
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
			if !bytes.Contains(recorder.Body.Bytes(), []byte("1-99")) || !bytes.Contains(recorder.Body.Bytes(), []byte("106-254")) {
				t.Fatalf("expected SSID range error message, got %s", recorder.Body.String())
			}
		})
	}
}

func TestSubmitDeviceConfigAcceptsDualNormalSSIDRangesBeforeMACValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []int{1, 99, 106, 254}
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

func TestBuildAvailableDynamicBindSSIDsFiltersUsedAndKeepsAscendingOrder(t *testing.T) {
	used := map[int]struct{}{
		1:   {},
		2:   {},
		99:  {},
		106: {},
		110: {},
		254: {},
	}

	got := buildAvailableDynamicBindSSIDs(used)
	if len(got) == 0 {
		t.Fatalf("expected available ssids, got empty list")
	}

	wantPrefix := []int{3, 4, 5, 6, 7}
	if !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("expected prefix %v, got %v", wantPrefix, got[:len(wantPrefix)])
	}

	for _, ssid := range got {
		if _, exists := used[ssid]; exists {
			t.Fatalf("unexpected used ssid in available list: %d", ssid)
		}
		if ssid == 100 || ssid == 105 || ssid == 255 {
			t.Fatalf("unexpected reserved ssid in available list: %d", ssid)
		}
	}
}
