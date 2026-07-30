//go:build embed
// +build embed

package server

import (
	"strings"
	"testing"

	"draarl/pkg/storage"
)

type frontendPublicURLTestStore struct {
	storage.Storage
	capabilities   storage.Capabilities
	publicURL      string
	publicURLCalls int
}

func (s *frontendPublicURLTestStore) Capabilities() storage.Capabilities {
	return s.capabilities
}

func (s *frontendPublicURLTestStore) PublicURL(string) string {
	s.publicURLCalls++
	return s.publicURL
}

func TestFrontendPublicBaseURLRejectsPrivateStorage(t *testing.T) {
	store := &frontendPublicURLTestStore{publicURL: "https://unsigned.example/frontend/v1"}
	_, err := frontendPublicBaseURL(store, "frontend/v1")
	if err == nil || !strings.Contains(err.Error(), "no public URL") {
		t.Fatalf("error=%v, want missing public URL capability", err)
	}
	if store.publicURLCalls != 0 {
		t.Fatal("private storage must be rejected before resolving or uploading frontend assets")
	}
}

func TestFrontendPublicBaseURLAcceptsConfiguredPublicStorage(t *testing.T) {
	store := &frontendPublicURLTestStore{
		capabilities: storage.Capabilities{PublicURL: true},
		publicURL:    "https://cdn.example.com/frontend/v1",
	}
	got, err := frontendPublicBaseURL(store, "frontend/v1")
	if err != nil || got != store.publicURL {
		t.Fatalf("public URL=%q err=%v, want %q", got, err, store.publicURL)
	}
}
