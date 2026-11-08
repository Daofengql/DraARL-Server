package accesspoint

import (
	"strings"
	"testing"
)

func TestNormalizeUDPHostAcceptsDNSAndIPWithoutTransportSyntax(t *testing.T) {
	tests := map[string]string{
		" Edge.Example.COM. ": "edge.example.com",
		"203.0.113.7":         "203.0.113.7",
		"[2001:db8::1]":       "2001:db8::1",
	}
	for input, want := range tests {
		got, err := NormalizeUDPHost(input)
		if err != nil || got != want {
			t.Errorf("NormalizeUDPHost(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestNormalizeUDPHostRejectsURLPortPathAndControls(t *testing.T) {
	for _, input := range []string{"https://edge.example.com", "edge.example.com:60050", "edge.example.com/path", "bad host", "edge.example.com\nspoof", "-edge.example.com"} {
		if _, err := NormalizeUDPHost(input); err == nil {
			t.Errorf("invalid host %q was accepted", input)
		}
	}
}

func TestNormalizePublicID(t *testing.T) {
	got, err := NormalizePublicID(" Center-Fuzhou_1 ")
	if err != nil || got != "center-fuzhou_1" {
		t.Fatalf("NormalizePublicID() = %q, %v", got, err)
	}
	for _, input := range []string{"", "internal node", "-edge", "edge/one", strings.Repeat("a", 65)} {
		if _, err := NormalizePublicID(input); err == nil {
			t.Errorf("invalid public ID %q was accepted", input)
		}
	}
}
