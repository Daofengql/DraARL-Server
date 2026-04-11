package buildinfo

import "testing"

func TestFrontendAssetVersionSanitizesVersion(t *testing.T) {
	previousVersion := Version
	t.Cleanup(func() {
		Version = previousVersion
	})

	Version = " Release Candidate/2026.04 "

	if got := FrontendAssetVersion(); got != "release-candidate-2026.04" {
		t.Fatalf("expected sanitized frontend asset version, got %q", got)
	}
}
