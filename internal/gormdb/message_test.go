package gormdb

import (
	"testing"
	"time"
)

func TestMergeMessagePagesOrdersByCursorAndBoundsResults(t *testing.T) {
	base := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	pages := [][]MessageRecord{
		{
			{ID: 9, SourceGroupID: 1, StartTime: base.Add(2 * time.Second)},
			{ID: 5, SourceGroupID: 1, StartTime: base},
		},
		{
			{ID: 8, SourceGroupID: 2, StartTime: base.Add(2 * time.Second)},
			{ID: 7, SourceGroupID: 2, StartTime: base.Add(time.Second)},
		},
	}
	records, hasMore, err := mergeMessagePages(pages, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore || len(records) != 3 {
		t.Fatalf("merged len=%d has_more=%v", len(records), hasMore)
	}
	want := []uint{9, 8, 7}
	for i := range want {
		if records[i].ID != want[i] {
			t.Fatalf("merged IDs=%v, want=%v", []uint{records[0].ID, records[1].ID, records[2].ID}, want)
		}
	}
}

func TestUniquePositiveGroupIDsPreservesFirstOccurrence(t *testing.T) {
	got := uniquePositiveGroupIDs([]int{3, 0, 2, 3, -1, 2, 4})
	want := []int{3, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("group IDs=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("group IDs=%v want=%v", got, want)
		}
	}
}
