package interconnect

// HTTP admission + UDP media transport for centre-to-centre links. The HTTP
// request is deliberately low frequency: it authenticates the invitation and
// provisions a short lived AES-GCM session. Audio never traverses the HTTP
// control plane.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"draarl/internal/protocol"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	httpPeerMagic               = "CPUD"
	httpPeerVersion        byte = 1
	httpPeerHello          byte = 1
	httpPeerHelloAck       byte = 2
	httpPeerAudio          byte = 3
	httpPeerHeaderSize          = 4 + 1 + 1 + 32 + 8 + 8 + 2
	httpPeerMaxAge              = 3 * time.Second
	httpPeerSessionKDFInfo      = "DraARL/center-peer/session-key/v1"
)

type CenterHTTPPeerSession struct {
	Link       CenterPeerLink
	SessionID  string
	SessionKey []byte
	RemoteUDP  string
	// The remote controls are returned by the admission endpoint. A packet is
	// sent only when both sides have enabled the corresponding local control.
	LocalSendAudio, LocalReceiveAudio   bool
	RemoteSendAudio, RemoteReceiveAudio bool
}

// DerivePeerSessionKey deterministically derives the UDP session key from the
// invitation verifier and the server-issued session ID. The verifier is the
// SHA-256 digest of the one-time invitation token; both centres already have
// it, so the raw token and the resulting AES key never need to cross the HTTP
// response. HTTPS still protects the bearer invitation while it is submitted
// for admission, and this KDF prevents a proxy or access log from receiving a
// reusable UDP key in the response body.
func DerivePeerSessionKey(tokenHash, sessionID string) ([]byte, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	sessionID = strings.TrimSpace(sessionID)
	if len(tokenHash) != sha256.Size*2 || len(sessionID) != 32 {
		return nil, errors.New("invalid peer session derivation input")
	}
	verifier, err := hex.DecodeString(tokenHash)
	if err != nil || len(verifier) != sha256.Size {
		return nil, errors.New("invalid peer invitation verifier")
	}
	info := []byte(httpPeerSessionKDFInfo + ":" + sessionID)
	reader := hkdf.New(sha256.New, verifier, nil, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

// PeerAdmissionProof authenticates the low-frequency HTTP admission request
// without sending the invitation secret over the network. The initiator sends
// the invitation ID, a fresh nonce and this proof; the accepting centre only
// needs the stored SHA-256 verifier to check it. All policy fields are bound to
// the proof, so an observer cannot reuse it for another centre or group.
func PeerAdmissionProof(tokenHash, inviteID, centerID string, localGroupID int, sendAudio, receiveAudio bool, nonce string) (string, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	if len(tokenHash) != sha256.Size*2 || strings.TrimSpace(inviteID) == "" || strings.TrimSpace(centerID) == "" || localGroupID <= 0 || strings.TrimSpace(nonce) == "" {
		return "", errors.New("invalid peer admission proof input")
	}
	verifier, err := hex.DecodeString(tokenHash)
	if err != nil || len(verifier) != sha256.Size {
		return "", errors.New("invalid peer invitation verifier")
	}
	message := fmt.Sprintf("DraARL/center-peer/admit/v1\n%s\n%s\n%d\n%t\n%t\n%s", inviteID, centerID, localGroupID, sendAudio, receiveAudio, nonce)
	mac := hmac.New(sha256.New, verifier)
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// PeerAdmissionResponseProof authenticates the accepting centre's HTTP
// response. The response contains the session and UDP endpoint that the
// importing centre will persist, so those values must be bound to the same
// invitation verifier before they are trusted. This keeps the control-plane
// exchange tamper-evident even when a deployment uses plain HTTP internally.
func PeerAdmissionResponseProof(tokenHash, inviteID, linkID, sessionID, acceptingCenterID, requestingCenterID string, acceptingGroupID, requestingGroupID int, requestingSendAudio, requestingReceiveAudio, acceptingSendAudio, acceptingReceiveAudio bool, udpAddress, virtualDeviceName string) (string, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	inviteID = strings.TrimSpace(inviteID)
	linkID = strings.TrimSpace(linkID)
	sessionID = strings.TrimSpace(sessionID)
	acceptingCenterID = strings.TrimSpace(acceptingCenterID)
	requestingCenterID = strings.TrimSpace(requestingCenterID)
	udpAddress = strings.TrimSpace(udpAddress)
	virtualDeviceName = strings.TrimSpace(virtualDeviceName)
	if len(tokenHash) != sha256.Size*2 || inviteID == "" || linkID == "" || len(sessionID) != 32 || acceptingCenterID == "" || requestingCenterID == "" || acceptingGroupID <= 0 || requestingGroupID <= 0 || udpAddress == "" {
		return "", errors.New("invalid peer admission response proof input")
	}
	verifier, err := hex.DecodeString(tokenHash)
	if err != nil || len(verifier) != sha256.Size {
		return "", errors.New("invalid peer invitation verifier")
	}
	message := fmt.Sprintf("DraARL/center-peer/admit-response/v1\n%s\n%s\n%s\n%s\n%s\n%s\n%d\n%d\n%t\n%t\n%t\n%t\n%s", inviteID, linkID, sessionID, acceptingCenterID, requestingCenterID, udpAddress, acceptingGroupID, requestingGroupID, requestingSendAudio, requestingReceiveAudio, acceptingSendAudio, acceptingReceiveAudio, virtualDeviceName)
	mac := hmac.New(sha256.New, verifier)
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

type httpPeerSession struct {
	cfg                                             CenterHTTPPeerSession
	key                                             []byte
	gcm                                             cipher.AEAD
	remote                                          *net.UDPAddr
	remoteReady                                     bool
	lastRecv                                        uint64
	seq                                             atomic.Uint64
	lastConnected                                   *time.Time
	inPackets, inBytes, outPackets, outBytes, drops uint64
}

type CenterHTTPPeerManager struct {
	mu       sync.RWMutex
	sessions map[string]*httpPeerSession
	receive  func(CenterPeerLink, []byte) bool
	writer   func(*net.UDPAddr, []byte) error
	closed   bool
	stop     chan struct{}
	wg       sync.WaitGroup
}

func NewCenterHTTPPeerManager(receive func(CenterPeerLink, []byte) bool) *CenterHTTPPeerManager {
	return &CenterHTTPPeerManager{sessions: make(map[string]*httpPeerSession), receive: receive, stop: make(chan struct{})}
}

func (m *CenterHTTPPeerManager) Start() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
				m.mu.RLock()
				ids := make([]string, 0, len(m.sessions))
				for _, session := range m.sessions {
					if session.remote != nil {
						ids = append(ids, session.cfg.Link.LinkID)
					}
				}
				m.mu.RUnlock()
				for _, id := range ids {
					_ = m.Bind(id)
				}
			}
		}
	}()
}

func (m *CenterHTTPPeerManager) SetWriter(writer func(*net.UDPAddr, []byte) error) {
	m.mu.Lock()
	m.writer = writer
	m.mu.Unlock()
}

func newPeerAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("peer session key must be 32 bytes")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func (m *CenterHTTPPeerManager) RegisterSession(cfg CenterHTTPPeerSession) error {
	if cfg.SessionID == "" || len(cfg.SessionID) != 32 {
		return errors.New("invalid peer session id")
	}
	aead, err := newPeerAEAD(cfg.SessionKey)
	if err != nil {
		return err
	}
	var addr *net.UDPAddr
	if cfg.RemoteUDP != "" {
		addr, err = net.ResolveUDPAddr("udp", cfg.RemoteUDP)
		if err != nil {
			return fmt.Errorf("invalid peer UDP address: %w", err)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("peer runtime closed")
	}
	m.sessions[cfg.SessionID] = &httpPeerSession{cfg: cfg, key: append([]byte(nil), cfg.SessionKey...), gcm: aead, remote: addr}
	return nil
}

func (m *CenterHTTPPeerManager) RemoveSession(sessionID string) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
}

func (m *CenterHTTPPeerManager) RemoveLink(linkID string) {
	m.mu.Lock()
	for id, session := range m.sessions {
		if session.cfg.Link.LinkID == linkID {
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
}

func (m *CenterHTTPPeerManager) DisableLink(linkID string) {
	m.mu.Lock()
	for _, session := range m.sessions {
		if session.cfg.Link.LinkID == linkID {
			session.cfg.LocalSendAudio = false
			session.cfg.LocalReceiveAudio = false
			session.remoteReady = false
		}
	}
	m.mu.Unlock()
}

// UpdateLinkPolicy applies an administrator's local and remote permission
// changes to an already admitted session. A disabled policy drops readiness;
// the next authenticated Hello can establish it again after both sides are
// enabled.
func (m *CenterHTTPPeerManager) UpdateLinkPolicy(link CenterPeerLink, localSend, localReceive, remoteSend, remoteReceive bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, session := range m.sessions {
		if session.cfg.Link.LinkID != link.LinkID {
			continue
		}
		session.cfg.Link = link
		session.cfg.LocalSendAudio = localSend
		session.cfg.LocalReceiveAudio = localReceive
		session.cfg.RemoteSendAudio = remoteSend
		session.cfg.RemoteReceiveAudio = remoteReceive
		if !link.Enabled || !link.Accepted || !link.ForwardAudio || !localSend && !localReceive {
			session.remoteReady = false
		}
	}
}

func (m *CenterHTTPPeerManager) Close() {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		close(m.stop)
	}
	m.sessions = make(map[string]*httpPeerSession)
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *CenterHTTPPeerManager) Status(linkID string) CenterPeerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.cfg.Link.LinkID != linkID {
			continue
		}
		role := "http-session"
		return CenterPeerStatus{Online: s.remoteReady, Role: role, VirtualDeviceID: CenterPeerVirtualDeviceID(s.cfg.Link.RemoteCenterID, s.cfg.Link.LinkID, s.cfg.Link.Direction), LastConnectedAt: s.lastConnected, InPackets: s.inPackets, InBytes: s.inBytes, OutPackets: s.outPackets, OutBytes: s.outBytes, DroppedPackets: s.drops}
	}
	return CenterPeerStatus{}
}

func peerSessionIDBytes(id string) []byte { return []byte(id) }

func (m *CenterHTTPPeerManager) seal(s *httpPeerSession, kind byte, payload []byte) ([]byte, error) {
	if s == nil || s.gcm == nil {
		return nil, errors.New("peer session unavailable")
	}
	seq := s.seq.Add(1)
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, httpPeerHeaderSize+12)
	copy(out[:4], httpPeerMagic)
	out[4] = httpPeerVersion
	out[5] = kind
	copy(out[6:38], peerSessionIDBytes(s.cfg.SessionID))
	binary.BigEndian.PutUint64(out[38:46], seq)
	binary.BigEndian.PutUint64(out[46:54], uint64(time.Now().UnixMilli()))
	binary.BigEndian.PutUint16(out[54:56], uint16(len(payload)))
	copy(out[56:], nonce)
	return append(out, s.gcm.Seal(nil, nonce, payload, out[:56])...), nil
}

func parsePeerHeader(data []byte) (kind byte, id string, seq uint64, sent int64, payload []byte, nonce []byte, aad []byte, err error) {
	if len(data) < httpPeerHeaderSize+12+16 || string(data[:4]) != httpPeerMagic || data[4] != httpPeerVersion {
		err = errors.New("invalid peer datagram")
		return
	}
	kind = data[5]
	id = string(data[6:38])
	seq = binary.BigEndian.Uint64(data[38:46])
	sent = int64(binary.BigEndian.Uint64(data[46:54]))
	n := int(binary.BigEndian.Uint16(data[54:56]))
	nonce = data[56:68]
	aad = data[:56]
	if n < 0 || 68+n+16 != len(data) {
		err = errors.New("invalid peer payload length")
		return
	}
	payload = data[68:]
	return
}

// Handle implements udphub.Type0Handler and is attached to the already bound
// main UDP socket. The authenticated CPUD envelope is disjoint from devices.
func (m *CenterHTTPPeerManager) Handle(data []byte, addr *net.UDPAddr) bool {
	kind, id, seq, sent, ciphertext, nonce, aad, err := parsePeerHeader(data)
	if err != nil {
		return false
	}
	m.mu.Lock()
	s := m.sessions[id]
	if s == nil {
		m.mu.Unlock()
		return false
	}
	if time.Since(time.UnixMilli(sent)) > httpPeerMaxAge || time.Since(time.UnixMilli(sent)) < -httpPeerMaxAge {
		s.drops++
		m.mu.Unlock()
		return true
	}
	if kind != httpPeerHello && s.remote != nil && (addr == nil || addr.String() != s.remote.String()) {
		s.drops++
		m.mu.Unlock()
		return true
	}
	plain, openErr := s.gcm.Open(nil, nonce, ciphertext, aad)
	if openErr != nil {
		s.drops++
		m.mu.Unlock()
		return true
	}
	// Hello packets are authenticated with the same session key as audio. They
	// may reset the peer's address after a restart, but a forged CPUD header
	// must never be allowed to rebind the endpoint before GCM verification.
	if kind == httpPeerHello {
		if addr == nil || len(plain) != 0 || !s.cfg.Link.Enabled || !s.cfg.Link.Accepted || !s.cfg.Link.ForwardAudio {
			s.drops++
			m.mu.Unlock()
			return true
		}
		s.remote = clonePeerUDPAddr(addr)
		s.remoteReady = true
		now := time.Now()
		s.lastConnected = &now
		writer := m.writer
		ack, ackErr := m.seal(s, httpPeerHelloAck, nil)
		m.mu.Unlock()
		if ackErr == nil && writer != nil {
			_ = writer(addr, ack)
		}
		return true
	}
	if seq == 0 || seq <= s.lastRecv {
		s.drops++
		m.mu.Unlock()
		return true
	}
	s.lastRecv = seq
	if kind == httpPeerHelloAck {
		s.remoteReady = true
		now := time.Now()
		s.lastConnected = &now
		m.mu.Unlock()
		return true
	}
	if kind != httpPeerAudio || !s.remoteReady || !s.cfg.LocalReceiveAudio || !s.cfg.RemoteSendAudio {
		s.drops++
		m.mu.Unlock()
		return true
	}
	s.inPackets++
	s.inBytes += uint64(len(plain))
	receive := m.receive
	link := s.cfg.Link
	m.mu.Unlock()
	if receive == nil || !receive(link, plain) {
		m.mu.Lock()
		s.drops++
		m.mu.Unlock()
	}
	return true
}

func (m *CenterHTTPPeerManager) send(s *httpPeerSession, kind byte, payload []byte) error {
	m.mu.RLock()
	remote := clonePeerUDPAddr(s.remote)
	writer := m.writer
	m.mu.RUnlock()
	if remote == nil {
		return errors.New("peer UDP endpoint is not learned")
	}
	data, err := m.seal(s, kind, payload)
	if err != nil {
		return err
	}
	if writer == nil || remote == nil {
		return errors.New("peer UDP writer is unavailable")
	}
	if err := writer(remote, data); err != nil {
		return err
	}
	return nil
}

func (m *CenterHTTPPeerManager) Bind(linkID string) error {
	m.mu.Lock()
	var target *httpPeerSession
	for _, s := range m.sessions {
		if s.cfg.Link.LinkID == linkID {
			target = s
			break
		}
	}
	m.mu.Unlock()
	if target == nil {
		return errors.New("peer session is not configured")
	}
	return m.send(target, httpPeerHello, nil)
}

func (m *CenterHTTPPeerManager) RelayGroup(groupID int, data []byte) error {
	if len(data) <= protocol.DraARLv1HeaderSize || len(data) > 1400 || data[48] != protocol.DraARLTypeOpus16K {
		return nil
	}
	payload := append([]byte(nil), data[protocol.DraARLv1HeaderSize:]...)
	m.mu.RLock()
	list := make([]*httpPeerSession, 0)
	for _, s := range m.sessions {
		if s.cfg.Link.LocalGroupID == groupID && s.cfg.LocalSendAudio && s.cfg.RemoteReceiveAudio && s.remoteReady {
			list = append(list, s)
		}
	}
	m.mu.RUnlock()
	for _, s := range list {
		if err := m.send(s, httpPeerAudio, payload); err != nil {
			m.mu.Lock()
			s.drops++
			m.mu.Unlock()
			continue
		}
		m.mu.Lock()
		s.outPackets++
		s.outBytes += uint64(len(payload))
		m.mu.Unlock()
	}
	return nil
}

func clonePeerUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	c := *addr
	c.IP = append(net.IP(nil), addr.IP...)
	return &c
}

func EncodePeerSessionKey(key []byte) string { return base64.RawURLEncoding.EncodeToString(key) }
func DecodePeerSessionKey(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(value)
}
