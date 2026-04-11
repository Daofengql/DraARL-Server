package config

import "testing"

func TestGetAllowedOriginsIncludesFrontendURL(t *testing.T) {
	cfg := &Configuration{}
	cfg.Web.AllowedOrigins = []string{
		"https://api.example.com/",
		"invalid-origin",
	}
	cfg.Web.FrontendURL = "https://app.example.com/dashboard"

	origins := cfg.GetAllowedOrigins()
	if len(origins) != 2 {
		t.Fatalf("expected 2 normalized origins, got %d (%v)", len(origins), origins)
	}
	if !containsOrigin(origins, "https://api.example.com") {
		t.Fatalf("expected explicit allowed origin to be preserved, got %v", origins)
	}
	if !containsOrigin(origins, "https://app.example.com") {
		t.Fatalf("expected frontend URL origin to be included, got %v", origins)
	}
}

func TestValidateAllowedOriginsAllowsFrontendURLInRelease(t *testing.T) {
	previousRelease := IsReleaseBuild()
	SetReleaseBuild(true)
	t.Cleanup(func() {
		SetReleaseBuild(previousRelease)
	})

	cfg := &Configuration{}
	cfg.Web.FrontendURL = "https://app.example.com/docs"

	if err := cfg.ValidateAllowedOrigins(); err != nil {
		t.Fatalf("expected frontend URL to satisfy release origin validation, got %v", err)
	}
}

func TestValidateAllowedOriginsRejectsMissingOriginsInRelease(t *testing.T) {
	previousRelease := IsReleaseBuild()
	SetReleaseBuild(true)
	t.Cleanup(func() {
		SetReleaseBuild(previousRelease)
	})

	cfg := &Configuration{}

	if err := cfg.ValidateAllowedOrigins(); err == nil {
		t.Fatal("expected release validation to fail when no origin is configured")
	}
}

func containsOrigin(origins []string, target string) bool {
	for _, origin := range origins {
		if origin == target {
			return true
		}
	}
	return false
}
