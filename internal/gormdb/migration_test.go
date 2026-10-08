package gormdb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestIsBcryptPasswordHash(t *testing.T) {
	validHash, err := bcrypt.GenerateFromPassword([]byte("private-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]bool{
		string(validHash):                   true,
		"$2b$10$malformed":                  false,
		"$2b$-prefixed-legacy-plaintext":    false,
		"$2b$10$" + strings.Repeat("!", 53): false,
		"legacy-plaintext":                  false,
		"":                                  false,
	}
	for value, want := range tests {
		if got := isBcryptPasswordHash(value); got != want {
			t.Fatalf("isBcryptPasswordHash(%q)=%t, want %t", value, got, want)
		}
	}
}

func TestRetryMigrationVersionRecordHandlesTransientAndAmbiguousWrites(t *testing.T) {
	t.Run("transient write failure", func(t *testing.T) {
		writes := 0
		verifies := 0
		err := retryMigrationVersionRecord(context.Background(), 3, time.Millisecond, func() error {
			writes++
			if writes == 1 {
				return errors.New("temporary connection failure")
			}
			return nil
		}, func() (bool, error) {
			verifies++
			return writes >= 2, nil
		}, nil)
		if err != nil || writes != 2 || verifies != 2 {
			t.Fatalf("err=%v writes=%d verifies=%d", err, writes, verifies)
		}
	})

	t.Run("commit result lost", func(t *testing.T) {
		writes := 0
		err := retryMigrationVersionRecord(context.Background(), 3, time.Millisecond, func() error {
			writes++
			return errors.New("connection lost after commit")
		}, func() (bool, error) {
			return true, nil
		}, nil)
		if err != nil || writes != 1 {
			t.Fatalf("err=%v writes=%d", err, writes)
		}
	})
}

func TestRetryMigrationVersionRecordHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writes := 0
	err := retryMigrationVersionRecord(ctx, 3, time.Hour, func() error {
		writes++
		return errors.New("unavailable")
	}, func() (bool, error) {
		return false, nil
	}, nil)
	if !errors.Is(err, context.Canceled) || writes != 1 {
		t.Fatalf("err=%v writes=%d", err, writes)
	}
}

func TestHashLegacyPrivateGroupPasswordKeepsTooLongValuesCompatible(t *testing.T) {
	hashed, migratable, err := hashLegacyPrivateGroupPassword("private-password")
	if err != nil || !migratable || !isBcryptPasswordHash(hashed) {
		t.Fatalf("normal password migration: hash=%q migratable=%t err=%v", hashed, migratable, err)
	}

	tooLong := strings.Repeat("x", 73)
	hashed, migratable, err = hashLegacyPrivateGroupPassword(tooLong)
	if err != nil || migratable || hashed != "" {
		t.Fatalf("too-long password migration: hash=%q migratable=%t err=%v", hashed, migratable, err)
	}
}

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

func TestValidateAppliedMigrationVersionsRequiresContiguousHistory(t *testing.T) {
	tests := []struct {
		name     string
		versions []int
		want     int
		wantErr  bool
	}{
		{name: "empty", versions: nil, want: 0},
		{name: "first", versions: []int{1}, want: 1},
		{name: "complete", versions: []int{1, 2}, want: 2},
		{name: "out of order", versions: []int{2, 1}, want: 2},
		{name: "gap", versions: []int{1, 3}, wantErr: true},
		{name: "starts too high", versions: []int{2}, wantErr: true},
		{name: "current", versions: []int{1, 2, 3, 4}, want: 4},
		{name: "future", versions: []int{1, 2, 3, 4, 5}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateAppliedMigrationVersions(test.versions)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%t", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("version=%d want=%d", got, test.want)
			}
		})
	}
}

func TestApplicationSchemaIsEmptyIgnoresMigrationLedgerOnly(t *testing.T) {
	for _, test := range []struct {
		name   string
		tables []string
		want   bool
	}{
		{name: "fresh ledger", tables: []string{"schema_migrations"}, want: true},
		{name: "fresh ledger mixed case", tables: []string{"SCHEMA_MIGRATIONS"}, want: true},
		{name: "application table", tables: []string{"schema_migrations", "users"}, want: false},
		{name: "empty", tables: nil, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := applicationSchemaIsEmpty(test.tables); got != test.want {
				t.Fatalf("applicationSchemaIsEmpty(%v)=%t, want %t", test.tables, got, test.want)
			}
		})
	}
}
