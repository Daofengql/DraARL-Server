package player

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"draarl/internal/broadcast/media"
	"draarl/internal/udphub"
)

type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) WaitUntil(ctx context.Context, target time.Time) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	c.waits = append(c.waits, target)
	if c.now.Before(target) {
		c.now = target
	}
	c.mu.Unlock()
	return contextError(ctx)
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

type fakeSource struct {
	clock       *fakeClock
	delayAfter  map[int]time.Duration
	sendErrorAt int
	sentAt      []time.Time
	finished    int
	cancelled   int
}

func (s *fakeSource) SendVoice(_ []byte, acceptedAt time.Time) (udphub.BroadcastFrameResult, error) {
	index := len(s.sentAt)
	if s.sendErrorAt >= 0 && index == s.sendErrorAt {
		return udphub.BroadcastFrameResult{}, udphub.ErrBroadcastLeaseLost
	}
	s.sentAt = append(s.sentAt, acceptedAt)
	if delay := s.delayAfter[index]; delay > 0 {
		s.clock.Advance(delay)
	}
	return udphub.BroadcastFrameResult{}, nil
}

func (s *fakeSource) Finish() udphub.BroadcastSourceStats {
	s.finished++
	return udphub.BroadcastSourceStats{SentPackets: len(s.sentAt), DroppedPackets: 2}
}

func (s *fakeSource) Cancel() udphub.BroadcastSourceStats {
	s.cancelled++
	return udphub.BroadcastSourceStats{SentPackets: len(s.sentAt), DroppedPackets: 1}
}

func testContainer(t *testing.T, frameCount int) *media.Container {
	t.Helper()
	frames := make([][]byte, frameCount)
	for index := range frames {
		frames[index] = []byte{0xf8, byte(index + 1)}
	}
	wire, _, err := media.BuildContainer(frames)
	if err != nil {
		t.Fatal(err)
	}
	container, err := media.ReadContainer(bytes.NewReader(wire), int64(len(wire)))
	if err != nil {
		t.Fatal(err)
	}
	return container
}

func TestPlayerPacesFromActualSendsWithoutCatchUp(t *testing.T) {
	start := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	source := &fakeSource{clock: clock, delayAfter: map[int]time.Duration{0: 200 * time.Millisecond}, sendErrorAt: -1}
	p, err := New(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p.clock = clock
	result, err := p.Play(context.Background(), testContainer(t, 5))
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Time{start, start.Add(200 * time.Millisecond), start.Add(320 * time.Millisecond)}
	if len(source.sentAt) != len(want) {
		t.Fatalf("send count=%d want=%d", len(source.sentAt), len(want))
	}
	for index := range want {
		if !source.sentAt[index].Equal(want[index]) {
			t.Fatalf("send %d at %v want %v", index, source.sentAt[index], want[index])
		}
	}
	if result.PlayedDuration != 300*time.Millisecond || !result.EndedAt.Equal(start.Add(380*time.Millisecond)) || result.SentPackets != 3 || result.DroppedPackets != 2 {
		t.Fatalf("result=%#v", result)
	}
	if source.finished != 1 || source.cancelled != 0 {
		t.Fatalf("finish=%d cancel=%d", source.finished, source.cancelled)
	}
	if _, err := p.Play(context.Background(), testContainer(t, 1)); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second play error=%v", err)
	}
}

func TestPlayerRealClockDoesNotBurstPackets(t *testing.T) {
	source := &fakeSource{delayAfter: map[int]time.Duration{}, sendErrorAt: -1}
	p, err := New(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := p.Play(context.Background(), testContainer(t, 4))
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if len(source.sentAt) != 2 {
		t.Fatalf("send count=%d want=2", len(source.sentAt))
	}
	interval := source.sentAt[1].Sub(source.sentAt[0])
	if interval < 100*time.Millisecond || interval > 500*time.Millisecond {
		t.Fatalf("packet interval=%s want approximately 120ms", interval)
	}
	if elapsed < 220*time.Millisecond || elapsed > 2*time.Second || result.PlayedDuration != 240*time.Millisecond {
		t.Fatalf("elapsed=%s result=%#v", elapsed, result)
	}
}

func TestPlayerValidatesBeforeEveryPacketAndCancels(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	source := &fakeSource{clock: clock, delayAfter: map[int]time.Duration{}, sendErrorAt: -1}
	wantErr := errors.New("schedule disabled")
	validations := 0
	p, err := New(source, Options{Validate: func(context.Context) error {
		validations++
		if validations == 2 {
			return wantErr
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	p.clock = clock
	result, err := p.Play(context.Background(), testContainer(t, 3))
	if !errors.Is(err, wantErr) || len(source.sentAt) != 1 || validations != 2 {
		t.Fatalf("err=%v sends=%d validations=%d", err, len(source.sentAt), validations)
	}
	if source.finished != 0 || source.cancelled != 1 || result.SentPackets != 1 || result.DroppedPackets != 1 {
		t.Fatalf("source/result=%#v result=%#v", source, result)
	}
}

func TestPlayerRejectsInvalidContainerBeforeSending(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	source := &fakeSource{clock: clock, delayAfter: map[int]time.Duration{}, sendErrorAt: -1}
	p, _ := New(source, Options{})
	p.clock = clock
	_, err := p.Play(context.Background(), &media.Container{Metadata: media.ContainerMetadata{PacketCount: 1, DurationMS: 60}, Packets: [][]byte{{0, 3, 1}}})
	if !errors.Is(err, ErrInvalidContainer) || len(source.sentAt) != 0 || source.cancelled != 1 {
		t.Fatalf("err=%v sends=%d cancelled=%d", err, len(source.sentAt), source.cancelled)
	}
}

func TestPlayerPropagatesContextCancellation(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	source := &fakeSource{clock: clock, delayAfter: map[int]time.Duration{}, sendErrorAt: -1}
	ctx, cancel := context.WithCancel(context.Background())
	validations := 0
	p, _ := New(source, Options{Validate: func(context.Context) error {
		validations++
		if validations == 1 {
			cancel()
		}
		return nil
	}})
	p.clock = clock
	result, err := p.Play(ctx, testContainer(t, 2))
	if !errors.Is(err, context.Canceled) || len(source.sentAt) != 0 || source.cancelled != 1 || result.SentPackets != 0 {
		t.Fatalf("err=%v sends=%d cancelled=%d result=%#v", err, len(source.sentAt), source.cancelled, result)
	}
}
