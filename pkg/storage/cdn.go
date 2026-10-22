package storage

import (
	"crypto/md5" // EdgeOne TypeA requires MD5 for protocol compatibility.
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"draarl/internal/config"
)

type cdnSigner struct {
	baseURL  *url.URL
	param    string
	key      string
	validity time.Duration
}

func newCDNSigner(cfg config.StorageConfig) (*cdnSigner, error) {
	if err := cfg.SetCDNDefaults(); err != nil {
		return nil, err
	}
	if !cfg.UseCDN {
		return nil, nil
	}
	key, err := resolveSecretReference(cfg.CDNKey, "Storage.CDNKey")
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(cfg.CDNBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Storage.CDNBaseURL: %w", err)
	}
	return &cdnSigner{
		baseURL:  base,
		param:    cfg.CDNSignParam,
		key:      key,
		validity: time.Duration(cfg.CDNExpirySeconds) * time.Second,
	}, nil
}

func (s *cdnSigner) sign(key string, now time.Time) (string, time.Time, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("generate CDN signing nonce: %w", err)
	}
	return s.signWithNonce(key, now, hex.EncodeToString(nonce[:]))
}

func (s *cdnSigner) signWithNonce(key string, now time.Time, nonce string) (string, time.Time, error) {
	if strings.TrimSpace(key) == "" {
		return "", time.Time{}, fmt.Errorf("CDN object key is empty")
	}
	// Sign the escaped path sent on the wire, including any CDN path prefix.
	// Do not clean the path: object-key slashes and literal percent signs matter.
	u := *s.baseURL
	escapedPath := strings.TrimRight(u.EscapedPath(), "/") + "/" + escapeObjectKeyForURL(key)
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("decode CDN object path: %w", err)
	}
	u.Path, u.RawPath = decodedPath, escapedPath
	timestamp := strconv.FormatInt(now.Unix(), 10)
	fields := timestamp + "-" + nonce + "-0"
	digest := md5.Sum([]byte(escapedPath + "-" + fields + "-" + s.key))
	query := url.Values{}
	query.Set(s.param, fields+"-"+hex.EncodeToString(digest[:]))
	u.RawQuery = query.Encode()
	return u.String(), time.Unix(now.Unix(), 0).Add(s.validity), nil
}
