package handler

import (
	"bytes"
	"errors"
	"testing"
	"testing/iotest"

	"draarl/internal/protocol"
)

func TestGenerateDevicePasswordFromReader(t *testing.T) {
	password, err := generateDevicePasswordFromReader(bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if password != "aaaaaaaa" {
		t.Fatalf("password=%q, want deterministic 8-character result", password)
	}
	if !protocol.IsValidDevicePassword(password) {
		t.Fatalf("generated password %q does not satisfy DraARLv1 format", password)
	}
}

func TestGenerateDevicePasswordFailsClosed(t *testing.T) {
	password, err := generateDevicePasswordFromReader(iotest.ErrReader(errors.New("rng unavailable")))
	if err == nil {
		t.Fatal("expected CSPRNG failure")
	}
	if password != "" {
		t.Fatalf("password=%q, want no credential on CSPRNG failure", password)
	}
}
