package interconnect

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// NodeDatagramServer is the authenticated, lossy Type 0 data plane.  A
// session is first established over the TLS control plane; the UDP packet is
// accepted only when its session ID and HMAC key match that session.
type NodeDatagramServerConfig struct {
	ListenAddr string
	Sessions   func() []*NodeSession
	OnDatagram func(*NodeSession, Envelope, *net.UDPAddr)
	MaxAge     time.Duration
}

type NodeDatagramServer struct {
	cfg       NodeDatagramServerConfig
	conn      *net.UDPConn
	closed    chan struct{}
	closeOnce sync.Once
}

func NewNodeDatagramServer(cfg NodeDatagramServerConfig) (*NodeDatagramServer, error) {
	if cfg.ListenAddr == "" {
		return nil, errors.New("node data listen address is required")
	}
	if cfg.Sessions == nil || cfg.OnDatagram == nil {
		return nil, errors.New("node data callbacks are required")
	}
	return &NodeDatagramServer{cfg: cfg, closed: make(chan struct{})}, nil
}
func (s *NodeDatagramServer) Start() error {
	addr, err := net.ResolveUDPAddr("udp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	s.conn = conn
	go s.readLoop()
	return nil
}
func (s *NodeDatagramServer) Addr() net.Addr {
	if s.conn == nil {
		return nil
	}
	return s.conn.LocalAddr()
}
func (s *NodeDatagramServer) readLoop() {
	buf := make([]byte, NodeMaxDatagramSize)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			continue
		}
		data := append([]byte(nil), buf[:n]...)
		for _, session := range s.cfg.Sessions() {
			if session == nil || session.Key == nil {
				continue
			}
			env, decodeErr := Unmarshal(data, session.Key)
			if decodeErr != nil || env.NodeSessionID != session.SessionID || env.SourceNodeID != session.NodeID {
				continue
			}
			if env.Expired(time.Now(), s.cfg.MaxAge) {
				continue
			}
			session.BindDataAddr(addr)
			s.cfg.OnDatagram(session, env, addr)
			break
		}
	}
}
func (s *NodeDatagramServer) WriteTo(session *NodeSession, data []byte) error {
	if s.conn == nil || session == nil {
		return errors.New("node data server is not ready")
	}
	addr := session.DataAddr()
	if addr == nil {
		return errors.New("node data address is not bound")
	}
	_, err := s.conn.WriteToUDP(data, addr)
	return err
}
func (s *NodeDatagramServer) Send(session *NodeSession, env Envelope) error {
	if session == nil {
		return errors.New("node session is required")
	}
	data, err := env.Marshal(session.Key)
	if err != nil {
		return err
	}
	return s.WriteTo(session, data)
}
func (s *NodeDatagramServer) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.conn != nil {
			err = s.conn.Close()
		}
	})
	return err
}

type NodeDatagramClient struct {
	conn    *net.UDPConn
	center  *net.UDPAddr
	session *NodeSession
	keyMu   sync.RWMutex
}

func DialNodeDatagram(ctx context.Context, addr string, session *NodeSession) (*NodeDatagramClient, error) {
	if session == nil || len(session.Key) == 0 {
		return nil, errors.New("node data session is required")
	}
	center, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	return &NodeDatagramClient{conn: conn, center: center, session: session}, nil
}
func (c *NodeDatagramClient) LocalAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}
func (c *NodeDatagramClient) Send(env Envelope) error {
	c.keyMu.RLock()
	key := append([]byte(nil), c.session.Key...)
	c.keyMu.RUnlock()
	data, err := env.Marshal(key)
	if err != nil {
		return err
	}
	_, err = c.conn.WriteToUDP(data, c.center)
	return err
}
func (c *NodeDatagramClient) Receive(ctx context.Context) (Envelope, *net.UDPAddr, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(deadline)
	}
	buf := make([]byte, NodeMaxDatagramSize)
	for {
		n, addr, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			return Envelope{}, nil, err
		}
		c.keyMu.RLock()
		key := append([]byte(nil), c.session.Key...)
		c.keyMu.RUnlock()
		env, err := Unmarshal(buf[:n], key)
		if err == nil && env.NodeSessionID == c.session.SessionID && env.SourceNodeID != "" {
			return env, addr, nil
		}
	}
}
func (c *NodeDatagramClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
