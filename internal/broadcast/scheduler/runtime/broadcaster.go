package runtime

import (
	"context"
	"errors"
	"time"

	"draarl/internal/broadcast/player"
	"draarl/internal/udphub"
)

type DefaultBroadcaster struct{}

func (DefaultBroadcaster) Broadcast(ctx context.Context, request BroadcastRequest) (outcome BroadcastOutcome, err error) {
	if request.Container == nil || request.RunID == 0 || request.SourceGroupID <= 0 {
		return outcome, errors.New("invalid broadcast request")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return outcome, err
	}
	outcome.Snapshot = LeaseSnapshot{
		DomainKey:      udphub.GetActiveCommunicationDomainKey(request.SourceGroupID),
		DomainGroupIDs: udphub.GetActiveCommunicationGroupIDs(request.SourceGroupID),
	}
	lease, lastVoiceAt, acquireResult := udphub.TryAcquireScheduledBroadcast(
		request.SourceGroupID, request.RunID, time.Now(), request.QuietWindow,
	)
	outcome.AcquireResult = acquireResult
	outcome.LastVoiceAt = lastVoiceAt
	if lease == nil {
		return outcome, nil
	}
	outcome.Snapshot = LeaseSnapshot{DomainKey: lease.DomainKey(), DomainGroupIDs: append([]int(nil), lease.DomainGroupIDs...)}

	source, err := udphub.NewBroadcastSource(lease)
	if err != nil {
		udphub.ReleaseScheduledBroadcast(lease)
		return outcome, err
	}
	if request.OnAcquired != nil {
		if err := request.OnAcquired(outcome.Snapshot); err != nil {
			source.Cancel()
			return outcome, err
		}
	}
	playback, err := player.New(source, player.Options{Validate: request.Validate})
	if err != nil {
		source.Cancel()
		return outcome, err
	}
	outcome.Playback, err = playback.Play(ctx, request.Container)
	return outcome, err
}
