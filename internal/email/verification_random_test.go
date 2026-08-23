package email

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestVerificationRandomGeneration(t *testing.T) {
	code, err := generateCodeFromReader(bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if code != "000000" {
		t.Fatalf("code=%q, want zero-padded six digits", code)
	}

	sessionID, err := generateSessionIDFromReader(bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionID) != 32 {
		t.Fatalf("session ID length=%d, want 32", len(sessionID))
	}
}

func TestVerificationRandomGenerationFailsClosed(t *testing.T) {
	randomErr := errors.New("random source unavailable")
	if code, err := generateCodeFromReader(io.MultiReader(bytes.NewReader(nil), errReader{randomErr})); err == nil || code != "" {
		t.Fatalf("code=%q err=%v, want failure", code, err)
	}
	if sessionID, err := generateSessionIDFromReader(errReader{randomErr}); err == nil || sessionID != "" {
		t.Fatalf("sessionID=%q err=%v, want failure", sessionID, err)
	}
}

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) { return 0, r.err }
