package common

import "time"

const APITimeLayout = time.DateTime

// FormatAPITime preserves the existing API's wall-clock representation. Values
// retain the configured database timezone; new APIs should use RFC3339 instead.
// Avoid caching mutable timestamps or converting historical DATETIME rows to UTC.
func FormatAPITime(value interface{ Format(string) string }) string {
	return value.Format(APITimeLayout)
}
