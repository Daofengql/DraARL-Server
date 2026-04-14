package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUpdateRadioSSIDReturnsGoneWithFixedSSID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPut, "/api/radio/ssid", nil)
	ctx.Request = req

	UpdateRadioSSID(ctx)

	if recorder.Code != http.StatusGone {
		t.Fatalf("expected status 410, got %d", recorder.Code)
	}

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			SSID int `json:"ssid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resp.Code != 410 || resp.Data.SSID != 105 {
		t.Fatalf("unexpected response payload: %#v", resp)
	}
}
