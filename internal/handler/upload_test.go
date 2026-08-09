package handler

import "testing"

func TestHasMeaningfulCallSignChange(t *testing.T) {
	testCases := []struct {
		name      string
		current   string
		submitted string
		want      bool
	}{
		{name: "empty submitted", current: "BG7ABC", submitted: "", want: false},
		{name: "same exact", current: "BG7ABC", submitted: "BG7ABC", want: false},
		{name: "same after normalize", current: "bg7abc", submitted: " BG7ABC ", want: false},
		{name: "different callsign", current: "BG7ABC", submitted: "BG7XYZ", want: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasMeaningfulCallSignChange(tc.current, tc.submitted); got != tc.want {
				t.Fatalf("hasMeaningfulCallSignChange(%q, %q) = %v, want %v", tc.current, tc.submitted, got, tc.want)
			}
		})
	}
}

func TestShouldRejectOperatorCertSubmission(t *testing.T) {
	testCases := []struct {
		name       string
		hasNewFile bool
		current    string
		submitted  string
		want       bool
	}{
		{name: "new file always allowed", hasNewFile: true, current: "BG7ABC", submitted: "", want: false},
		{name: "no file no callsign", hasNewFile: false, current: "BG7ABC", submitted: "", want: true},
		{name: "no file normalized same callsign", hasNewFile: false, current: "bg7abc", submitted: " BG7ABC ", want: true},
		{name: "no file different callsign", hasNewFile: false, current: "BG7ABC", submitted: "BG7XYZ", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRejectOperatorCertSubmission(tc.hasNewFile, tc.current, tc.submitted); got != tc.want {
				t.Fatalf("shouldRejectOperatorCertSubmission(%v, %q, %q) = %v, want %v", tc.hasNewFile, tc.current, tc.submitted, got, tc.want)
			}
		})
	}
}
