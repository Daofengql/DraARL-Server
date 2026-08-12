package player

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"draarl/internal/broadcast/media"
	"draarl/internal/udphub"
)

var (
	ErrInvalidSource    = errors.New("invalid broadcast voice source")
	ErrInvalidContainer = errors.New("invalid broadcast playback container")
	ErrAlreadyStarted   = errors.New("broadcast player already started")
)

type Source interface {
	SendVoice(payload []byte, acceptedAt time.Time) (udphub.BroadcastFrameResult, error)
	Finish() udphub.BroadcastSourceStats
	Cancel() udphub.BroadcastSourceStats
}

type Validator func(context.Context) error

type Options struct {
	Validate         Validator
	ValidateInterval time.Duration
}

type Result struct {
	StartedAt      time.Time
	EndedAt        time.Time
	PlayedDuration time.Duration
	SentPackets    int
	DroppedPackets int
	SourceStats    udphub.BroadcastSourceStats
}

type playbackClock interface {
	Now() time.Time
	WaitUntil(context.Context, time.Time) error
}

type realPlaybackClock struct{}

func (realPlaybackClock) Now() time.Time { return time.Now() }

func (realPlaybackClock) WaitUntil(ctx context.Context, target time.Time) error {
	delay := time.Until(target)
	if delay <= 0 {
		return contextError(ctx)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return contextError(ctx)
	}
}

type Player struct {
	source           Source
	validate         Validator
	validateInterval time.Duration
	clock            playbackClock
	started          atomic.Bool
}

func New(source Source, options Options) (*Player, error) {
	if source == nil {
		return nil, ErrInvalidSource
	}
	return &Player{
		source:           source,
		validate:         options.Validate,
		validateInterval: options.ValidateInterval,
		clock:            realPlaybackClock{},
	}, nil
}

func (p *Player) Play(ctx context.Context, container *media.Container) (result Result, err error) {
	if p == nil || p.source == nil || p.clock == nil {
		return Result{}, ErrInvalidSource
	}
	if !p.started.CompareAndSwap(false, true) {
		return Result{}, ErrAlreadyStarted
	}
	if ctx == nil {
		ctx = context.Background()
	}
	durations, err := validateContainer(container)
	if err != nil {
		p.source.Cancel()
		return Result{}, err
	}

	normalFinish := false
	defer func() {
		if normalFinish {
			result.SourceStats = p.source.Finish()
		} else {
			result.SourceStats = p.source.Cancel()
		}
		result.SentPackets = result.SourceStats.SentPackets
		result.DroppedPackets = result.SourceStats.DroppedPackets
		if result.EndedAt.IsZero() && !result.StartedAt.IsZero() {
			result.EndedAt = p.clock.Now()
		}
	}()

	nextSendAt := p.clock.Now()
	nextValidationAt := time.Time{}
	for index, packet := range container.Packets {
		if err = p.clock.WaitUntil(ctx, nextSendAt); err != nil {
			return result, err
		}
		if p.validate != nil {
			now := p.clock.Now()
			if nextValidationAt.IsZero() || p.validateInterval <= 0 || !now.Before(nextValidationAt) {
				if err = p.validate(ctx); err != nil {
					return result, err
				}
				if p.validateInterval > 0 {
					nextValidationAt = p.clock.Now().Add(p.validateInterval)
				}
			}
		}
		if err = contextError(ctx); err != nil {
			return result, err
		}

		sentAt := p.clock.Now()
		if _, err = p.source.SendVoice(packet, sentAt); err != nil {
			return result, err
		}
		if result.StartedAt.IsZero() {
			result.StartedAt = sentAt
		}
		result.PlayedDuration += durations[index]

		packetDuration := durations[index]
		if sentAt.Sub(nextSendAt) >= packetDuration {
			// A full packet behind would make the next send immediate. Rebase after
			// exceptional stalls, but keep small wake-up delays off the media clock.
			nextSendAt = sentAt.Add(packetDuration)
		} else {
			nextSendAt = nextSendAt.Add(packetDuration)
		}
	}
	if err = p.clock.WaitUntil(ctx, nextSendAt); err != nil {
		return result, err
	}
	if err = contextError(ctx); err != nil {
		return result, err
	}
	result.EndedAt = p.clock.Now()
	normalFinish = true
	return result, nil
}

func validateContainer(container *media.Container) ([]time.Duration, error) {
	if container == nil || len(container.Packets) == 0 || container.Metadata.PacketCount != len(container.Packets) {
		return nil, ErrInvalidContainer
	}
	durations := make([]time.Duration, len(container.Packets))
	var total time.Duration
	for index, packet := range container.Packets {
		duration, err := media.PacketDuration(packet)
		if err != nil {
			return nil, errors.Join(ErrInvalidContainer, err)
		}
		durations[index] = duration
		total += duration
	}
	frameDuration := time.Duration(media.OpusFrameDurationMS) * time.Millisecond
	if total != time.Duration(container.Metadata.DurationMS)*time.Millisecond ||
		container.Metadata.FrameCount != int(total/frameDuration) ||
		total > 60*time.Second {
		return nil, ErrInvalidContainer
	}
	return durations, nil
}

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
