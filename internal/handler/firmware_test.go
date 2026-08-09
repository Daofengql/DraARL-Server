package handler

import (
	"strings"
	"testing"
)

func TestFirmwareVersionValidationRetainsLegacyCompatibility(t *testing.T) {
	tests := map[string]bool{
		"1.2.3":            true,
		"010.0.0":          true,
		"2.0.0-preview_2":  true,
		"1.2":              false,
		"v1.2.3":           false,
		"1.2.3+build.7":    false,
		"1.2.3-preview..2": true,
	}

	for version, want := range tests {
		if got := firmwareSemverRegex.MatchString(version); got != want {
			t.Errorf("firmwareSemverRegex.MatchString(%q)=%t, want %t", version, got, want)
		}
	}
}

func TestFirmwareObjectKeyIsImmutableAndSanitizesFileName(t *testing.T) {
	digest := strings.Repeat("a", 64)
	got := firmwareObjectKey(2, "1.2.3", digest, `..\images/firmware.bin`)
	want := "firmware/2/1.2.3/" + digest + "/firmware.bin"
	if got != want {
		t.Fatalf("firmwareObjectKey()=%q, want %q", got, want)
	}
}

func TestFirmwareInputNormalizationRejectsIntegerWrapAndInvalidNames(t *testing.T) {
	if isSupportedFirmwareDevModel(257) || isSupportedFirmwareDevModel(-255) || !isSupportedFirmwareDevModel(1) {
		t.Fatal("firmware model validation accepted integer wrapping or rejected a supported model")
	}
	got, err := normalizeFirmwareFileName(`..\images/firmware.bin`)
	if err != nil || got != "firmware.bin" {
		t.Fatalf("normalizeFirmwareFileName()=(%q, %v)", got, err)
	}
	if _, err := normalizeFirmwareFileName(strings.Repeat("a", 256) + ".bin"); err == nil {
		t.Fatal("overlong firmware file name must be rejected")
	}
}
