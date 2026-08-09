package runtime

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"draarl/internal/broadcast/media"
	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/player"
	core "draarl/internal/broadcast/scheduler"
	"draarl/internal/config"
	"draarl/internal/udphub"
)

type finishedRun struct {
	status           string
	playedDurationMS int
	sentPackets      int
	droppedPackets   int
	lastVoiceAt      *time.Time
	domainKey        string
	domainGroupIDs   []int
	errorCode        string
}

type fakeRuntimeRepository struct {
	mu                  sync.Mutex
	run                 model.BroadcastRun
	execution           *core.RunExecution
	claimed             bool
	markCode            string
	validateCode        string
	marked              int
	validated           int
	finished            []finishedRun
	finishedCh          chan struct{}
	loadStarted         chan struct{}
	blockLoad           bool
	operationalEnabled  bool
	setOperationalCalls int
	backlog             int64
	emergencyFences     int
}

func (r *fakeRuntimeRepository) EnsureOperationalEnabled(context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.operationalEnabled, nil
}

func (r *fakeRuntimeRepository) OperationalEnabled(context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.operationalEnabled, nil
}

func (r *fakeRuntimeRepository) SetOperationalEnabled(_ context.Context, enabled bool, _ time.Time) error {
	r.mu.Lock()
	r.operationalEnabled = enabled
	r.setOperationalCalls++
	r.mu.Unlock()
	return nil
}

func (r *fakeRuntimeRepository) DueBacklog(context.Context, time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.backlog, nil
}

func (r *fakeRuntimeRepository) FenceEmergencyStop(context.Context, time.Time) error {
	r.mu.Lock()
	r.emergencyFences++
	r.mu.Unlock()
	return nil
}

func (r *fakeRuntimeRepository) ClaimDue(context.Context, time.Time, string, time.Duration, time.Duration, int) ([]model.BroadcastRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimed {
		return nil, nil
	}
	r.claimed = true
	return []model.BroadcastRun{r.run}, nil
}

func (r *fakeRuntimeRepository) RecoverExpiredRuns(context.Context, time.Time, string, time.Duration, time.Duration, int) ([]model.BroadcastRun, error) {
	return nil, nil
}

func (r *fakeRuntimeRepository) ClaimManualRun(context.Context, int, uint, time.Time, string, time.Duration) (*model.BroadcastRun, string, error) {
	r.mu.Lock()
	r.claimed = true
	r.mu.Unlock()
	run := r.run
	return &run, "", nil
}

func (r *fakeRuntimeRepository) LoadClaimedExecution(ctx context.Context, _ uint, _ string, _ time.Time) (*core.RunExecution, string, error) {
	if r.blockLoad {
		select {
		case r.loadStarted <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, "", ctx.Err()
	}
	return r.execution, "", nil
}

func (r *fakeRuntimeRepository) MarkRunPlaying(context.Context, uint, string, string, []int, time.Time, time.Duration) (string, error) {
	r.mu.Lock()
	r.marked++
	code := r.markCode
	r.mu.Unlock()
	return code, nil
}

func (r *fakeRuntimeRepository) ValidateAndRenewRun(context.Context, uint, string, time.Time, time.Duration) (string, error) {
	r.mu.Lock()
	r.validated++
	code := r.validateCode
	r.mu.Unlock()
	return code, nil
}

func (r *fakeRuntimeRepository) FinishRun(_ context.Context, _ uint, _ string, status string, _ time.Time, playedDurationMS, sentPackets, droppedPackets int, lastVoiceAt *time.Time, domainKey string, domainGroupIDs []int, errorCode, _ string) error {
	r.mu.Lock()
	r.finished = append(r.finished, finishedRun{
		status: status, playedDurationMS: playedDurationMS, sentPackets: sentPackets, droppedPackets: droppedPackets,
		lastVoiceAt: lastVoiceAt, domainKey: domainKey, domainGroupIDs: append([]int(nil), domainGroupIDs...), errorCode: errorCode,
	})
	if r.finishedCh != nil {
		select {
		case r.finishedCh <- struct{}{}:
		default:
		}
	}
	r.mu.Unlock()
	return nil
}

type memoryObjectStore struct {
	data []byte
}

func (s memoryObjectStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

type broadcasterFunc func(context.Context, BroadcastRequest) (BroadcastOutcome, error)

func (f broadcasterFunc) Broadcast(ctx context.Context, request BroadcastRequest) (BroadcastOutcome, error) {
	return f(ctx, request)
}

func engineFixture(t *testing.T) (*Engine, *fakeRuntimeRepository, *media.Container) {
	t.Helper()
	frames := [][]byte{{1, 2}, {3, 4}, {5, 6}}
	wire, metadata, err := media.BuildContainer(frames)
	if err != nil {
		t.Fatal(err)
	}
	container, err := media.ReadContainer(bytes.NewReader(wire), int64(len(wire)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run := model.BroadcastRun{ID: 71, ScheduleID: 81, AudioID: 91, SourceGroupID: 101, ScheduledFor: now, Status: model.RunStatusClaimed}
	repo := &fakeRuntimeRepository{
		run: run, finishedCh: make(chan struct{}, 2), operationalEnabled: true,
		execution: &core.RunExecution{
			Run:      run,
			Schedule: model.BroadcastSchedule{ID: run.ScheduleID, GroupID: run.SourceGroupID, AudioID: run.AudioID, Enabled: true},
			Audio: model.BroadcastAudio{
				ID: run.AudioID, GroupID: run.SourceGroupID, Status: model.AudioStatusReady,
				PlaybackObjectKey: "fixture.dabr", PlaybackSize: int64(len(wire)), DurationMS: metadata.DurationMS, PacketCount: metadata.PacketCount,
			},
		},
	}
	cfg := config.BroadcastConfig{Enabled: true, ScanIntervalMS: 250, ClaimBatchSize: 2, QuietWindowSeconds: 5, RecoveryWindowSeconds: 10}
	engine, err := NewEngine(cfg, repo, memoryObjectStore{data: wire})
	if err != nil {
		t.Fatal(err)
	}
	engine.now = func() time.Time { return now }
	return engine, repo, container
}

func waitFinished(t *testing.T, repo *fakeRuntimeRepository) finishedRun {
	t.Helper()
	select {
	case <-repo.finishedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("broadcast run did not finish")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return repo.finished[len(repo.finished)-1]
}

func stopEngine(t *testing.T, engine *Engine) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := engine.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEngineExecutesClaimedContainerAndFinalizesSuccess(t *testing.T) {
	engine, repo, container := engineFixture(t)
	engine.broadcaster = broadcasterFunc(func(ctx context.Context, request BroadcastRequest) (BroadcastOutcome, error) {
		snapshot := LeaseSnapshot{DomainKey: "101,102", DomainGroupIDs: []int{101, 102}}
		if err := request.OnAcquired(snapshot); err != nil {
			return BroadcastOutcome{AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot}, err
		}
		for range request.Container.Packets {
			if err := request.Validate(ctx); err != nil {
				return BroadcastOutcome{AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot}, err
			}
		}
		return BroadcastOutcome{
			AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot,
			Playback: player.Result{PlayedDuration: time.Duration(container.Metadata.DurationMS) * time.Millisecond, SentPackets: len(container.Packets), DroppedPackets: 1, EndedAt: time.Now()},
		}, nil
	})
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	finished := waitFinished(t, repo)
	stopEngine(t, engine)
	if finished.status != model.RunStatusSucceeded || finished.playedDurationMS != container.Metadata.DurationMS || finished.sentPackets != len(container.Packets) || finished.droppedPackets != 1 || finished.domainKey != "101,102" || len(finished.domainGroupIDs) != 2 {
		t.Fatalf("finished=%#v", finished)
	}
	repo.mu.Lock()
	marked, validated := repo.marked, repo.validated
	repo.mu.Unlock()
	if marked != 1 || validated != len(container.Packets) {
		t.Fatalf("marked=%d validated=%d", marked, validated)
	}
}

func TestEngineMapsQuietGateAndManualCancellation(t *testing.T) {
	t.Run("recent voice", func(t *testing.T) {
		engine, repo, _ := engineFixture(t)
		lastVoice := time.Now().Add(-time.Second)
		engine.broadcaster = broadcasterFunc(func(context.Context, BroadcastRequest) (BroadcastOutcome, error) {
			return BroadcastOutcome{
				AcquireResult: udphub.ScheduledBroadcastRecentVoice, LastVoiceAt: lastVoice,
				Snapshot: LeaseSnapshot{DomainKey: "101", DomainGroupIDs: []int{101}},
			}, nil
		})
		if err := engine.Start(); err != nil {
			t.Fatal(err)
		}
		finished := waitFinished(t, repo)
		stopEngine(t, engine)
		if finished.status != model.RunStatusSkippedRecentVoice || finished.errorCode != "recent_voice" || finished.lastVoiceAt == nil || !finished.lastVoiceAt.Equal(lastVoice) {
			t.Fatalf("finished=%#v", finished)
		}
	})

	t.Run("manual stop", func(t *testing.T) {
		engine, repo, _ := engineFixture(t)
		playing := make(chan struct{})
		engine.broadcaster = broadcasterFunc(func(ctx context.Context, request BroadcastRequest) (BroadcastOutcome, error) {
			snapshot := LeaseSnapshot{DomainKey: "101", DomainGroupIDs: []int{101}}
			if err := request.OnAcquired(snapshot); err != nil {
				return BroadcastOutcome{AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot}, err
			}
			close(playing)
			<-ctx.Done()
			return BroadcastOutcome{
				AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot,
				Playback: player.Result{PlayedDuration: 120 * time.Millisecond, SentPackets: 1, EndedAt: time.Now()},
			}, ctx.Err()
		})
		if err := engine.Start(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-playing:
		case <-time.After(3 * time.Second):
			t.Fatal("run did not enter playback")
		}
		if !engine.CancelRun(repo.run.ID, ErrManualStop) {
			t.Fatal("active run was not cancelled")
		}
		finished := waitFinished(t, repo)
		stopEngine(t, engine)
		if finished.status != model.RunStatusCancelled || finished.errorCode != "manual_stop" || finished.sentPackets != 1 {
			t.Fatalf("finished=%#v", finished)
		}
	})
}

func TestEngineManualTriggerUsesTheSameExecutionPath(t *testing.T) {
	engine, repo, container := engineFixture(t)
	repo.claimed = true
	engine.broadcaster = broadcasterFunc(func(ctx context.Context, request BroadcastRequest) (BroadcastOutcome, error) {
		snapshot := LeaseSnapshot{DomainKey: "101", DomainGroupIDs: []int{101}}
		if err := request.OnAcquired(snapshot); err != nil {
			return BroadcastOutcome{AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot}, err
		}
		return BroadcastOutcome{
			AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot,
			Playback: player.Result{PlayedDuration: time.Duration(container.Metadata.DurationMS) * time.Millisecond, SentPackets: len(container.Packets), EndedAt: time.Now()},
		}, nil
	})
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	run, code, err := engine.TriggerManual(context.Background(), repo.run.SourceGroupID, repo.run.ScheduleID)
	if err != nil || code != "" || run == nil || run.ID != repo.run.ID {
		t.Fatalf("manual run=%#v code=%q err=%v", run, code, err)
	}
	finished := waitFinished(t, repo)
	stopEngine(t, engine)
	if finished.status != model.RunStatusSucceeded || finished.sentPackets != len(container.Packets) {
		t.Fatalf("finished=%#v", finished)
	}
}

func TestEngineManualStopBeforeExecutionLoadIsStillCancelled(t *testing.T) {
	engine, repo, _ := engineFixture(t)
	repo.claimed = true
	repo.blockLoad = true
	repo.loadStarted = make(chan struct{}, 1)
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	run, code, err := engine.TriggerManual(context.Background(), repo.run.SourceGroupID, repo.run.ScheduleID)
	if err != nil || code != "" || run == nil {
		t.Fatalf("manual run=%#v code=%q err=%v", run, code, err)
	}
	select {
	case <-repo.loadStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not enter execution load")
	}
	if !engine.CancelRun(run.ID, ErrManualStop) {
		t.Fatal("run was not active")
	}
	finished := waitFinished(t, repo)
	stopEngine(t, engine)
	if finished.status != model.RunStatusCancelled || finished.errorCode != "manual_stop" || finished.sentPackets != 0 {
		t.Fatalf("finished=%#v", finished)
	}
}

func TestEngineCancelRunsWaitsForExecutionRelease(t *testing.T) {
	engine, repo, _ := engineFixture(t)
	repo.claimed = true
	repo.blockLoad = true
	repo.loadStarted = make(chan struct{}, 1)
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.TriggerManual(context.Background(), repo.run.SourceGroupID, repo.run.ScheduleID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-repo.loadStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not enter execution load")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	count, err := engine.CancelRunsAndWait(ctx, []uint{repo.run.ID}, ErrInterconnectChange)
	if err != nil || count != 1 {
		t.Fatalf("cancel count=%d err=%v", count, err)
	}
	finished := waitFinished(t, repo)
	stopEngine(t, engine)
	if finished.status != model.RunStatusCancelledInterconnectEnabled || finished.errorCode != "interconnect_changed" {
		t.Fatalf("finished=%#v", finished)
	}
}

func TestEngineOperationalDisablePersistsCancelsAndReportsMetrics(t *testing.T) {
	engine, repo, _ := engineFixture(t)
	playing := make(chan struct{})
	engine.broadcaster = broadcasterFunc(func(ctx context.Context, request BroadcastRequest) (BroadcastOutcome, error) {
		snapshot := LeaseSnapshot{DomainKey: "101", DomainGroupIDs: []int{101}}
		if err := request.OnAcquired(snapshot); err != nil {
			return BroadcastOutcome{AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot}, err
		}
		close(playing)
		<-ctx.Done()
		return BroadcastOutcome{
			AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot,
			Playback: player.Result{PlayedDuration: 120 * time.Millisecond, SentPackets: 1, DroppedPackets: 2, EndedAt: time.Now()},
		}, ctx.Err()
	})
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-playing:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not enter playback")
	}
	health, err := engine.SetOperationalEnabled(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitFinished(t, repo)
	if finished.status != model.RunStatusCancelledSiteDisabled || finished.errorCode != "site_broadcast_disabled" {
		t.Fatalf("finished=%#v", finished)
	}
	if health.OperationalEnabled || health.ActiveRuns != 0 || !health.Healthy {
		t.Fatalf("health=%#v", health)
	}
	metrics := engine.Metrics()
	if metrics.CurrentPlaying != 0 || metrics.RunsByStatus[model.RunStatusCancelledSiteDisabled] != 1 || metrics.SentPackets != 1 || metrics.DroppedPackets != 2 {
		t.Fatalf("metrics=%#v", metrics)
	}
	repo.mu.Lock()
	setCalls := repo.setOperationalCalls
	repo.mu.Unlock()
	if setCalls != 1 {
		t.Fatalf("set operational calls=%d", setCalls)
	}
	stopEngine(t, engine)
}

func TestEngineEmergencyStopDoesNotDisableFutureScheduling(t *testing.T) {
	engine, repo, _ := engineFixture(t)
	playing := make(chan struct{})
	engine.broadcaster = broadcasterFunc(func(ctx context.Context, request BroadcastRequest) (BroadcastOutcome, error) {
		snapshot := LeaseSnapshot{DomainKey: "101", DomainGroupIDs: []int{101}}
		if err := request.OnAcquired(snapshot); err != nil {
			return BroadcastOutcome{AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot}, err
		}
		close(playing)
		<-ctx.Done()
		return BroadcastOutcome{AcquireResult: udphub.ScheduledBroadcastAcquired, Snapshot: snapshot}, ctx.Err()
	})
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-playing:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not enter playback")
	}
	stopped, err := engine.EmergencyStop(context.Background())
	if err != nil || stopped != 1 {
		t.Fatalf("emergency stop count=%d err=%v", stopped, err)
	}
	finished := waitFinished(t, repo)
	if finished.status != model.RunStatusCancelled || finished.errorCode != "emergency_stop" {
		t.Fatalf("finished=%#v", finished)
	}
	health, err := engine.Health(context.Background())
	if err != nil || !health.OperationalEnabled {
		t.Fatalf("health=%#v err=%v", health, err)
	}
	repo.mu.Lock()
	fences := repo.emergencyFences
	repo.mu.Unlock()
	if fences != 1 {
		t.Fatalf("emergency fences=%d", fences)
	}
	stopEngine(t, engine)
}
