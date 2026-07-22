package interconnect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"
)

type nodeDatagram struct {
	session *NodeSession
	env     Envelope
	addr    *net.UDPAddr
}

// NodeDatagramBridge authenticates and demultiplexes Type 0 packets received
// by udphub. It owns no socket and starts no network listener. The only UDP
// socket remains the one owned by udphub for ordinary DraARL traffic.
type NodeDatagramBridge struct {
	lookup     func(sourceNodeID string, sessionID uint64) *NodeSession
	onDatagram func(*NodeSession, Envelope, *net.UDPAddr)
	maxAge     time.Duration

	writerMu sync.RWMutex
	writer   func(*net.UDPAddr, []byte) error

	queue     chan nodeDatagram
	closed    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
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
	if err != nil || env.SourceNodeID != "center" || env.NodeSessionID != p.session.SessionID || env.KeyEpoch != p.session.KeyEpoch || env.Expired(now, 2*time.Second) || !p.session.AcceptMessage(env.MessageID, now) {
		return false
	}
	p.Metrics.AddIn(len(data))
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

func (p *NodeDatagramPeer) Bind() error {
	env := NewEnvelope(SubtypeNodeHeartbeat, p.session.NodeID, p.session.SessionID, randomUint64(), nil)
	return p.Send(env)
}

func NewNodeDatagramBridge(lookup func(string, uint64) *NodeSession, onDatagram func(*NodeSession, Envelope, *net.UDPAddr), maxAge time.Duration) (*NodeDatagramBridge, error) {
	if lookup == nil || onDatagram == nil {
		return nil, errors.New("node data callbacks are required")
	}
	if maxAge <= 0 {
		maxAge = 2 * time.Second
	}
	b := &NodeDatagramBridge{
		lookup: lookup, onDatagram: onDatagram, maxAge: maxAge,
		queue: make(chan nodeDatagram, 4096), closed: make(chan struct{}),
	}
	workers := 2
	for i := 0; i < workers; i++ {
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
	sourceID := string(bytes.TrimRight(data[6:38], "\x00"))
	sessionID := binary.BigEndian.Uint64(data[DraARLHeaderSize+20 : DraARLHeaderSize+28])
	session := b.lookup(sourceID, sessionID)
	if session == nil || len(session.Key) == 0 {
		return false
	}
	now := time.Now()
	env, err := Unmarshal(data, session.Key)
	if err != nil || env.NodeSessionID != session.SessionID || env.KeyEpoch != session.KeyEpoch || env.SourceNodeID != session.NodeID || env.Expired(now, b.maxAge) || !session.AcceptMessage(env.MessageID, now) {
		return false
	}
	session.DataMetrics.AddIn(len(data))
	session.BindDataAddr(addr)
	item := nodeDatagram{session: session, env: env, addr: cloneUDPAddr(addr)}
	select {
	case <-b.closed:
		return true
	case b.queue <- item:
		return true
	default:
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
				b.onDatagram(item.session, item.env, item.addr)
			}
		}
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
		close(b.closed)
		b.wg.Wait()
	})
}
