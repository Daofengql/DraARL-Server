package gormdb

import "testing"

func TestSchemaIsEmptyRequiresZeroTables(t *testing.T) {
	if !schemaIsEmpty(nil) || !schemaIsEmpty([]string{}) {
		t.Fatal("zero-table schema was not considered empty")
	}
	for _, tables := range [][]string{{"users"}, {"schema_migrations"}, {"unrelated_table", "users"}} {
		if schemaIsEmpty(tables) {
			t.Fatalf("non-empty schema was considered empty: %v", tables)
		}
	}
}
