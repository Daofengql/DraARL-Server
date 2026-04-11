package udphub

import "testing"

func TestNormalizeDeviceConfigsDoesNotHydrateNewToneFieldsFromLegacyOnly(t *testing.T) {
	configs := NormalizeDeviceConfigs(map[string]string{
		"rx_ctcss":                     "88.5",
		"tx_ctcss":                     "0",
		"sql_level":                    "9",
		"power_level":                  "2",
		ConfigKeyRFGuardEnabled:        "",
		ConfigKeyRFGuardSingleTxLimitS: "9999",
		ConfigKeyRFGuardWindowS:        "1",
		ConfigKeyRFGuardMaxTxInWindowS: "9999",
	})

	if _, ok := configs[ConfigKeyRxToneMode]; ok {
		t.Fatalf("expected legacy-only config to not hydrate %q", ConfigKeyRxToneMode)
	}
	if _, ok := configs[ConfigKeyRxToneValue]; ok {
		t.Fatalf("expected legacy-only config to not hydrate %q", ConfigKeyRxToneValue)
	}
	if configs["sql_level"] != "8" {
		t.Fatalf("expected sql level to clamp to 8, got %q", configs["sql_level"])
	}
	if configs["power_level"] != "3" {
		t.Fatalf("expected medium power to map to high(3), got %q", configs["power_level"])
	}
	if configs[ConfigKeyRFGuardEnabled] != "1" {
		t.Fatalf("expected rf guard enabled default to 1, got %q", configs[ConfigKeyRFGuardEnabled])
	}
	if configs[ConfigKeyRFGuardSingleTxLimitS] != "1800" {
		t.Fatalf("expected single tx limit clamp to 1800, got %q", configs[ConfigKeyRFGuardSingleTxLimitS])
	}
	if configs[ConfigKeyRFGuardWindowS] != "5" {
		t.Fatalf("expected rf guard window clamp to 5, got %q", configs[ConfigKeyRFGuardWindowS])
	}
	if configs[ConfigKeyRFGuardMaxTxInWindowS] != "5" {
		t.Fatalf("expected rf guard max tx in window clamp to window size 5, got %q", configs[ConfigKeyRFGuardMaxTxInWindowS])
	}
}

func TestEncodeDecodeTLVSupportsDigitalToneCompatibility(t *testing.T) {
	original := map[string]string{
		"rx_freq":                      "439500000",
		"tx_freq":                      "439500000",
		ConfigKeyRxToneMode:            ToneModeCDCSSN,
		ConfigKeyRxToneValue:           "023",
		ConfigKeyTxToneMode:            ToneModeCTCSS,
		ConfigKeyTxToneValue:           "88.5",
		"sql_level":                    "9",
		"power_level":                  "2",
		"tx_bandwidth":                 "2",
		ConfigKeyRFGuardEnabled:        "0",
		ConfigKeyRFGuardSingleTxLimitS: "45",
		ConfigKeyRFGuardWindowS:        "600",
		ConfigKeyRFGuardMaxTxInWindowS: "90",
	}

	encoded, _ := encodeTLV(original)
	decoded := decodeTLV(encoded)

	if decoded[ConfigKeyRxToneMode] != ToneModeCDCSSN {
		t.Fatalf("expected rx digital tone mode %q, got %q", ToneModeCDCSSN, decoded[ConfigKeyRxToneMode])
	}
	if decoded[ConfigKeyRxToneValue] != "023" {
		t.Fatalf("expected rx digital tone value 023, got %q", decoded[ConfigKeyRxToneValue])
	}
	if decoded["rx_ctcss"] != "0" {
		t.Fatalf("expected rx legacy ctcss fallback 0 for digital tone, got %q", decoded["rx_ctcss"])
	}
	if decoded[ConfigKeyTxToneMode] != ToneModeCTCSS {
		t.Fatalf("expected tx tone mode %q, got %q", ToneModeCTCSS, decoded[ConfigKeyTxToneMode])
	}
	if decoded[ConfigKeyTxToneValue] != "88.5" {
		t.Fatalf("expected tx tone value 88.5, got %q", decoded[ConfigKeyTxToneValue])
	}
	if decoded["tx_ctcss"] != "88.5" {
		t.Fatalf("expected tx legacy ctcss to remain 88.5, got %q", decoded["tx_ctcss"])
	}
	if decoded["sql_level"] != "8" {
		t.Fatalf("expected sql level to clamp to 8, got %q", decoded["sql_level"])
	}
	if decoded["power_level"] != "3" {
		t.Fatalf("expected medium power to normalize to 3, got %q", decoded["power_level"])
	}
	if decoded[ConfigKeyRFGuardEnabled] != "0" {
		t.Fatalf("expected rf guard enabled to round-trip as 0, got %q", decoded[ConfigKeyRFGuardEnabled])
	}
	if decoded[ConfigKeyRFGuardSingleTxLimitS] != "45" {
		t.Fatalf("expected single tx limit to round-trip as 45, got %q", decoded[ConfigKeyRFGuardSingleTxLimitS])
	}
	if decoded[ConfigKeyRFGuardWindowS] != "600" {
		t.Fatalf("expected rf guard window to round-trip as 600, got %q", decoded[ConfigKeyRFGuardWindowS])
	}
	if decoded[ConfigKeyRFGuardMaxTxInWindowS] != "90" {
		t.Fatalf("expected rf guard max tx in window to round-trip as 90, got %q", decoded[ConfigKeyRFGuardMaxTxInWindowS])
	}
}

func TestBuildConfigSetPacketCountsOnlyKnownTLVs(t *testing.T) {
	packet := buildConfigSetPacket(map[string]string{
		"rx_freq":               "439500000",
		"unknown_key":           "ignored",
		"tx_bandwidth":          "2",
		ConfigKeyRFGuardEnabled: "1",
	})

	if len(packet) < 2 {
		t.Fatalf("expected config packet, got length %d", len(packet))
	}
	if packet[1] != 3 {
		t.Fatalf("expected packet item count 3, got %d", packet[1])
	}
}

func TestDecodeTLVReadFailureFallsBackToNoTone(t *testing.T) {
	decoded := decodeTLV([]byte{
		TLVTypeRxToneMode, 0x01, 0x01, // CTCSS
		TLVTypeRxToneValue, 0x03, '8', '8', '5', // 非预期长度(应为8)，应回退
	})

	if decoded[ConfigKeyRxToneMode] != ToneModeOff {
		t.Fatalf("expected rx tone mode to fallback OFF, got %q", decoded[ConfigKeyRxToneMode])
	}
	if decoded[ConfigKeyRxToneValue] != "0" {
		t.Fatalf("expected rx tone value to fallback 0, got %q", decoded[ConfigKeyRxToneValue])
	}
	if decoded["rx_ctcss"] != "0" {
		t.Fatalf("expected legacy rx_ctcss to fallback 0, got %q", decoded["rx_ctcss"])
	}
}

func TestBuildConfigSnapshotForOverwriteFillsMissingKeys(t *testing.T) {
	snapshot := buildConfigSnapshotForOverwrite(map[string]string{
		"rx_freq": "439500000",
	})

	for _, key := range managedConfigKeys {
		if _, ok := snapshot[key]; !ok {
			t.Fatalf("expected key %q to exist in overwrite snapshot", key)
		}
	}

	if snapshot["rx_freq"] != "439500000" {
		t.Fatalf("expected rx_freq preserved, got %q", snapshot["rx_freq"])
	}
	if snapshot["tx_freq"] != "" {
		t.Fatalf("expected missing tx_freq to be empty, got %q", snapshot["tx_freq"])
	}
	if snapshot[ConfigKeyRxToneMode] != ToneModeOff || snapshot[ConfigKeyRxToneValue] != "0" {
		t.Fatalf("expected missing tone fields fallback to OFF/0, got mode=%q value=%q", snapshot[ConfigKeyRxToneMode], snapshot[ConfigKeyRxToneValue])
	}
	if snapshot[ConfigKeyRFGuardEnabled] != "1" {
		t.Fatalf("expected missing rf guard enabled to fallback to 1, got %q", snapshot[ConfigKeyRFGuardEnabled])
	}
	if snapshot[ConfigKeyRFGuardSingleTxLimitS] != "30" {
		t.Fatalf("expected missing single tx limit to fallback to 30, got %q", snapshot[ConfigKeyRFGuardSingleTxLimitS])
	}
	if snapshot[ConfigKeyRFGuardWindowS] != "300" {
		t.Fatalf("expected missing rf guard window to fallback to 300, got %q", snapshot[ConfigKeyRFGuardWindowS])
	}
	if snapshot[ConfigKeyRFGuardMaxTxInWindowS] != "60" {
		t.Fatalf("expected missing rf guard max tx in window to fallback to 60, got %q", snapshot[ConfigKeyRFGuardMaxTxInWindowS])
	}
}
