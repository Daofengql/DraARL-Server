package main

import (
	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
	"draarl/internal/udphub"
	appcrypto "draarl/pkg/crypto"
	"encoding/base64"
	"errors"
	"sync/atomic"
)

func localSourceGrant(source *udphub.CenterLocalSource) interconnect.LocalDevice {
	if source == nil {
		return interconnect.LocalDevice{}
	}
	return interconnect.LocalDevice{
		SessionID: source.SessionID, SessionEpoch: source.SessionEpoch, DeviceID: source.DeviceID, OwnerID: source.OwnerID,
		Username: source.Username, CallSign: source.CallSign, Nickname: source.Nickname, SSID: source.SSID, DevModel: source.DevModel, DMRID: source.DMRID,
		GroupID: source.GroupID, DomainID: source.DomainID, RxGroupIDs: append([]int(nil), source.RxGroupIDs...), RxDomainIDs: append([]uint64(nil), source.RxDomainIDs...),
		GhostSessionID: source.GhostSessionID, ClientInstanceID: source.ClientInstanceID, SessionTag: source.SessionTag,
		GhostProtocolVersion: source.GhostProtocolVersion, SourceGroupV1: source.SourceGroupV1,
		DisableSend: source.DisableSend, DisableRecv: source.DisableRecv,
	}
}

func startCenterInterconnect(cfg *config.Configuration) (*interconnect.CenterRuntime, error) {
	if cfg == nil {
		return nil, errors.New("center configuration is nil")
	}
	var runtimeRef atomic.Pointer[interconnect.CenterRuntime]
	acceptPeerAudio := func(link interconnect.CenterPeerLink, payload []byte) bool {
		runtime := runtimeRef.Load()
		domain := udphub.GetActiveCommunicationDomainID(link.LocalGroupID)
		if runtime == nil || !runtime.Local.AcquirePeerVoice(link.LinkID, domain) {
			return false
		}
		return udphub.DeliverCenterPeerAudio(link.LocalGroupID, payload, link.VirtualDeviceName, link.RemoteCenterID, link.LinkID, link.Direction)
	}
	runtime, err := interconnect.StartCenterRuntime(interconnect.CenterRuntimeConfig{OnPeerAudio: acceptPeerAudio})
	if err != nil {
		return nil, err
	}
	runtimeRef.Store(runtime)
	// Restore the last admitted UDP session after a process restart. B's
	// persisted endpoint sends a fresh Hello; A learns it again without any
	// control TCP listener.
	if links, loadErr := gormdb.NewInterCenterLinkRepository().List(); loadErr == nil {
		for _, link := range links {
			if link.LocalCenterID != cfg.Interconnect.CenterID || link.SessionID == "" || link.SessionKeyCiphertext == "" {
				continue
			}
			encoded, decErr := appcrypto.Decrypt(link.SessionKeyCiphertext)
			if decErr != nil {
				continue
			}
			key, keyErr := base64.RawURLEncoding.DecodeString(encoded)
			if keyErr != nil {
				continue
			}
			_ = runtime.HTTPPeers.RegisterSession(interconnect.CenterHTTPPeerSession{Link: interconnect.CenterPeerLink{LinkID: link.LinkID, LocalCenterID: link.LocalCenterID, RemoteCenterID: link.RemoteCenterID, LocalGroupID: link.LocalGroupID, RemoteGroupID: link.RemoteGroupID, InitiatorCenterID: link.InitiatorCenterID, Direction: link.Direction, Enabled: link.Enabled, Accepted: link.Accepted, ForwardAudio: link.ForwardAudio, VirtualDeviceName: link.VirtualDeviceName, CredentialEpoch: link.CredentialEpoch}, SessionID: link.SessionID, SessionKey: key, RemoteUDP: link.RemoteUDPAddress, LocalSendAudio: link.LocalSendAudio, LocalReceiveAudio: link.LocalReceiveAudio, RemoteSendAudio: link.RemoteSendAudio, RemoteReceiveAudio: link.RemoteReceiveAudio})
		}
	}
	return runtime, nil
}
