package interconnect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// DeviceRoute is the only device information required by an edge for local
// fan-out and permission checks.  It intentionally contains no credentials.
type DeviceRoute struct {
	SessionID            uint64   `json:"session_id"`
	DeviceID             int      `json:"device_id"`
	Username             string   `json:"username"`
	CallSign             string   `json:"callsign"`
	Nickname             string   `json:"nickname"`
	SSID                 byte     `json:"ssid"`
	DevModel             byte     `json:"dev_model"`
	GroupID              int      `json:"group_id"`
	DomainID             uint64   `json:"domain_id"`
	RxGroupIDs           []int    `json:"rx_group_ids,omitempty"`
	RxDomainIDs          []uint64 `json:"rx_domain_ids,omitempty"`
	GhostSessionID       string   `json:"ghost_session_id,omitempty"`
	ClientInstanceID     string   `json:"client_instance_id,omitempty"`
	SessionTag           uint32   `json:"session_tag,omitempty"`
	GhostProtocolVersion uint16   `json:"ghost_protocol_version,omitempty"`
	SourceGroupV1        bool     `json:"source_group_v1,omitempty"`
	SessionEpoch         uint64   `json:"session_epoch"`
	DisableSend          bool     `json:"disable_send"`
	DisableRecv          bool     `json:"disable_recv"`
}

func (r DeviceRoute) clone() DeviceRoute {
	r.RxGroupIDs = append([]int(nil), r.RxGroupIDs...)
	r.RxDomainIDs = append([]uint64(nil), r.RxDomainIDs...)
	return r
}

type Projection struct {
	ClusterEpoch uint64                 `json:"cluster_epoch"`
	Version      uint64                 `json:"version"`
	Devices      map[uint64]DeviceRoute `json:"devices"`
}

func NewProjection(epoch uint64) *Projection {
	return &Projection{ClusterEpoch: epoch, Devices: make(map[uint64]DeviceRoute)}
}

func (p *Projection) Clone() *Projection {
	if p == nil {
		return NewProjection(0)
	}
	out := &Projection{ClusterEpoch: p.ClusterEpoch, Version: p.Version, Devices: make(map[uint64]DeviceRoute, len(p.Devices))}
	for id, route := range p.Devices {
		out.Devices[id] = route.clone()
	}
	return out
}

type DeltaOperation struct {
	Kind      string       `json:"kind"`
	Route     *DeviceRoute `json:"route,omitempty"`
	SessionID uint64       `json:"session_id,omitempty"`
}

type RouteDelta struct {
	ClusterEpoch uint64           `json:"cluster_epoch"`
	BaseVersion  uint64           `json:"base_version"`
	NewVersion   uint64           `json:"new_version"`
	Operations   []DeltaOperation `json:"operations"`
	Checksum     string           `json:"checksum"`
}

func (d RouteDelta) withChecksum() RouteDelta {
	d.Checksum = ""
	data, _ := json.Marshal(d)
	sum := sha256.Sum256(data)
	d.Checksum = hex.EncodeToString(sum[:])
	return d
}

func (d RouteDelta) Validate() error {
	if d.NewVersion <= d.BaseVersion {
		return errors.New("route delta version must advance")
	}
	if d.ClusterEpoch == 0 {
		return errors.New("route delta cluster epoch is required")
	}
	if d.Checksum == "" {
		return errors.New("route delta checksum is required")
	}
	if d.withChecksum().Checksum != d.Checksum {
		return errors.New("route delta checksum mismatch")
	}
	for _, op := range d.Operations {
		switch op.Kind {
		case "upsert":
			if op.Route == nil || op.Route.SessionID == 0 {
				return errors.New("upsert operation requires a session route")
			}
		case "remove":
			if op.SessionID == 0 {
				return errors.New("remove operation requires a session id")
			}
		default:
			return fmt.Errorf("unknown route operation %q", op.Kind)
		}
	}
	return nil
}

func NewRouteDelta(epoch, base, next uint64, operations []DeltaOperation) RouteDelta {
	return (RouteDelta{ClusterEpoch: epoch, BaseVersion: base, NewVersion: next, Operations: operations}).withChecksum()
}

func (p *Projection) ApplyDelta(d RouteDelta) error {
	if p == nil {
		return errors.New("nil projection")
	}
	if err := d.Validate(); err != nil {
		return err
	}
	if p.ClusterEpoch != d.ClusterEpoch {
		return fmt.Errorf("cluster epoch mismatch: have=%d delta=%d", p.ClusterEpoch, d.ClusterEpoch)
	}
	if p.Version != d.BaseVersion {
		return fmt.Errorf("projection version mismatch: have=%d base=%d", p.Version, d.BaseVersion)
	}
	next := p.Clone()
	for _, op := range d.Operations {
		if op.Kind == "upsert" {
			next.Devices[op.Route.SessionID] = op.Route.clone()
		} else {
			delete(next.Devices, op.SessionID)
		}
	}
	next.Version = d.NewVersion
	p.ClusterEpoch, p.Version, p.Devices = next.ClusterEpoch, next.Version, next.Devices
	return nil
}

func MarshalProjection(p *Projection) ([]byte, error) { return json.Marshal(p) }
func UnmarshalProjection(data []byte) (*Projection, error) {
	var p Projection
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.ClusterEpoch == 0 || p.Devices == nil {
		return nil, errors.New("invalid route projection")
	}
	return &p, nil
}

// ProjectionStore provides copy-on-write snapshots for the edge hot path.
type ProjectionStore struct {
	mu      sync.Mutex
	current *Projection
}

func NewProjectionStore(p *Projection) *ProjectionStore { return &ProjectionStore{current: p.Clone()} }
func (s *ProjectionStore) Snapshot() *Projection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current.Clone()
}
func (s *ProjectionStore) ApplyDelta(d RouteDelta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.current.Clone()
	if err := next.ApplyDelta(d); err != nil {
		return err
	}
	s.current = next
	return nil
}
func (s *ProjectionStore) Replace(p *Projection) error {
	if p == nil || p.ClusterEpoch == 0 {
		return errors.New("invalid route projection")
	}
	s.mu.Lock()
	s.current = p.Clone()
	s.mu.Unlock()
	return nil
}
