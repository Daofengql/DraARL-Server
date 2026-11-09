package gormdb

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestRepositoryContextCancelsQueriesWithoutMutatingOriginal(t *testing.T) {
	db, _ := deviceGroupDryRunDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sessions := []*gorm.DB{
		(&DeviceRepository{db: db}).WithContext(ctx).db,
		(&UserRepository{db: db}).WithContext(ctx).db,
		(&GroupRepository{db: db}).WithContext(ctx).db,
		(&GroupMemberRepository{db: db}).WithContext(ctx).db,
		(&GroupLinkRepository{db: db}).WithContext(ctx).db,
	}
	for _, session := range sessions {
		if !errors.Is(session.Statement.Context.Err(), context.Canceled) {
			t.Fatal("repository lost request cancellation")
		}
	}
	if db.Statement.Context.Err() != nil {
		t.Fatal("scoping a repository canceled the original database session")
	}
}

func TestUserRepositoryConvenienceMethodsPreserveRequestContext(t *testing.T) {
	db, _ := deviceGroupDryRunDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	queries := 0
	capture := func(tx *gorm.DB) {
		queries++
		if tx.Statement.Context != ctx {
			t.Error("user repository replaced the caller's context")
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register("test:request_context", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("test:request_context", capture); err != nil {
		t.Fatal(err)
	}
	repo := (&UserRepository{db: db}).WithContext(ctx)
	if _, err := repo.GetUserByID(7); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetUserByName("request-context-test"); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateUserDevicePassword(7, "request-context-test"); err != nil {
		t.Fatal(err)
	}
	if queries != 3 {
		t.Fatalf("captured %d database operations, want 3", queries)
	}
}
