package firmwareversion

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name     string
		lhs      string
		rhs      string
		expected int
	}{
		{name: "PatchGreater", lhs: "1.0.1", rhs: "1.0.0", expected: 1},
		{name: "ReleaseBeatsPrerelease", lhs: "1.0.0", rhs: "1.0.0-beta.1", expected: 1},
		{name: "PrereleaseNumeric", lhs: "1.0.0-beta.10", rhs: "1.0.0-beta.2", expected: 1},
		{name: "PrereleaseLexical", lhs: "1.0.0-rc.1", rhs: "1.0.0-beta.9", expected: 1},
		{name: "ShorterPrereleaseLower", lhs: "1.0.0-beta", rhs: "1.0.0-beta.1", expected: -1},
		{name: "Equal", lhs: "2.3.4", rhs: "2.3.4", expected: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CompareVersions(tc.lhs, tc.rhs)
			switch {
			case tc.expected < 0 && got >= 0:
				t.Fatalf("expected %s < %s, got %d", tc.lhs, tc.rhs, got)
			case tc.expected > 0 && got <= 0:
				t.Fatalf("expected %s > %s, got %d", tc.lhs, tc.rhs, got)
			case tc.expected == 0 && got != 0:
				t.Fatalf("expected %s == %s, got %d", tc.lhs, tc.rhs, got)
			}
		})
	}
}
