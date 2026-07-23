package interconnect

// This file implements the reliable Type 0 control plane.  It is deliberately
// a small length-prefixed protocol over TLS rather than an HTTP endpoint: the
// edge has no HTTP/database dependency and a single connection carries node
// registration, route snapshots, deltas, acknowledgements and heartbeats.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	controlMaxFrame      = 4 << 20
	controlHelloMaxFrame = 16 << 10
	controlHello         = "node_enroll"
	controlAuthOK        = "node_auth_ok"
	controlAuthError     = "node_auth_error"
	controlProtocolError = "node_protocol_error"
	controlHeartbeat     = "node_heartbeat"
)

var ErrNodeAuthenticationRejected = errors.New("node authentication rejected")
var ErrNodeProtocolIncompatible = errors.New("node protocol incompatible")

type NodeCapabilities struct {
	MinProtocolVersion byte
	MaxProtocolVersion byte
	Features           uint64
	RequiredFeatures   uint64
}

func (c NodeCapabilities) normalized() (NodeCapabilities, error) {
	if c.MinProtocolVersion == 0 {
		c.MinProtocolVersion = NodeProtocolMinVersion
	}
	if c.MaxProtocolVersion == 0 {
		c.MaxProtocolVersion = NodeProtocolMaxVersion
	}
	if c.Features == 0 {
		c.Features = NodeSupportedFeatures
	}
	if c.RequiredFeatures == 0 {
		c.RequiredFeatures = NodeRequiredFeatures
	}
	if c.MinProtocolVersion > c.MaxProtocolVersion {
		return c, errors.New("minimum node protocol version exceeds maximum")
	}
	if c.RequiredFeatures & ^c.Features != 0 {
		return c, errors.New("required node features are not advertised")
	}
	return c, nil
}

func negotiateNodeCapabilities(server, client NodeCapabilities) (byte, uint64, error) {
	var err error
	if server, err = server.normalized(); err != nil {
		return 0, 0, err
	}
	if client, err = client.normalized(); err != nil {
		return 0, 0, err
	}
	minimum := server.MinProtocolVersion
	if client.MinProtocolVersion > minimum {
		minimum = client.MinProtocolVersion
	}
	maximum := server.MaxProtocolVersion
	if client.MaxProtocolVersion < maximum {
		maximum = client.MaxProtocolVersion
	}
	if minimum > maximum {
		return 0, 0, ErrNodeProtocolIncompatible
	}
	features := server.Features & client.Features
	if server.RequiredFeatures & ^features != 0 || client.RequiredFeatures & ^features != 0 {
		return 0, 0, ErrNodeProtocolIncompatible
	}
	return maximum, features, nil
}

type ControlMessage struct {
	Kind             string          `json:"kind"`
	NodeID           string          `json:"node_id,omitempty"`
	Token            string          `json:"token,omitempty"`
	Credential       string          `json:"credential,omitempty"`
	CredentialEpoch  uint32          `json:"credential_epoch,omitempty"`
	MinProtocol      byte            `json:"min_protocol,omitempty"`
	MaxProtocol      byte            `json:"max_protocol,omitempty"`
	Protocol         byte            `json:"protocol,omitempty"`
	Features         uint64          `json:"features,omitempty"`
	RequiredFeatures uint64          `json:"required_features,omitempty"`
	SessionID        uint64          `json:"session_id,omitempty"`
	KeyEpoch         uint32          `json:"key_epoch,omitempty"`
	Key              string          `json:"key,omitempty"`
	MessageID        uint64          `json:"message_id,omitempty"`
	Payload          json.RawMessage `json:"payload,omitempty"`
	Packet           string          `json:"packet,omitempty"`
	Error            string          `json:"error,omitempty"`
}

type NodeAuthentication struct {
	Accepted         bool
	IssuedCredential string
	CredentialEpoch  uint32
}

type NodeAuthenticationEvent struct {
	NodeID     string
	RemoteAddr string
	Accepted   bool
	Registered bool
	Reason     string
	Protocol   byte
	Features   uint64
}

func marshalControlMessage(msg ControlMessage) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if len(data) > controlMaxFrame {
		return nil, errors.New("control frame too large")
	}
	wire := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(wire[:4], uint32(len(data)))
	copy(wire[4:], data)
	return wire, nil
}

func writeControlMessage(w io.Writer, msg ControlMessage) error {
	wire, err := marshalControlMessage(msg)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, bytes.NewReader(wire))
	return err
}

func readControlMessage(r io.Reader) (ControlMessage, error) {
	msg, _, err := readControlMessageSize(r)
	return msg, err
}

func readControlMessageSize(r io.Reader) (ControlMessage, int, error) {
	return readControlMessageSizeLimit(r, controlMaxFrame)
}

func readControlMessageSizeLimit(r io.Reader, maxFrame int) (ControlMessage, int, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return ControlMessage{}, 0, err
	}
	n := int(binary.BigEndian.Uint32(header[:]))
	if n <= 0 || n > maxFrame {
		return ControlMessage{}, 4, errors.New("invalid control frame length")
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return ControlMessage{}, 4, err
	}
	var msg ControlMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return ControlMessage{}, 4 + n, fmt.Errorf("decode control message: %w", err)
	}
	return msg, 4 + n, nil
}

type NodeSession struct {
	NodeID                 string
	SessionID              uint64
	KeyEpoch               uint32
	Key                    []byte
	ProtocolVersion        byte
	Features               uint64
	RemoteAddr             string
	ConnectedAt            time.Time
	LastHeartbeat          atomic.Int64
	conn                   net.Conn
	writeMu                sync.Mutex
	dataMu                 sync.RWMutex
	dataAddr               *net.UDPAddr
	DataMetrics            Metrics
	ControlMetrics         Metrics
	AckedProjectionVersion atomic.Uint64
	replay                 replayWindow
	protectionOnce         sync.Once
	protection             *nodeProtection
	dataBindMu             sync.Mutex
	dataBindChallenge      [32]byte
	dataBindChallengeUntil time.Time
}

func (s *NodeSession) Send(msg ControlMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	wire, err := marshalControlMessage(msg)
	if err != nil {
		s.ControlMetrics.AddError()
		return err
	}
	if _, err = io.Copy(s.conn, bytes.NewReader(wire)); err != nil {
		s.ControlMetrics.AddError()
		return err
	}
	s.ControlMetrics.AddOut(len(wire))
	return nil
}

func (s *NodeSession) SendEnvelope(env Envelope) error {
	if s == nil {
		return errors.New("node session is unavailable")
	}
	env.NodeSessionID, env.KeyEpoch = s.SessionID, s.KeyEpoch
	if env.SourceNodeID == "" {
		env.SourceNodeID = "center"
	}
	wire, err := env.MarshalControl(s.Key)
	if err != nil {
		return err
	}
	return s.Send(ControlMessage{Kind: "type0", Packet: base64.RawStdEncoding.EncodeToString(wire), MessageID: env.MessageID})
}
func (s *NodeSession) Touch() { s.LastHeartbeat.Store(time.Now().UnixMilli()) }

// AcceptMessage rejects replays inside a fixed, allocation-free sliding
// window. The envelope timestamp independently rejects stale network traffic.
func (s *NodeSession) AcceptMessage(messageID uint64, now time.Time) bool {
	_ = now
	return s.replay.accept(messageID)
}

func (s *NodeSession) resourceProtection() *nodeProtection {
	s.protectionOnce.Do(func() {
		if s.protection == nil {
			s.protection = newNodeProtection(ResourceLimits{})
		}
	})
	return s.protection
}

func (s *NodeSession) ProtectionSnapshot() NodeProtectionSnapshot {
	if s == nil {
		return NodeProtectionSnapshot{}
	}
	return s.resourceProtection().snapshot()
}
func (s *NodeSession) BindDataAddr(addr *net.UDPAddr) {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	if addr == nil {
		s.dataAddr = nil
		return
	}
	copyAddr := *addr
	copyAddr.IP = append(net.IP(nil), addr.IP...)
	s.dataAddr = &copyAddr
}
func (s *NodeSession) DataAddr() *net.UDPAddr {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()
	if s.dataAddr == nil {
		return nil
	}
	copyAddr := *s.dataAddr
	copyAddr.IP = append(net.IP(nil), s.dataAddr.IP...)
	return &copyAddr
}

func (s *NodeSession) DataAddrMatches(addr *net.UDPAddr) bool {
	if s == nil || addr == nil {
		return false
	}
	s.dataMu.RLock()
	bound := s.dataAddr
	matches := bound != nil && bound.Port == addr.Port && bound.Zone == addr.Zone && bound.IP.Equal(addr.IP)
	s.dataMu.RUnlock()
	return matches
}

func (s *NodeSession) IssueDataBindChallenge(now time.Time) ([]byte, error) {
	if s == nil {
		return nil, errors.New("node session is required")
	}
	var challenge [32]byte
	if _, err := rand.Read(challenge[:]); err != nil {
		return nil, err
	}
	s.dataBindMu.Lock()
	s.dataBindChallenge = challenge
	s.dataBindChallengeUntil = now.Add(10 * time.Second)
	s.dataBindMu.Unlock()
	return append([]byte(nil), challenge[:]...), nil
}

func (s *NodeSession) ConsumeDataBindChallenge(challenge []byte, now time.Time) bool {
	if s == nil || len(challenge) != len(s.dataBindChallenge) {
		return false
	}
	s.dataBindMu.Lock()
	defer s.dataBindMu.Unlock()
	valid := !s.dataBindChallengeUntil.IsZero() && !now.After(s.dataBindChallengeUntil) &&
		hmac.Equal(challenge, s.dataBindChallenge[:])
	clear(s.dataBindChallenge[:])
	s.dataBindChallengeUntil = time.Time{}
	return valid
}

type NodeServerConfig struct {
	ListenAddr       string
	TLSConfig        *tls.Config
	ValidateToken    func(nodeID, token string) bool
	Authenticate     func(nodeID, token string) (NodeAuthentication, error)
	OnConnect        func(*NodeSession)
	OnMessage        func(*NodeSession, ControlMessage)
	OnEnvelope       func(*NodeSession, Envelope)
	OnDisconnect     func(*NodeSession, error)
	OnAuthentication func(NodeAuthenticationEvent)
	ResourceLimits   ResourceLimits
	Capabilities     NodeCapabilities
}

func (s *NodeServer) emitAuthentication(event NodeAuthenticationEvent) {
	if s != nil && s.cfg.OnAuthentication != nil {
		s.cfg.OnAuthentication(event)
	}
}

type NodeServer struct {
	cfg          NodeServerConfig
	listener     net.Listener
	sessions     sync.Map
	closed       chan struct{}
	closeOnce    sync.Once
	limits       ResourceLimits
	pending      atomic.Int64
	sessionMu    sync.Mutex
	active       int
	occupied     int
	reserved     map[string]int
	attempts     *handshakeLimiter
	protection   NodeServerProtection
	capabilities NodeCapabilities
}

type NodeServerProtection struct {
	PendingRejected         atomic.Uint64
	AuthRateRejected        atomic.Uint64
	AuthFailed              atomic.Uint64
	MaxNodesRejected        atomic.Uint64
	ProtocolRejected        atomic.Uint64
	UnsupportedSubtypeDrops atomic.Uint64
}

type NodeServerProtectionSnapshot struct {
	PendingHandshakes       int64  `json:"pending_handshakes"`
	ActiveNodes             int    `json:"active_nodes"`
	PendingRejected         uint64 `json:"pending_rejected"`
	AuthRateRejected        uint64 `json:"auth_rate_rejected"`
	AuthFailed              uint64 `json:"auth_failed"`
	MaxNodesRejected        uint64 `json:"max_nodes_rejected"`
	ProtocolRejected        uint64 `json:"protocol_rejected"`
	UnsupportedSubtypeDrops uint64 `json:"unsupported_subtype_drops"`
}

func NewNodeServer(cfg NodeServerConfig) (*NodeServer, error) {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		return nil, errors.New("node control listen address is required")
	}
	if cfg.TLSConfig == nil {
		return nil, errors.New("node control TLS config is required")
	}
	if cfg.ValidateToken == nil && cfg.Authenticate == nil {
		return nil, errors.New("node token validator is required")
	}
	limits, err := cfg.ResourceLimits.normalized()
	if err != nil {
		return nil, err
	}
	capabilities, err := cfg.Capabilities.normalized()
	if err != nil {
		return nil, err
	}
	return &NodeServer{cfg: cfg, limits: limits, capabilities: capabilities, attempts: newHandshakeLimiter(limits.AuthAttemptsPerMinutePerIP), reserved: make(map[string]int), closed: make(chan struct{})}, nil
}

func (s *NodeServer) Start() error {
	listener, err := tls.Listen("tcp", s.cfg.ListenAddr, s.cfg.TLSConfig)
	if err != nil {
		return err
	}
	s.listener = listener
	go s.acceptLoop()
	return nil
}
func (s *NodeServer) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}
func (s *NodeServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			continue
		}
		if s.pending.Add(1) > int64(s.limits.MaxPendingHandshakes) {
			s.pending.Add(-1)
			s.protection.PendingRejected.Add(1)
			_ = conn.Close()
			continue
		}
		go s.handleConn(conn)
	}
}
func (s *NodeServer) handleConn(conn net.Conn) {
	pending := true
	releasePending := func() {
		if pending {
			pending = false
			s.pending.Add(-1)
		}
	}
	defer releasePending()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	hello, helloBytes, err := readControlMessageSizeLimit(conn, controlHelloMaxFrame)
	authentication := NodeAuthentication{}
	validHello := err == nil && hello.Kind == controlHello && hello.NodeID != "" && hello.NodeID != CenterLocalNodeID
	protocolVersion, features, negotiationErr := negotiateNodeCapabilities(s.capabilities, NodeCapabilities{
		MinProtocolVersion: hello.MinProtocol, MaxProtocolVersion: hello.MaxProtocol,
		Features: hello.Features, RequiredFeatures: hello.RequiredFeatures,
	})
	if validHello && negotiationErr != nil {
		s.protection.ProtocolRejected.Add(1)
		s.emitAuthentication(NodeAuthenticationEvent{NodeID: hello.NodeID, RemoteAddr: conn.RemoteAddr().String(), Reason: "protocol_incompatible"})
		_ = writeControlMessage(conn, ControlMessage{Kind: controlProtocolError, Error: "node protocol negotiation failed"})
		_ = conn.Close()
		return
	}
	rateRejected := false
	if validHello {
		if !s.attempts.allow(conn.RemoteAddr(), time.Now()) {
			rateRejected = true
			s.protection.AuthRateRejected.Add(1)
		} else if s.cfg.Authenticate != nil {
			authentication, err = s.cfg.Authenticate(hello.NodeID, hello.Token)
		} else if s.cfg.ValidateToken != nil {
			authentication.Accepted = s.cfg.ValidateToken(hello.NodeID, hello.Token)
		}
	}
	if err != nil || !validHello || !authentication.Accepted {
		if !rateRejected {
			s.protection.AuthFailed.Add(1)
		}
		reason := "credential_rejected"
		if !validHello {
			reason = "invalid_hello"
		} else if rateRejected {
			reason = "rate_limited"
		}
		s.emitAuthentication(NodeAuthenticationEvent{NodeID: hello.NodeID, RemoteAddr: conn.RemoteAddr().String(), Reason: reason, Protocol: protocolVersion, Features: features})
		_ = writeControlMessage(conn, ControlMessage{Kind: controlAuthError, Error: "node authentication failed"})
		_ = conn.Close()
		return
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		_ = conn.Close()
		return
	}
	sid := randomUint64()
	session := &NodeSession{NodeID: hello.NodeID, SessionID: sid, KeyEpoch: 1, Key: key, ProtocolVersion: protocolVersion, Features: features, RemoteAddr: conn.RemoteAddr().String(), ConnectedAt: time.Now(), conn: conn, protection: newNodeProtection(s.limits)}
	session.Touch()
	if !s.reserveSession(session.NodeID) {
		s.protection.MaxNodesRejected.Add(1)
		s.emitAuthentication(NodeAuthenticationEvent{NodeID: session.NodeID, RemoteAddr: session.RemoteAddr, Reason: "node_capacity", Protocol: protocolVersion, Features: features})
		_ = writeControlMessage(conn, ControlMessage{Kind: controlAuthError, Error: "node capacity reached"})
		_ = conn.Close()
		return
	}
	reserved := true
	defer func() {
		if reserved {
			s.releaseReservation(session.NodeID)
		}
	}()
	response := ControlMessage{Kind: controlAuthOK, NodeID: session.NodeID, SessionID: sid, KeyEpoch: session.KeyEpoch, Key: base64.RawStdEncoding.EncodeToString(key), Credential: authentication.IssuedCredential, CredentialEpoch: authentication.CredentialEpoch, Protocol: protocolVersion, Features: features}
	responseWire, err := marshalControlMessage(response)
	if err != nil {
		_ = conn.Close()
		return
	}
	if _, err := io.Copy(conn, bytes.NewReader(responseWire)); err != nil {
		_ = conn.Close()
		return
	}
	session.ControlMetrics.AddIn(helloBytes)
	session.ControlMetrics.AddOut(len(responseWire))
	_ = conn.SetDeadline(time.Time{})
	old, loaded, installed := s.installReservedSession(session)
	if !installed {
		_ = conn.Close()
		return
	}
	reserved = false
	releasePending()
	if loaded {
		if s.cfg.OnConnect != nil {
			s.cfg.OnConnect(session)
		}
		_ = old.Close()
	} else if s.cfg.OnConnect != nil {
		s.cfg.OnConnect(session)
	}
	s.emitAuthentication(NodeAuthenticationEvent{NodeID: session.NodeID, RemoteAddr: session.RemoteAddr, Accepted: true, Registered: authentication.IssuedCredential != "", Reason: "accepted", Protocol: protocolVersion, Features: features})
	defer func() {
		s.removeSession(session)
		_ = conn.Close()
		if s.cfg.OnDisconnect != nil {
			s.cfg.OnDisconnect(session, err)
		}
	}()
	for {
		msg, frameBytes, readErr := readControlMessageSize(conn)
		if readErr != nil {
			err = readErr
			return
		}
		session.ControlMetrics.AddIn(frameBytes)
		now := time.Now()
		if !session.resourceProtection().allowControl(frameBytes, now) {
			session.ControlMetrics.AddDrop()
			continue
		}
		if msg.Kind == controlHeartbeat {
			session.Touch()
		}
		if msg.Kind == controlProtocolError {
			err = ErrNodeProtocolIncompatible
			return
		}
		if msg.Kind == "type0" {
			wire, decodeErr := base64.RawStdEncoding.DecodeString(msg.Packet)
			if decodeErr != nil {
				continue
			}
			env, decodeErr := UnmarshalControl(wire, session.Key)
			if decodeErr != nil {
				session.resourceProtection().recordInvalidAuthTag()
				continue
			}
			if env.NodeSessionID != session.SessionID || env.KeyEpoch != session.KeyEpoch || env.SourceNodeID != session.NodeID {
				session.resourceProtection().recordIdentityReject()
				continue
			}
			if !IsKnownSubtype(env.Subtype) {
				s.protection.UnsupportedSubtypeDrops.Add(1)
				session.ControlMetrics.AddDrop()
				if env.Flags&FlagCritical != 0 {
					_ = session.Send(ControlMessage{Kind: controlProtocolError, Error: "unsupported critical Type 0 subtype"})
					err = ErrNodeProtocolIncompatible
					return
				}
				continue
			}
			if env.Expired(now, 30*time.Second) {
				session.resourceProtection().recordExpiredDrop()
				continue
			}
			if env.Subtype == SubtypeDeviceAuth && !session.resourceProtection().allowDeviceAuth(now) {
				session.ControlMetrics.AddDrop()
				continue
			}
			if !session.AcceptMessage(env.MessageID, now) {
				if env.Subtype != SubtypeDeviceConfig {
					session.resourceProtection().recordReplayDrop()
					continue
				}
				env.Duplicate = true
			}
			session.Touch()
			if s.cfg.OnEnvelope != nil {
				s.cfg.OnEnvelope(session, env)
			}
			continue
		}
		if s.cfg.OnMessage != nil {
			s.cfg.OnMessage(session, msg)
		}
	}
}

func (s *NodeServer) reserveSession(nodeID string) bool {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	_, active := s.sessions.Load(nodeID)
	if !active && s.reserved[nodeID] == 0 {
		if s.occupied >= s.limits.MaxNodes {
			return false
		}
		s.occupied++
	}
	s.reserved[nodeID]++
	return true
}

func (s *NodeServer) releaseReservation(nodeID string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.reserved[nodeID] <= 1 {
		delete(s.reserved, nodeID)
		if _, active := s.sessions.Load(nodeID); !active && s.occupied > 0 {
			s.occupied--
		}
		return
	}
	s.reserved[nodeID]--
}

func (s *NodeServer) installReservedSession(session *NodeSession) (old *NodeSession, loaded, installed bool) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.reserved[session.NodeID] == 0 {
		return nil, false, false
	}
	if value, ok := s.sessions.Load(session.NodeID); ok {
		old = value.(*NodeSession)
		loaded = true
	} else {
		s.active++
	}
	s.sessions.Store(session.NodeID, session)
	if s.reserved[session.NodeID] == 1 {
		delete(s.reserved, session.NodeID)
	} else {
		s.reserved[session.NodeID]--
	}
	return old, loaded, true
}

func (s *NodeServer) removeSession(session *NodeSession) bool {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	current, ok := s.sessions.Load(session.NodeID)
	if !ok || current != session {
		return false
	}
	s.sessions.Delete(session.NodeID)
	if s.active > 0 {
		s.active--
	}
	if s.reserved[session.NodeID] == 0 && s.occupied > 0 {
		s.occupied--
	}
	return true
}

func (s *NodeServer) ProtectionSnapshot() NodeServerProtectionSnapshot {
	if s == nil {
		return NodeServerProtectionSnapshot{}
	}
	s.sessionMu.Lock()
	active := s.active
	s.sessionMu.Unlock()
	return NodeServerProtectionSnapshot{
		PendingHandshakes: s.pending.Load(), ActiveNodes: active,
		PendingRejected: s.protection.PendingRejected.Load(), AuthRateRejected: s.protection.AuthRateRejected.Load(),
		AuthFailed: s.protection.AuthFailed.Load(), MaxNodesRejected: s.protection.MaxNodesRejected.Load(),
		ProtocolRejected: s.protection.ProtocolRejected.Load(), UnsupportedSubtypeDrops: s.protection.UnsupportedSubtypeDrops.Load(),
	}
}
func (s *NodeServer) Session(nodeID string) (*NodeSession, bool) {
	v, ok := s.sessions.Load(nodeID)
	if !ok {
		return nil, false
	}
	return v.(*NodeSession), true
}

// SessionByID is used by the UDP data plane to avoid trying every connected
// node's HMAC key for each datagram. The session ID is random per TLS
// connection, so it is a safe first-stage lookup key; the decoder still
// verifies the source NodeID and authentication tag afterwards.
func (s *NodeServer) SessionByID(nodeID string, sessionID uint64) *NodeSession {
	if nodeID != "" {
		if session, ok := s.Session(nodeID); ok && session.SessionID == sessionID {
			return session
		}
		return nil
	}
	var result *NodeSession
	s.sessions.Range(func(_, value any) bool {
		session := value.(*NodeSession)
		if session.SessionID == sessionID {
			result = session
			return false
		}
		return true
	})
	return result
}
func (s *NodeServer) Sessions() []*NodeSession {
	var result []*NodeSession
	s.sessions.Range(func(_, value any) bool { result = append(result, value.(*NodeSession)); return true })
	return result
}
func (s *NodeServer) Send(nodeID string, msg ControlMessage) error {
	session, ok := s.Session(nodeID)
	if !ok {
		return errors.New("node is offline")
	}
	return session.Send(msg)
}
func (s *NodeServer) SendEnvelope(nodeID string, env Envelope) error {
	session, ok := s.Session(nodeID)
	if !ok {
		return errors.New("node is offline")
	}
	return session.SendEnvelope(env)
}
func (s *NodeServer) Disconnect(nodeID string) bool {
	session, ok := s.Session(nodeID)
	if !ok {
		return false
	}
	_ = session.Close()
	return true
}
func (s *NodeServer) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.listener != nil {
			err = s.listener.Close()
		}
		s.sessions.Range(func(_, value any) bool { _ = value.(*NodeSession).Close(); return true })
	})
	return err
}
func (s *NodeSession) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

type NodeClientConfig struct {
	CenterAddr   string
	TLSConfig    *tls.Config
	NodeID       string
	Token        string
	OnMessage    func(ControlMessage)
	OnEnvelope   func(Envelope)
	OnDisconnect func(error)
	Capabilities NodeCapabilities
}
type NodeClient struct {
	cfg              NodeClientConfig
	conn             net.Conn
	Session          *NodeSession
	closed           chan struct{}
	writeMu          sync.Mutex
	callbackMu       sync.RWMutex
	IssuedCredential string
	CredentialEpoch  uint32
}

func DialNode(ctx context.Context, cfg NodeClientConfig) (*NodeClient, error) {
	if cfg.TLSConfig == nil {
		return nil, errors.New("node client TLS config is required")
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{}, Config: cfg.TLSConfig}
	conn, err := dialer.DialContext(ctx, "tcp", cfg.CenterAddr)
	if err != nil {
		return nil, err
	}
	capabilities, err := cfg.Capabilities.normalized()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	client := &NodeClient{cfg: cfg, conn: conn, closed: make(chan struct{})}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	helloWire, err := marshalControlMessage(ControlMessage{Kind: controlHello, NodeID: cfg.NodeID, Token: cfg.Token, MinProtocol: capabilities.MinProtocolVersion, MaxProtocol: capabilities.MaxProtocolVersion, Features: capabilities.Features, RequiredFeatures: capabilities.RequiredFeatures})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := io.Copy(conn, bytes.NewReader(helloWire)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	response, responseBytes, err := readControlMessageSize(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if response.Kind == controlProtocolError {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %s", ErrNodeProtocolIncompatible, response.Error)
	}
	if response.Kind != controlAuthOK {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %s", ErrNodeAuthenticationRejected, response.Error)
	}
	key, err := base64.RawStdEncoding.DecodeString(response.Key)
	if err != nil || len(key) < 16 {
		_ = conn.Close()
		return nil, errors.New("invalid node session key")
	}
	if response.Protocol < capabilities.MinProtocolVersion || response.Protocol > capabilities.MaxProtocolVersion ||
		response.Features&^capabilities.Features != 0 || capabilities.RequiredFeatures&^response.Features != 0 {
		_ = conn.Close()
		return nil, ErrNodeProtocolIncompatible
	}
	_ = conn.SetDeadline(time.Time{})
	client.Session = &NodeSession{NodeID: response.NodeID, SessionID: response.SessionID, KeyEpoch: response.KeyEpoch, Key: key, ProtocolVersion: response.Protocol, Features: response.Features, RemoteAddr: conn.RemoteAddr().String(), ConnectedAt: time.Now(), conn: conn}
	client.Session.ControlMetrics.AddOut(len(helloWire))
	client.Session.ControlMetrics.AddIn(responseBytes)
	client.IssuedCredential = response.Credential
	client.CredentialEpoch = response.CredentialEpoch
	client.Session.Touch()
	go client.readLoop()
	return client, nil
}
func (c *NodeClient) readLoop() {
	var err error
	defer func() {
		close(c.closed)
		c.callbackMu.RLock()
		handler := c.cfg.OnDisconnect
		c.callbackMu.RUnlock()
		if handler != nil {
			handler(err)
		}
	}()
	for {
		msg, frameBytes, readErr := readControlMessageSize(c.conn)
		if readErr != nil {
			err = readErr
			return
		}
		c.Session.ControlMetrics.AddIn(frameBytes)
		if msg.Kind == controlHeartbeat {
			c.Session.Touch()
		}
		if msg.Kind == controlProtocolError {
			err = fmt.Errorf("%w: %s", ErrNodeProtocolIncompatible, msg.Error)
			return
		}
		if msg.Kind == "type0" {
			wire, decodeErr := base64.RawStdEncoding.DecodeString(msg.Packet)
			if decodeErr != nil {
				continue
			}
			env, decodeErr := UnmarshalControl(wire, c.Session.Key)
			now := time.Now()
			if decodeErr != nil || env.NodeSessionID != c.Session.SessionID || env.KeyEpoch != c.Session.KeyEpoch || env.SourceNodeID != "center" || env.Expired(now, 30*time.Second) {
				continue
			}
			if !IsKnownSubtype(env.Subtype) {
				c.Session.ControlMetrics.AddDrop()
				if env.Flags&FlagCritical != 0 {
					_ = c.Send(ControlMessage{Kind: controlProtocolError, Error: "unsupported critical Type 0 subtype"})
					err = ErrNodeProtocolIncompatible
					return
				}
				continue
			}
			if !c.Session.AcceptMessage(env.MessageID, now) {
				if env.Subtype != SubtypeRouteDelta && env.Subtype != SubtypeRouteSnapshotCommit && env.Subtype != SubtypeDeviceConfig {
					continue
				}
				env.Duplicate = true
			}
			c.Session.Touch()
			c.callbackMu.RLock()
			handler := c.cfg.OnEnvelope
			c.callbackMu.RUnlock()
			if handler != nil {
				handler(env)
			}
			continue
		}
		c.callbackMu.RLock()
		handler := c.cfg.OnMessage
		c.callbackMu.RUnlock()
		if handler != nil {
			handler(msg)
		}
	}
}
func (c *NodeClient) Send(msg ControlMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	wire, err := marshalControlMessage(msg)
	if err != nil {
		c.Session.ControlMetrics.AddError()
		return err
	}
	if _, err = io.Copy(c.conn, bytes.NewReader(wire)); err != nil {
		c.Session.ControlMetrics.AddError()
		return err
	}
	c.Session.ControlMetrics.AddOut(len(wire))
	return nil
}
func (c *NodeClient) SendEnvelope(env Envelope) error {
	if c.Session == nil {
		return errors.New("node client is not authenticated")
	}
	env.SourceNodeID, env.NodeSessionID, env.KeyEpoch = c.Session.NodeID, c.Session.SessionID, c.Session.KeyEpoch
	wire, err := env.MarshalControl(c.Session.Key)
	if err != nil {
		return err
	}
	return c.Send(ControlMessage{Kind: "type0", Packet: base64.RawStdEncoding.EncodeToString(wire), MessageID: env.MessageID})
}
func (c *NodeClient) SetEnvelopeHandler(handler func(Envelope)) {
	c.callbackMu.Lock()
	c.cfg.OnEnvelope = handler
	c.callbackMu.Unlock()
}
func (c *NodeClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
func (c *NodeClient) Done() <-chan struct{} { return c.closed }

func randomUint64() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint64(b[:])
}

// NewSelfSignedTLSConfig is intended for local development and tests.
// Production should load a certificate issued by the deployment CA.
func NewSelfSignedTLSConfig(serverName string) (*tls.Config, *x509.CertPool, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: serverName}, DNSNames: []string{serverName, "localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	certPEM := pemEncode("CERTIFICATE", der)
	keyPEM := pemEncode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, ClientAuth: tls.NoClientCert}, pool, nil
}
func pemEncode(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}
