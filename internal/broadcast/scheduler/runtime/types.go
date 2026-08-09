package runtime

import (
	"context"
	"io"
	"time"

	"draarl/internal/broadcast/media"
	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/player"
	core "draarl/internal/broadcast/scheduler"
	"draarl/internal/udphub"
)

type RuntimeRepository interface {
	ClaimDue(context.Context, time.Time, string, time.Duration, time.Duration, int) ([]model.BroadcastRun, error)
	RecoverExpiredRuns(context.Context, time.Time, string, time.Duration, time.Duration, int) ([]model.BroadcastRun, error)
	LoadClaimedExecution(context.Context, uint, string, time.Time) (*core.RunExecution, string, error)
	MarkRunPlaying(context.Context, uint, string, string, []int, time.Time, time.Duration) (string, error)
	ValidateAndRenewRun(context.Context, uint, string, time.Time, time.Duration) (string, error)
	FinishRun(context.Context, uint, string, string, time.Time, int, int, int, *time.Time, string, []int, string, string) error
}

type ObjectStore interface {
	Open(context.Context, string) (io.ReadCloser, error)
}

type LeaseSnapshot struct {
	DomainKey      string
	DomainGroupIDs []int
}

type BroadcastRequest struct {
	RunID         uint
	SourceGroupID int
	QuietWindow   time.Duration
	Container     *media.Container
	OnAcquired    func(LeaseSnapshot) error
	Validate      player.Validator
}

type BroadcastOutcome struct {
	AcquireResult udphub.ScheduledBroadcastAcquireResult
	LastVoiceAt   time.Time
	Snapshot      LeaseSnapshot
	Playback      player.Result
}

type Broadcaster interface {
	Broadcast(context.Context, BroadcastRequest) (BroadcastOutcome, error)
}
