package udphub

import (
	"testing"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

func TestNormalizeDeviceConfigsDoesNotHydrateNewToneFieldsFromLegacyOnly(t *testing.T) {
	configs := NormalizeDeviceConfigs(map[string]string{
		"rx_ctcss":             "88.5",
		"tx_ctcss":             "0",
		"sql_level":            "9",
		"power_level":          "2",
		ConfigKeyADCGainDB:     "20",
		ConfigKeyADCVolume:     "-1",
		ConfigKeyDACVolume:     "999",
		ConfigKeySQLActiveHigh: "invalid",
		ConfigKeyPTTActiveHigh: "true",
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
	if configs[ConfigKeyADCGainDB] != "21" {
		t.Fatalf("expected adc gain 20 dB to normalize to 21 dB, got %q", configs[ConfigKeyADCGainDB])
	}
	if configs[ConfigKeyADCVolume] != "0" {
		t.Fatalf("expected adc volume to clamp to 0, got %q", configs[ConfigKeyADCVolume])
	}
	if configs[ConfigKeyDACVolume] != "100" {
		t.Fatalf("expected dac volume to clamp to 100, got %q", configs[ConfigKeyDACVolume])
	}
	if configs[ConfigKeySQLActiveHigh] != "0" {
		t.Fatalf("expected invalid sql polarity to fallback low, got %q", configs[ConfigKeySQLActiveHigh])
	}
	if configs[ConfigKeyPTTActiveHigh] != "1" {
		t.Fatalf("expected high ptt polarity to normalize to 1, got %q", configs[ConfigKeyPTTActiveHigh])
	}
}

func TestEncodeDecodeTLVSupportsDigitalToneCompatibility(t *testing.T) {
	original := map[string]string{
		"rx_freq":              "439500000",
		"tx_freq":              "439500000",
		ConfigKeyRxToneMode:    ToneModeCDCSSN,
		ConfigKeyRxToneValue:   "023",
		ConfigKeyTxToneMode:    ToneModeCTCSS,
		ConfigKeyTxToneValue:   "88.5",
		"sql_level":            "9",
		"power_level":          "2",
		"tx_bandwidth":         "2",
		ConfigKeyADCGainDB:     "20",
		ConfigKeyADCVolume:     "75",
		ConfigKeyDACVolume:     "80",
		ConfigKeySQLActiveHigh: "1",
		ConfigKeyPTTActiveHigh: "0",
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
	if decoded[ConfigKeyADCGainDB] != "21" {
		t.Fatalf("expected normalized adc gain to round-trip as 21, got %q", decoded[ConfigKeyADCGainDB])
	}
	if decoded[ConfigKeyADCVolume] != "75" {
		t.Fatalf("expected adc volume to round-trip as 75, got %q", decoded[ConfigKeyADCVolume])
	}
	if decoded[ConfigKeyDACVolume] != "80" {
		t.Fatalf("expected dac volume to round-trip as 80, got %q", decoded[ConfigKeyDACVolume])
	}
	if decoded[ConfigKeySQLActiveHigh] != "1" || decoded[ConfigKeyPTTActiveHigh] != "0" {
		t.Fatalf("expected gpio polarities to round-trip as sql=1 ptt=0, got sql=%q ptt=%q", decoded[ConfigKeySQLActiveHigh], decoded[ConfigKeyPTTActiveHigh])
	}
}

func TestBuildConfigSetPacketCountsOnlyKnownTLVs(t *testing.T) {
	packet := buildConfigSetPacket(map[string]string{
		"rx_freq":      "439500000",
		"unknown_key":  "ignored",
		"tx_bandwidth": "2",
	})

	if len(packet) < 2 {
		t.Fatalf("expected config packet, got length %d", len(packet))
	}
	if packet[1] != 2 {
		t.Fatalf("expected packet item count 2, got %d", packet[1])
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

func TestConfigSnapshotDoesNotInventAbsentHardwareSettings(t *testing.T) {
	snapshot := buildConfigSnapshot(map[string]string{"rx_freq": "439500000", "adc_volume": "75", "rf_guard_enabled": "1"})
	if len(snapshot) != 2 || snapshot["rx_freq"] != "439500000" || snapshot["adc_volume"] != "75" {
		t.Fatalf("unexpected reported settings: %#v", snapshot)
	}
	for _, key := range []string{"tx_freq", "rx_tone_mode", "sql_active_high", "rf_guard_enabled"} {
		if _, exists := snapshot[key]; exists {
			t.Fatalf("invented field %q", key)
		}
	}
}

func TestObsoleteRFGuardTLVsAreIgnored(t *testing.T) {
	decoded := decodeTLV([]byte{0x0C, 1, 1, 0x0D, 2, 0, 30, TLVTypeADCVolume, 1, 70})
	if len(decoded) != 1 || decoded[ConfigKeyADCVolume] != "70" {
		t.Fatalf("obsolete TLVs affected config: %#v", decoded)
	}
	_, count := encodeTLV(map[string]string{"rf_guard_enabled": "1", "adc_volume": "70"})
	if count != 1 {
		t.Fatalf("encoded obsolete field, count=%d", count)
	}
}

func TestAudioConfigsAreSupportedByESP32Profiles(t *testing.T) {
	configs := map[string]string{
		ConfigKeyADCGainDB:     "20",
		ConfigKeyADCVolume:     "90",
		ConfigKeyDACVolume:     "80",
		ConfigKeySQLActiveHigh: "1",
		ConfigKeyPTTActiveHigh: "1",
		"rx_freq":              "439500000",
	}

	sa818Configs := filterConfigsForDevice(&models.Device{
		DevModel: protocol.DraARLDevModelESP32Radio,
	}, configs)
	if sa818Configs[ConfigKeyADCGainDB] != "21" ||
		sa818Configs[ConfigKeyADCVolume] != "90" ||
		sa818Configs[ConfigKeyDACVolume] != "80" {
		t.Fatalf("expected normalized audio configs for SA818 profile, got %#v", sa818Configs)
	}
	if _, ok := sa818Configs[ConfigKeySQLActiveHigh]; ok {
		t.Fatalf("expected gpio polarity configs to be filtered for SA818 profile, got %#v", sa818Configs)
	}

	noRadioConfigs := filterConfigsForDevice(&models.Device{
		DevModel: protocol.DraARLDevModelESP32NoRadio,
	}, configs)
	if noRadioConfigs[ConfigKeyADCGainDB] != "21" ||
		noRadioConfigs[ConfigKeyADCVolume] != "90" ||
		noRadioConfigs[ConfigKeyDACVolume] != "80" ||
		noRadioConfigs[ConfigKeySQLActiveHigh] != "1" ||
		noRadioConfigs[ConfigKeyPTTActiveHigh] != "1" {
		t.Fatalf("expected normalized audio configs for no-radio ESP32 profile, got %#v", noRadioConfigs)
	}
	if _, ok := noRadioConfigs["rx_freq"]; ok {
		t.Fatalf("expected RF config to remain filtered for no-radio ESP32 profile, got %#v", noRadioConfigs)
	}
}
