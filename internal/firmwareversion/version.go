package firmwareversion

import (
	"strconv"
	"strings"
)

type parsedVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func parseVersion(value string) (parsedVersion, bool) {
	version := strings.TrimSpace(value)
	if version == "" {
		return parsedVersion{}, false
	}

	core := version
	prerelease := ""
	if dash := strings.IndexByte(version, '-'); dash >= 0 {
		core = version[:dash]
		prerelease = version[dash+1:]
		if prerelease == "" {
			return parsedVersion{}, false
		}
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return parsedVersion{}, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return parsedVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return parsedVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return parsedVersion{}, false
	}

	return parsedVersion{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: prerelease,
	}, true
}

func isNumericIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func comparePrerelease(lhs, rhs string) int {
	if lhs == rhs {
		return 0
	}
	if lhs == "" {
		return 1
	}
	if rhs == "" {
		return -1
	}

	leftParts := strings.Split(lhs, ".")
	rightParts := strings.Split(rhs, ".")
	limit := len(leftParts)
	if len(rightParts) > limit {
		limit = len(rightParts)
	}

	for i := 0; i < limit; i++ {
		if i >= len(leftParts) {
			return -1
		}
		if i >= len(rightParts) {
			return 1
		}

		leftID := leftParts[i]
		rightID := rightParts[i]
		if leftID == rightID {
			continue
		}

		leftNumeric := isNumericIdentifier(leftID)
		rightNumeric := isNumericIdentifier(rightID)

		switch {
		case leftNumeric && rightNumeric:
			leftValue, _ := strconv.Atoi(leftID)
			rightValue, _ := strconv.Atoi(rightID)
			if leftValue < rightValue {
				return -1
			}
			return 1
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		case leftID < rightID:
			return -1
		default:
			return 1
		}
	}

	return 0
}

func CompareVersions(lhs, rhs string) int {
	left, leftOK := parseVersion(lhs)
	right, rightOK := parseVersion(rhs)
	if !leftOK || !rightOK {
		switch {
		case strings.TrimSpace(lhs) < strings.TrimSpace(rhs):
			return -1
		case strings.TrimSpace(lhs) > strings.TrimSpace(rhs):
			return 1
		default:
			return 0
		}
	}

	switch {
	case left.major != right.major:
		if left.major < right.major {
			return -1
		}
		return 1
	case left.minor != right.minor:
		if left.minor < right.minor {
			return -1
		}
		return 1
	case left.patch != right.patch:
		if left.patch < right.patch {
			return -1
		}
		return 1
	default:
		return comparePrerelease(left.prerelease, right.prerelease)
	}
}

func IsNewerVersion(candidate, current string) bool {
	return CompareVersions(candidate, current) > 0
}
