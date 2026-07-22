package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
	"draarl/internal/udphub"
)

func localSourceGrant(source *udphub.CenterLocalSource) interconnect.DeviceGrant {
	if source == nil {
		return interconnect.DeviceGrant{}
	}
	return interconnect.DeviceGrant{
		SessionID: source.SessionID, SessionEpoch: source.SessionEpoch, DeviceID: source.DeviceID, OwnerID: source.OwnerID,
		Username: source.Username, CallSign: source.CallSign, SSID: source.SSID, DevModel: source.DevModel, DMRID: source.DMRID,
		GroupID: source.GroupID, DomainID: source.DomainID, DisableSend: source.DisableSend, DisableRecv: source.DisableRecv,
	}
}

func runEdgeMode(configPath string) error {
	if strings.TrimSpace(configPath) == "" {
		configPath = config.DefaultConfigFileName
	}
	edgeCfg, err := interconnect.LoadEdgeConfig(configPath)
	if err != nil {
		return err
	}
	rootPool, err := edgeRootPool(edgeCfg.Edge.TLSCAFile)
	if err != nil {
		return err
	}
	serverName := edgeCfg.Edge.TLSServerName
	if serverName == "" {
		serverName = "localhost"
	}
	tlsCfg := &tls.Config{RootCAs: rootPool, ServerName: serverName, MinVersion: tls.VersionTLS13, InsecureSkipVerify: edgeCfg.Edge.InsecureSkipVerify} // #nosec G402 -- only explicit local/test configuration may skip verification.
	fallbackNodeID, fallbackToken, _ := edgeCfg.RegistrationFallback()
	runtime, err := interconnect.StartEdgeRuntime(interconnect.EdgeRuntimeConfig{
		NodeID: edgeCfg.Edge.NodeID, Token: edgeCfg.Edge.Token, FallbackNodeID: fallbackNodeID, FallbackToken: fallbackToken,
		CenterControl: edgeCfg.Edge.Center, CenterUDP: edgeCfg.Edge.CenterUDP, Listen: edgeCfg.Edge.Listen, ProxyProtocol: edgeCfg.Edge.ProxyProtocol, TLSConfig: tlsCfg,
		DeviceSessionTimeout: time.Duration(edgeCfg.Edge.DeviceSessionTimeoutSeconds) * time.Second,
		GrantRenewBefore:     time.Duration(edgeCfg.Edge.GrantRenewBeforeSeconds) * time.Second,
		DisconnectedGrace:    time.Duration(edgeCfg.Edge.DisconnectedLocalGraceSeconds) * time.Second,
		OnCredential: func(identity interconnect.EdgeIdentity) error {
			if err := interconnect.SaveEdgeIdentity(edgeCfg.Edge.IdentityFile, identity); err != nil {
				return fmt.Errorf("save issued edge identity: %w", err)
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	defer runtime.Close()
	stdlog.Printf("DraARL edge node %s started: shared_udp=%s center_control=%s center_udp=%s", edgeCfg.Edge.NodeID, runtime.Gateway.Addr(), edgeCfg.Edge.Center, edgeCfg.Edge.CenterUDP)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		return nil
	case err := <-runtime.Fatal():
		return err
	}
}

func edgeRootPool(path string) (*x509.CertPool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	return interconnect.LoadRootPool(path)
}

func startCenterInterconnect(cfg *config.Configuration) (*interconnect.CenterRuntime, error) {
	if cfg == nil {
		return nil, errors.New("center configuration is nil")
	}
	var tlsCfg *tls.Config
	if cfg.Interconnect.TLSCertFile != "" || cfg.Interconnect.TLSKeyFile != "" {
		if cfg.Interconnect.TLSCertFile == "" || cfg.Interconnect.TLSKeyFile == "" {
			return nil, errors.New("both Interconnect.TLSCertFile and TLSKeyFile are required")
		}
		loaded, err := interconnect.LoadTLSCertificate(cfg.Interconnect.TLSCertFile, cfg.Interconnect.TLSKeyFile, nil)
		if err != nil {
			return nil, err
		}
		tlsCfg = loaded
	} else if cfg.Interconnect.AllowSelfSigned {
		generated, _, err := interconnect.NewSelfSignedTLSConfig("localhost")
		if err != nil {
			return nil, err
		}
		tlsCfg = generated
	} else {
		return nil, errors.New("Type 0 requires TLS certificate files or explicit AllowSelfSigned=true")
	}
	tokens := make(map[string]string, len(cfg.Interconnect.NodeTokens))
	for node, token := range cfg.Interconnect.NodeTokens {
		tokens[node] = token
	}
	validateStaticToken := func(nodeID, token string) bool {
		expected, ok := tokens[nodeID]
		return ok && expected != "" && token == expected
	}
	authenticateNode := func(nodeID, token string) (interconnect.NodeAuthentication, error) {
		if validateStaticToken(nodeID, token) {
			return interconnect.NodeAuthentication{Accepted: true}, nil
		}
		if interconnect.CredentialNodeID(token) != nodeID {
			return interconnect.NodeAuthentication{}, gormdb.ErrNodeCredentialInvalid
		}
		issuedCredential, err := interconnect.NewLongTermCredential(nodeID)
		if err != nil {
			return interconnect.NodeAuthentication{}, err
		}
		result, err := gormdb.NewServerRepository().AuthenticateNode(
			nodeID, interconnect.HashCredential(token), interconnect.HashCredential(issuedCredential), time.Now(),
		)
		if err != nil || !result.Accepted {
			return interconnect.NodeAuthentication{}, err
		}
		authentication := interconnect.NodeAuthentication{Accepted: true, CredentialEpoch: result.CredentialEpoch}
		if result.IssueCredential {
			authentication.IssuedCredential = issuedCredential
		}
		return authentication, nil
	}
	authHandler := func(session *interconnect.NodeSession, request interconnect.DeviceAuthRequest) (interconnect.DeviceAuthResponse, error) {
		result := udphub.AuthenticateProxiedDevice(request.SourceIP, request.Packet)
		if !result.Success {
			return interconnect.DeviceAuthResponse{RequestID: request.RequestID, Success: false, Error: result.Error, ResponsePacket: result.ResponsePacket}, nil
		}
		grant := &interconnect.DeviceGrant{DeviceID: result.DeviceID, OwnerID: result.OwnerID, Username: result.Username, CallSign: result.CallSign, SSID: result.SSID, DevModel: result.DevModel, DMRID: result.DMRID, GroupID: result.GroupID, DomainID: udphub.GetActiveCommunicationDomainID(result.GroupID), DisableSend: result.DisableSend, DisableRecv: result.DisableRecv, ExpiresAtMillis: time.Now().Add(2 * time.Minute).UnixMilli()}
		return interconnect.DeviceAuthResponse{RequestID: request.RequestID, Success: true, Grant: grant, ResponsePacket: result.ResponsePacket}, nil
	}
	configHandler := func(deviceID int, kind string, data []byte) ([][]byte, error) {
		switch kind {
		case interconnect.DeviceConfigKindSync:
			return udphub.BuildDeviceConfigSyncPackets(deviceID)
		case interconnect.DeviceConfigKindReport:
			packet, err := udphub.SaveDeviceConfigReportAndBuildAck(deviceID, data)
			if err != nil {
				return nil, err
			}
			return [][]byte{packet}, nil
		default:
			return nil, errors.New("unsupported device config request")
		}
	}
	activateDevice := func(session *interconnect.NodeSession, grant *interconnect.DeviceGrant) error {
		if session == nil || grant == nil || grant.DeviceID <= 0 {
			return nil
		}
		now := time.Now()
		entryMode := "edge"
		if session.NodeID == interconnect.CenterLocalNodeID {
			entryMode = "center"
		}
		if err := gormdb.NewDeviceRepository().UpdateDeviceEntry(grant.DeviceID, session.NodeID, entryMode, session.SessionID, true, now); err != nil {
			return err
		}
		udphub.SyncRuntimeDeviceEntry(grant.DeviceID, session.NodeID, entryMode, session.SessionID, true, now)
		return nil
	}
	onNodeStatus := func(session *interconnect.NodeSession, heartbeat *interconnect.NodeHeartbeat, online bool) {
		if session == nil {
			return
		}
		now := time.Now()
		fields := map[string]interface{}{
			"node_last_seen_at": now,
			"node_remote_addr":  session.RemoteAddr,
		}
		if online && heartbeat == nil {
			fields["is_online"] = true
			fields["node_control_session_id"] = session.SessionID
		}
		if heartbeat != nil {
			fields["node_instance_id"] = heartbeat.InstanceID
			fields["node_connection_count"] = heartbeat.ConnectionCount
			fields["node_device_in_packets"] = heartbeat.Device.InPackets
			fields["node_device_in_bytes"] = heartbeat.Device.InBytes
			fields["node_device_out_packets"] = heartbeat.Device.OutPackets
			fields["node_device_out_bytes"] = heartbeat.Device.OutBytes
			fields["node_interconnect_in_packets"] = heartbeat.Interconnect.InPackets
			fields["node_interconnect_in_bytes"] = heartbeat.Interconnect.InBytes
			fields["node_interconnect_out_packets"] = heartbeat.Interconnect.OutPackets
			fields["node_interconnect_out_bytes"] = heartbeat.Interconnect.OutBytes
			fields["node_projection_version"] = heartbeat.ProjectionVersion
			fields["node_acked_projection_version"] = session.AckedProjectionVersion.Load()
			fields["node_metrics_sampled_at"] = now
		}
		repo := gormdb.NewServerRepository()
		if online && heartbeat == nil {
			if err := repo.UpdateNodeRuntime(session.NodeID, fields); err != nil {
				stdlog.Printf("persist edge node connect failed: node=%s err=%v", session.NodeID, err)
			}
		} else if online {
			if _, err := repo.UpdateNodeRuntimeForSession(session.NodeID, session.SessionID, fields); err != nil {
				stdlog.Printf("persist edge node heartbeat failed: node=%s err=%v", session.NodeID, err)
			}
		} else {
			fields["is_online"] = false
			fields["node_control_session_id"] = 0
			if _, err := repo.MarkNodeDisconnected(session.NodeID, session.SessionID, fields); err != nil {
				stdlog.Printf("persist edge node disconnect failed: node=%s err=%v", session.NodeID, err)
			}
			affected, err := repo.ClearCurrentEntryForSession(session.NodeID, session.SessionID)
			if err == nil {
				for _, device := range affected {
					udphub.ClearRuntimeDeviceEntryIfSession(device.ID, session.NodeID, session.SessionID)
				}
			}
		}
	}
	return interconnect.StartCenterRuntime(interconnect.CenterRuntimeConfig{ControlListen: cfg.Interconnect.ControlListen, TLSConfig: tlsCfg, Authenticate: authenticateNode, Auth: authHandler, Activate: activateDevice, Config: configHandler, OnNodeStatus: onNodeStatus})
}
