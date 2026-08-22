package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gormdb "draarl/internal/gormdb"
)

func newTestUserCache(t *testing.T) *UserCache {
	t.Helper()
	cache, err := NewTwoLevelCache(CacheConfig{LocalTTL: time.Minute, MaxSize: 128})
	if err != nil {
		t.Fatal(err)
	}
	return newUserCache(cache)
}

func TestUserCacheCoalescesConcurrentNegativeNameLoads(t *testing.T) {
	userCache := newTestUserCache(t)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	userCache.loadByName = func(context.Context, string) (*gormdb.User, error) {
		calls.Add(1)
		startOnce.Do(func() { close(started) })
		<-release
		return nil, nil
	}

	const callers = 32
	results := make(chan error, callers)
	for range callers {
		go func() {
			user, err := userCache.GetUserByName(context.Background(), "missing-user")
			if err == nil && user != nil {
				err = errors.New("missing user unexpectedly resolved")
			}
			results <- err
		}()
	}
	<-started
	close(release)
	for range callers {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("database loader calls = %d, want 1", got)
	}

	if user, err := userCache.GetUserByName(context.Background(), "missing-user"); err != nil || user != nil {
		t.Fatalf("negative cache result = %#v, %v", user, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("negative cache reloaded database: calls=%d", got)
	}
}

func TestUserCacheInvalidationClearsNegativeAndPopulatesBothIndexes(t *testing.T) {
	userCache := newTestUserCache(t)
	var nameCalls atomic.Int32
	userCache.loadByName = func(context.Context, string) (*gormdb.User, error) {
		if nameCalls.Add(1) == 1 {
			return nil, nil
		}
		return &gormdb.User{ID: 7, Name: "new-user", CallSign: "BG7NEW"}, nil
	}
	var idCalls atomic.Int32
	userCache.loadByID = func(context.Context, int) (*gormdb.User, error) {
		idCalls.Add(1)
		return nil, errors.New("ID loader should not run after name cache population")
	}

	if user, err := userCache.GetUserByName(context.Background(), "new-user"); err != nil || user != nil {
		t.Fatalf("initial negative lookup = %#v, %v", user, err)
	}
	if err := userCache.InvalidateUser(context.Background(), 7, "new-user"); err != nil {
		t.Fatal(err)
	}
	user, err := userCache.GetUserByName(context.Background(), "new-user")
	if err != nil || user == nil || user.ID != 7 {
		t.Fatalf("lookup after invalidation = %#v, %v", user, err)
	}
	byID, err := userCache.GetUserByID(context.Background(), 7)
	if err != nil || byID == nil || byID.Name != "new-user" {
		t.Fatalf("ID index lookup = %#v, %v", byID, err)
	}
	if got := nameCalls.Load(); got != 2 {
		t.Fatalf("name loader calls = %d, want 2", got)
	}
	if got := idCalls.Load(); got != 0 {
		t.Fatalf("ID loader calls = %d, want 0", got)
	}
}

func TestUserCacheCoalescedWaiterHonorsContext(t *testing.T) {
	userCache := newTestUserCache(t)
	started := make(chan struct{})
	release := make(chan struct{})
	userCache.loadByName = func(context.Context, string) (*gormdb.User, error) {
		close(started)
		<-release
		return &gormdb.User{ID: 9, Name: "slow-user"}, nil
	}

	leaderDone := make(chan error, 1)
	go func() {
		_, err := userCache.GetUserByName(context.Background(), "slow-user")
		leaderDone <- err
	}()
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := userCache.GetUserByName(ctx, "slow-user"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("coalesced waiter error = %v, want deadline exceeded", err)
	}
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatal(err)
	}
}

func TestUserCacheLeaderCancellationDoesNotPoisonWaiter(t *testing.T) {
	userCache := newTestUserCache(t)
	started := make(chan struct{})
	release := make(chan struct{})
	userCache.loadByName = func(ctx context.Context, _ string) (*gormdb.User, error) {
		close(started)
		select {
		case <-release:
			return &gormdb.User{ID: 12, Name: "cancelled-leader"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := userCache.GetUserByName(leaderCtx, "cancelled-leader")
		leaderDone <- err
	}()
	<-started
	cancelLeader()

	waiterDone := make(chan error, 1)
	go func() {
		user, err := userCache.GetUserByName(context.Background(), "cancelled-leader")
		if err == nil && (user == nil || user.ID != 12) {
			err = errors.New("waiter received invalid user")
		}
		waiterDone <- err
	}()
	close(release)

	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader wait error = %v, want context.Canceled", err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatalf("waiter was poisoned by leader cancellation: %v", err)
	}
}

func TestUserCacheCoalescedResultsAreIndependent(t *testing.T) {
	userCache := newTestUserCache(t)
	started := make(chan struct{})
	release := make(chan struct{})
	userCache.loadByName = func(context.Context, string) (*gormdb.User, error) {
		close(started)
		<-release
		reviewerID := 3
		return &gormdb.User{ID: 11, Name: "shared-user", ReviewerID: &reviewerID}, nil
	}

	firstResult := make(chan *gormdb.User, 1)
	secondResult := make(chan *gormdb.User, 1)
	go func() {
		user, _ := userCache.GetUserByName(context.Background(), "shared-user")
		firstResult <- user
	}()
	<-started
	go func() {
		user, _ := userCache.GetUserByName(context.Background(), "shared-user")
		secondResult <- user
	}()
	close(release)
	first, second := <-firstResult, <-secondResult
	if first == nil || second == nil || first == second || first.ReviewerID == second.ReviewerID {
		t.Fatalf("coalesced callers shared mutable user state: first=%p second=%p", first, second)
	}
}
