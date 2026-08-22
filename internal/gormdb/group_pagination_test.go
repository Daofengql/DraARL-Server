package gormdb

import "testing"

func TestNormalizePageOffsetDefaultsAndPreservesLimit(t *testing.T) {
	limit, page, offset, err := NormalizePageOffset(1000, 3)
	if err != nil || limit != 1000 || page != 3 || offset != 2000 {
		t.Fatalf("normalized=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
	limit, page, offset, err = NormalizePageOffset(0, 0)
	if err != nil || limit != 20 || page != 1 || offset != 0 {
		t.Fatalf("defaults=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
}

func TestNormalizePageOffsetRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, _, _, err := NormalizePageOffset(100, maxInt); err == nil {
		t.Fatal("expected oversized group page to be rejected")
	}
}
