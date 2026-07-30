package handler

import "testing"

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
