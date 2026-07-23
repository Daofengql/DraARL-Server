package accesspoint

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var publicIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9_-]{0,62}[a-z0-9])?$`)

var chineseProvinceNames = map[string]struct{}{
	"北京市": {}, "天津市": {}, "河北省": {}, "山西省": {}, "内蒙古自治区": {},
	"辽宁省": {}, "吉林省": {}, "黑龙江省": {}, "上海市": {}, "江苏省": {},
	"浙江省": {}, "安徽省": {}, "福建省": {}, "江西省": {}, "山东省": {},
	"河南省": {}, "湖北省": {}, "湖南省": {}, "广东省": {}, "广西壮族自治区": {},
	"海南省": {}, "重庆市": {}, "四川省": {}, "贵州省": {}, "云南省": {},
	"西藏自治区": {}, "陕西省": {}, "甘肃省": {}, "青海省": {}, "宁夏回族自治区": {},
	"新疆维吾尔自治区": {}, "台湾省": {}, "香港特别行政区": {}, "澳门特别行政区": {},
}

func NewPublicID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "ap-" + hex.EncodeToString(value[:]), nil
}

func NormalizeUDPHost(value string) (string, error) {
	host := strings.TrimSpace(value)
	if host == "" {
		return "", errors.New("public UDP host is required")
	}
	if len(host) > 253 || strings.ContainsAny(host, "/\\@?#%") {
		return "", errors.New("invalid public UDP host")
	}
	for _, r := range host {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", errors.New("invalid public UDP host")
		}
	}
	ipValue := strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	if ip := net.ParseIP(ipValue); ip != nil {
		return ip.String(), nil
	}
	if strings.Contains(host, ":") {
		return "", errors.New("public UDP host must not contain a port")
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || len(host) > 253 {
		return "", errors.New("invalid public UDP hostname")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid public UDP hostname")
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return "", errors.New("invalid public UDP hostname")
			}
		}
	}
	return host, nil
}

func ValidateUDPPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("public UDP port must be between 1 and 65535")
	}
	return nil
}

func NormalizeLabel(value string, maxLength int) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxLength {
		return "", errors.New("public access label is too long")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", errors.New("public access label contains control characters")
		}
	}
	return value, nil
}

// NormalizeAdministrativeRegion stores one stable human-readable location.
// Chinese locations use the same "province city [area]" shape as repeaters;
// overseas locations remain free-form because the bundled division dataset is
// intentionally limited to Chinese administrative divisions.
func NormalizeAdministrativeRegion(value string, maxLength int) (string, error) {
	value, err := NormalizeLabel(value, maxLength)
	if err != nil {
		return "", err
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return "", errors.New("region is required")
	}
	if _, domestic := chineseProvinceNames[parts[0]]; domestic && len(parts) < 2 {
		return "", errors.New("Chinese region must include a city")
	}
	return strings.Join(parts, " "), nil
}

func NormalizePublicID(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !publicIDPattern.MatchString(value) {
		return "", errors.New("invalid public access ID")
	}
	return value, nil
}
