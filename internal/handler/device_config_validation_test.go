package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestValidateDeviceConfigValuesRequiresStrictNumericInput(t *testing.T) {
	valid := map[string]string{
		"rx_freq":      "439500000",
		"tx_freq":      "439500000",
		"sql_level":    "0",
		"power_level":  "15",
		"tx_bandwidth": "2",
		"rx_ctcss":     "88.5",
		"rx_tone_mode": "cdcss-n",
	}
	if err := validateDeviceConfigValues(valid); err != nil {
		t.Fatalf("valid configuration rejected: %v", err)
	}

	invalid := []struct {
		name  string
		key   string
		value string
	}{
		{name: "frequency suffix", key: "rx_freq", value: "439500000junk"},
		{name: "integer suffix", key: "sql_level", value: "1.0"},
		{name: "nan ctcss", key: "rx_ctcss", value: "NaN"},
		{name: "infinite ctcss", key: "tx_ctcss", value: "+Inf"},
		{name: "float suffix", key: "rx_ctcss", value: "88.5junk"},
		{name: "unknown tone mode", key: "tx_tone_mode", value: "invalid-mode"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateDeviceConfigValues(map[string]string{tc.key: tc.value}); err == nil {
				t.Fatalf("accepted invalid %s=%q", tc.key, tc.value)
			}
		})
	}
}

func TestValidateDeviceConfigValuesPreservesUnknownKeyForwardCompatibility(t *testing.T) {
	if err := validateDeviceConfigValues(map[string]string{"future_radio_option": "device-specific"}); err != nil {
		t.Fatalf("unknown configuration key rejected: %v", err)
	}
}

func TestAdminUpdateDeviceConfigRejectsInvalidValueBeforePersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "42"}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/admin/devices/42/config", bytes.NewBufferString(`{"rx_freq":"439500000junk"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	AdminUpdateDeviceConfig(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
}
