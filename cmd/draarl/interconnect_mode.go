package main

import (
	"context"
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
	"draarl/internal/interconnect"
	"draarl/internal/udphub"
)

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
	runtime, err := interconnect.StartEdgeRuntime(interconnect.EdgeRuntimeConfig{NodeID: edgeCfg.Edge.NodeID, Token: edgeCfg.Edge.Token, CenterControl: edgeCfg.Edge.Center, CenterUDP: edgeCfg.Edge.CenterUDP, Listen: edgeCfg.Edge.Listen, TLSConfig: tlsCfg})
	if err != nil {
		return err
	}
	defer runtime.Close()
	stdlog.Printf("DraARL edge node %s started: shared_udp=%s center_control=%s center_udp=%s", edgeCfg.Edge.NodeID, runtime.Gateway.Addr(), edgeCfg.Edge.Center, edgeCfg.Edge.CenterUDP)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	return nil
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
	validateToken := func(nodeID, token string) bool {
		expected, ok := tokens[nodeID]
		return ok && expected != "" && token == expected
	}
	authHandler := func(_ *interconnect.NodeSession, request interconnect.DeviceAuthRequest) (interconnect.DeviceAuthResponse, error) {
		result := udphub.AuthenticateProxiedDevice(request.SourceIP, request.Packet)
		if !result.Success {
			return interconnect.DeviceAuthResponse{RequestID: request.RequestID, Success: false, Error: result.Error, ResponsePacket: result.ResponsePacket}, nil
		}
		grant := &interconnect.DeviceGrant{DeviceID: result.DeviceID, OwnerID: result.OwnerID, Username: result.Username, CallSign: result.CallSign, SSID: result.SSID, DevModel: result.DevModel, DMRID: result.DMRID, GroupID: result.GroupID, DomainID: udphub.GetCommunicationDomainID(result.GroupID), DisableSend: result.DisableSend, DisableRecv: result.DisableRecv, SessionEpoch: uint64(time.Now().UnixNano()), ExpiresAtMillis: time.Now().Add(2 * time.Minute).UnixMilli()}
		return interconnect.DeviceAuthResponse{RequestID: request.RequestID, Success: true, Grant: grant, ResponsePacket: result.ResponsePacket}, nil
	}
	return interconnect.StartCenterRuntime(interconnect.CenterRuntimeConfig{ControlListen: cfg.Interconnect.ControlListen, TLSConfig: tlsCfg, ValidateToken: validateToken, Auth: authHandler})
}

var _ = context.Background
var _ = fmt.Sprintf
