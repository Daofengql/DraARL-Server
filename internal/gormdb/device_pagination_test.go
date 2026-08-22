package gormdb

import "testing"

func TestNormalizeDevicePageOffsetDefaultsAndPreservesLimit(t *testing.T) {
	limit, page, offset, err := NormalizeDevicePageOffset(1, 1)
	if err != nil || limit != 1 || page != 1 || offset != 0 {
		t.Fatalf("single-item first page=(%d,%d,%d) err=%v", limit, page, offset, err)
	}

	limit, page, offset, err = NormalizeDevicePageOffset(1000, 3)
	if err != nil || limit != 1000 || page != 3 || offset != 2000 {
		t.Fatalf("normalized=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
	limit, page, offset, err = NormalizeDevicePageOffset(0, 0)
	if err != nil || limit != 20 || page != 1 || offset != 0 {
		t.Fatalf("defaults=(%d,%d,%d) err=%v", limit, page, offset, err)
	}
}

func TestNormalizeDevicePageOffsetRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, _, _, err := NormalizeDevicePageOffset(100, maxInt); err == nil {
		t.Fatal("expected oversized device page to be rejected")
	}
}
