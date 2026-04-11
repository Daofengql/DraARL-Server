package udphub

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	ToneModeOff    = "off"
	ToneModeCTCSS  = "ctcss"
	ToneModeCDCSSN = "cdcss_n"
	ToneModeCDCSSI = "cdcss_i"

	ConfigKeyRxToneMode  = "rx_tone_mode"
	ConfigKeyRxToneValue = "rx_tone_value"
	ConfigKeyTxToneMode  = "tx_tone_mode"
	ConfigKeyTxToneValue = "tx_tone_value"

	ConfigKeyRFGuardEnabled        = "rf_guard_enabled"
	ConfigKeyRFGuardSingleTxLimitS = "rf_guard_single_tx_limit_s"
	ConfigKeyRFGuardWindowS        = "rf_guard_window_s"
	ConfigKeyRFGuardMaxTxInWindowS = "rf_guard_max_tx_in_window_s"
	RFGuardSingleTxLimitMinS       = 1
	RFGuardSingleTxLimitMaxS       = 1800
	RFGuardWindowMinS              = 5
	RFGuardWindowMaxS              = 3600
	RFGuardMaxTxInWindowMinS       = 1
	RFGuardEnabledDefault          = true
	RFGuardSingleTxLimitDefaultS   = 30
	RFGuardWindowDefaultS          = 300
	RFGuardMaxTxInWindowDefaultS   = 60
)

var dcsTonePattern = regexp.MustCompile(`^(\d{3})([NI])$`)

// NormalizeDeviceConfigs 统一设备配置的存储与回显格式。
// 当前策略：
// 1. 新字段 rx/tx_tone_mode + rx/tx_tone_value 作为唯一可信表达。
// 2. 旧字段 rx/tx_ctcss 只作为镜像输出，不再反向推导新字段。
// 3. 若历史数据缺少新字段，则保持空值/原值，等待后续用户保存时覆盖。
func NormalizeDeviceConfigs(configs map[string]string) map[string]string {
	if len(configs) == 0 {
		return map[string]string{}
	}

	normalized := make(map[string]string, len(configs)+4)
	for key, value := range configs {
		normalized[key] = strings.TrimSpace(value)
	}

	if hasAnyConfigKey(normalized, ConfigKeyRxToneMode, ConfigKeyRxToneValue) {
		normalizeToneConfig(normalized, "rx")
	}
	if hasAnyConfigKey(normalized, ConfigKeyTxToneMode, ConfigKeyTxToneValue) {
		normalizeToneConfig(normalized, "tx")
	}

	if value, ok := normalized["sql_level"]; ok {
		normalized["sql_level"] = normalizeSQLLevel(value)
	}
	if value, ok := normalized["power_level"]; ok {
		normalized["power_level"] = normalizePowerLevel(value)
	}
	if value, ok := normalized["tx_bandwidth"]; ok {
		normalized["tx_bandwidth"] = normalizeBandwidthLevel(value)
	}
	if value, ok := normalized[ConfigKeyRFGuardEnabled]; ok {
		normalized[ConfigKeyRFGuardEnabled] = normalizeRFGuardEnabled(value)
	}
	if value, ok := normalized[ConfigKeyRFGuardSingleTxLimitS]; ok {
		normalized[ConfigKeyRFGuardSingleTxLimitS] = normalizeRFGuardSingleTxLimit(value)
	}
	windowValue := ""
	if value, ok := normalized[ConfigKeyRFGuardWindowS]; ok {
		windowValue = normalizeRFGuardWindow(value)
		normalized[ConfigKeyRFGuardWindowS] = windowValue
	}
	if value, ok := normalized[ConfigKeyRFGuardMaxTxInWindowS]; ok {
		normalized[ConfigKeyRFGuardMaxTxInWindowS] = normalizeRFGuardMaxTxInWindow(value, windowValue)
	}

	return normalized
}

func normalizeToneConfig(configs map[string]string, prefix string) {
	modeKey := prefix + "_tone_mode"
	valueKey := prefix + "_tone_value"
	legacyKey := prefix + "_ctcss"

	mode := normalizeToneMode(configs[modeKey])
	value := strings.TrimSpace(configs[valueKey])
	mode, value = normalizeTonePair(mode, value)

	configs[modeKey] = mode
	configs[valueKey] = value
	configs[legacyKey] = buildLegacyToneValue(mode, value)
}

func hasAnyConfigKey(configs map[string]string, keys ...string) bool {
	for _, key := range keys {
		if _, ok := configs[key]; ok {
			return true
		}
	}
	return false
}

func normalizeTonePair(modeRaw, valueRaw string) (string, string) {
	mode := normalizeToneMode(modeRaw)
	value := strings.TrimSpace(strings.ToUpper(valueRaw))

	if match := dcsTonePattern.FindStringSubmatch(value); len(match) == 3 {
		switch match[2] {
		case "N":
			mode = ToneModeCDCSSN
		case "I":
			mode = ToneModeCDCSSI
		}
		value = match[1]
	}

	if mode == "" {
		return ToneModeOff, "0"
	}

	switch mode {
	case ToneModeOff:
		return ToneModeOff, "0"
	case ToneModeCTCSS:
		if value == "" || value == "0" || value == "OFF" {
			return ToneModeOff, "0"
		}
		if !looksLikeCTCSS(value) {
			return ToneModeOff, "0"
		}
		return ToneModeCTCSS, normalizeCTCSSValue(value)
	case ToneModeCDCSSN, ToneModeCDCSSI:
		dcsValue := normalizeDCSValue(value)
		if dcsValue == "" {
			return ToneModeOff, "0"
		}
		return mode, dcsValue
	default:
		return ToneModeOff, "0"
	}
}

func normalizeToneMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0":
		return ""
	case ToneModeOff:
		return ToneModeOff
	case ToneModeCTCSS:
		return ToneModeCTCSS
	case ToneModeCDCSSN, "cdcss-n", "cdcssn", "dcsn":
		return ToneModeCDCSSN
	case ToneModeCDCSSI, "cdcss-i", "cdcssi", "dcsi":
		return ToneModeCDCSSI
	default:
		return ""
	}
}

func buildLegacyToneValue(mode, value string) string {
	if mode == ToneModeCTCSS && value != "" && value != "0" {
		return value
	}
	return "0"
}

func looksLikeCTCSS(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return err == nil && parsed > 0
}

func normalizeCTCSSValue(raw string) string {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || parsed <= 0 {
		return "0"
	}
	return fmt.Sprintf("%.1f", parsed)
}

func normalizeDCSValue(raw string) string {
	value := strings.TrimSpace(strings.ToUpper(raw))
	if value == "" || value == "0" || value == "OFF" {
		return ""
	}

	digitsOnly := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, value)
	if digitsOnly == "" {
		return ""
	}

	parsed, err := strconv.Atoi(digitsOnly)
	if err != nil || parsed < 0 || parsed > 999 {
		return ""
	}

	return fmt.Sprintf("%03d", parsed)
}

func normalizeSQLLevel(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return ""
	}
	if value < 0 {
		value = 0
	}
	if value > 8 {
		value = 8
	}
	return strconv.Itoa(value)
}

func normalizePowerLevel(raw string) string {
	switch strings.TrimSpace(raw) {
	case "":
		return ""
	case "1":
		return "1"
	case "2", "3":
		return "3"
	default:
		return ""
	}
}

func normalizeBandwidthLevel(raw string) string {
	trimmed := strings.TrimSpace(raw)
	switch trimmed {
	case "":
		return ""
	case "1":
		return "1"
	case "2":
		return "2"
	default:
		return ""
	}
}

func normalizeRFGuardEnabled(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "1", "true", "on", "enabled":
		if RFGuardEnabledDefault {
			return "1"
		}
		return "0"
	case "0", "false", "off", "disabled":
		return "0"
	default:
		if RFGuardEnabledDefault {
			return "1"
		}
		return "0"
	}
}

func normalizeRFGuardSingleTxLimit(raw string) string {
	return normalizeRFDuration(raw, RFGuardSingleTxLimitDefaultS, RFGuardSingleTxLimitMinS, RFGuardSingleTxLimitMaxS)
}

func normalizeRFGuardWindow(raw string) string {
	return normalizeRFDuration(raw, RFGuardWindowDefaultS, RFGuardWindowMinS, RFGuardWindowMaxS)
}

func normalizeRFGuardMaxTxInWindow(raw, windowRaw string) string {
	window := RFGuardWindowDefaultS
	if parsed, err := strconv.Atoi(strings.TrimSpace(windowRaw)); err == nil {
		if parsed < RFGuardWindowMinS {
			parsed = RFGuardWindowMinS
		}
		if parsed > RFGuardWindowMaxS {
			parsed = RFGuardWindowMaxS
		}
		window = parsed
	}

	defaultValue := RFGuardMaxTxInWindowDefaultS
	if defaultValue > window {
		defaultValue = window
	}

	return normalizeRFDuration(raw, defaultValue, RFGuardMaxTxInWindowMinS, window)
}

func normalizeRFDuration(raw string, defaultValue, minValue, maxValue int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return strconv.Itoa(defaultValue)
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return strconv.Itoa(defaultValue)
	}
	if value < minValue {
		value = minValue
	}
	if value > maxValue {
		value = maxValue
	}
	return strconv.Itoa(value)
}

func toneModeToByte(mode string) byte {
	switch normalizeToneMode(mode) {
	case ToneModeCTCSS:
		return 1
	case ToneModeCDCSSN:
		return 2
	case ToneModeCDCSSI:
		return 3
	default:
		return 0
	}
}

func byteToToneMode(value byte) string {
	switch value {
	case 1:
		return ToneModeCTCSS
	case 2:
		return ToneModeCDCSSN
	case 3:
		return ToneModeCDCSSI
	default:
		return ToneModeOff
	}
}
