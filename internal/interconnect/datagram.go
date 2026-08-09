package interconnect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type nodeDatagram struct {
	session  *NodeSession
	env      Envelope
	addr     *net.UDPAddr
	queuedAt time.Time
}

// NodeDatagramBridge authenticates and demultiplexes Type 0 packets received
// by udphub. It owns no socket and starts no network listener. The only UDP
// socket remains the one owned by udphub for ordinary DraARL traffic.
type NodeDatagramBridge struct {
	lookup     func(sourceNodeID string, sessionID uint64) *NodeSession
	onDatagram func(*NodeSession, Envelope, *net.UDPAddr)
	maxAge     time.Duration
	limits     ResourceLimits

	writerMu sync.RWMutex
	writer   func(*net.UDPAddr, []byte) error
	gate     sync.RWMutex

	queue     chan nodeDatagram
	closed    chan struct{}
	closing   atomic.Bool
	closeOnce sync.Once
	wg        sync.WaitGroup

	unauthenticated atomic.Uint64
	invalid         atomic.Uint64
	globalQueueDrop atomic.Uint64
}

type NodeDatagramBridgeSnapshot struct {
	UnauthenticatedType0 uint64 `json:"unauthenticated_type0"`
	InvalidType0         uint64 `json:"invalid_type0"`
	GlobalQueueDrops     uint64 `json:"global_queue_drops"`
}

// NodeDatagramPeer is the edge side of the shared udphub UDP data path. It
// owns no socket; EdgeEndpoint supplies the writer and feeds incoming packets
// to Handle.
type NodeDatagramPeer struct {
	session *NodeSession
	center  *net.UDPAddr
	onData  func(Envelope)

	writerMu sync.RWMutex
	writer   func(*net.UDPAddr, []byte) error
	Metrics  Metrics
}

func NewNodeDatagramPeer(center string, session *NodeSession, onData func(Envelope)) (*NodeDatagramPeer, error) {
	if session == nil || len(session.Key) == 0 {
		return nil, errors.New("node data session is required")
	}
	addr, err := net.ResolveUDPAddr("udp", center)
	if err != nil {
		return nil, err
	}
	return &NodeDatagramPeer{session: session, center: addr, onData: onData}, nil
}

func (p *NodeDatagramPeer) SetWriter(writer func(*net.UDPAddr, []byte) error) {
	p.writerMu.Lock()
	p.writer = writer
	p.writerMu.Unlock()
}

func (p *NodeDatagramPeer) Handle(data []byte, _ *net.UDPAddr) bool {
	if p == nil || len(data) < NodeHeaderSize+NodeAuthTagSize || string(data[:4]) != NodeMagic || data[48] != NodePacketType || string(data[86:90]) != "NOD0" {
		return false
	}
	now := time.Now()
	env, err := Unmarshal(data, p.session.Key)
	if err != nil {
		p.session.resourceProtection().recordInvalidAuthTag()
		return false
	}
	if env.SourceNodeID != "center" || env.NodeSessionID != p.session.SessionID || env.KeyEpoch != p.session.KeyEpoch {
		p.session.resourceProtection().recordIdentityReject()
		return false
	}
	p.Metrics.AddIn(len(data))
	if env.Expired(now, 2*time.Second) {
		p.session.resourceProtection().recordExpiredDrop()
		p.Metrics.AddDrop()
		return false
	}
	if !p.session.AcceptMessage(env.MessageID, now) {
		p.session.resourceProtection().recordReplayDrop()
		p.Metrics.AddDrop()
		return false
	}
	if !p.session.resourceProtection().allowData(len(data), now) {
		p.Metrics.AddDrop()
		return true
	}
	if p.onData != nil {
		p.onData(env)
	}
	return true
}

func (p *NodeDatagramPeer) Send(env Envelope) error {
	if p == nil || p.session == nil {
		return errors.New("node data peer is not ready")
	}
	env.SourceNodeID, env.NodeSessionID, env.KeyEpoch = p.session.NodeID, p.session.SessionID, p.session.KeyEpoch
	data, err := env.Marshal(p.session.Key)
	if err != nil {
		return err
	}
	p.writerMu.RLock()
	writer := p.writer
	p.writerMu.RUnlock()
	if writer == nil {
		return errors.New("udphub edge writer is not configured")
	}
	if err := writer(p.center, data); err != nil {
		p.Metrics.AddError()
		return err
	}
	p.Metrics.AddOut(len(data))
	return nil
}

func (p *NodeDatagramPeer) ProveDataBind(challenge []byte, messageID uint64) error {
	payload, err := EncodeJSON(NodeDataBind{Action: NodeDataBindProof, Challenge: challenge})
	if err != nil {
		return err
	}
	env := NewEnvelope(SubtypeNodeDataBind, p.session.NodeID, p.session.SessionID, messageID, payload)
	return p.Send(env)
}

func NewNodeDatagramBridge(lookup func(string, uint64) *NodeSession, onDatagram func(*NodeSession, Envelope, *net.UDPAddr), maxAge time.Duration, configured ...ResourceLimits) (*NodeDatagramBridge, error) {
	if lookup == nil || onDatagram == nil {
		return nil, errors.New("node data callbacks are required")
	}
	if maxAge <= 0 {
		maxAge = 2 * time.Second
	}
	limits := ResourceLimits{}
	if len(configured) > 0 {
		limits = configured[0]
	}
	limits, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	b := &NodeDatagramBridge{
		lookup: lookup, onDatagram: onDatagram, maxAge: maxAge, limits: limits,
		queue: make(chan nodeDatagram, limits.DataQueueGlobal), closed: make(chan struct{}),
	}
	for i := 0; i < limits.DataWorkers; i++ {
		b.wg.Add(1)
		go b.worker()
	}
	return b, nil
}

func (b *NodeDatagramBridge) SetWriter(writer func(*net.UDPAddr, []byte) error) {
	b.writerMu.Lock()
	b.writer = writer
	b.writerMu.Unlock()
}

// Handle implements udphub.Type0Handler. It returns true only after a Type 0
// packet has been authenticated against an active TLS-created NodeSession.
// Valid packets therefore bypass the ordinary per-device rate limiter, while
// forged Type 0-shaped packets receive no exemption.
func (b *NodeDatagramBridge) Handle(data []byte, addr *net.UDPAddr) bool {
	if b == nil || len(data) < NodeHeaderSize+NodeAuthTagSize || string(data[:4]) != NodeMagic || data[48] != NodePacketType || string(data[86:90]) != "NOD0" {
		return false
	}
	b.gate.RLock()
	defer b.gate.RUnlock()
	if b.closing.Load() {
		return true
	}
	sourceID := string(bytes.TrimRight(data[6:38], "\x00"))
	sessionID := binary.BigEndian.Uint64(data[DraARLHeaderSize+20 : DraARLHeaderSize+28])
	session := b.lookup(sourceID, sessionID)
	if session == nil || len(session.Key) == 0 {
		b.unauthenticated.Add(1)
		return false
	}
	now := time.Now()
	env, err := Unmarshal(data, session.Key)
	if err != nil {
		b.invalid.Add(1)
		session.resourceProtection().recordInvalidAuthTag()
		return false
	}
	if env.NodeSessionID != session.SessionID || env.KeyEpoch != session.KeyEpoch || env.SourceNodeID != session.NodeID {
		b.invalid.Add(1)
		session.resourceProtection().recordIdentityReject()
		return false
	}
	session.DataMetrics.AddIn(len(data))
	if env.Expired(now, b.maxAge) {
		b.invalid.Add(1)
		session.resourceProtection().recordExpiredDrop()
		session.DataMetrics.AddDrop()
		return false
	}
	if !session.AcceptMessage(env.MessageID, now) {
		b.invalid.Add(1)
		session.resourceProtection().recordReplayDrop()
		session.DataMetrics.AddDrop()
		return false
	}
	protection := session.resourceProtection()
	if !protection.allowData(len(data), now) {
		session.DataMetrics.AddDrop()
		return true
	}
	if env.Subtype == SubtypeNodeDataBind {
		var bind NodeDataBind
		if DecodeJSON(env.Payload, &bind) != nil || bind.Action != NodeDataBindProof || !session.ConsumeDataBindChallenge(bind.Challenge, now) {
			protection.recordDataBindReject()
			session.DataMetrics.AddDrop()
			return true
		}
		session.BindDataAddr(addr)
		return true
	}
	if !session.DataAddrMatches(addr) {
		protection.recordUnboundAddressDrop()
		session.DataMetrics.AddDrop()
		return true
	}
	if !protection.reserveQueue() {
		session.DataMetrics.AddDrop()
		return true
	}
	if b.closing.Load() {
		protection.releaseQueue()
		return true
	}
	item := nodeDatagram{session: session, env: env, addr: cloneUDPAddr(addr), queuedAt: now}
	select {
	case <-b.closed:
		protection.releaseQueue()
		return true
	case b.queue <- item:
		return true
	default:
		protection.releaseQueue()
		protection.recordQueueDrop()
		b.globalQueueDrop.Add(1)
		session.DataMetrics.AddDrop()
		return true
	}
}

func (b *NodeDatagramBridge) worker() {
	defer b.wg.Done()
	for {
		select {
		case <-b.closed:
			return
		case item := <-b.queue:
			if item.session != nil {
				protection := item.session.resourceProtection()
				protection.releaseQueue()
				if !item.queuedAt.IsZero() && time.Since(item.queuedAt) > b.limits.DataMaxQueueAge {
					protection.recordStaleDrop()
					item.session.DataMetrics.AddDrop()
					continue
				}
				b.onDatagram(item.session, item.env, item.addr)
			}
		}
	}
}

func (b *NodeDatagramBridge) ProtectionSnapshot() NodeDatagramBridgeSnapshot {
	if b == nil {
		return NodeDatagramBridgeSnapshot{}
	}
	return NodeDatagramBridgeSnapshot{
		UnauthenticatedType0: b.unauthenticated.Load(),
		InvalidType0:         b.invalid.Load(),
		GlobalQueueDrops:     b.globalQueueDrop.Load(),
	}
}

func (b *NodeDatagramBridge) Send(session *NodeSession, env Envelope) error {
	if b == nil || session == nil {
		return errors.New("node data session is required")
	}
	env.SourceNodeID, env.NodeSessionID, env.KeyEpoch = "center", session.SessionID, session.KeyEpoch
	data, err := env.Marshal(session.Key)
	if err != nil {
		return err
	}
	addr := session.DataAddr()
	if addr == nil {
		return errors.New("node data address is not bound")
	}
	b.writerMu.RLock()
	writer := b.writer
	b.writerMu.RUnlock()
	if writer == nil {
		return errors.New("udphub Type 0 writer is not configured")
	}
	if err := writer(addr, data); err != nil {
		session.DataMetrics.AddError()
		return err
	}
	session.DataMetrics.AddOut(len(data))
	return nil
}

func (b *NodeDatagramBridge) Close() {
	if b == nil {
		return
	}
	b.closeOnce.Do(func() {
		b.closing.Store(true)
		b.gate.Lock()
		close(b.closed)
		b.gate.Unlock()
		b.wg.Wait()
		for {
			select {
			case item := <-b.queue:
				if item.session != nil {
					item.session.resourceProtection().releaseQueue()
				}
			default:
				return
			}
		}
	})
}
