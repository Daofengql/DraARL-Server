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
