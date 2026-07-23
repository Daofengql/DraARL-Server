package interconnect

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

var activeCenter struct {
	sync.RWMutex
	runtime *CenterRuntime
}

func SetActiveCenterRuntime(runtime *CenterRuntime) {
	activeCenter.Lock()
	activeCenter.runtime = runtime
	activeCenter.Unlock()
}
func ActiveCenterRuntime() *CenterRuntime {
	activeCenter.RLock()
	runtime := activeCenter.runtime
	activeCenter.RUnlock()
	return runtime
}

type CenterRuntimeConfig struct {
	ControlListen    string
	TLSConfig        *tls.Config
	ValidateToken    func(nodeID, token string) bool
	Authenticate     func(nodeID, token string) (NodeAuthentication, error)
	Auth             DeviceAuthHandler
	Activate         DeviceActivationHandler
	Confirm          DeviceSessionConfirmHandler
	Config           DeviceConfigHandler
	OnAcceptedRelay  AcceptedRelayHandler
	OnNodeStatus     func(*NodeSession, *NodeHeartbeat, bool)
	OnAuthentication func(NodeAuthenticationEvent)
	ResourceLimits   ResourceLimits
}
type CenterRuntime struct {
	Cluster           *ClusterManager
	Gateway           *CenterGateway
	Control           *NodeServer
	UDPBridge         *NodeDatagramBridge
	status            *NodeStatusDispatcher
	credentialMu      sync.Mutex
	credentialPending map[uint64]*pendingNodeCredentialRotation
}

type pendingNodeCredentialRotation struct {
	nodeID          string
	sessionID       uint64
	credentialEpoch uint32
	result          chan NodeCredentialControl
}

func StartCenterRuntime(cfg CenterRuntimeConfig) (*CenterRuntime, error) {
	if cfg.TLSConfig == nil {
		return nil, errors.New("center node TLS config is required")
	}
	if cfg.ValidateToken == nil && cfg.Authenticate == nil {
		return nil, errors.New("center node token validator is required")
	}
	cluster := NewClusterManager(0)
	gateway := NewCenterGateway(cluster, cfg.Auth, cfg.Activate)
	if err := gateway.SetResourceLimits(cfg.ResourceLimits); err != nil {
		cluster.Close()
		return nil, err
	}
	gateway.SetDeviceConfigHandler(cfg.Config)
	gateway.SetDeviceSessionConfirmHandler(cfg.Confirm)
	gateway.SetAcceptedRelayHandler(cfg.OnAcceptedRelay)
	status := NewNodeStatusDispatcher(cfg.OnNodeStatus)
	if status != nil {
		gateway.onNodeStatus = status.Submit
	}
	server, err := NewNodeServer(NodeServerConfig{ListenAddr: cfg.ControlListen, TLSConfig: cfg.TLSConfig, ValidateToken: cfg.ValidateToken, Authenticate: cfg.Authenticate, OnConnect: gateway.OnConnect, OnMessage: gateway.OnMessage, OnEnvelope: gateway.OnEnvelope, OnDisconnect: gateway.OnDisconnect, OnAuthentication: cfg.OnAuthentication, ResourceLimits: cfg.ResourceLimits})
	if err != nil {
		return nil, err
	}
	data, err := NewNodeDatagramBridge(server.SessionByID, gateway.OnDatagram, 2*time.Second, cfg.ResourceLimits)
	if err != nil {
		return nil, err
	}
	gateway.Bind(server, data)
	if err := server.Start(); err != nil {
		data.Close()
		gateway.Close()
		cluster.Close()
		return nil, err
	}
	runtime := &CenterRuntime{Cluster: cluster, Gateway: gateway, Control: server, UDPBridge: data, status: status, credentialPending: make(map[uint64]*pendingNodeCredentialRotation)}
	gateway.onCredentialResult = runtime.finishCredentialRotation
	return runtime, nil
}
func (r *CenterRuntime) Close() {
	if r == nil {
		return
	}
	r.credentialMu.Lock()
	for messageID, pending := range r.credentialPending {
		delete(r.credentialPending, messageID)
		select {
		case pending.result <- NodeCredentialControl{Kind: NodeCredentialKindResult, AckForMessageID: messageID, CredentialEpoch: pending.credentialEpoch, Error: "runtime_closed"}:
		default:
		}
	}
	r.credentialMu.Unlock()
	if r.UDPBridge != nil {
		r.UDPBridge.Close()
	}
	if r.Gateway != nil {
		r.Gateway.Close()
	}
	if r.Control != nil {
		_ = r.Control.Close()
	}
	if r.status != nil {
		r.status.Close()
	}
	if r.Cluster != nil {
		r.Cluster.Close()
	}
}

type EdgeRuntimeConfig struct {
	NodeID               string
	Token                string
	FallbackNodeID       string
	FallbackToken        string
	CenterControl        string
	CenterUDP            string
	Listen               string
	ProxyProtocol        string
	TLSConfig            *tls.Config
	DeviceSessionTimeout time.Duration
	GrantRenewBefore     time.Duration
	DisconnectedGrace    time.Duration
	ConnectTimeout       time.Duration
	ReconnectMin         time.Duration
	ReconnectMax         time.Duration
	OnCredential         func(EdgeIdentity) error
}
type EdgeRuntime struct {
	Client  *NodeClient
	Gateway *EdgeGateway

	cfg       EdgeRuntimeConfig
	mu        sync.RWMutex
	closed    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
	fatal     chan error
	instance  string
}

func StartEdgeRuntime(cfg EdgeRuntimeConfig) (*EdgeRuntime, error) {
	if cfg.TLSConfig == nil {
		return nil, errors.New("edge node TLS config is required")
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 2 * time.Second
	}
	if cfg.ReconnectMin <= 0 {
		cfg.ReconnectMin = 250 * time.Millisecond
	}
	if cfg.ReconnectMax < cfg.ReconnectMin {
		cfg.ReconnectMax = 5 * time.Second
	}
	gateway, err := NewEdgeGateway(cfg.Listen, nil, cfg.ProxyProtocol)
	if err != nil {
		return nil, err
	}
	if cfg.DeviceSessionTimeout > 0 {
		gateway.sessionTimeout = cfg.DeviceSessionTimeout
	}
	if cfg.GrantRenewBefore > 0 {
		gateway.grantRenewBefore = cfg.GrantRenewBefore
	}
	if cfg.DisconnectedGrace > 0 {
		gateway.localGrace = cfg.DisconnectedGrace
	}
	if err := gateway.Start(); err != nil {
		return nil, err
	}
	runtime := &EdgeRuntime{cfg: cfg, Gateway: gateway, closed: make(chan struct{}), fatal: make(chan error, 1), instance: fmt.Sprintf("%s-%d", cfg.NodeID, randomUint64())}
	gateway.installCredential = runtime.installCredential
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	client, link, connectErr := runtime.connectWithFallback(ctx)
	cancel()
	if connectErr != nil && (errors.Is(connectErr, errEdgeCredentialPersistence) || errors.Is(connectErr, ErrNodeAuthenticationRejected)) {
		gateway.Close()
		return nil, connectErr
	}
	runtime.wg.Add(2)
	go runtime.connectionLoop(client)
	go runtime.heartbeatLoop()
	if client != nil && link != nil {
		select {
		case <-link.readyCh:
		case <-client.Done():
		case <-time.After(cfg.ConnectTimeout):
		}
	}
	return runtime, nil
}

func (r *EdgeRuntime) installCredential(message NodeCredentialControl) error {
	if r == nil || message.Kind != NodeCredentialKindRotate {
		return errors.New("invalid node credential rotation")
	}
	r.mu.RLock()
	nodeID := r.cfg.NodeID
	onCredential := r.cfg.OnCredential
	r.mu.RUnlock()
	identity := EdgeIdentity{NodeID: nodeID, Credential: message.Credential, CredentialEpoch: message.CredentialEpoch}
	if CredentialNodeID(identity.Credential) != identity.NodeID {
		return errors.New("rotated node credential identity mismatch")
	}
	if onCredential == nil {
		return errors.New("edge credential persistence callback is unavailable")
	}
	if err := onCredential(identity); err != nil {
		return err
	}
	r.mu.Lock()
	r.cfg.Token = identity.Credential
	r.mu.Unlock()
	return nil
}

func (r *CenterRuntime) RotateNodeCredential(nodeID, credential string, epoch uint32, previousValidUntil time.Time) (uint64, error) {
	if r == nil || r.Control == nil {
		return 0, errors.New("center interconnect runtime is unavailable")
	}
	session, ok := r.Control.Session(nodeID)
	if !ok {
		return 0, errors.New("node is offline")
	}
	if session.Features&NodeFeatureCredentialRotation == 0 {
		return 0, errors.New("node does not support online credential rotation")
	}
	message := NodeCredentialControl{Kind: NodeCredentialKindRotate, Credential: credential, CredentialEpoch: epoch, PreviousValidUntilMillis: previousValidUntil.UnixMilli()}
	if err := message.Validate(nodeID); err != nil {
		return 0, err
	}
	payload, err := EncodeJSON(message)
	if err != nil {
		return 0, err
	}
	messageID := r.Cluster.NextMessageID()
	result := make(chan NodeCredentialControl, 1)
	pending := &pendingNodeCredentialRotation{nodeID: nodeID, sessionID: session.SessionID, credentialEpoch: epoch, result: result}
	r.credentialMu.Lock()
	r.credentialPending[messageID] = pending
	r.credentialMu.Unlock()
	defer func() {
		r.credentialMu.Lock()
		delete(r.credentialPending, messageID)
		r.credentialMu.Unlock()
	}()
	env := NewEnvelope(SubtypeNodeCredential, "center", 0, messageID, payload)
	env.Flags = FlagControl | FlagAck | FlagCritical
	if err := session.SendEnvelope(env); err != nil {
		return 0, err
	}
	select {
	case reply, ok := <-result:
		if !ok {
			return messageID, errors.New("center interconnect runtime closed during credential rotation")
		}
		if !reply.Success {
			return messageID, errors.New("edge failed to persist rotated credential")
		}
		return messageID, nil
	case <-time.After(3 * time.Second):
		return messageID, errors.New("edge credential rotation acknowledgement timed out")
	}
}

func (r *CenterRuntime) finishCredentialRotation(session *NodeSession, message NodeCredentialControl) {
	if r == nil || session == nil || message.Kind != NodeCredentialKindResult {
		return
	}
	r.credentialMu.Lock()
	pending := r.credentialPending[message.AckForMessageID]
	r.credentialMu.Unlock()
	if pending == nil || pending.nodeID != session.NodeID || pending.sessionID != session.SessionID || pending.credentialEpoch != message.CredentialEpoch {
		return
	}
	select {
	case pending.result <- message:
	default:
	}
}

var errEdgeCredentialPersistence = errors.New("persist issued edge credential")

func (r *EdgeRuntime) connectWithFallback(ctx context.Context) (*NodeClient, *edgeControlLink, error) {
	client, link, err := r.connectOnce(ctx)
	if err == nil || !errors.Is(err, ErrNodeAuthenticationRejected) {
		return client, link, err
	}
	r.mu.Lock()
	if strings.TrimSpace(r.cfg.FallbackNodeID) == "" || strings.TrimSpace(r.cfg.FallbackToken) == "" {
		r.mu.Unlock()
		return nil, nil, err
	}
	r.cfg.NodeID, r.cfg.Token = r.cfg.FallbackNodeID, r.cfg.FallbackToken
	r.cfg.FallbackNodeID, r.cfg.FallbackToken = "", ""
	r.mu.Unlock()
	return r.connectOnce(ctx)
}

func (r *EdgeRuntime) connectOnce(ctx context.Context) (*NodeClient, *edgeControlLink, error) {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()
	client, err := DialNode(ctx, NodeClientConfig{CenterAddr: cfg.CenterControl, TLSConfig: cfg.TLSConfig, NodeID: cfg.NodeID, Token: cfg.Token})
	if err != nil {
		return nil, nil, err
	}
	if client.IssuedCredential != "" {
		identity := EdgeIdentity{NodeID: client.Session.NodeID, Credential: client.IssuedCredential, CredentialEpoch: client.CredentialEpoch}
		if cfg.OnCredential != nil {
			if err := cfg.OnCredential(identity); err != nil {
				_ = client.Close()
				return nil, nil, fmt.Errorf("%w: %v", errEdgeCredentialPersistence, err)
			}
		}
		r.mu.Lock()
		r.cfg.NodeID, r.cfg.Token = identity.NodeID, identity.Credential
		r.mu.Unlock()
	}
	var peer *NodeDatagramPeer
	if strings.TrimSpace(cfg.CenterUDP) != "" {
		peer, err = NewNodeDatagramPeer(cfg.CenterUDP, client.Session, nil)
		if err != nil {
			_ = client.Close()
			return nil, nil, err
		}
	}
	client.SetEnvelopeHandler(func(env Envelope) { r.Gateway.onEnvelopeFrom(client, env) })
	link, err := r.Gateway.attachControl(client, peer)
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	if peer != nil {
		peer.onData = func(env Envelope) { r.Gateway.onEnvelopeFrom(client, env) }
	}
	if err := r.Gateway.confirmActiveSessions(link); err != nil {
		r.Gateway.detachControl(client, time.Now())
		_ = client.Close()
		return nil, nil, err
	}
	if err := client.Send(ControlMessage{Kind: "node_ready"}); err != nil {
		r.Gateway.detachControl(client, time.Now())
		_ = client.Close()
		return nil, nil, err
	}
	r.setClient(client)
	return client, link, nil
}

func (r *EdgeRuntime) connectionLoop(client *NodeClient) {
	defer r.wg.Done()
	backoff := r.cfg.ReconnectMin
	var lastFailureLog time.Time
	for {
		if client != nil {
			select {
			case <-r.closed:
				r.Gateway.detachControl(client, time.Now())
				r.clearClient(client)
				_ = client.Close()
				return
			case <-client.Done():
				r.Gateway.detachControl(client, time.Now())
				r.clearClient(client)
				client = nil
			}
		}
		timer := time.NewTimer(jitterReconnectDelay(backoff))
		select {
		case <-r.closed:
			timer.Stop()
			return
		case <-timer.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), r.cfg.ConnectTimeout)
		next, _, err := r.connectWithFallback(ctx)
		cancel()
		if err != nil {
			if errors.Is(err, errEdgeCredentialPersistence) {
				select {
				case r.fatal <- err:
				default:
				}
				return
			}
			if lastFailureLog.IsZero() || time.Since(lastFailureLog) >= 30*time.Second {
				log.Printf("[INTERCONNECT] edge control reconnect failed: %v", err)
				lastFailureLog = time.Now()
			}
			backoff *= 2
			if backoff > r.cfg.ReconnectMax {
				backoff = r.cfg.ReconnectMax
			}
			continue
		}
		client = next
		backoff = r.cfg.ReconnectMin
		lastFailureLog = time.Time{}
	}
}

func jitterReconnectDelay(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	spread := base / 5
	if spread <= 0 {
		return base
	}
	width := uint64(spread*2 + 1)
	offset := time.Duration(randomUint64()%width) - spread
	return base + offset
}

func (r *EdgeRuntime) setClient(client *NodeClient) {
	r.mu.Lock()
	r.Client = client
	r.mu.Unlock()
}

func (r *EdgeRuntime) clearClient(client *NodeClient) {
	r.mu.Lock()
	if r.Client == client {
		r.Client = nil
	}
	r.mu.Unlock()
}

func (r *EdgeRuntime) CurrentClient() *NodeClient {
	r.mu.RLock()
	client := r.Client
	r.mu.RUnlock()
	return client
}

func (r *EdgeRuntime) Fatal() <-chan error { return r.fatal }

func (r *EdgeRuntime) heartbeatLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.closed:
			return
		case <-ticker.C:
			link := r.Gateway.currentControl(false)
			if link == nil || link.client == nil || link.client.Session == nil {
				continue
			}
			if link.peer != nil {
				_ = r.Gateway.requestDataBind(link)
			}
			snapshot := r.Gateway.projection.Snapshot()
			interconnectMetrics := link.client.Session.ControlMetrics.Snapshot()
			if link.peer != nil {
				interconnectMetrics = AddMetricsSnapshots(interconnectMetrics, link.peer.Metrics.Snapshot())
			}
			payload, _ := EncodeJSON(NodeHeartbeat{InstanceID: r.instance, SentAtMillis: time.Now().UnixMilli(), ConnectionCount: r.Gateway.ConnectionCount(), Device: r.Gateway.metrics.Snapshot(), Interconnect: interconnectMetrics, ProjectionVersion: snapshot.Version, Protection: link.client.Session.ProtectionSnapshot()})
			env := NewEnvelope(SubtypeNodeHeartbeat, link.client.Session.NodeID, link.client.Session.SessionID, r.Gateway.nextRequest.Add(1), payload)
			env.ClusterEpoch, env.ProjectionVersion, env.Flags = snapshot.ClusterEpoch, snapshot.Version, FlagControl
			_ = link.client.SendEnvelope(env)
		}
	}
}
func (r *EdgeRuntime) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() { close(r.closed) })
	if client := r.CurrentClient(); client != nil {
		_ = client.Close()
	}
	r.wg.Wait()
	if r.Gateway != nil {
		_ = r.Gateway.Close()
	}
}

func LoadTLSCertificate(certFile, keyFile string, roots *x509.CertPool) (*tls.Config, error) {
	if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" {
		return nil, errors.New("TLS certificate and key files are required")
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS key pair: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, MinVersion: tls.VersionTLS13}, nil
}
func LoadRootPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("no certificates found in CA file")
	}
	return pool, nil
}
