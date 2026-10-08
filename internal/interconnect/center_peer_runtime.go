package interconnect

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"draarl/internal/protocol"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type CenterPeerConfig struct {
	Link          CenterPeerLink
	RemoteAddress string
	TLSServerName string
	TLSPinSHA256  string
	Credential    string // decrypted in application startup; never returned in API
}

type CenterPeerStatus struct {
	Online          bool       `json:"online"`
	Role            string     `json:"role"`
	VirtualDeviceID string     `json:"virtual_device_id"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	InPackets       uint64     `json:"in_packets"`
	InBytes         uint64     `json:"in_bytes"`
	OutPackets      uint64     `json:"out_packets"`
	OutBytes        uint64     `json:"out_bytes"`
	DroppedPackets  uint64     `json:"dropped_packets"`
}
type peerAudioJob struct {
	payload []byte
	at      time.Time
}
type peerConnection struct {
	lastReceived uint64
	rateStart    time.Time
	rateCount    int
	session      *NodeSession
	client       *NodeClient
	ready        atomic.Bool
	next         atomic.Uint64
	queue        chan peerAudioJob
	done         chan struct{}
	once         sync.Once
}

func (c *peerConnection) close() {
	c.once.Do(func() {
		close(c.done)
		if c.client != nil {
			_ = c.client.Close()
		} else {
			_ = c.session.Close()
		}
	})
}
func (c *peerConnection) send(e Envelope) error {
	if c.client != nil {
		return c.client.SendEnvelope(e)
	}
	e.SourceNodeID = "center"
	return c.session.SendEnvelope(e)
}

type peerEntry struct {
	cfg        CenterPeerConfig
	connection *peerConnection
	cancel     context.CancelFunc
	status     CenterPeerStatus
}
type CenterPeerManager struct {
	mu      sync.RWMutex
	entries map[string]*peerEntry
	closed  bool
	wg      sync.WaitGroup
	receive func(CenterPeerLink, []byte) bool
}

func NewCenterPeerManager(receivers ...func(CenterPeerLink, []byte) bool) *CenterPeerManager {
	m := &CenterPeerManager{entries: make(map[string]*peerEntry)}
	if len(receivers) > 0 {
		m.receive = receivers[0]
	}
	return m
}

func PeerTLSConfig(cfg CenterPeerConfig) (*tls.Config, error) {
	host, port, err := net.SplitHostPort(cfg.RemoteAddress)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || portNumber < 1 || portNumber > 65535 || len(cfg.RemoteAddress) > 255 || strings.ContainsAny(host, " \t\r\n") {
		return nil, errors.New("远端地址必须为 主机:端口")
	}
	name := cfg.TLSServerName
	if len(name) > 253 || strings.ContainsAny(name, " \t\r\n") {
		return nil, errors.New("TLS 证书域名格式无效")
	}
	if name == "" {
		name = host
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: name}
	if cfg.TLSPinSHA256 != "" {
		pin, err := hex.DecodeString(cfg.TLSPinSHA256)
		if err != nil || len(pin) != 32 {
			return nil, errors.New("TLS 证书指纹必须为 64 位 SHA-256 十六进制")
		}
		// An explicit certificate pin replaces CA validation, never authentication.
		tc.InsecureSkipVerify = true
		tc.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("peer certificate missing")
			}
			cert := cs.PeerCertificates[0]
			hash := sha256.Sum256(cert.Raw)
			if subtle.ConstantTimeCompare(pin, hash[:]) != 1 || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
				return errors.New("peer certificate pin or validity rejected")
			}
			return nil
		}
	}
	return tc, nil
}

func (m *CenterPeerManager) SetConfig(cfg CenterPeerConfig) error {
	if err := cfg.Link.Validate(); err != nil {
		return err
	}
	if cfg.Link.CredentialEpoch == 0 {
		return errors.New("credential epoch must be positive")
	}
	active := cfg.Link.Enabled && cfg.Link.Accepted && cfg.Link.ForwardAudio
	initiating := cfg.Link.LocalCenterID == cfg.Link.InitiatorCenterID
	if active && initiating {
		if len(cfg.Credential) < 32 {
			return errors.New("互联凭据至少为 32 字节")
		}
		if _, err := PeerTLSConfig(cfg); err != nil {
			return err
		}
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("peer runtime closed")
	}
	old := m.entries[cfg.Link.LinkID]
	if old != nil && reflect.DeepEqual(old.cfg, cfg) {
		m.mu.Unlock()
		return nil
	}
	status := CenterPeerStatus{Role: "acceptor", VirtualDeviceID: CenterPeerVirtualDeviceID(cfg.Link.RemoteCenterID, cfg.Link.LinkID, cfg.Link.Direction)}
	if initiating {
		status.Role = "initiator"
	}
	if old != nil {
		status.InPackets = old.status.InPackets
		status.InBytes = old.status.InBytes
		status.OutPackets = old.status.OutPackets
		status.OutBytes = old.status.OutBytes
		status.DroppedPackets = old.status.DroppedPackets
	}
	entry := &peerEntry{cfg: cfg, status: status}
	m.entries[cfg.Link.LinkID] = entry
	if old != nil {
		if old.cancel != nil {
			old.cancel()
		}
		if old.connection != nil {
			old.connection.close()
		}
	}
	if initiating && active {
		ctx, cancel := context.WithCancel(context.Background())
		entry.cancel = cancel
		m.wg.Add(1)
		go m.connectLoop(ctx, entry)
	}
	m.mu.Unlock()
	return nil
}
func (m *CenterPeerManager) RemoveLink(id string) {
	m.mu.Lock()
	e := m.entries[id]
	delete(m.entries, id)
	if e != nil {
		if e.cancel != nil {
			e.cancel()
		}
		if e.connection != nil {
			e.connection.close()
		}
	}
	m.mu.Unlock()
}
func (m *CenterPeerManager) Status(id string) CenterPeerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e := m.entries[id]
	if e == nil {
		return CenterPeerStatus{}
	}
	s := e.status
	s.Online = e.connection != nil && e.connection.ready.Load()
	return s
}
func (m *CenterPeerManager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, e := range m.entries {
		if e.cancel != nil {
			e.cancel()
		}
		if e.connection != nil {
			e.connection.close()
		}
	}
	m.mu.Unlock()
	m.wg.Wait()
}
func (m *CenterPeerManager) connectLoop(ctx context.Context, e *peerEntry) {
	defer m.wg.Done()
	delay := time.Second
	for ctx.Err() == nil {
		tc, _ := PeerTLSConfig(e.cfg)
		dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		gate := make(chan struct{})
		var connection *peerConnection
		client, err := DialNode(dialCtx, NodeClientConfig{CenterAddr: e.cfg.RemoteAddress, TLSConfig: tc, NodeID: CenterPeerTransportID(e.cfg.Link.LinkID), Token: e.cfg.Credential, Capabilities: NodeCapabilities{Features: NodeFeatureCenterPeer, RequiredFeatures: NodeFeatureCenterPeer}, OnEnvelope: func(env Envelope) {
			select {
			case <-gate:
			case <-ctx.Done():
				return
			}
			if connection != nil {
				m.handle(e, connection, env)
			}
		}})
		cancel()
		if err == nil && client.CredentialEpoch != e.cfg.Link.CredentialEpoch {
			_ = client.Close()
			err = errors.New("peer credential epoch differs")
		}
		if err == nil {
			connection = &peerConnection{session: client.Session, client: client, queue: make(chan peerAudioJob, CenterPeerQueueSize), done: make(chan struct{})}
			m.mu.Lock()
			if m.entries[e.cfg.Link.LinkID] != e || m.closed || ctx.Err() != nil {
				m.mu.Unlock()
				connection.close()
				close(gate)
				return
			}
			e.connection = connection
			m.wg.Add(1)
			go m.connectionWorker(e, connection)
			m.mu.Unlock()
			close(gate)
			err = m.sendHello(e, connection)
			if err == nil {
				select {
				case <-client.Done():
				case <-ctx.Done():
				case <-connection.done:
				}
				err = errors.New("peer connection closed")
			}
			connection.close()
			m.detach(e, connection, err)
			delay = time.Second
		} else {
			close(gate)
			m.mu.Lock()
			if m.entries[e.cfg.Link.LinkID] == e {
				e.status.LastError = "连接或认证失败，请检查地址、TLS 与双方凭据"
			}
			m.mu.Unlock()
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
		delay = min(delay*2, 10*time.Second)
	}
}
func (m *CenterPeerManager) AcceptSession(session *NodeSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[session.PeerLinkID]
	if m.closed || e == nil || !e.cfg.Link.Enabled || !e.cfg.Link.Accepted || !e.cfg.Link.ForwardAudio || e.cfg.Link.InitiatorCenterID != e.cfg.Link.RemoteCenterID || session.CredentialEpoch != e.cfg.Link.CredentialEpoch {
		_ = session.Close()
		return
	}
	if e.connection != nil {
		e.connection.close()
	}
	c := &peerConnection{session: session, queue: make(chan peerAudioJob, CenterPeerQueueSize), done: make(chan struct{})}
	e.connection = c
	m.wg.Add(1)
	go m.connectionWorker(e, c)
}
func (m *CenterPeerManager) DetachSession(s *NodeSession) {
	m.mu.RLock()
	e := m.entries[s.PeerLinkID]
	var c *peerConnection
	if e != nil && e.connection != nil && e.connection.session == s {
		c = e.connection
	}
	m.mu.RUnlock()
	if c != nil {
		c.close()
		m.detach(e, c, errors.New("peer disconnected"))
	}
}
func (m *CenterPeerManager) detach(e *peerEntry, c *peerConnection, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries[e.cfg.Link.LinkID] == e && e.connection == c {
		e.connection = nil
		if e.status.LastError == "" {
			e.status.LastError = "互联已断开，等待重连"
		}
	}
}
func (m *CenterPeerManager) HandleEnvelope(s *NodeSession, env Envelope) {
	m.mu.RLock()
	e := m.entries[s.PeerLinkID]
	var c *peerConnection
	if e != nil {
		c = e.connection
	}
	m.mu.RUnlock()
	if c == nil || c.session != s {
		return
	}
	m.handle(e, c, env)
}
func (m *CenterPeerManager) sendHello(e *peerEntry, c *peerConnection) error {
	payload, _ := EncodeJSON(e.cfg.Link.Hello())
	env := NewEnvelope(SubtypeCenterPeerHello, "", c.session.SessionID, c.next.Add(1), payload)
	env.Flags = FlagControl | FlagCritical
	return c.send(env)
}
func (m *CenterPeerManager) handle(e *peerEntry, c *peerConnection, env Envelope) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries[e.cfg.Link.LinkID] != e || e.connection != c {
		return
	}
	if env.HopCount != 0 || env.Duplicate {
		e.status.DroppedPackets++
		return
	}
	if env.Subtype == SubtypeCenterPeerHello {
		var hello CenterPeerHello
		if DecodeJSON(env.Payload, &hello) != nil || !e.cfg.Link.MatchesHello(hello) {
			e.status.LastError = "双方中心身份、群组映射、方向或凭据代数不一致"
			c.close()
			return
		}
		if !c.ready.Swap(true) {
			now := time.Now()
			e.status.LastConnectedAt = &now
			e.status.LastError = ""
			if c.client == nil {
				if err := m.sendHello(e, c); err != nil {
					c.close()
				}
			}
		}
		return
	}
	if env.Subtype != SubtypeCenterPeerRelay || !c.ready.Load() {
		e.status.DroppedPackets++
		return
	}
	var f CenterPeerFrame
	if DecodeJSON(env.Payload, &f) != nil || f.Validate(time.Now(), CenterPeerMaxAge) != nil || f.Sequence != env.MessageID || f.MessageID != fmt.Sprintf("%016x:%016x", c.session.SessionID, f.Sequence) || f.OriginCenterID != e.cfg.Link.RemoteCenterID || f.OriginLinkID != e.cfg.Link.LinkID || f.CredentialEpoch != e.cfg.Link.CredentialEpoch || f.TargetGroupID != e.cfg.Link.LocalGroupID || f.OriginGroupID != e.cfg.Link.RemoteGroupID || !e.cfg.Link.AllowsOrigin(f.OriginCenterID) {
		e.status.DroppedPackets++
		return
	}
	// Injection is bounded in-process fan-out; it never calls RelayGroup.
	now := time.Now()
	if now.Sub(c.rateStart) >= time.Second {
		c.rateStart = now
		c.rateCount = 0
	}
	c.rateCount++
	if f.Sequence <= c.lastReceived || c.rateCount > 100 {
		e.status.DroppedPackets++
		return
	}
	c.lastReceived = f.Sequence
	if m.receive == nil || !m.receive(e.cfg.Link, f.Payload) {
		e.status.DroppedPackets++
		return
	}
	e.status.InPackets++
	e.status.InBytes += uint64(len(f.Payload))
}
func (m *CenterPeerManager) RelayGroup(groupID int, data []byte) error {
	if protocol.ValidateRelayInnerPacket(data) != nil || data[48] != protocol.DraARLTypeOpus16K || len(data) <= protocol.DraARLv1HeaderSize {
		return nil
	}
	// Only the media body leaves this centre.
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		c := e.connection
		if e.cfg.Link.LocalGroupID != groupID || !e.cfg.Link.AllowsOrigin(e.cfg.Link.LocalCenterID) || c == nil || !c.ready.Load() {
			continue
		}
		job := peerAudioJob{append([]byte(nil), data[protocol.DraARLv1HeaderSize:]...), time.Now()}
		select {
		case c.queue <- job:
		default:
			e.status.DroppedPackets++
		}
	}
	return nil
}
func (m *CenterPeerManager) connectionWorker(e *peerEntry, c *peerConnection) {
	defer m.wg.Done()
	defer c.close()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if !c.ready.Load() {
				return
			}
			if c.client != nil {
				if err := c.client.Send(ControlMessage{Kind: controlHeartbeat}); err != nil {
					return
				}
			}
			if c.client == nil {
				if err := c.session.Send(ControlMessage{Kind: controlHeartbeat}); err != nil {
					return
				}
			}
		case job := <-c.queue:
			m.mu.Lock()
			if m.entries[e.cfg.Link.LinkID] != e || e.connection != c || !c.ready.Load() {
				m.mu.Unlock()
				continue
			}
			if time.Since(job.at) > 200*time.Millisecond {
				e.status.DroppedPackets++
				m.mu.Unlock()
				continue
			}
			seq := c.next.Add(1)
			f, err := NewCenterPeerFrame(e.cfg.Link, CenterLinkMediaAudio, job.payload, seq, job.at)
			f.MessageID = fmt.Sprintf("%016x:%016x", c.session.SessionID, seq)
			m.mu.Unlock()
			if err != nil {
				continue
			}
			payload, _ := EncodeJSON(f)
			env := NewEnvelope(SubtypeCenterPeerRelay, "", c.session.SessionID, seq, payload)
			env.Flags = FlagControl | FlagCritical
			if err := c.send(env); err != nil {
				return
			}
			m.mu.Lock()
			e.status.OutPackets++
			e.status.OutBytes += uint64(len(job.payload))
			m.mu.Unlock()
		}
	}
}

// Fingerprint is public configuration, never a private key.
func CertificateFingerprint(tc *tls.Config) string {
	if tc == nil || len(tc.Certificates) == 0 || len(tc.Certificates[0].Certificate) == 0 {
		return ""
	}
	h := sha256.Sum256(tc.Certificates[0].Certificate[0])
	return strings.ToLower(hex.EncodeToString(h[:]))
}
