package cache

import (
	"draarl/internal/gormdb"
	"testing"
)

func TestDeviceQueryKeysIncludePermissionsAndEveryFilter(t *testing.T) {
	group, online := 0, false
	filters := []gormdb.DeviceListFilter{{}, {OwnerID: 1}, {OwnerID: 2}, {Keyword: "alice"}, {Keyword: "bob"}, {CallSign: "TEST"}, {GroupID: &group}, {IsOnline: &online}}
	keys := map[string]bool{}
	for _, filter := range filters {
		key := filteredDeviceListKey(filter, 1, 10)
		if keys[key] {
			t.Fatalf("filter collided: %+v", filter)
		}
		keys[key] = true
	}
	for _, key := range []string{filteredDeviceListKey(filters[0], 2, 10), filteredDeviceListKey(filters[0], 1, 20)} {
		if keys[key] {
			t.Fatal("pagination collided")
		}
	}
}
