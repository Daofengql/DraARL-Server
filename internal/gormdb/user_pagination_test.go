package gormdb

import "testing"

func TestNormalizeUserPaginationDefaultsAndCaps(t *testing.T) {
	limit, page, offset, err := NormalizeUserPagination(0, 0)
	if err != nil || limit != 20 || page != 1 || offset != 0 {
		t.Fatalf("defaults=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
	limit, page, offset, err = NormalizeUserPagination(1000, 3)
	if err != nil || limit != maxUserPageSize || page != 3 || offset != 2*maxUserPageSize {
		t.Fatalf("caps=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
}

func TestNormalizeUserPaginationRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, _, _, err := NormalizeUserPagination(maxUserPageSize, maxInt); err == nil {
		t.Fatal("expected oversized user page to be rejected")
	}
}
