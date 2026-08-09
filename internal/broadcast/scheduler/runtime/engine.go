package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"draarl/internal/broadcast/media"
	"draarl/internal/broadcast/model"
	core "draarl/internal/broadcast/scheduler"
	"draarl/internal/config"
	"draarl/internal/udphub"
)

const (
	RunLeaseDuration = 5 * time.Second
	finalizeTimeout  = 5 * time.Second
)

var (
	ErrSchedulerDisabled   = errors.New("broadcast scheduler is disabled")
	ErrSchedulerStopped    = errors.New("broadcast scheduler is stopped")
	ErrSchedulerBusy       = errors.New("broadcast scheduler is at capacity")
	ErrManualStop          = errors.New("broadcast manually stopped")
	ErrInterconnectChange  = errors.New("broadcast stopped by interconnect change")
	ErrOperationalDisabled = errors.New("site automatic broadcasts are disabled")
	ErrEmergencyStop       = errors.New("all automatic broadcasts were stopped by an administrator")
	ErrServiceShutdown     = errors.New("broadcast stopped by service shutdown")
)

type RunValidationError struct {
	Code string
}

func (e *RunValidationError) Error() string {
	if e == nil || e.Code == "" {
		return "broadcast run is no longer valid"
	}
	return "broadcast run is no longer valid: " + e.Code
}

type activeRun struct {
	groupID int
	cancel  context.CancelCauseFunc
	done    <-chan struct{}
}

type Engine struct {
	config      config.BroadcastConfig
	repository  RuntimeRepository
	store       ObjectStore
	broadcaster Broadcaster
	instanceID  string
	now         func() time.Time

	lifecycleMu        sync.Mutex
	ctx                context.Context
	cancel             context.CancelCauseFunc
	started            bool
	stopped            bool
	wg                 sync.WaitGroup
	operationalMu      sync.RWMutex
	operationalEnabled bool

	activeMu sync.Mutex
	active   map[uint]activeRun
	slots    chan struct{}
	metrics  engineMetrics
}

func NewEngine(cfg config.BroadcastConfig, repository RuntimeRepository, store ObjectStore) (*Engine, error) {
	if !cfg.Enabled {
		return nil, ErrSchedulerDisabled
	}
	if err := cfg.SetDefaults(); err != nil {
		return nil, err
	}
	if repository == nil || store == nil {
		return nil, errors.New("broadcast scheduler dependencies are incomplete")
	}
	hostname, _ := os.Hostname()
	instanceID := fmt.Sprintf("%s:%d:%d", hostname, os.Getpid(), time.Now().UnixNano())
	return &Engine{
		config: cfg, repository: repository, store: store, broadcaster: DefaultBroadcaster{},
		instanceID: instanceID, now: time.Now, active: make(map[uint]activeRun),
		operationalEnabled: true,
		slots:              make(chan struct{}, cfg.ClaimBatchSize),
	}, nil
}

func (e *Engine) Start() error {
	if e == nil {
		return errors.New("nil broadcast scheduler")
	}
	e.lifecycleMu.Lock()
	defer e.lifecycleMu.Unlock()
	if e.stopped {
		return ErrSchedulerStopped
	}
	if e.started {
		return nil
	}
	e.ctx, e.cancel = context.WithCancelCause(context.Background())
	enabled, err := e.repository.EnsureOperationalEnabled(e.ctx)
	if err != nil {
		e.cancel(err)
		e.ctx, e.cancel = nil, nil
		return fmt.Errorf("load broadcast runtime switch: %w", err)
	}
	e.operationalMu.Lock()
	e.operationalEnabled = enabled
	e.operationalMu.Unlock()
	e.started = true
	e.wg.Add(1)
	go e.scanLoop()
	return nil
}

func (e *Engine) scanLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(time.Duration(e.config.ScanIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := e.scanOnce(e.ctx); err != nil && contextError(e.ctx) == nil {
			log.Printf("[BROADCAST] scheduler scan failed")
		}
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *Engine) scanOnce(ctx context.Context) error {
	scanAt := e.now().UTC()
	e.metrics.scanStarted(scanAt)
	succeeded := false
	defer func() { e.metrics.scanFinished(succeeded, e.now().UTC()) }()

	e.operationalMu.Lock()
	enabled, err := e.repository.OperationalEnabled(ctx)
	if err != nil {
		e.operationalMu.Unlock()
		return err
	}
	e.operationalEnabled = enabled
	e.operationalMu.Unlock()
	if !enabled {
		e.CancelAll(ErrOperationalDisabled)
		succeeded = true
		return nil
	}

	e.operationalMu.RLock()
	defer e.operationalMu.RUnlock()
	if !e.operationalEnabled {
		succeeded = true
		return nil
	}
	available := cap(e.slots) - len(e.slots)
	if available <= 0 {
		succeeded = true
		return nil
	}
	now := scanAt
	recoveryWindow := time.Duration(e.config.RecoveryWindowSeconds) * time.Second
	recovered, err := e.repository.RecoverExpiredRuns(ctx, now, e.instanceID, RunLeaseDuration, recoveryWindow, available)
	if err != nil {
		return err
	}
	available -= len(recovered)
	claimed := make([]model.BroadcastRun, 0, len(recovered)+max(available, 0))
	claimed = append(claimed, recovered...)
	if available > 0 {
		due, claimErr := e.repository.ClaimDue(ctx, now, e.instanceID, RunLeaseDuration, recoveryWindow, available)
		if claimErr != nil {
			return claimErr
		}
		claimed = append(claimed, due...)
	}
	for index := range claimed {
		e.metrics.observeClaim(claimed[index], index < len(recovered), now)
		e.launch(claimed[index])
	}
	succeeded = true
	return nil
}

func (e *Engine) launch(run model.BroadcastRun) {
	select {
	case e.slots <- struct{}{}:
	default:
		return
	}
	e.launchReserved(run)
}

func (e *Engine) launchReserved(run model.BroadcastRun) {
	runCtx, cancel := context.WithCancelCause(e.ctx)
	done := make(chan struct{})
	e.activeMu.Lock()
	if _, exists := e.active[run.ID]; exists {
		e.activeMu.Unlock()
		cancel(ErrSchedulerStopped)
		<-e.slots
		return
	}
	e.active[run.ID] = activeRun{groupID: run.SourceGroupID, cancel: cancel, done: done}
	e.activeMu.Unlock()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			e.activeMu.Lock()
			delete(e.active, run.ID)
			e.activeMu.Unlock()
			<-e.slots
			close(done)
		}()
		e.executeRun(runCtx, run)
	}()
}

func (e *Engine) TriggerManual(ctx context.Context, groupID int, scheduleID uint) (*model.BroadcastRun, string, error) {
	if e == nil {
		return nil, "", ErrSchedulerStopped
	}
	e.lifecycleMu.Lock()
	started, stopped := e.started, e.stopped
	e.lifecycleMu.Unlock()
	if !started || stopped {
		return nil, "", ErrSchedulerStopped
	}
	e.operationalMu.RLock()
	defer e.operationalMu.RUnlock()
	if !e.operationalEnabled {
		return nil, "", ErrOperationalDisabled
	}
	select {
	case e.slots <- struct{}{}:
	default:
		return nil, "", ErrSchedulerBusy
	}
	run, code, err := e.repository.ClaimManualRun(ctx, groupID, scheduleID, e.now().UTC(), e.instanceID, RunLeaseDuration)
	if err != nil || code != "" {
		<-e.slots
		return nil, code, err
	}
	e.launchReserved(*run)
	return run, "", nil
}

func (e *Engine) executeRun(ctx context.Context, run model.BroadcastRun) {
	playing := false
	defer func() {
		if playing {
			e.metrics.currentPlaying.Add(-1)
		}
	}()
	execution, code, err := e.repository.LoadClaimedExecution(ctx, run.ID, e.instanceID, e.now().UTC())
	if err != nil {
		if contextError(ctx) != nil {
			status, errorCode := playbackTerminal(ctx, ctx.Err())
			e.finish(run, status, playerSummary{}, nil, LeaseSnapshot{}, errorCode, "broadcast execution was cancelled before playback")
			return
		}
		e.finish(run, model.RunStatusFailed, playerSummary{}, nil, LeaseSnapshot{}, "execution_load_failed", "broadcast execution could not be loaded")
		return
	}
	if code != "" {
		status := model.RunStatusCancelled
		if code == "virtual_group_broadcast_suspended" {
			status = model.RunStatusSkippedInterconnected
		} else if code == "site_broadcast_disabled" {
			status = model.RunStatusSkippedSiteDisabled
		}
		e.finish(run, status, playerSummary{}, nil, LeaseSnapshot{}, code, "broadcast execution was no longer eligible")
		return
	}
	container, loadCode := e.loadContainer(ctx, execution)
	if loadCode != "" {
		e.finish(run, model.RunStatusFailed, playerSummary{}, nil, LeaseSnapshot{}, loadCode, "broadcast playback media is unavailable or invalid")
		return
	}

	request := BroadcastRequest{
		RunID: run.ID, SourceGroupID: run.SourceGroupID,
		QuietWindow: time.Duration(e.config.QuietWindowSeconds) * time.Second,
		Container:   container,
		OnAcquired: func(snapshot LeaseSnapshot) error {
			code, err := e.repository.MarkRunPlaying(ctx, run.ID, e.instanceID, snapshot.DomainKey, snapshot.DomainGroupIDs, e.now().UTC(), RunLeaseDuration)
			if err != nil {
				return err
			}
			if code != "" {
				return &RunValidationError{Code: code}
			}
			playing = true
			e.metrics.currentPlaying.Add(1)
			return nil
		},
		Validate: func(validateCtx context.Context) error {
			code, err := e.repository.ValidateAndRenewRun(validateCtx, run.ID, e.instanceID, e.now().UTC(), RunLeaseDuration)
			if err != nil {
				return err
			}
			if code != "" {
				return &RunValidationError{Code: code}
			}
			return nil
		},
	}
	outcome, playErr := e.broadcaster.Broadcast(ctx, request)
	if playErr != nil && outcome.AcquireResult != udphub.ScheduledBroadcastAcquired {
		status, errorCode := playbackTerminal(ctx, playErr)
		e.finish(run, status, playerSummary{}, nil, outcome.Snapshot, errorCode, "broadcast playback stopped before acquiring the communication domain")
		return
	}
	if outcome.AcquireResult != udphub.ScheduledBroadcastAcquired {
		status, errorCode := acquireTerminal(outcome.AcquireResult)
		var lastVoiceAt *time.Time
		if !outcome.LastVoiceAt.IsZero() {
			value := outcome.LastVoiceAt.UTC()
			lastVoiceAt = &value
		}
		e.finish(run, status, playerSummary{}, lastVoiceAt, outcome.Snapshot, errorCode, "scheduled broadcast did not acquire the communication domain")
		return
	}
	summary := playerSummary{
		playedDurationMS: int(outcome.Playback.PlayedDuration.Milliseconds()),
		sentPackets:      outcome.Playback.SentPackets, droppedPackets: outcome.Playback.DroppedPackets,
		endedAt: outcome.Playback.EndedAt,
	}
	if playErr == nil {
		e.finish(run, model.RunStatusSucceeded, summary, nil, outcome.Snapshot, "", "")
		return
	}
	status, errorCode := playbackTerminal(ctx, playErr)
	e.finish(run, status, summary, nil, outcome.Snapshot, errorCode, "broadcast playback stopped before normal completion")
}

func (e *Engine) loadContainer(ctx context.Context, execution *core.RunExecution) (*media.Container, string) {
	if execution == nil || execution.Audio.PlaybackSize <= 0 || strings.TrimSpace(execution.Audio.PlaybackObjectKey) == "" {
		return nil, "playback_metadata_invalid"
	}
	reader, err := e.store.Open(ctx, execution.Audio.PlaybackObjectKey)
	if err != nil {
		return nil, "playback_object_unavailable"
	}
	container, readErr := media.ReadContainer(reader, execution.Audio.PlaybackSize)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return nil, "playback_container_invalid"
	}
	if container.Metadata.Size != execution.Audio.PlaybackSize ||
		container.Metadata.DurationMS != execution.Audio.DurationMS ||
		container.Metadata.PacketCount != execution.Audio.PacketCount {
		return nil, "playback_metadata_mismatch"
	}
	return container, ""
}

type playerSummary struct {
	playedDurationMS int
	sentPackets      int
	droppedPackets   int
	endedAt          time.Time
}

func (e *Engine) finish(run model.BroadcastRun, status string, summary playerSummary, lastVoiceAt *time.Time, snapshot LeaseSnapshot, errorCode, errorMessage string) {
	endedAt := summary.endedAt
	if endedAt.IsZero() {
		endedAt = e.now()
	}
	ctx, cancel := context.WithTimeout(context.Background(), finalizeTimeout)
	defer cancel()
	e.metrics.observeTerminal(status, summary.sentPackets, summary.droppedPackets)
	if err := e.repository.FinishRun(
		ctx, run.ID, e.instanceID, status, endedAt.UTC(), summary.playedDurationMS,
		summary.sentPackets, summary.droppedPackets, lastVoiceAt,
		snapshot.DomainKey, snapshot.DomainGroupIDs, errorCode, errorMessage,
	); err != nil && !errors.Is(err, context.Canceled) {
		e.metrics.finalizeErrors.Add(1)
		log.Printf("[BROADCAST] finalize run failed: run_id=%d status=%s", run.ID, status)
	}
}

func acquireTerminal(result udphub.ScheduledBroadcastAcquireResult) (string, string) {
	switch result {
	case udphub.ScheduledBroadcastRecentVoice:
		return model.RunStatusSkippedRecentVoice, "recent_voice"
	case udphub.ScheduledBroadcastDomainBusy:
		return model.RunStatusSkippedDomainBusy, "domain_busy"
	case udphub.ScheduledBroadcastNoReceiver:
		return model.RunStatusSkippedNoReceiver, "no_receiver"
	default:
		return model.RunStatusFailed, "invalid_communication_domain"
	}
}

func playbackTerminal(ctx context.Context, err error) (string, string) {
	var validation *RunValidationError
	if errors.As(err, &validation) {
		if validation.Code == "virtual_group_broadcast_suspended" {
			return model.RunStatusCancelledInterconnectEnabled, validation.Code
		}
		if validation.Code == "site_broadcast_disabled" {
			return model.RunStatusCancelledSiteDisabled, validation.Code
		}
		if validation.Code == "emergency_stop" {
			return model.RunStatusCancelled, validation.Code
		}
		return model.RunStatusCancelled, validation.Code
	}
	cause := context.Cause(ctx)
	if errors.As(cause, &validation) {
		if validation.Code == "virtual_group_broadcast_suspended" {
			return model.RunStatusCancelledInterconnectEnabled, validation.Code
		}
		if validation.Code == "site_broadcast_disabled" {
			return model.RunStatusCancelledSiteDisabled, validation.Code
		}
		return model.RunStatusCancelled, validation.Code
	}
	switch {
	case errors.Is(cause, ErrInterconnectChange):
		return model.RunStatusCancelledInterconnectEnabled, "interconnect_changed"
	case errors.Is(cause, ErrOperationalDisabled):
		return model.RunStatusCancelledSiteDisabled, "site_broadcast_disabled"
	case errors.Is(cause, ErrEmergencyStop):
		return model.RunStatusCancelled, "emergency_stop"
	case errors.Is(cause, ErrManualStop):
		return model.RunStatusCancelled, "manual_stop"
	case errors.Is(cause, ErrServiceShutdown):
		return model.RunStatusCancelled, "service_shutdown"
	case errors.Is(err, udphub.ErrBroadcastLeaseLost):
		return model.RunStatusCancelled, "communication_domain_changed"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return model.RunStatusCancelled, "playback_cancelled"
	default:
		return model.RunStatusFailed, "playback_failed"
	}
}

func (e *Engine) CancelRun(runID uint, cause error) bool {
	if cause == nil {
		cause = ErrManualStop
	}
	e.activeMu.Lock()
	active, ok := e.active[runID]
	e.activeMu.Unlock()
	if ok {
		active.cancel(cause)
	}
	return ok
}

func (e *Engine) CancelGroups(groupIDs []int, cause error) int {
	count, _ := e.CancelGroupsAndWait(context.Background(), groupIDs, cause, false)
	return count
}

func (e *Engine) CancelAll(cause error) int {
	count, _ := e.CancelAllAndWait(context.Background(), cause, false)
	return count
}

func (e *Engine) CancelAllAndWait(ctx context.Context, cause error, wait bool) (int, error) {
	if cause == nil {
		cause = ErrManualStop
	}
	e.activeMu.Lock()
	activeRuns := make([]activeRun, 0, len(e.active))
	for _, active := range e.active {
		activeRuns = append(activeRuns, active)
	}
	e.activeMu.Unlock()
	for _, active := range activeRuns {
		active.cancel(cause)
	}
	if !wait {
		return len(activeRuns), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, active := range activeRuns {
		select {
		case <-active.done:
		case <-ctx.Done():
			return len(activeRuns), ctx.Err()
		}
	}
	return len(activeRuns), nil
}

func (e *Engine) SetOperationalEnabled(ctx context.Context, enabled bool) (HealthSnapshot, error) {
	if e == nil {
		return HealthSnapshot{}, ErrSchedulerStopped
	}
	e.lifecycleMu.Lock()
	started, stopped := e.started, e.stopped
	e.lifecycleMu.Unlock()
	if !started || stopped {
		return HealthSnapshot{}, ErrSchedulerStopped
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e.operationalMu.Lock()
	err := e.repository.SetOperationalEnabled(ctx, enabled, e.now().UTC())
	if err == nil {
		e.operationalEnabled = enabled
	}
	e.operationalMu.Unlock()
	if err != nil {
		return HealthSnapshot{}, err
	}
	if !enabled {
		if _, err := e.CancelAllAndWait(ctx, ErrOperationalDisabled, true); err != nil {
			return HealthSnapshot{}, err
		}
	}
	return e.Health(ctx)
}

func (e *Engine) EmergencyStop(ctx context.Context) (int, error) {
	if e == nil {
		return 0, ErrSchedulerStopped
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e.operationalMu.Lock()
	err := e.repository.FenceEmergencyStop(ctx, e.now().UTC())
	e.operationalMu.Unlock()
	if err != nil {
		return 0, err
	}
	return e.CancelAllAndWait(ctx, ErrEmergencyStop, true)
}

func (e *Engine) Metrics() MetricsSnapshot {
	if e == nil {
		return MetricsSnapshot{RunsByStatus: map[string]uint64{}}
	}
	return e.metrics.snapshot()
}

func (e *Engine) Health(ctx context.Context) (HealthSnapshot, error) {
	if e == nil {
		return HealthSnapshot{}, ErrSchedulerStopped
	}
	e.lifecycleMu.Lock()
	started, stopped := e.started, e.stopped
	e.lifecycleMu.Unlock()
	e.operationalMu.RLock()
	enabled := e.operationalEnabled
	e.operationalMu.RUnlock()
	e.activeMu.Lock()
	activeCount := len(e.active)
	e.activeMu.Unlock()
	backlog, err := e.repository.DueBacklog(ctx, e.now().UTC())
	if err != nil {
		return HealthSnapshot{}, err
	}
	metrics := e.metrics.snapshot()
	healthy := started && !stopped && metrics.ConsecutiveScanErrors < 3
	if metrics.LastScanAt != nil && e.now().Sub(*metrics.LastScanAt) > 3*time.Duration(e.config.ScanIntervalMS)*time.Millisecond {
		healthy = false
	}
	return HealthSnapshot{
		Started: started, Stopped: stopped, OperationalEnabled: enabled, Healthy: healthy,
		ActiveRuns: activeCount, Capacity: cap(e.slots), DueBacklog: backlog,
		LastScanAt: metrics.LastScanAt, LastSuccessfulScanAt: metrics.LastSuccessfulScanAt,
		ConsecutiveScanErrors: metrics.ConsecutiveScanErrors,
	}, nil
}

func (e *Engine) CancelGroupsAndWait(ctx context.Context, groupIDs []int, cause error, wait bool) (int, error) {
	set := make(map[int]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID > 0 {
			set[groupID] = struct{}{}
		}
	}
	e.activeMu.Lock()
	activeRuns := make([]activeRun, 0)
	for _, active := range e.active {
		if _, ok := set[active.groupID]; ok {
			activeRuns = append(activeRuns, active)
		}
	}
	e.activeMu.Unlock()
	for _, active := range activeRuns {
		active.cancel(cause)
	}
	if !wait {
		return len(activeRuns), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, active := range activeRuns {
		select {
		case <-active.done:
		case <-ctx.Done():
			return len(activeRuns), ctx.Err()
		}
	}
	return len(activeRuns), nil
}

func (e *Engine) CancelRunsAndWait(ctx context.Context, runIDs []uint, cause error) (int, error) {
	set := make(map[uint]struct{}, len(runIDs))
	for _, runID := range runIDs {
		if runID > 0 {
			set[runID] = struct{}{}
		}
	}
	e.activeMu.Lock()
	activeRuns := make([]activeRun, 0, len(set))
	for runID := range set {
		if active, ok := e.active[runID]; ok {
			activeRuns = append(activeRuns, active)
		}
	}
	e.activeMu.Unlock()
	for _, active := range activeRuns {
		active.cancel(cause)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, active := range activeRuns {
		select {
		case <-active.done:
		case <-ctx.Done():
			return len(activeRuns), ctx.Err()
		}
	}
	return len(activeRuns), nil
}

func (e *Engine) Stop(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.lifecycleMu.Lock()
	if e.stopped {
		e.lifecycleMu.Unlock()
		return nil
	}
	e.stopped = true
	if e.cancel != nil {
		e.cancel(ErrServiceShutdown)
	}
	e.lifecycleMu.Unlock()
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
