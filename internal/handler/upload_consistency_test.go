package handler

import (
	"context"
	"errors"
	"testing"
	"time"
)

type avatarUpdaterStub struct {
	err error
}

func (s avatarUpdaterStub) UpdateUserAvatar(int, string) error {
	return s.err
}

func TestPersistAvatarReferenceRollsBackWithIndependentContext(t *testing.T) {
	updateErr := errors.New("database unavailable")
	called := false
	err := persistAvatarReference(avatarUpdaterStub{err: updateErr}, 7, "2026/08/avatar.jpg", "uploads/avatar/2026/08/avatar.jpg", func(ctx context.Context, objectName string) error {
		called = true
		if ctx.Err() != nil {
			t.Fatalf("rollback context already canceled: %v", ctx.Err())
		}
		if objectName != "uploads/avatar/2026/08/avatar.jpg" {
			t.Fatalf("rollback object=%q", objectName)
		}
		return nil
	})
	if !errors.Is(err, updateErr) {
		t.Fatalf("error=%v, want update error", err)
	}
	if !called {
		t.Fatal("uploaded avatar was not rolled back")
	}
}

func TestPersistAvatarReferenceDoesNotDeleteCommittedObject(t *testing.T) {
	err := persistAvatarReference(avatarUpdaterStub{}, 7, "2026/08/avatar.jpg", "uploads/avatar/2026/08/avatar.jpg", func(context.Context, string) error {
		t.Fatal("delete called after successful database update")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteStoredObjectUsesIndependentTimeoutContext(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	if requestCtx.Err() == nil {
		t.Fatal("request context should be canceled")
	}

	called := false
	err := deleteStoredObjectWithIndependentContext("uploads/assets/new.bin", func(ctx context.Context, objectName string) error {
		called = true
		if ctx.Err() != nil {
			t.Fatalf("cleanup context inherited cancellation: %v", ctx.Err())
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("cleanup context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > storedObjectCleanupTimeout {
			t.Fatalf("cleanup deadline remaining=%v", remaining)
		}
		if objectName != "uploads/assets/new.bin" {
			t.Fatalf("objectName=%q", objectName)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("cleanup was not called")
	}
}

func TestDeleteStoredObjectReturnsDeleteError(t *testing.T) {
	want := errors.New("storage unavailable")
	err := deleteStoredObjectWithIndependentContext("uploads/operator_cert/new.jpg", func(context.Context, string) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error=%v, want %v", err, want)
	}
}
