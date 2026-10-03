package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/config"
)

type recoveryTestRepository struct {
	listFn func(context.Context, uint, int) ([]model.BroadcastAudio, error)
	status string
}

func (r *recoveryTestRepository) ListProcessingAudiosAfter(ctx context.Context, afterID uint, limit int) ([]model.BroadcastAudio, error) {
	return r.listFn(ctx, afterID, limit)
}

func (r *recoveryTestRepository) GetAudioByID(context.Context, uint) (*model.BroadcastAudio, error) {
	status := r.status
	if status == "" {
		status = model.AudioStatusReady
	}
	return &model.BroadcastAudio{Status: status}, nil
}

func (r *recoveryTestRepository) MarkAudioFailed(context.Context, uint, string) error { return nil }

func (r *recoveryTestRepository) MarkAudioReady(context.Context, uint, string, int64, string, int64, int, int) error {
	return nil
}

func newRecoveryTestProcessor(repo processorRepository) *Processor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Processor{
		// These recovery tests only need existing executables for Start's path
		// validation. Use the test binary itself so they work on Windows too.
		config: config.BroadcastConfig{FFmpegPath: recoveryTestExecutable(), FFprobePath: recoveryTestExecutable(), TranscodeWorkers: 1},
		repo:   repo, jobs: make(chan uint, 1), ctx: ctx, cancel: cancel,
	}
}

func recoveryTestExecutable() string {
	path, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return path
}

func waitForRecovery(t *testing.T, processor *Processor, want uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if processor.Metrics().RecoveryScanned >= want && !processor.Metrics().RecoveryRunning {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("recovery did not finish: %#v", processor.Metrics())
}

func TestEnqueueBlockingWaitsForCapacityAndHonorsShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processor := &Processor{jobs: make(chan uint, 1), ctx: ctx, cancel: cancel}
	processor.jobs <- 1

	done := make(chan error, 1)
	go func() { done <- processor.enqueueBlocking(2) }()
	select {
	case err := <-done:
		t.Fatalf("enqueue returned before capacity was available: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	<-processor.jobs
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("enqueue after capacity: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("enqueue did not resume after capacity was available")
	}

	<-processor.jobs
	processor.jobs <- 3
	done = make(chan error, 1)
	go func() { done <- processor.enqueueBlocking(4) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("enqueue shutdown error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("enqueue did not stop after shutdown")
	}
}

func TestEnqueueIsIdempotentAndReleasesReservationOnQueueFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processor := &Processor{jobs: make(chan uint, 1), ctx: ctx, cancel: cancel}

	if err := processor.Enqueue(11); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if err := processor.Enqueue(11); err != nil {
		t.Fatalf("duplicate enqueue should be idempotent: %v", err)
	}
	if got := len(processor.jobs); got != 1 {
		t.Fatalf("duplicate enqueue changed queue depth to %d", got)
	}
	if err := processor.Enqueue(12); err == nil {
		t.Fatal("queue-full enqueue unexpectedly succeeded")
	}
	<-processor.jobs
	if err := processor.Enqueue(12); err != nil {
		t.Fatalf("reservation was not released after queue-full failure: %v", err)
	}
}

func TestConcurrentDuplicateEnqueueAdmitsOneJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processor := &Processor{jobs: make(chan uint, 1), ctx: ctx, cancel: cancel}
	const callers = 64
	results := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- processor.Enqueue(21)
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent duplicate enqueue: %v", err)
		}
	}
	if got := len(processor.jobs); got != 1 {
		t.Fatalf("concurrent duplicate enqueue admitted %d jobs, want 1", got)
	}
}

func TestWorkerReleasesAudioReservationAfterProcessing(t *testing.T) {
	repo := &recoveryTestRepository{status: model.AudioStatusProcessing}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processor := &Processor{
		config: config.BroadcastConfig{TranscodeTimeoutSeconds: 1},
		repo:   repo, jobs: make(chan uint, 1), ctx: ctx, cancel: cancel,
	}
	if !processor.reserveAudioJob(31) {
		t.Fatal("initial reservation failed")
	}
	processor.processQueuedAudio(31)
	if !processor.reserveAudioJob(31) {
		t.Fatal("reservation was not released after worker processing")
	}
	processor.releaseAudioJob(31)
}

func TestProcessorStartReturnsBeforeRecoveryQueryCompletes(t *testing.T) {
	queryStarted := make(chan struct{})
	repo := &recoveryTestRepository{listFn: func(ctx context.Context, _ uint, _ int) ([]model.BroadcastAudio, error) {
		select {
		case <-queryStarted:
		default:
			close(queryStarted)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	processor := newRecoveryTestProcessor(repo)
	startedAt := time.Now()
	if err := processor.Start(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(startedAt); elapsed > 200*time.Millisecond {
		t.Fatalf("Start waited for recovery query: %s", elapsed)
	}
	select {
	case <-queryStarted:
	case <-time.After(time.Second):
		t.Fatal("recovery query did not start in background")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := processor.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func TestProcessorRecoveryUsesKeysetPagesBeyondThousandLimit(t *testing.T) {
	const total = 1205
	rows := make([]model.BroadcastAudio, total)
	for i := range rows {
		rows[i] = model.BroadcastAudio{ID: uint(i + 1), Status: model.AudioStatusProcessing}
	}
	var mu sync.Mutex
	var cursors []uint
	repo := &recoveryTestRepository{listFn: func(_ context.Context, afterID uint, limit int) ([]model.BroadcastAudio, error) {
		mu.Lock()
		cursors = append(cursors, afterID)
		mu.Unlock()
		start := int(afterID)
		if start >= len(rows) {
			return nil, nil
		}
		end := start + limit
		if end > len(rows) {
			end = len(rows)
		}
		return rows[start:end], nil
	}}
	processor := newRecoveryTestProcessor(repo)
	if err := processor.Start(); err != nil {
		t.Fatal(err)
	}
	waitForRecovery(t, processor, total)
	if got := processor.Metrics().RecoveryLastID; got != total {
		t.Fatalf("recovery last id=%d, want %d", got, total)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cursors) < 3 || cursors[0] != 0 || cursors[1] != 100 || cursors[2] != 200 {
		t.Fatalf("keyset cursors=%v, want pages starting at 0,100,200", cursors)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := processor.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func TestProcessorRecoveryRetriesQueryErrorsAndExposesMetrics(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	repo := &recoveryTestRepository{listFn: func(_ context.Context, afterID uint, _ int) ([]model.BroadcastAudio, error) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts <= 2 {
			return nil, fmt.Errorf("temporary recovery failure %d", attempts)
		}
		if afterID == 0 {
			return []model.BroadcastAudio{{ID: 7, Status: model.AudioStatusProcessing}}, nil
		}
		return nil, nil
	}}
	processor := newRecoveryTestProcessor(repo)
	if err := processor.Start(); err != nil {
		t.Fatal(err)
	}
	waitForRecovery(t, processor, 1)
	snapshot := processor.Metrics()
	if snapshot.RecoveryErrors != 2 || snapshot.RecoveryLastID != 7 {
		t.Fatalf("recovery metrics=%#v, want 2 errors and last id 7", snapshot)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := processor.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}
