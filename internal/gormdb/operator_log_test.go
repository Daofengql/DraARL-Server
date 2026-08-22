package gormdb

import "testing"

func TestNormalizeOperatorLogPaginationDefaultsAndCaps(t *testing.T) {
	limit, page, offset, err := NormalizeOperatorLogPagination(0, 0)
	if err != nil || limit != 20 || page != 1 || offset != 0 {
		t.Fatalf("defaults=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
	limit, page, offset, err = NormalizeOperatorLogPagination(1000, 3)
	if err != nil || limit != maxOperatorLogPageSize || page != 3 || offset != 2*maxOperatorLogPageSize {
		t.Fatalf("caps=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
}

func TestNormalizeOperatorLogPaginationRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, _, _, err := NormalizeOperatorLogPagination(maxOperatorLogPageSize, maxInt); err == nil {
		t.Fatal("overflowing operator log page was accepted")
	}
}

func TestNormalizeOperatorLogPaginationAllowsLargestRepresentableOffset(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	page := maxInt/maxOperatorLogPageSize + 1
	limit, gotPage, offset, err := NormalizeOperatorLogPagination(maxOperatorLogPageSize, page)
	if err != nil || limit != maxOperatorLogPageSize || gotPage != page || offset < 0 {
		t.Fatalf("largest representable=(%d,%d,%d) err=%v", limit, gotPage, offset, err)
	}
}
