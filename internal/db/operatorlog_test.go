package db

import "testing"

func TestNormalizeOperatorLogPaginationDefaultsAndCaps(t *testing.T) {
	limit, page, offset, err := normalizeOperatorLogPagination(0, 0)
	if err != nil || limit != defaultOperatorLogPageSize || page != 1 || offset != 0 {
		t.Fatalf("defaults=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
	limit, page, offset, err = normalizeOperatorLogPagination(1000, 3)
	if err != nil || limit != maxOperatorLogPageSize || page != 3 || offset != 2*maxOperatorLogPageSize {
		t.Fatalf("caps=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
}

func TestNormalizeOperatorLogPaginationRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, _, _, err := normalizeOperatorLogPagination(maxOperatorLogPageSize, maxInt); err == nil {
		t.Fatal("expected oversized operator log page to be rejected")
	}
}
