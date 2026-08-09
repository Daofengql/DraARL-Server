package udphub

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/interfaces"
	"draarl/internal/protocol"
)

const (
	SystemBroadcastUsername = "system-broadcast"
	SystemBroadcastNickname = "自动播报"
	SystemBroadcastCallSign = "AUTO"
	SystemBroadcastSSID     = byte(255)
)

var (
	ErrInvalidBroadcastSource  = errors.New("invalid scheduled broadcast source")
	ErrBroadcastLeaseLost      = errors.New("scheduled broadcast lease lost")
	ErrInvalidBroadcastPayload = errors.New("invalid scheduled broadcast payload")
)

type BroadcastFrameResult struct {
	Packet      []byte
	WSSent      int
	WSDropped   int
	UDPQueued   bool
	EdgeRelayed bool
}

type BroadcastSourceStats struct {
	SentPackets       int
	DroppedPackets    int
	UDPTargetsSent    int64
	UDPTargetsDropped int64
	WSTargetsSent     int64
	WSTargetsDropped  int64
	EdgeRelayErrors   int64
}

// BroadcastSource is an internal voice producer. It has no network session,
// device allocation, heartbeat or user-owned identity. One instance owns one
// scheduled-broadcast lease and its fixed delivery snapshots.
type BroadcastSource struct {
	lease      *ScheduledBroadcastLease
	mu         sync.Mutex
	finished   bool
	generation atomic.Uint64
	inFlight   sync.WaitGroup

	sentPackets       atomic.Int64
	droppedPackets    atomic.Int64
	udpTargetsSent    atomic.Int64
	udpTargetsDropped atomic.Int64
	wsTargetsSent     atomic.Int64
	wsTargetsDropped  atomic.Int64
	edgeRelayErrors   atomic.Int64
}

func NewBroadcastSource(lease *ScheduledBroadcastLease) (*BroadcastSource, error) {
	if lease == nil || lease.RunID == 0 || lease.SourceGroupID <= 0 || lease.domainID == 0 || lease.closed.Load() || lease.receiverSnap == nil {
		return nil, ErrInvalidBroadcastSource
	}
	source := &BroadcastSource{lease: lease}
	source.generation.Store(1)
	return source, nil
}

func (s *BroadcastSource) RunID() uint {
	if s == nil || s.lease == nil {
		return 0
	}
	return s.lease.RunID
}

func (s *BroadcastSource) SourceGroupID() int {
	if s == nil || s.lease == nil {
		return 0
	}
	return s.lease.SourceGroupID
}

func (s *BroadcastSource) DomainGroupIDs() []int {
	if s == nil || s.lease == nil {
		return nil
	}
	return append([]int(nil), s.lease.DomainGroupIDs...)
}

func (s *BroadcastSource) SendVoice(payload []byte, acceptedAt time.Time) (BroadcastFrameResult, error) {
	if s == nil || s.lease == nil {
		return BroadcastFrameResult{}, ErrInvalidBroadcastSource
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return BroadcastFrameResult{}, ErrBroadcastLeaseLost
	}
	if len(payload) == 0 || len(payload) > protocol.DraARLv1MaxPacketSize-protocol.DraARLv1HeaderSize {
		return BroadcastFrameResult{}, ErrInvalidBroadcastPayload
	}
	if !AcceptScheduledBroadcastFrame(s.lease, acceptedAt) {
		return BroadcastFrameResult{}, ErrBroadcastLeaseLost
	}

	packet := protocol.EncodeDraARLv1(
		SystemBroadcastUsername, "", SystemBroadcastSSID,
		protocol.DraARLTypeOpus16K, protocol.DraARLDevModelUnknown,
		0, SystemBroadcastCallSign, payload,
	)
	result := BroadcastFrameResult{Packet: packet}

	snap := s.lease.receiverSnap
	if snap != nil && len(snap.entries) > 0 {
		s.inFlight.Add(1)
		onComplete := func(writeResult fanoutWriteResult) {
			s.udpTargetsSent.Add(writeResult.sent)
			s.udpTargetsDropped.Add(writeResult.dropped + writeResult.errors)
			if writeResult.sent == 0 && writeResult.dropped+writeResult.errors > 0 {
				s.droppedPackets.Add(1)
			}
			s.inFlight.Done()
		}
		if sender := getFanoutSender(); sender != nil && sender.enqueueFixedDomainFrame(packet, snap, s.lease.SourceGroupID, &s.generation, onComplete) {
			result.UDPQueued = true
		} else {
			s.inFlight.Done()
			s.droppedPackets.Add(1)
		}
	}

	if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
		result.WSSent, result.WSDropped = GlobalMessageRouter.wsManager.BroadcastToGroups(
			s.lease.DomainGroupIDs, packet, 2,
			interfaces.WSBroadcastFilter{SourceGroupID: s.lease.SourceGroupID},
		)
		s.wsTargetsSent.Add(int64(result.WSSent))
		s.wsTargetsDropped.Add(int64(result.WSDropped))
	}

	hooks := centerHooks()
	if hooks.RelayBroadcast != nil {
		if err := hooks.RelayBroadcast(s.lease.RunID, s.lease.SourceGroupID, s.lease.domainID, packet); err != nil {
			s.edgeRelayErrors.Add(1)
		} else {
			result.EdgeRelayed = true
		}
	}
	s.sentPackets.Add(1)
	return result, nil
}

// Finish retains the normal half-duplex tail hold after the final packet.
func (s *BroadcastSource) Finish() BroadcastSourceStats {
	if s != nil && s.lease != nil {
		s.mu.Lock()
		if !s.finished {
			s.finished = true
		}
		s.mu.Unlock()
		s.inFlight.Wait()
		FinishScheduledBroadcast(s.lease)
	}
	return s.Stats()
}

// Cancel invalidates queued UDP work and releases the arbiter immediately.
func (s *BroadcastSource) Cancel() BroadcastSourceStats {
	if s != nil && s.lease != nil {
		s.mu.Lock()
		if !s.finished {
			s.finished = true
			s.generation.Add(1)
			ReleaseScheduledBroadcast(s.lease)
		}
		s.mu.Unlock()
		s.inFlight.Wait()
	}
	return s.Stats()
}

func (s *BroadcastSource) Stats() BroadcastSourceStats {
	if s == nil {
		return BroadcastSourceStats{}
	}
	return BroadcastSourceStats{
		SentPackets:       int(s.sentPackets.Load()),
		DroppedPackets:    int(s.droppedPackets.Load()),
		UDPTargetsSent:    s.udpTargetsSent.Load(),
		UDPTargetsDropped: s.udpTargetsDropped.Load(),
		WSTargetsSent:     s.wsTargetsSent.Load(),
		WSTargetsDropped:  s.wsTargetsDropped.Load(),
		EdgeRelayErrors:   s.edgeRelayErrors.Load(),
	}
}
