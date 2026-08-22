package handler

import (
	"bytes"
	"errors"
	"testing"
	"testing/iotest"
)

func TestGenerateStateFromReader(t *testing.T) {
	state, err := generateStateFromReader(bytes.NewReader([]byte{
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if state != "000102030405060708090a0b0c0d0e0f" {
		t.Fatalf("state=%q", state)
	}
}

func TestGenerateStateFailsClosed(t *testing.T) {
	state, err := generateStateFromReader(iotest.ErrReader(errors.New("rng unavailable")))
	if err == nil {
		t.Fatal("expected CSPRNG failure")
	}
	if state != "" {
		t.Fatalf("state=%q, want empty on CSPRNG failure", state)
	}
}
