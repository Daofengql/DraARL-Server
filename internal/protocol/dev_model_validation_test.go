package protocol

import "testing"

func TestIsValidClientReportedDevModel(t *testing.T) {
	validCases := []byte{
		0, 1, 2, 99,
		100, 101, 104, 105,
		106, 107,
		110, 150,
		151, 200, 255,
	}
	for _, model := range validCases {
		if !IsValidClientReportedDevModel(model) {
			t.Fatalf("expected dev_model=%d to be valid", model)
		}
	}

	invalidCases := []byte{
		108, 109,
	}
	for _, model := range invalidCases {
		if IsValidClientReportedDevModel(model) {
			t.Fatalf("expected dev_model=%d to be invalid", model)
		}
	}
}
