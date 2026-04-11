package buildinfo

import "strings"

var (
	Version   = "dev"
	BuildTime = "unknown"
	Release   = "false"
)

func VersionString() string {
	version := strings.TrimSpace(Version)
	if version == "" {
		return "dev"
	}
	return version
}

func BuildTimeString() string {
	buildTime := strings.TrimSpace(BuildTime)
	if buildTime == "" {
		return "unknown"
	}
	return buildTime
}

func IsRelease() bool {
	return strings.EqualFold(strings.TrimSpace(Release), "true")
}

func FrontendAssetVersion() string {
	raw := strings.ToLower(strings.TrimSpace(VersionString()))
	if raw == "" {
		return "dev"
	}

	var builder strings.Builder
	builder.Grow(len(raw))

	lastWasDash := false
	for _, r := range raw {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if allowed {
			builder.WriteRune(r)
			lastWasDash = r == '-'
			continue
		}

		if !lastWasDash {
			builder.WriteByte('-')
			lastWasDash = true
		}
	}

	sanitized := strings.Trim(builder.String(), "-.")
	if sanitized == "" {
		return "dev"
	}
	return sanitized
}
