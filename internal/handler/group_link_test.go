package handler

import (
	"testing"

	"draarl/internal/gormdb"
)

func TestCanUseGroupAsLinkTarget(t *testing.T) {
	tests := []struct {
		name  string
		group *gormdb.Group
		want  bool
	}{
		{name: "enabled public", group: &gormdb.Group{Type: groupTypePublic, Status: 1}, want: true},
		{name: "enabled private", group: &gormdb.Group{Type: groupTypePrivate, Status: 1}, want: true},
		{name: "disabled", group: &gormdb.Group{Type: groupTypePublic, Status: 0}},
		{name: "virtual", group: &gormdb.Group{Type: groupTypePublic, Status: 1, IsVirtual: true}},
		{name: "unsupported type", group: &gormdb.Group{Type: 99, Status: 1}},
		{name: "missing group", group: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canUseGroupAsLinkTarget(tt.group); got != tt.want {
				t.Fatalf("canUseGroupAsLinkTarget() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterAvailableGroupLinkTargets(t *testing.T) {
	groups := []*gormdb.Group{
		{ID: 1, Type: groupTypePublic, Status: 1},
		{ID: 2, Type: groupTypePrivate, Status: 1},
		{ID: 3, Type: groupTypePublic, Status: 0},
		{ID: 4, Type: groupTypePublic, Status: 1, IsVirtual: true},
	}

	got := filterAvailableGroupLinkTargets(groups, map[int]struct{}{2: {}})
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("available targets = %#v, want only group 1", got)
	}
}
