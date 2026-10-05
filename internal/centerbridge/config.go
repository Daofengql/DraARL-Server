package centerbridge

import (
	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
	appcrypto "draarl/pkg/crypto"
)

func Policy(l gormdb.InterCenterLink) interconnect.CenterPeerLink {
	return interconnect.CenterPeerLink{LinkID: l.LinkID, LocalCenterID: l.LocalCenterID, RemoteCenterID: l.RemoteCenterID, LocalGroupID: l.LocalGroupID, RemoteGroupID: l.RemoteGroupID, InitiatorCenterID: l.InitiatorCenterID, Direction: l.Direction, Enabled: l.Enabled, Accepted: l.Accepted, ForwardAudio: l.ForwardAudio, ForwardText: l.ForwardText, ForwardBroadcast: l.ForwardBroadcast, VirtualDeviceName: l.VirtualDeviceName, CredentialEpoch: l.CredentialEpoch}
}
func Config(l gormdb.InterCenterLink) (interconnect.CenterPeerConfig, error) {
	cfg := interconnect.CenterPeerConfig{Link: Policy(l), RemoteAddress: l.RemoteAddress, TLSServerName: l.TLSServerName, TLSPinSHA256: l.TLSPinSHA256}
	if l.InitiatorCenterID == l.LocalCenterID && l.CredentialCiphertext != "" {
		token, err := appcrypto.Decrypt(l.CredentialCiphertext)
		if err != nil {
			return cfg, err
		}
		cfg.Credential = token
	}
	return cfg, nil
}
