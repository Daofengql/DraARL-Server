package gormdb

import "testing"

func TestNormalizeOperatorCertPaginationDefaultsAndCaps(t *testing.T) {
	limit, offset, err := NormalizeOperatorCertPagination(0, 0)
	if err != nil || limit != defaultOperatorCertPageSize || offset != 0 {
		t.Fatalf("defaults=(%d,%d) err=%v", limit, offset, err)
	}
	limit, page, offset, err := NormalizeOperatorCertPage(1000, 3)
	if err != nil || limit != maxOperatorCertPageSize || page != 3 || offset != 2*maxOperatorCertPageSize {
		t.Fatalf("page=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
}

func TestNormalizeOperatorCertPaginationRejectsInvalidOffsetAndOverflow(t *testing.T) {
	if _, _, err := NormalizeOperatorCertPagination(20, -1); err == nil {
		t.Fatal("negative operator certificate offset was accepted")
	}
	maxInt := int(^uint(0) >> 1)
	if _, _, _, err := NormalizeOperatorCertPage(maxOperatorCertPageSize, maxInt); err == nil {
		t.Fatal("overflowing operator certificate page was accepted")
	}
}
