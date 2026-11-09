package interconnect

import (
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
	defer activeCenter.RUnlock()
	return activeCenter.runtime
}

// CenterPeerConfig retains the persisted site mapping used by the management API.
// Transport is exclusively HTTP admission and authenticated UDP on the main socket.
type CenterPeerConfig struct {
	Link          CenterPeerLink
	RemoteAddress string
	TLSServerName string
	TLSPinSHA256  string
	Credential    string
}

type CenterPeerStatus struct {
	Online          bool       `json:"online"`
	Role            string     `json:"role"`
	VirtualDeviceID string     `json:"virtual_device_id"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	InPackets       uint64     `json:"in_packets"`
	InBytes         uint64     `json:"in_bytes"`
	OutPackets      uint64     `json:"out_packets"`
	OutBytes        uint64     `json:"out_bytes"`
	DroppedPackets  uint64     `json:"dropped_packets"`
}

type CenterRuntimeConfig struct {
	OnPeerAudio func(CenterPeerLink, []byte) bool
}

type CenterRuntime struct {
	Local     *LocalSessions
	HTTPPeers *CenterHTTPPeerManager
}

func StartCenterRuntime(cfg CenterRuntimeConfig) (*CenterRuntime, error) {
	peers := NewCenterHTTPPeerManager(cfg.OnPeerAudio)
	peers.Start()
	return &CenterRuntime{Local: NewLocalSessions(), HTTPPeers: peers}, nil
}

func (r *CenterRuntime) Close() {
	if r != nil && r.HTTPPeers != nil {
		r.HTTPPeers.Close()
	}
}
