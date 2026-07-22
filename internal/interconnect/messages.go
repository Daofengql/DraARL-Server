package interconnect

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type DeviceAuthRequest struct {
	RequestID uint64 `json:"request_id"`
	SourceIP  string `json:"source_ip"`
	Packet    []byte `json:"packet"`
}

type DeviceGrant struct {
	SessionID       uint64 `json:"session_id"`
	SessionEpoch    uint64 `json:"session_epoch"`
	DeviceID        int    `json:"device_id"`
	OwnerID         int    `json:"owner_id"`
	Username        string `json:"username"`
	CallSign        string `json:"callsign"`
	SSID            byte   `json:"ssid"`
	DevModel        byte   `json:"dev_model"`
	DMRID           uint32 `json:"dmrid"`
	GroupID         int    `json:"group_id"`
	DomainID        uint64 `json:"domain_id"`
	DisableSend     bool   `json:"disable_send"`
	DisableRecv     bool   `json:"disable_recv"`
	ExpiresAtMillis int64  `json:"expires_at_ms"`
}

func (g DeviceGrant) Route() DeviceRoute {
	return DeviceRoute{SessionID: g.SessionID, DeviceID: g.DeviceID, Username: g.Username, CallSign: g.CallSign, SSID: g.SSID, GroupID: g.GroupID, DomainID: g.DomainID, SessionEpoch: g.SessionEpoch, DisableSend: g.DisableSend, DisableRecv: g.DisableRecv}
}

type DeviceAuthResponse struct {
	RequestID      uint64       `json:"request_id"`
	Success        bool         `json:"success"`
	Error          string       `json:"error,omitempty"`
	Grant          *DeviceGrant `json:"grant,omitempty"`
	ResponsePacket []byte       `json:"response_packet,omitempty"`
}

type DeviceSessionRenewRequest struct {
	RequestID    uint64 `json:"request_id"`
	SessionID    uint64 `json:"session_id"`
	SessionEpoch uint64 `json:"session_epoch"`
}

type DeviceSessionRenewResponse struct {
	RequestID       uint64 `json:"request_id"`
	SessionID       uint64 `json:"session_id"`
	SessionEpoch    uint64 `json:"session_epoch"`
	Success         bool   `json:"success"`
	Error           string `json:"error,omitempty"`
	ExpiresAtMillis int64  `json:"expires_at_ms,omitempty"`
}

type DeviceSessionRevoke struct {
	SessionID    uint64 `json:"session_id"`
	SessionEpoch uint64 `json:"session_epoch"`
	DeviceID     int    `json:"device_id,omitempty"`
	Reason       string `json:"reason"`
}

type DeviceSessionReport struct {
	SessionID        uint64 `json:"session_id"`
	SessionEpoch     uint64 `json:"session_epoch"`
	DeviceID         int    `json:"device_id,omitempty"`
	Online           bool   `json:"online"`
	Reason           string `json:"reason,omitempty"`
	ReportedAtMillis int64  `json:"reported_at_ms"`
}

const (
	DeviceConfigKindSync   = "sync"
	DeviceConfigKindReport = "report"
	DeviceConfigKindDown   = "down"
	DeviceConfigKindResult = "result"
)

// DeviceConfigControl carries Type 3 configuration over the reliable node
// control plane. Data is the ordinary device DATA region for an upstream
// report; Packet is a complete Type 3 device packet for an exact downstream
// session. Results acknowledge the envelope MessageID in AckForMessageID.
type DeviceConfigControl struct {
	Kind            string `json:"kind"`
	SessionID       uint64 `json:"session_id"`
	SessionEpoch    uint64 `json:"session_epoch"`
	DeviceID        int    `json:"device_id"`
	Data            []byte `json:"data,omitempty"`
	Packet          []byte `json:"packet,omitempty"`
	AckForMessageID uint64 `json:"ack_for_message_id,omitempty"`
	Success         bool   `json:"success,omitempty"`
	Error           string `json:"error,omitempty"`
}

func (m DeviceConfigControl) Validate() error {
	if m.SessionID == 0 || m.SessionEpoch == 0 || m.DeviceID <= 0 {
		return errors.New("device config session identity is incomplete")
	}
	if len(m.Error) > 128 {
		return errors.New("device config error is too long")
	}
	switch m.Kind {
	case DeviceConfigKindSync:
		if len(m.Data) != 0 || len(m.Packet) != 0 || m.AckForMessageID != 0 {
			return errors.New("invalid device config sync request")
		}
	case DeviceConfigKindReport:
		if len(m.Data) == 0 || len(m.Data) > 800-DraARLHeaderSize || len(m.Packet) != 0 || m.AckForMessageID != 0 {
			return errors.New("invalid device config report")
		}
	case DeviceConfigKindDown:
		if len(m.Packet) < DraARLHeaderSize || len(m.Packet) > 800 || len(m.Data) != 0 || m.AckForMessageID != 0 {
			return errors.New("invalid device config delivery")
		}
	case DeviceConfigKindResult:
		if m.AckForMessageID == 0 || len(m.Data) != 0 || len(m.Packet) != 0 {
			return errors.New("invalid device config result")
		}
	default:
		return errors.New("unknown device config kind")
	}
	return nil
}

type RouteAck struct {
	ClusterEpoch      uint64 `json:"cluster_epoch"`
	ProjectionVersion uint64 `json:"projection_version"`
	AckForMessageID   uint64 `json:"ack_for_message_id,omitempty"`
	Error             string `json:"error,omitempty"`
}
type ResyncRequest struct {
	ClusterEpoch      uint64 `json:"cluster_epoch"`
	ProjectionVersion uint64 `json:"projection_version"`
	Reason            string `json:"reason"`
}

type SnapshotBegin struct {
	SnapshotID        uint64 `json:"snapshot_id"`
	ClusterEpoch      uint64 `json:"cluster_epoch"`
	ProjectionVersion uint64 `json:"projection_version"`
	Chunks            int    `json:"chunks"`
	TotalBytes        int    `json:"total_bytes"`
	Checksum          string `json:"checksum"`
}
type SnapshotChunk struct {
	SnapshotID uint64 `json:"snapshot_id"`
	Index      int    `json:"index"`
	Data       []byte `json:"data"`
}
type SnapshotCommit struct {
	SnapshotID uint64 `json:"snapshot_id"`
}

const SnapshotRawChunkSize = 32 << 10

func SplitProjection(snapshotID uint64, p *Projection) (SnapshotBegin, []SnapshotChunk, error) {
	data, err := MarshalProjection(p)
	if err != nil {
		return SnapshotBegin{}, nil, err
	}
	sum := sha256.Sum256(data)
	count := (len(data) + SnapshotRawChunkSize - 1) / SnapshotRawChunkSize
	if count == 0 {
		count = 1
	}
	chunks := make([]SnapshotChunk, 0, count)
	for i := 0; i < count; i++ {
		start, end := i*SnapshotRawChunkSize, (i+1)*SnapshotRawChunkSize
		if start > len(data) {
			start = len(data)
		}
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, SnapshotChunk{SnapshotID: snapshotID, Index: i, Data: append([]byte(nil), data[start:end]...)})
	}
	return SnapshotBegin{SnapshotID: snapshotID, ClusterEpoch: p.ClusterEpoch, ProjectionVersion: p.Version, Chunks: count, TotalBytes: len(data), Checksum: hex.EncodeToString(sum[:])}, chunks, nil
}

type SnapshotAssembler struct {
	Begin     SnapshotBegin
	chunks    map[int][]byte
	startedAt time.Time
}

func NewSnapshotAssembler(begin SnapshotBegin) (*SnapshotAssembler, error) {
	if begin.SnapshotID == 0 || begin.ClusterEpoch == 0 || begin.Chunks <= 0 || begin.Chunks > 10000 || begin.TotalBytes < 0 || begin.TotalBytes > 64<<20 {
		return nil, errors.New("invalid snapshot begin")
	}
	return &SnapshotAssembler{Begin: begin, chunks: make(map[int][]byte, begin.Chunks), startedAt: time.Now()}, nil
}
func (a *SnapshotAssembler) Add(chunk SnapshotChunk) error {
	if a == nil || chunk.SnapshotID != a.Begin.SnapshotID || chunk.Index < 0 || chunk.Index >= a.Begin.Chunks || len(chunk.Data) > SnapshotRawChunkSize {
		return errors.New("invalid snapshot chunk")
	}
	if _, exists := a.chunks[chunk.Index]; exists {
		return nil
	}
	a.chunks[chunk.Index] = append([]byte(nil), chunk.Data...)
	return nil
}
func (a *SnapshotAssembler) Commit(commit SnapshotCommit) (*Projection, error) {
	if a == nil || commit.SnapshotID != a.Begin.SnapshotID || len(a.chunks) != a.Begin.Chunks {
		return nil, errors.New("snapshot is incomplete")
	}
	data := make([]byte, 0, a.Begin.TotalBytes)
	for i := 0; i < a.Begin.Chunks; i++ {
		data = append(data, a.chunks[i]...)
	}
	if len(data) != a.Begin.TotalBytes {
		return nil, errors.New("snapshot length mismatch")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != a.Begin.Checksum {
		return nil, errors.New("snapshot checksum mismatch")
	}
	p, err := UnmarshalProjection(data)
	if err != nil {
		return nil, err
	}
	if p.ClusterEpoch != a.Begin.ClusterEpoch || p.Version != a.Begin.ProjectionVersion {
		return nil, errors.New("snapshot metadata mismatch")
	}
	return p, nil
}

type NodeHeartbeat struct {
	InstanceID        string          `json:"instance_id"`
	SentAtMillis      int64           `json:"sent_at_ms"`
	ConnectionCount   int             `json:"connection_count"`
	Device            MetricsSnapshot `json:"device"`
	Interconnect      MetricsSnapshot `json:"interconnect"`
	ProjectionVersion uint64          `json:"projection_version"`
}

func EncodeJSON(value any) ([]byte, error) { return json.Marshal(value) }
func DecodeJSON(data []byte, value any) error {
	if len(data) == 0 {
		return errors.New("empty payload")
	}
	return json.Unmarshal(data, value)
}

const relayHeaderSize = 34

type RelayFrame struct {
	SessionID                 uint64
	SessionEpoch              uint64
	DomainID                  uint64
	RequiredProjectionVersion uint64
	InnerPacket               []byte
}

func (f RelayFrame) MarshalBinary() ([]byte, error) {
	if f.SessionID == 0 || f.SessionEpoch == 0 || f.DomainID == 0 {
		return nil, errors.New("relay identity is incomplete")
	}
	if len(f.InnerPacket) < DraARLHeaderSize || len(f.InnerPacket) > 800 {
		return nil, errors.New("invalid relay inner packet size")
	}
	out := make([]byte, relayHeaderSize+len(f.InnerPacket))
	binary.BigEndian.PutUint64(out[0:8], f.SessionID)
	binary.BigEndian.PutUint64(out[8:16], f.SessionEpoch)
	binary.BigEndian.PutUint64(out[16:24], f.DomainID)
	binary.BigEndian.PutUint64(out[24:32], f.RequiredProjectionVersion)
	binary.BigEndian.PutUint16(out[32:34], uint16(len(f.InnerPacket)))
	copy(out[34:], f.InnerPacket)
	return out, nil
}
func UnmarshalRelayFrame(data []byte) (RelayFrame, error) {
	var f RelayFrame
	if len(data) < relayHeaderSize {
		return f, errors.New("relay frame too short")
	}
	n := int(binary.BigEndian.Uint16(data[32:34]))
	if n < DraARLHeaderSize || n > 800 || relayHeaderSize+n != len(data) {
		return f, fmt.Errorf("invalid relay inner length %d", n)
	}
	f.SessionID = binary.BigEndian.Uint64(data[0:8])
	f.SessionEpoch = binary.BigEndian.Uint64(data[8:16])
	f.DomainID = binary.BigEndian.Uint64(data[16:24])
	f.RequiredProjectionVersion = binary.BigEndian.Uint64(data[24:32])
	f.InnerPacket = append([]byte(nil), data[34:]...)
	return f, nil
}
