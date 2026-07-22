package interconnect

// This file implements the reliable Type 0 control plane.  It is deliberately
// a small length-prefixed protocol over TLS rather than an HTTP endpoint: the
// edge has no HTTP/database dependency and a single connection carries node
// registration, route snapshots, deltas, acknowledgements and heartbeats.

import (
	"context"
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
	controlMaxFrame  = 4 << 20
	controlHello     = "node_enroll"
	controlAuthOK    = "node_auth_ok"
	controlAuthError = "node_auth_error"
	controlHeartbeat = "node_heartbeat"
)

type ControlMessage struct {
	Kind      string          `json:"kind"`
	NodeID    string          `json:"node_id,omitempty"`
	Token     string          `json:"token,omitempty"`
	SessionID uint64          `json:"session_id,omitempty"`
	KeyEpoch  uint32          `json:"key_epoch,omitempty"`
	Key       string          `json:"key,omitempty"`
	MessageID uint64          `json:"message_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Packet    string          `json:"packet,omitempty"`
	Error     string          `json:"error,omitempty"`
}

func writeControlMessage(w io.Writer, msg ControlMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(data) > controlMaxFrame {
		return errors.New("control frame too large")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err = w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func readControlMessage(r io.Reader) (ControlMessage, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return ControlMessage{}, err
	}
	n := int(binary.BigEndian.Uint32(header[:]))
	if n <= 0 || n > controlMaxFrame {
		return ControlMessage{}, errors.New("invalid control frame length")
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return ControlMessage{}, err
	}
	var msg ControlMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return ControlMessage{}, fmt.Errorf("decode control message: %w", err)
	}
	return msg, nil
}

type NodeSession struct {
	NodeID        string
	SessionID     uint64
	KeyEpoch      uint32
	Key           []byte
	RemoteAddr    string
	ConnectedAt   time.Time
	LastHeartbeat atomic.Int64
	conn          net.Conn
	writeMu       sync.Mutex
	dataMu        sync.RWMutex
	dataAddr      *net.UDPAddr
}

func (s *NodeSession) Send(msg ControlMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeControlMessage(s.conn, msg)
}
func (s *NodeSession) Touch() { s.LastHeartbeat.Store(time.Now().UnixMilli()) }
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

type NodeServerConfig struct {
	ListenAddr    string
	TLSConfig     *tls.Config
	ValidateToken func(nodeID, token string) bool
	OnConnect     func(*NodeSession)
	OnMessage     func(*NodeSession, ControlMessage)
	OnEnvelope    func(*NodeSession, Envelope)
	OnDisconnect  func(*NodeSession, error)
}

type NodeServer struct {
	cfg       NodeServerConfig
	listener  net.Listener
	sessions  sync.Map
	closed    chan struct{}
	closeOnce sync.Once
}

func NewNodeServer(cfg NodeServerConfig) (*NodeServer, error) {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		return nil, errors.New("node control listen address is required")
	}
	if cfg.TLSConfig == nil {
		return nil, errors.New("node control TLS config is required")
	}
	if cfg.ValidateToken == nil {
		return nil, errors.New("node token validator is required")
	}
	return &NodeServer{cfg: cfg, closed: make(chan struct{})}, nil
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
		go s.handleConn(conn)
	}
}
func (s *NodeServer) handleConn(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	hello, err := readControlMessage(conn)
	if err != nil || hello.Kind != controlHello || hello.NodeID == "" || !s.cfg.ValidateToken(hello.NodeID, hello.Token) {
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
	session := &NodeSession{NodeID: hello.NodeID, SessionID: sid, KeyEpoch: 1, Key: key, RemoteAddr: conn.RemoteAddr().String(), ConnectedAt: time.Now(), conn: conn}
	session.Touch()
	response := ControlMessage{Kind: controlAuthOK, NodeID: session.NodeID, SessionID: sid, KeyEpoch: session.KeyEpoch, Key: base64.RawStdEncoding.EncodeToString(key)}
	if err := writeControlMessage(conn, response); err != nil {
		_ = conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	if old, loaded := s.sessions.LoadOrStore(session.NodeID, session); loaded {
		_ = old.(*NodeSession).Close()
	}
	if s.cfg.OnConnect != nil {
		s.cfg.OnConnect(session)
	}
	defer func() {
		if current, ok := s.sessions.Load(session.NodeID); ok && current == session {
			s.sessions.Delete(session.NodeID)
		}
		_ = conn.Close()
		if s.cfg.OnDisconnect != nil {
			s.cfg.OnDisconnect(session, err)
		}
	}()
	for {
		msg, readErr := readControlMessage(conn)
		if readErr != nil {
			err = readErr
			return
		}
		if msg.Kind == controlHeartbeat {
			session.Touch()
		}
		if msg.Kind == "type0" {
			wire, decodeErr := base64.RawStdEncoding.DecodeString(msg.Packet)
			if decodeErr != nil {
				continue
			}
			env, decodeErr := UnmarshalControl(wire, session.Key)
			if decodeErr != nil || env.NodeSessionID != session.SessionID || env.SourceNodeID != session.NodeID || env.Expired(time.Now(), 30*time.Second) {
				continue
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
func (s *NodeServer) Session(nodeID string) (*NodeSession, bool) {
	v, ok := s.sessions.Load(nodeID)
	if !ok {
		return nil, false
	}
	return v.(*NodeSession), true
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
	env.NodeSessionID, env.KeyEpoch = session.SessionID, session.KeyEpoch
	if env.SourceNodeID == "" {
		env.SourceNodeID = "center"
	}
	wire, err := env.MarshalControl(session.Key)
	if err != nil {
		return err
	}
	return session.Send(ControlMessage{Kind: "type0", Packet: base64.RawStdEncoding.EncodeToString(wire), MessageID: env.MessageID})
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
	DataAddr     string
	TLSConfig    *tls.Config
	NodeID       string
	Token        string
	OnMessage    func(ControlMessage)
	OnEnvelope   func(Envelope)
	OnDatagram   func(Envelope)
	OnDisconnect func(error)
}
type NodeClient struct {
	cfg        NodeClientConfig
	conn       net.Conn
	Session    *NodeSession
	closed     chan struct{}
	writeMu    sync.Mutex
	datagram   *NodeDatagramClient
	callbackMu sync.RWMutex
}

func DialNode(ctx context.Context, cfg NodeClientConfig) (*NodeClient, error) {
	if cfg.TLSConfig == nil {
		return nil, errors.New("node client TLS config is required")
	}
	dialer := &net.Dialer{}
	conn, err := tls.DialWithDialer(dialer, "tcp", cfg.CenterAddr, cfg.TLSConfig)
	if err != nil {
		return nil, err
	}
	client := &NodeClient{cfg: cfg, conn: conn, closed: make(chan struct{})}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := writeControlMessage(conn, ControlMessage{Kind: controlHello, NodeID: cfg.NodeID, Token: cfg.Token}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	response, err := readControlMessage(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if response.Kind != controlAuthOK {
		_ = conn.Close()
		return nil, fmt.Errorf("node authentication rejected: %s", response.Error)
	}
	key, err := base64.RawStdEncoding.DecodeString(response.Key)
	if err != nil || len(key) < 16 {
		_ = conn.Close()
		return nil, errors.New("invalid node session key")
	}
	_ = conn.SetDeadline(time.Time{})
	client.Session = &NodeSession{NodeID: response.NodeID, SessionID: response.SessionID, KeyEpoch: response.KeyEpoch, Key: key, RemoteAddr: conn.RemoteAddr().String(), ConnectedAt: time.Now(), conn: conn}
	client.Session.Touch()
	if cfg.DataAddr != "" {
		datagram, datagramErr := DialNodeDatagram(ctx, cfg.DataAddr, client.Session)
		if datagramErr != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("dial node data plane: %w", datagramErr)
		}
		client.datagram = datagram
		go client.datagramLoop()
	}
	go client.readLoop()
	return client, nil
}
func (c *NodeClient) readLoop() {
	var err error
	defer func() {
		close(c.closed)
		if c.cfg.OnDisconnect != nil {
			c.cfg.OnDisconnect(err)
		}
	}()
	for {
		msg, readErr := readControlMessage(c.conn)
		if readErr != nil {
			err = readErr
			return
		}
		if msg.Kind == controlHeartbeat {
			c.Session.Touch()
		}
		if msg.Kind == "type0" {
			wire, decodeErr := base64.RawStdEncoding.DecodeString(msg.Packet)
			if decodeErr != nil {
				continue
			}
			env, decodeErr := UnmarshalControl(wire, c.Session.Key)
			if decodeErr != nil || env.NodeSessionID != c.Session.SessionID || env.Expired(time.Now(), 30*time.Second) {
				continue
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
	return writeControlMessage(c.conn, msg)
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
	if (env.Subtype == SubtypeRelayUpstream || env.Subtype == SubtypeRelayDownstream) && c.datagram != nil {
		return c.datagram.Send(env)
	}
	return c.Send(ControlMessage{Kind: "type0", Packet: base64.RawStdEncoding.EncodeToString(wire), MessageID: env.MessageID})
}
func (c *NodeClient) datagramLoop() {
	for {
		env, _, err := c.datagram.Receive(context.Background())
		if err != nil {
			return
		}
		c.callbackMu.RLock()
		datagramHandler, envelopeHandler := c.cfg.OnDatagram, c.cfg.OnEnvelope
		c.callbackMu.RUnlock()
		if datagramHandler != nil {
			datagramHandler(env)
		} else if envelopeHandler != nil {
			envelopeHandler(env)
		}
	}
}
func (c *NodeClient) SetEnvelopeHandler(handler func(Envelope)) {
	c.callbackMu.Lock()
	c.cfg.OnEnvelope = handler
	c.callbackMu.Unlock()
}
func (c *NodeClient) SetDatagramHandler(handler func(Envelope)) {
	c.callbackMu.Lock()
	c.cfg.OnDatagram = handler
	c.callbackMu.Unlock()
}
func (c *NodeClient) Close() error {
	if c.conn == nil {
		return nil
	}
	if c.datagram != nil {
		_ = c.datagram.Close()
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
