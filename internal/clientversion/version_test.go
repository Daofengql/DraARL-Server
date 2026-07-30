package clientversion

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		name     string
		lhs      string
		rhs      string
		expected int
	}{
		{name: "patch greater", lhs: "1.0.1", rhs: "1.0.0", expected: 1},
		{name: "release beats prerelease", lhs: "1.0.0", rhs: "1.0.0-beta.1", expected: 1},
		{name: "prerelease numeric", lhs: "1.0.0-beta.10", rhs: "1.0.0-beta.2", expected: 1},
		{name: "large numeric prerelease", lhs: "1.0.0-beta.999999999999999999999999999999", rhs: "1.0.0-beta.10", expected: 1},
		{name: "build metadata ignored", lhs: "1.0.0+linux.arm64", rhs: "1.0.0+windows.x64", expected: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Compare(test.lhs, test.rhs)
			if test.expected < 0 && got >= 0 || test.expected > 0 && got <= 0 || test.expected == 0 && got != 0 {
				t.Fatalf("Compare(%q, %q)=%d, want sign %d", test.lhs, test.rhs, got, test.expected)
			}
		})
	}
}

func TestIsValid(t *testing.T) {
	tests := map[string]bool{
		"1.0.0":              true,
		"1.0.0-beta.1":       true,
		"1.0.0+build.202607": true,
		"1.0":                false,
		"v1.0.0":             false,
		" 1.0.0":             false,
		"01.0.0":             false,
		"1.0.0-beta_1":       false,
		"1.0.0-beta..1":      false,
	}
	for value, want := range tests {
		if got := IsValid(value); got != want {
			t.Errorf("IsValid(%q)=%t, want %t", value, got, want)
		}
	}
}
