package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	gormdb "draarl/internal/gormdb"
)

func testGroupCache(t *testing.T) *GroupCache {
	t.Helper()
	groupCache, err := NewGroupCache(GroupCacheConfig{LocalTTL: time.Minute, MaxSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	return groupCache
}

func TestGroupListCacheUsesDatabasePaginationOnMiss(t *testing.T) {
	groupCache := testGroupCache(t)
	fullCalls, pageCalls, countCalls := 0, 0, 0
	groupCache.listGroupsPaginated = func(limit, page int) ([]*gormdb.Group, int64, error) {
		fullCalls++
		if limit != 2 || page != 3 {
			t.Fatalf("unexpected pagination arguments: limit=%d page=%d", limit, page)
		}
		return []*gormdb.Group{{ID: 5}, {ID: 4}}, 5, nil
	}
	groupCache.listGroupsPage = func(int, int) ([]*gormdb.Group, error) {
		pageCalls++
		return nil, errors.New("page-only loader should not run on complete miss")
	}
	groupCache.countGroups = func() (int64, error) {
		countCalls++
		return 0, errors.New("count loader should not run on complete miss")
	}

	groups, total, err := groupCache.GetGroupList(context.Background(), 3, 2)
	if err != nil || len(groups) != 2 || total != 5 {
		t.Fatalf("first list = groups=%#v total=%d err=%v", groups, total, err)
	}
	if fullCalls != 1 || pageCalls != 0 || countCalls != 0 {
		t.Fatalf("unexpected loader calls: full=%d page=%d count=%d", fullCalls, pageCalls, countCalls)
	}

	if _, _, err := groupCache.GetGroupList(context.Background(), 3, 2); err != nil {
		t.Fatal(err)
	}
	if fullCalls != 1 || pageCalls != 0 || countCalls != 0 {
		t.Fatalf("cached list reloaded: full=%d page=%d count=%d", fullCalls, pageCalls, countCalls)
	}
}

func TestGroupListCachePartialHitsQueryOnlyMissingView(t *testing.T) {
	groupCache := testGroupCache(t)
	ctx := context.Background()
	groupCache.listGroupsPaginated = func(int, int) ([]*gormdb.Group, int64, error) {
		return nil, 0, errors.New("full loader should not run for partial hit")
	}
	pageCalls, countCalls := 0, 0
	groupCache.listGroupsPage = func(limit, page int) ([]*gormdb.Group, error) {
		pageCalls++
		if limit != 3 || page != 2 {
			t.Fatalf("unexpected page arguments: limit=%d page=%d", limit, page)
		}
		return []*gormdb.Group{{ID: 8}}, nil
	}
	groupCache.countGroups = func() (int64, error) {
		countCalls++
		return 9, nil
	}

	// Total hit: only the page query is needed.
	if err := groupCache.cache.Set(ctx, groupListTotalKey(), int64(9), time.Minute); err != nil {
		t.Fatal(err)
	}
	groups, total, err := groupCache.GetGroupList(ctx, 2, 3)
	if err != nil || len(groups) != 1 || total != 9 {
		t.Fatalf("total-hit list = groups=%#v total=%d err=%v", groups, total, err)
	}
	if pageCalls != 1 || countCalls != 0 {
		t.Fatalf("total-hit loaders: page=%d count=%d", pageCalls, countCalls)
	}

	// Item hit with a different page size/key and missing total: only Count is needed.
	if err := groupCache.cache.Delete(ctx, groupListTotalKey()); err != nil {
		t.Fatal(err)
	}
	if err := groupCache.cache.Set(ctx, groupListKey(4, 3), []*gormdb.Group{{ID: 12}}, time.Minute); err != nil {
		t.Fatal(err)
	}
	groups, total, err = groupCache.GetGroupList(ctx, 4, 3)
	if err != nil || len(groups) != 1 || total != 9 {
		t.Fatalf("item-hit list = groups=%#v total=%d err=%v", groups, total, err)
	}
	if pageCalls != 1 || countCalls != 1 {
		t.Fatalf("item-hit loaders: page=%d count=%d", pageCalls, countCalls)
	}
}

func TestGroupListCacheNormalizesInvalidPageArguments(t *testing.T) {
	groupCache := testGroupCache(t)
	groupCache.listGroupsPaginated = func(limit, page int) ([]*gormdb.Group, int64, error) {
		if limit != 20 || page != 1 {
			t.Fatalf("arguments were not normalized: limit=%d page=%d", limit, page)
		}
		return nil, 0, nil
	}
	groupCache.listGroupsPage = func(int, int) ([]*gormdb.Group, error) {
		return nil, errors.New("unexpected page loader")
	}
	groupCache.countGroups = func() (int64, error) {
		return 0, errors.New("unexpected count loader")
	}
	if _, _, err := groupCache.GetGroupList(context.Background(), 0, 0); err != nil {
		t.Fatal(err)
	}
}
