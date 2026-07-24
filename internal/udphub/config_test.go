package udphub

import (
	"bytes"
	"testing"
	"time"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

func TestSendConfigToRemoteDeviceUsesInterconnectHookWithoutUDPAddress(t *testing.T) {
	oldHooks := centerHooks()
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })
	var delivered []byte
	SetCenterInterconnectHooks(CenterInterconnectHooks{SendConfig: func(deviceID int, packet []byte, timeout time.Duration) (bool, error) {
		if deviceID != 77 {
			t.Fatalf("deviceID = %d, want 77", deviceID)
		}
		if timeout <= 0 {
			t.Fatal("remote config delivery has no timeout")
		}
		delivered = append([]byte(nil), packet...)
		return true, nil
	}})
	dev := &models.Device{ID: 77, Username: "alice", CallSign: "BG5CFG", SSID: 1, DevModel: protocol.DraARLDevModelESP32NoRadio, ISOnline: true}
	if err := sendConfigToDevice(dev, map[string]string{ConfigKeyADCVolume: "42"}); err != nil {
		t.Fatal(err)
	}
	decoded, err := protocol.NewDraARLv1Packet(nil, delivered)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Type != protocol.DraARLTypeConfig || decoded.Username != dev.Username || decoded.CallSign != dev.CallSign || decoded.SSID != dev.SSID {
		t.Fatalf("unexpected config identity: %#v", decoded)
	}
	want := buildConfigSetPacket(map[string]string{ConfigKeyADCVolume: "42"})
	if !bytes.Equal(decoded.DATA, want) {
		t.Fatalf("DATA = %v, want %v", decoded.DATA, want)
	}
}

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
		ConfigKeyADCGainDB:             "20",
		ConfigKeyADCVolume:             "-1",
		ConfigKeyDACVolume:             "999",
		ConfigKeySQLActiveHigh:         "invalid",
		ConfigKeyPTTActiveHigh:         "true",
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
		ConfigKeyADCGainDB:             "20",
		ConfigKeyADCVolume:             "75",
		ConfigKeyDACVolume:             "80",
		ConfigKeySQLActiveHigh:         "1",
		ConfigKeyPTTActiveHigh:         "0",
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
	if snapshot[ConfigKeyADCGainDB] != "18" {
		t.Fatalf("expected missing adc gain to fallback to 18, got %q", snapshot[ConfigKeyADCGainDB])
	}
	if snapshot[ConfigKeyADCVolume] != "100" {
		t.Fatalf("expected missing adc volume to fallback to 100, got %q", snapshot[ConfigKeyADCVolume])
	}
	if snapshot[ConfigKeyDACVolume] != "80" {
		t.Fatalf("expected missing dac volume to fallback to 80, got %q", snapshot[ConfigKeyDACVolume])
	}
	if snapshot[ConfigKeySQLActiveHigh] != "0" || snapshot[ConfigKeyPTTActiveHigh] != "0" {
		t.Fatalf("expected missing gpio polarities to default low, got sql=%q ptt=%q", snapshot[ConfigKeySQLActiveHigh], snapshot[ConfigKeyPTTActiveHigh])
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
