package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	stdlog "log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"draarl/internal/config"
	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
	oplog "draarl/internal/log"
	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

func localSourceGrant(source *udphub.CenterLocalSource) interconnect.DeviceGrant {
	if source == nil {
		return interconnect.DeviceGrant{}
	}
	return interconnect.DeviceGrant{
		SessionID: source.SessionID, SessionEpoch: source.SessionEpoch, DeviceID: source.DeviceID, OwnerID: source.OwnerID,
		Username: source.Username, CallSign: source.CallSign, Nickname: source.Nickname, SSID: source.SSID, DevModel: source.DevModel, DMRID: source.DMRID,
		GroupID: source.GroupID, DomainID: source.DomainID, RxGroupIDs: append([]int(nil), source.RxGroupIDs...), RxDomainIDs: append([]uint64(nil), source.RxDomainIDs...),
		GhostSessionID: source.GhostSessionID, ClientInstanceID: source.ClientInstanceID, SessionTag: source.SessionTag,
		GhostProtocolVersion: source.GhostProtocolVersion, SourceGroupV1: source.SourceGroupV1,
		DisableSend: source.DisableSend, DisableRecv: source.DisableRecv,
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
	ghostsession.ConfigureGlobal(
		edgeCfg.GhostSessions.MaxSessionsPerOwner,
		edgeCfg.GhostSessions.MaxSubscriptionsPerSession,
	)
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
	recoveryWindow := time.Duration(cfg.Interconnect.SessionRecoveryWindowSeconds) * time.Second
	if recoveryWindow <= 0 {
		recoveryWindow = 3 * time.Minute
	}
	ticketSigner, err := newGhostRecoveryTicketSigner(cfg.JWT.Secret)
	if err != nil {
		return nil, err
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
	var runtime *interconnect.CenterRuntime
	ghostHooks := func() udphub.ProxiedGhostSessionHooks {
		return udphub.ProxiedGhostSessionHooks{
			ApplyRouting: func(ghostSessionID string, _ int, _ byte, _ string, routing ghostsession.Routing) error {
				if runtime == nil || runtime.Gateway == nil {
					return nil
				}
				_, err := runtime.Gateway.UpdateActiveGhostRoute(ghostSessionID, routing.TxGroupID, routing.RxGroupIDs, udphub.GetActiveCommunicationDomainID)
				return err
			},
			Disconnect: func(ghostSessionID, reason string) {
				if runtime != nil && runtime.Gateway != nil {
					_, _ = runtime.Gateway.RevokeActiveGhost(ghostSessionID, reason)
				}
			},
		}
	}
	authHandler := func(session *interconnect.NodeSession, request interconnect.DeviceAuthRequest) (interconnect.DeviceAuthResponse, error) {
		ghostFeatures := interconnect.NodeFeatureGhostMultiSession | interconnect.NodeFeatureGhostRecoveryTicket
		allowGhostMulti := session != nil && session.Features&ghostFeatures == ghostFeatures
		endpoint := request.SourceIP
		if session != nil {
			endpoint = session.NodeID + "/" + request.SourceIP
		}
		result := udphub.AuthenticateProxiedDevice(request.SourceIP, request.Packet, udphub.ProxiedDeviceAuthOptions{
			AllowGhostMultiSession: allowGhostMulti, Endpoint: endpoint,
			Ghost: ghostHooks(),
		})
		if !result.Success {
			return interconnect.DeviceAuthResponse{RequestID: request.RequestID, Success: false, Error: result.Error, ResponsePacket: result.ResponsePacket}, nil
		}
		expiresAt := time.Now().Add(2 * time.Minute)
		grant := &interconnect.DeviceGrant{
			DeviceID: result.DeviceID, OwnerID: result.OwnerID, Username: result.Username, CallSign: result.CallSign, Nickname: result.Nickname,
			SSID: result.SSID, DevModel: result.DevModel, DMRID: result.DMRID, GroupID: result.GroupID, DomainID: udphub.GetActiveCommunicationDomainID(result.GroupID),
			RxGroupIDs: append([]int(nil), result.RxGroupIDs...), RxDomainIDs: udphub.GetActiveCommunicationDomainIDs(result.RxGroupIDs),
			GhostSessionID: result.GhostSessionID, ClientInstanceID: result.ClientInstanceID, SessionTag: result.SessionTag,
			GhostProtocolVersion: result.GhostProtocolVersion, SourceGroupV1: result.SourceGroupV1,
			DisableSend: result.DisableSend, DisableRecv: result.DisableRecv, ExpiresAtMillis: expiresAt.UnixMilli(),
		}
		if result.GhostSessionID != "" {
			ghost, exists := ghostsession.Global.Get(result.GhostSessionID)
			if !exists || session == nil {
				ghostsession.Global.Remove(result.GhostSessionID)
				return interconnect.DeviceAuthResponse{RequestID: request.RequestID, Error: "ghost_recovery_ticket_unavailable"}, nil
			}
			ticket, ticketErr := ticketSigner.Sign(ghost, session.NodeID, session.SessionID, expiresAt)
			if ticketErr != nil {
				ghostsession.Global.Remove(result.GhostSessionID)
				return interconnect.DeviceAuthResponse{RequestID: request.RequestID, Error: "ghost_recovery_ticket_unavailable"}, ticketErr
			}
			grant.RecoveryTicket = ticket
		}
		return interconnect.DeviceAuthResponse{RequestID: request.RequestID, Success: true, Grant: grant, ResponsePacket: result.ResponsePacket}, nil
	}
	confirmHandler := func(session *interconnect.NodeSession, items []interconnect.DeviceSessionConfirmItem) ([]interconnect.DeviceSessionConfirmResult, error) {
		ids := make([]int, 0, len(items))
		for _, item := range items {
			if item.DeviceID > 0 {
				ids = append(ids, item.DeviceID)
			}
		}
		var devices []*gormdb.Device
		if len(ids) > 0 {
			var err error
			devices, err = gormdb.NewDeviceRepository().ListDevicesByIDsWithOwner(ids)
			if err != nil {
				return nil, err
			}
		}
		byID := make(map[int]*gormdb.Device, len(devices))
		for _, device := range devices {
			byID[device.ID] = device
		}
		now := time.Now()
		results := make([]interconnect.DeviceSessionConfirmResult, 0, len(items))
		for _, item := range items {
			result := interconnect.DeviceSessionConfirmResult{SessionID: item.SessionID, SessionEpoch: item.SessionEpoch}
			if item.DeviceID == 0 {
				claims, ticketErr := ticketSigner.Verify(item.RecoveryTicket, now)
				if ticketErr != nil || session == nil || !claims.Matches(session.NodeID, item) {
					result.Error = "ghost_recovery_ticket_invalid"
					results = append(results, result)
					continue
				}
				ghost, ghostErr := udphub.RecoverProxiedGhostSession(udphub.ProxiedGhostRecovery{
					SessionID: claims.GhostSessionID, SessionTag: claims.SessionTag, ClientInstanceID: claims.ClientInstanceID,
					OwnerID: claims.OwnerID, SSID: claims.SSID, DevModel: claims.DevModel, EdgeNodeID: session.NodeID, Now: now,
				}, ghostHooks())
				if ghostErr != nil {
					result.Error = "persisted_ghost_session_mismatch"
					results = append(results, result)
					continue
				}
				expiresAt := now.Add(2 * time.Minute)
				nextTicket, ticketErr := ticketSigner.Sign(ghost, session.NodeID, session.SessionID, expiresAt)
				if ticketErr != nil {
					result.Error = "ghost_recovery_ticket_unavailable"
					results = append(results, result)
					continue
				}
				result.Success = true
				result.Grant = &interconnect.DeviceGrant{
					OwnerID: ghost.OwnerID, Username: ghost.Username, CallSign: ghost.CallSign, Nickname: ghost.Nickname,
					SSID: ghost.SSID, DevModel: ghost.DevModel, GroupID: ghost.TxGroupID, DomainID: udphub.GetActiveCommunicationDomainID(ghost.TxGroupID),
					RxGroupIDs: append([]int(nil), ghost.RxGroupIDs...), RxDomainIDs: udphub.GetActiveCommunicationDomainIDs(ghost.RxGroupIDs),
					GhostSessionID: ghost.SessionID, ClientInstanceID: ghost.ClientInstanceID, SessionTag: ghost.SessionTag,
					GhostProtocolVersion: ghost.ProtocolVersion, SourceGroupV1: ghost.HasCapability("source_group_v1"),
					RecoveryTicket: nextTicket, DisableSend: ghost.DisableSend, DisableRecv: ghost.DisableRecv, ExpiresAtMillis: expiresAt.UnixMilli(),
				}
				results = append(results, result)
				continue
			}
			device := byID[item.DeviceID]
			valid := device != nil && device.Owner != nil && device.OwnerID == item.OwnerID && byte(device.SSID) == item.SSID &&
				device.CurrentEntryNodeID == session.NodeID && device.CurrentEntrySessionID == item.ControlSessionID &&
				device.Owner.Status == 1 && device.Owner.ApprovalStatus == 1
			if !valid {
				result.Error = "persisted_ownership_mismatch"
				results = append(results, result)
				continue
			}
			result.Success = true
			result.Grant = &interconnect.DeviceGrant{
				DeviceID: device.ID, OwnerID: device.OwnerID, Username: device.Owner.Name, CallSign: device.Owner.CallSign, Nickname: device.Owner.NickName,
				SSID: byte(device.SSID), DevModel: byte(device.DevModel), DMRID: uint32(device.DMRID),
				GroupID: device.GroupID, DomainID: udphub.GetActiveCommunicationDomainID(device.GroupID),
				DisableSend: device.DisableSend, DisableRecv: device.DisableRecv,
				ExpiresAtMillis: now.Add(2 * time.Minute).UnixMilli(),
			}
			results = append(results, result)
		}
		return results, nil
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
			affected, err := repo.MarkCurrentEntriesOfflineForSession(session.NodeID, session.SessionID)
			if err == nil {
				for _, device := range affected {
					udphub.SyncRuntimeDeviceEntry(device.ID, session.NodeID, device.EntryMode, session.SessionID, false, now)
				}
			}
			if len(affected) > 0 {
				nodeID, sessionID := session.NodeID, session.SessionID
				time.AfterFunc(recoveryWindow, func() {
					stale, clearErr := gormdb.NewServerRepository().ClearCurrentEntryForSession(nodeID, sessionID)
					if clearErr != nil {
						stdlog.Printf("clear expired disconnected edge sessions failed: node=%s err=%v", nodeID, clearErr)
						return
					}
					for _, device := range stale {
						udphub.ClearRuntimeDeviceEntryIfSession(device.ID, nodeID, sessionID)
					}
				})
			}
		}
	}
	onNodeAuthentication := func(event interconnect.NodeAuthenticationEvent) {
		remoteIP := event.RemoteAddr
		if host, _, err := net.SplitHostPort(event.RemoteAddr); err == nil {
			remoteIP = host
		}
		if event.Registered {
			oplog.AddLog("边缘节点完成首次注册: "+event.NodeID, "edge_node_register", 0, "system", "", remoteIP)
			return
		}
		if !event.Accepted {
			nodeID := strings.TrimSpace(event.NodeID)
			if nodeID == "" {
				nodeID = "unknown"
			}
			oplog.AddLog("边缘节点认证失败: "+nodeID+" reason="+event.Reason, "edge_node_auth_failed", 0, "system", "", remoteIP)
		}
	}
	r := cfg.Interconnect.Resources
	limits := interconnect.ResourceLimits{
		MaxNodes: r.MaxNodes, MaxPendingHandshakes: r.MaxPendingHandshakes, AuthAttemptsPerMinutePerIP: r.AuthAttemptsPerMinutePerIP,
		DataSoftPPSPerNode: r.DataSoftPPSPerNode, DataHardPPSPerNode: r.DataHardPPSPerNode, DataHardMbpsPerNode: r.DataHardMbpsPerNode,
		DataQueuePerNode: r.DataQueuePerNode, DataQueueGlobal: r.DataQueueGlobal, DataWorkers: r.DataWorkers, DataMaxQueueAge: time.Duration(r.DataMaxQueueAgeMS) * time.Millisecond,
		ControlSoftPPSPerNode: r.ControlSoftPPSPerNode, ControlHardPPSPerNode: r.ControlHardPPSPerNode, ControlHardMbpsPerNode: r.ControlHardMbpsPerNode,
		DeviceAuthPPSPerNode: r.DeviceAuthPPSPerNode, MaxDeviceSessionsPerNode: r.MaxDeviceSessionsPerNode,
	}
	startupEntries, err := gormdb.NewDeviceRepository().ListOfflineRemoteEntrySessions()
	if err != nil {
		return nil, fmt.Errorf("list edge restart recovery sessions: %w", err)
	}
	recordAcceptedRelay := func(relay interconnect.AcceptedRelay) {
		if len(relay.Payload) == 0 {
			return
		}
		var groupID *uint
		if relay.GroupID > 0 {
			value := uint(relay.GroupID)
			groupID = &value
		}
		var ownerID *uint
		if relay.OwnerID > 0 {
			value := uint(relay.OwnerID)
			ownerID = &value
		}
		sender := udphub.CommSenderSnapshot{
			Username: relay.Username, CallSign: relay.CallSign, Nickname: relay.Nickname, DevModel: int(relay.DevModel),
		}
		switch relay.Type {
		case protocol.DraARLTypeOpus16K:
			udphub.MarkAcceptedVoice(relay.GroupID, time.Now())
			sourceKey := udphub.InterconnectCommRecordSourceKey(relay.SessionID)
			udphub.RecordCommPacket(sourceKey, relay.DeviceID, relay.SSID, groupID, ownerID, sender, relay.Payload)
		case protocol.DraARLTypeTextMessage:
			udphub.RecordTextMessage(relay.DeviceID, relay.SSID, groupID, ownerID, sender, string(relay.Payload))
		}
	}
	runtime, err = interconnect.StartCenterRuntime(interconnect.CenterRuntimeConfig{
		ControlListen: cfg.Interconnect.ControlListen, TLSConfig: tlsCfg, Authenticate: authenticateNode,
		Auth: authHandler, Activate: activateDevice, Confirm: confirmHandler, Config: configHandler,
		OnAcceptedRelay: recordAcceptedRelay, OnNodeStatus: onNodeStatus, OnAuthentication: onNodeAuthentication, ResourceLimits: limits,
	})
	if err != nil {
		return nil, err
	}
	runtime.Gateway.SetGhostRecoveryWindow(recoveryWindow)
	runtime.Gateway.SetGhostSessionHandlers(
		func(sessionID, nodeID string, controlSessionID uint64, now, expiresAt time.Time) (string, error) {
			if !ghostsession.Global.UpdateActivity(sessionID, "", now) {
				return "", ghostsession.ErrSessionNotFound
			}
			ghost, exists := ghostsession.Global.Get(sessionID)
			if !exists {
				return "", ghostsession.ErrSessionNotFound
			}
			return ticketSigner.Sign(ghost, nodeID, controlSessionID, expiresAt)
		},
		func(sessionID, _ string) { ghostsession.Global.Remove(sessionID) },
	)
	time.AfterFunc(recoveryWindow, func() {
		cleared := 0
		for _, entry := range startupEntries {
			stale, clearErr := gormdb.NewServerRepository().ClearCurrentEntryForSession(entry.NodeID, entry.SessionID)
			if clearErr != nil {
				stdlog.Printf("clear expired edge restart ownership failed: node=%s session=%d err=%v", entry.NodeID, entry.SessionID, clearErr)
				continue
			}
			for _, device := range stale {
				udphub.ClearRuntimeDeviceEntryIfSession(device.ID, entry.NodeID, entry.SessionID)
			}
			cleared += len(stale)
		}
		if cleared > 0 {
			stdlog.Printf("cleared %d unclaimed edge device entries after restart recovery window", cleared)
		}
	})
	return runtime, nil
}
