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
