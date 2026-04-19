package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	gormdb "draarl/internal/gormdb"

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

func TestBuildReplaceableDynamicBindDevicesFiltersOnlinePendingAndRuntimeActive(t *testing.T) {
	devices := []*gormdb.Device{
		{ID: 11, Name: "离线旧机", OwnerID: 7, SSID: 11, ISOnline: false, LastOnlineIP: "10.0.0.11"},
		{ID: 12, Name: "在线设备", OwnerID: 7, SSID: 12, ISOnline: true},
		{ID: 13, Name: "待绑定占用", OwnerID: 7, SSID: 13, ISOnline: false},
		{ID: 14, Name: "运行时活跃", OwnerID: 7, SSID: 14, ISOnline: false},
		{ID: 15, Name: "更高SSID", OwnerID: 7, SSID: 21, ISOnline: false},
		{ID: 16, Name: "保留段", OwnerID: 7, SSID: 105, ISOnline: false},
	}

	pendingUsed := map[int]struct{}{
		13: {},
	}

	got := buildReplaceableDynamicBindDevices(devices, pendingUsed, "BG7XXX", func(ownerID int, ssid byte) bool {
		return ownerID == 7 && ssid == 14
	})

	if len(got) != 2 {
		t.Fatalf("expected 2 replaceable devices, got %d", len(got))
	}
	if got[0].DeviceID != 11 || got[0].SSID != 11 || got[0].CallSign != "BG7XXX" {
		t.Fatalf("unexpected first replaceable device: %#v", got[0])
	}
	if got[1].DeviceID != 15 || got[1].SSID != 21 {
		t.Fatalf("unexpected second replaceable device: %#v", got[1])
	}
}
