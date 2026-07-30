package clientversion

import (
	"strings"

	"golang.org/x/mod/semver"
)

// IsValid accepts complete SemVer 2.0.0 versions without a leading v.
// x/mod also accepts abbreviated Go module versions, so require three core
// components before delegating prerelease and build-metadata validation.
func IsValid(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	core := value
	if index := strings.IndexAny(core, "-+"); index >= 0 {
		core = core[:index]
	}
	if len(strings.Split(core, ".")) != 3 {
		return false
	}
	return semver.IsValid("v" + value)
}

// Compare applies SemVer precedence. Build metadata does not affect ordering.
// Callers validate input before comparing; the lexical fallback is defensive.
func Compare(lhs, rhs string) int {
	if !IsValid(lhs) || !IsValid(rhs) {
		return strings.Compare(lhs, rhs)
	}
	return semver.Compare("v"+lhs, "v"+rhs)
}
