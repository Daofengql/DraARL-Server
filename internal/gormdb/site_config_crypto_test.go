package gormdb

import (
	"strings"
	"testing"

	"draarl/pkg/crypto"
)

func TestSiteConfigSensitiveKeyPolicy(t *testing.T) {
	for _, key := range []string{"smtp.password", "keycloak.client_secret", "auth.api_key", "device.token"} {
		if !IsSensitiveSiteConfigKey(key) {
			t.Fatalf("key %q should be sensitive", key)
		}
	}
	for _, key := range []string{"smtp.host", "web.name", "broadcast.enabled"} {
		if IsSensitiveSiteConfigKey(key) {
			t.Fatalf("key %q should not be sensitive", key)
		}
	}
}

func TestSiteConfigSensitiveValueRoundTripAndLegacyCompatibility(t *testing.T) {
	if err := crypto.InitAES("01234567890123456789012345678901"); err != nil {
		t.Fatal(err)
	}

	sealed, err := sealSiteConfigValue("smtp.password", "smtp-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, encryptedSiteConfigPrefix) || strings.Contains(sealed, "smtp-secret") {
		t.Fatalf("sealed value=%q is not encrypted", sealed)
	}
	opened, err := unsealSiteConfigValue("smtp.password", sealed)
	if err != nil || opened != "smtp-secret" {
		t.Fatalf("opened=%q err=%v", opened, err)
	}

	legacy, err := unsealSiteConfigValue("smtp.password", "legacy-plain")
	if err != nil || legacy != "legacy-plain" {
		t.Fatalf("legacy=%q err=%v", legacy, err)
	}

	ordinary, err := sealSiteConfigValue("smtp.host", "smtp.example.test")
	if err != nil || ordinary != "smtp.example.test" {
		t.Fatalf("ordinary=%q err=%v", ordinary, err)
	}

	prefixInput, err := sealSiteConfigValue("smtp.password", encryptedSiteConfigPrefix+"user-input")
	if err != nil {
		t.Fatal(err)
	}
	prefixOpened, err := unsealSiteConfigValue("smtp.password", prefixInput)
	if err != nil || prefixOpened != encryptedSiteConfigPrefix+"user-input" {
		t.Fatalf("prefix input opened=%q err=%v", prefixOpened, err)
	}
}

func TestSiteConfigSensitiveValueRequiresInitializedAES(t *testing.T) {
	// The package-level crypto instance is initialized by the test above in the
	// normal package order; use a malformed encrypted value to verify read-side
	// failures remain explicit instead of silently returning ciphertext.
	if _, err := unsealSiteConfigValue("smtp.password", encryptedSiteConfigPrefix+"not-valid"); err == nil {
		t.Fatal("malformed encrypted value must return an error")
	}
}

func TestLegacyPlainSensitiveSiteConfigDetection(t *testing.T) {
	if !isLegacyPlainSensitiveSiteConfig("smtp.password", "legacy-plain") {
		t.Fatal("legacy sensitive plaintext was not detected")
	}
	if isLegacyPlainSensitiveSiteConfig("smtp.password", "") {
		t.Fatal("empty sensitive value must not be migrated")
	}
	if isLegacyPlainSensitiveSiteConfig("smtp.password", encryptedSiteConfigPrefix+"ciphertext") {
		t.Fatal("versioned ciphertext must not be treated as legacy plaintext")
	}
	if isLegacyPlainSensitiveSiteConfig("smtp.host", "smtp.example.test") {
		t.Fatal("ordinary configuration must not be migrated")
	}
}
