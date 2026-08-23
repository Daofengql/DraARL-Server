package handler

import (
	"testing"

	gormdb "draarl/internal/gormdb"
)

func TestUserAdminPaginationRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, _, _, err := gormdb.NormalizeUserPagination(100, maxInt); err == nil {
		t.Fatal("expected user admin page overflow to be rejected")
	}
}
