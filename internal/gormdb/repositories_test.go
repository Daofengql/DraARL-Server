package gormdb

import (
	"strings"
	"testing"
)

func TestGroupKeywordConditionKeepsNumericSearchSargable(t *testing.T) {
	condition, args := groupKeywordCondition("123", "%123%")
	if !strings.Contains(condition, "id = ?") || strings.Contains(condition, "CAST(id AS CHAR)") {
		t.Fatalf("numeric condition is not sargable: %q", condition)
	}
	if len(args) != 2 || args[0] != uint(123) || args[1] != "%123%" {
		t.Fatalf("numeric condition args=%#v", args)
	}

	condition, args = groupKeywordCondition("radio", "%radio%")
	if !strings.Contains(condition, "CAST(id AS CHAR) LIKE ?") || len(args) != 2 {
		t.Fatalf("text condition=%q args=%#v", condition, args)
	}
}
