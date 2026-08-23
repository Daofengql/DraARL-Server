package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/config"
)

type sameSizeCorruptStorage struct{ Storage }

func (s sameSizeCorruptStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if len(data) > 0 {
		data[0] ^= 0xff
	}
	return s.Storage.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType)
}

type flakyPutStorage struct {
	Storage
	remaining atomic.Int32
}

type countingOpenStorage struct {
	Storage
	opens atomic.Int32
}

type flakyOpenStorage struct {
	Storage
	remaining atomic.Int32
}

func (s *flakyOpenStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	for {
		remaining := s.remaining.Load()
		if remaining <= 0 {
			return s.Storage.Open(ctx, key)
		}
		if s.remaining.CompareAndSwap(remaining, remaining-1) {
			return nil, errors.New("injected transient open failure")
		}
	}
}

func (s *countingOpenStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	s.opens.Add(1)
	return s.Storage.Open(ctx, key)
}

func TestMigrateWorkerCount(t *testing.T) {
	tests := []struct {
		configured int
		want       int
	}{
		{configured: 0, want: defaultMigrateWorkers},
		{configured: -1, want: 1},
		{configured: 1, want: 1},
		{configured: 8, want: 8},
		{configured: 99, want: maxMigrateWorkers},
	}
	for _, tt := range tests {
		if got := migrateWorkerCount(tt.configured); got != tt.want {
			t.Fatalf("workers(%d)=%d, want %d", tt.configured, got, tt.want)
		}
	}
}

func TestMigrateRateLimiterIsSharedAcrossWorkers(t *testing.T) {
	dir := t.TempDir()
	secret := "secret-key-0123456789abcdef0123456789abcdef"
	src, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "src")}, secret)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "dst")}, secret)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		key := filepath.ToSlash(filepath.Join("uploads", "rate", string(rune('a'+i))+".bin"))
		data := bytes.Repeat([]byte{byte(i)}, 64*1024)
		if err := src.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	res, err := migrateWith(context.Background(), src, dst, MigrateOptions{
		Workers:           4,
		MaxBytesPerSecond: 128 * 1024,
		RetryAttempts:     1,
		ProgressEvery:     1000,
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("rate-limited migration: %v", err)
	}
	if res.Copied != 4 || res.Failed != 0 {
		t.Fatalf("result=%+v, want copied=4 failed=0", res)
	}
	// 256 KiB at 128 KiB/s cannot complete immediately. The initial burst is
	// bounded, so this also detects accidentally creating one limiter per worker.
	if elapsed < 900*time.Millisecond {
		t.Fatalf("migration completed too quickly: %s", elapsed)
	}
}

func TestMigrateRateLimiterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	limiter := newMigrateRateLimiter(1024)
	if err := limiter.wait(ctx, 1024); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := limiter.wait(ctx, 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait after cancellation=%v, want context.Canceled", err)
	}
}

func (s *flakyPutStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	for {
		remaining := s.remaining.Load()
		if remaining <= 0 {
			return s.Storage.Put(ctx, key, r, size, contentType)
		}
		if s.remaining.CompareAndSwap(remaining, remaining-1) {
			return errors.New("injected transient destination failure")
		}
	}
}

func TestMigrateRetriesTransientObjectFailure(t *testing.T) {
	dir := t.TempDir()
	src, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "src")}, "secret-key-0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	dstBase, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "dst")}, "secret-key-0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	const key = "uploads/other/retry.bin"
	data := []byte("retryable object")
	if err := src.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	dst := &flakyPutStorage{Storage: dstBase}
	dst.remaining.Store(2)
	res, err := migrateWith(context.Background(), src, dst, MigrateOptions{
		RetryAttempts: 3,
		RetryDelay:    time.Millisecond,
	})
	if err != nil {
		t.Fatalf("transient failure should recover: %v", err)
	}
	if res.Copied != 1 || res.Failed != 0 {
		t.Fatalf("retry result=%+v, want copied=1 failed=0", res)
	}
	if size, _, err := dst.Stat(context.Background(), key); err != nil || size != int64(len(data)) {
		t.Fatalf("destination after retry=(%d,%v)", size, err)
	}
}

func TestMigrateRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	_, err := retryMigrateOperation(ctx, 5, time.Hour, func() (struct{}, error) {
		attempts++
		cancel()
		return struct{}{}, errors.New("injected transient failure")
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("canceled retry err=%v attempts=%d, want context.Canceled/1", err, attempts)
	}
}

// TestMigrateConcurrentLocalVerify 验证并发迁移（Workers>1）的正确性：
// 首次全部复制、重跑断点续传全部跳过、DeleteSource 时删除源端且不误删。
func TestMigrateConcurrentLocalVerify(t *testing.T) {
	dir, err := os.MkdirTemp("", "migrate-verify-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	srcRoot := filepath.Join(dir, "src")
	dstRoot := filepath.Join(dir, "dst")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	secret := "secret-key-0123456789abcdef0123456789abcdef"
	src, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: srcRoot}, secret)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: dstRoot}, secret)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	for i := 0; i < 50; i++ {
		key := "uploads/other/2026/08/" + string(rune(97+i%26)) + string(rune(48+i%10)) + ".bin"
		data := make([]byte, 1024)
		for j := range data {
			data[j] = byte(i)
		}
		if err := src.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			t.Fatalf("put src %d: %v", i, err)
		}
	}

	res, err := migrateWith(ctx, src, dst, MigrateOptions{Workers: 4})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res.Copied != 50 || res.Failed != 0 {
		t.Fatalf("copied=%d failed=%d want 50/0", res.Copied, res.Failed)
	}

	// 重跑（断点续传 + 删源）应全部跳过并删除源端
	res2, err := migrateWith(ctx, src, dst, MigrateOptions{Workers: 4, DeleteSource: true})
	if err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if res2.Skipped != 50 || res2.Deleted != 50 || res2.Failed != 0 {
		t.Fatalf("re-run skipped=%d deleted=%d failed=%d want 50/50/0", res2.Skipped, res2.Deleted, res2.Failed)
	}
}

func TestMigrateDeleteSourceVerifiesFreshCopyHash(t *testing.T) {
	dir := t.TempDir()
	src, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "src")}, "secret-key-0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	dstBase, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "dst")}, "secret-key-0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	const key = "uploads/other/2026/08/corrupt.bin"
	data := []byte("same-size source payload")
	if err := src.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}

	res, err := migrateWith(context.Background(), src, sameSizeCorruptStorage{Storage: dstBase}, MigrateOptions{DeleteSource: true})
	if err == nil || res.Failed != 1 || res.Deleted != 0 {
		t.Fatalf("corrupt fresh copy result=%+v err=%v, want one failed and source retained", res, err)
	}
	if _, _, err := src.Stat(context.Background(), key); err != nil {
		t.Fatalf("source object was deleted after hash mismatch: %v", err)
	}
}

func TestMigrateRepairsSameSizeCorruptDestination(t *testing.T) {
	dir := t.TempDir()
	secret := "secret-key-0123456789abcdef0123456789abcdef"
	src, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "src")}, secret)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "dst")}, secret)
	if err != nil {
		t.Fatal(err)
	}
	const key = "uploads/other/same-size-corrupt.bin"
	sourceData := []byte("source payload")
	corruptData := []byte("corruptpayload")
	if len(sourceData) != len(corruptData) {
		t.Fatal("test fixture must use equal-sized payloads")
	}
	if err := src.Put(context.Background(), key, bytes.NewReader(sourceData), int64(len(sourceData)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if err := dst.Put(context.Background(), key, bytes.NewReader(corruptData), int64(len(corruptData)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	res, err := migrateWith(context.Background(), src, dst, MigrateOptions{RetryAttempts: 2, RetryDelay: time.Millisecond})
	if err != nil {
		t.Fatalf("repair same-size destination: %v", err)
	}
	if res.Copied != 1 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("repair result=%+v, want copied=1 skipped=0 failed=0", res)
	}
	reader, err := dst.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sourceData) {
		t.Fatalf("destination content=%q, want %q", got, sourceData)
	}
}

func TestMigrateHashMismatchDoesNotRetryFullObjectReads(t *testing.T) {
	dir := t.TempDir()
	secret := "secret-key-0123456789abcdef0123456789abcdef"
	srcBase, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "src")}, secret)
	if err != nil {
		t.Fatal(err)
	}
	dstBase, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "dst")}, secret)
	if err != nil {
		t.Fatal(err)
	}
	const key = "uploads/other/mismatch.bin"
	if err := srcBase.Put(context.Background(), key, bytes.NewReader([]byte("source")), 6, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if err := dstBase.Put(context.Background(), key, bytes.NewReader([]byte("target")), 6, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	src := &countingOpenStorage{Storage: srcBase}
	dst := &countingOpenStorage{Storage: dstBase}
	if retryMigrateObjectHash(context.Background(), src, dst, key, 5, time.Millisecond) {
		t.Fatal("mismatched objects unexpectedly verified")
	}
	if src.opens.Load() != 1 || dst.opens.Load() != 1 {
		t.Fatalf("hash mismatch opens source=%d destination=%d, want 1/1", src.opens.Load(), dst.opens.Load())
	}
}

func TestMigrateHashRetriesTransientReadFailure(t *testing.T) {
	dir := t.TempDir()
	secret := "secret-key-0123456789abcdef0123456789abcdef"
	srcBase, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "src")}, secret)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: filepath.Join(dir, "dst")}, secret)
	if err != nil {
		t.Fatal(err)
	}
	const key = "uploads/other/transient-hash.bin"
	data := []byte("same-content")
	if err := srcBase.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if err := dst.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	src := &flakyOpenStorage{Storage: srcBase}
	src.remaining.Store(1)
	if !retryMigrateObjectHash(context.Background(), src, dst, key, 2, time.Millisecond) {
		t.Fatal("transient hash read failure did not recover")
	}
}
