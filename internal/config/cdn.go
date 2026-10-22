package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultCDNSignParam     = "sign"
	DefaultCDNExpirySeconds = 3600
	MaxCDNExpirySeconds     = 630720000
)

var cdnSignParamPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,100}$`)

// SetCDNDefaults validates the EdgeOne TypeA read-URL configuration. The
// effective duration must also be configured on the EdgeOne authentication rule.
func (c *StorageConfig) SetCDNDefaults() error {
	if c.CDNEnabledCompat != nil {
		c.UseCDN = *c.CDNEnabledCompat
	}
	if strings.TrimSpace(c.CDNBaseURLCompat) != "" {
		c.CDNBaseURL = c.CDNBaseURLCompat
	}
	if strings.TrimSpace(c.CDNSignParamCompat) != "" {
		c.CDNSignParam = c.CDNSignParamCompat
	}
	if strings.TrimSpace(c.CDNSignKeyCompat) != "" {
		c.CDNKey = c.CDNSignKeyCompat
	}
	if strings.TrimSpace(c.CDNSignTTLCompat) != "" {
		ttl, err := parseCDNSignTTL(c.CDNSignTTLCompat)
		if err != nil {
			return err
		}
		c.CDNExpirySeconds = ttl
	}
	// Do not retain aliases in memory after normalization. This also prevents
	// SaveToFile from emitting duplicate settings under two different names.
	c.CDNEnabledCompat = nil
	c.CDNBaseURLCompat = ""
	c.CDNSignParamCompat = ""
	c.CDNSignTTLCompat = ""
	c.CDNSignKeyCompat = ""

	c.CDNBaseURL = strings.TrimRight(strings.TrimSpace(c.CDNBaseURL), "/")
	c.CDNSignParam = strings.TrimSpace(c.CDNSignParam)
	if c.CDNSignParam == "" {
		c.CDNSignParam = DefaultCDNSignParam
	}
	if c.CDNExpirySeconds == 0 {
		c.CDNExpirySeconds = DefaultCDNExpirySeconds
	}
	c.CDNKey = strings.TrimSpace(c.CDNKey)
	if !c.UseCDN {
		return nil
	}
	base, err := url.Parse(c.CDNBaseURL)
	if err != nil || base.Hostname() == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return fmt.Errorf("Storage.CDNBaseURL must be an absolute http(s) URL")
	}
	if base.User != nil || base.RawQuery != "" || base.ForceQuery || strings.Contains(c.CDNBaseURL, "#") {
		return fmt.Errorf("Storage.CDNBaseURL cannot contain credentials, query parameters or a fragment")
	}
	if !cdnSignParamPattern.MatchString(c.CDNSignParam) {
		return fmt.Errorf("Storage.CDNSignParam must contain 1-100 letters, digits or underscores")
	}
	if c.CDNExpirySeconds < 1 || c.CDNExpirySeconds > MaxCDNExpirySeconds {
		return fmt.Errorf("Storage.CDNExpirySeconds must be between 1 and %d", MaxCDNExpirySeconds)
	}
	if c.CDNKey == "" {
		return fmt.Errorf("Storage.CDNKey is required when Storage.UseCDN is true")
	}
	return nil
}

func parseCDNSignTTL(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 || duration%time.Second != 0 {
		return 0, fmt.Errorf("Storage.cdn_sign_ttl must be a positive duration such as 24h")
	}
	seconds := duration / time.Second
	if seconds > MaxCDNExpirySeconds {
		return 0, fmt.Errorf("Storage.cdn_sign_ttl must be no longer than %ds", MaxCDNExpirySeconds)
	}
	return int(seconds), nil
}
