package interconnect

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type CenterRuntimeConfig struct {
	ControlListen string
	TLSConfig     *tls.Config
	ValidateToken func(nodeID, token string) bool
	Auth          DeviceAuthHandler
}
type CenterRuntime struct {
	Cluster   *ClusterManager
	Gateway   *CenterGateway
	Control   *NodeServer
	UDPBridge *NodeDatagramBridge
}

func StartCenterRuntime(cfg CenterRuntimeConfig) (*CenterRuntime, error) {
	if cfg.TLSConfig == nil {
		return nil, errors.New("center node TLS config is required")
	}
	if cfg.ValidateToken == nil {
		return nil, errors.New("center node token validator is required")
	}
	cluster := NewClusterManager(0)
	gateway := NewCenterGateway(cluster, cfg.Auth)
	server, err := NewNodeServer(NodeServerConfig{ListenAddr: cfg.ControlListen, TLSConfig: cfg.TLSConfig, ValidateToken: cfg.ValidateToken, OnConnect: gateway.OnConnect, OnMessage: gateway.OnMessage, OnEnvelope: gateway.OnEnvelope, OnDisconnect: gateway.OnDisconnect})
	if err != nil {
		return nil, err
	}
	data, err := NewNodeDatagramBridge(server.SessionByID, gateway.OnDatagram, 2*time.Second)
	if err != nil {
		return nil, err
	}
	gateway.Bind(server, data)
	if err := server.Start(); err != nil {
		return nil, err
	}
	return &CenterRuntime{Cluster: cluster, Gateway: gateway, Control: server, UDPBridge: data}, nil
}
func (r *CenterRuntime) Close() {
	if r == nil {
		return
	}
	if r.UDPBridge != nil {
		r.UDPBridge.Close()
	}
	if r.Control != nil {
		_ = r.Control.Close()
	}
	if r.Cluster != nil {
		r.Cluster.Close()
	}
}

type EdgeRuntimeConfig struct {
	NodeID        string
	Token         string
	CenterControl string
	CenterUDP     string
	Listen        string
	TLSConfig     *tls.Config
}
type EdgeRuntime struct {
	Client  *NodeClient
	Gateway *EdgeGateway
	closed  chan struct{}
}

func StartEdgeRuntime(cfg EdgeRuntimeConfig) (*EdgeRuntime, error) {
	if cfg.TLSConfig == nil {
		return nil, errors.New("edge node TLS config is required")
	}
	client, err := DialNode(context.Background(), NodeClientConfig{CenterAddr: cfg.CenterControl, TLSConfig: cfg.TLSConfig, NodeID: cfg.NodeID, Token: cfg.Token})
	if err != nil {
		return nil, err
	}
	gateway, err := NewEdgeGateway(cfg.Listen, client)
	if err != nil {
		client.Close()
		return nil, err
	}
	client.SetEnvelopeHandler(gateway.OnEnvelope)
	if strings.TrimSpace(cfg.CenterUDP) != "" {
		peer, peerErr := NewNodeDatagramPeer(cfg.CenterUDP, client.Session, gateway.OnEnvelope)
		if peerErr != nil {
			client.Close()
			return nil, peerErr
		}
		gateway.peer = peer
	}
	if err := client.Send(ControlMessage{Kind: "node_ready"}); err != nil {
		client.Close()
		return nil, err
	}
	if err := gateway.Start(); err != nil {
		client.Close()
		return nil, err
	}
	runtime := &EdgeRuntime{Client: client, Gateway: gateway, closed: make(chan struct{})}
	go runtime.heartbeatLoop()
	return runtime, nil
}
func (r *EdgeRuntime) heartbeatLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	instanceID := fmt.Sprintf("%s-%d", r.Client.Session.NodeID, r.Client.Session.ConnectedAt.UnixNano())
	for {
		select {
		case <-r.closed:
			return
		case <-ticker.C:
			snapshot := r.Gateway.projection.Snapshot()
			interconnectMetrics := MetricsSnapshot{}
			if r.Gateway.peer != nil {
				interconnectMetrics = r.Gateway.peer.Metrics.Snapshot()
			}
			payload, _ := EncodeJSON(NodeHeartbeat{InstanceID: instanceID, SentAtMillis: time.Now().UnixMilli(), ConnectionCount: r.Gateway.ConnectionCount(), Device: r.Gateway.metrics.Snapshot(), Interconnect: interconnectMetrics, ProjectionVersion: snapshot.Version})
			env := NewEnvelope(SubtypeNodeHeartbeat, r.Client.Session.NodeID, r.Client.Session.SessionID, randomUint64(), payload)
			env.ClusterEpoch, env.ProjectionVersion, env.Flags = snapshot.ClusterEpoch, snapshot.Version, FlagControl
			_ = r.Client.SendEnvelope(env)
		}
	}
}
func (r *EdgeRuntime) Close() {
	if r == nil {
		return
	}
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	if r.Gateway != nil {
		_ = r.Gateway.Close()
	}
	if r.Client != nil {
		_ = r.Client.Close()
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
