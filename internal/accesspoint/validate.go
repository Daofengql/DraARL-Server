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

func NormalizePublicID(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !publicIDPattern.MatchString(value) {
		return "", errors.New("invalid public access ID")
	}
	return value, nil
}
