package handler

import "testing"

func TestNormalizeGroupPaginationDefaultsAndCaps(t *testing.T) {
	page, pageSize, offset, err := normalizeGroupPagination(0, 0)
	if err != nil || page != 1 || pageSize != 20 || offset != 0 {
		t.Fatalf("defaults=(%d,%d,%d) err=%v", page, pageSize, offset, err)
	}
	page, pageSize, offset, err = normalizeGroupPagination(3, 1000)
	if err != nil || page != 3 || pageSize != maxGroupPageSize || offset != 2*maxGroupPageSize {
		t.Fatalf("capped=(%d,%d,%d) err=%v", page, pageSize, offset, err)
	}
}

func TestNormalizeGroupPaginationRejectsOffsetOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	page, pageSize, offset, err := normalizeGroupPagination(maxInt, maxGroupPageSize)
	if err == nil {
		t.Fatalf("overflow page accepted: page=%d page_size=%d offset=%d", page, pageSize, offset)
	}
}

func TestNormalizeGroupPaginationAllowsLargestRepresentableOffset(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	pageSize := maxGroupPageSize
	page := maxInt/pageSize + 1
	gotPage, gotSize, offset, err := normalizeGroupPagination(page, pageSize)
	if err != nil || gotPage != page || gotSize != pageSize || offset < 0 {
		t.Fatalf("largest representable page=(%d,%d,%d) err=%v", gotPage, gotSize, offset, err)
	}
}
