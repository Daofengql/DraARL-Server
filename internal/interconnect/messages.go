package interconnect

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	NodeCredentialKindRotate = "rotate"
	NodeCredentialKindResult = "result"
)

// NodeCredentialControl carries a newly generated long-term credential only
// over the authenticated TLS control plane. The centre stores only its hash.
type NodeCredentialControl struct {
	Kind                     string `json:"kind"`
	Credential               string `json:"credential,omitempty"`
	CredentialEpoch          uint32 `json:"credential_epoch"`
	PreviousValidUntilMillis int64  `json:"previous_valid_until_ms,omitempty"`
	AckForMessageID          uint64 `json:"ack_for_message_id,omitempty"`
	Success                  bool   `json:"success,omitempty"`
	Error                    string `json:"error,omitempty"`
}

func (m NodeCredentialControl) Validate(nodeID string) error {
	if m.CredentialEpoch == 0 || len(m.Error) > 128 {
		return errors.New("invalid node credential control metadata")
	}
	switch m.Kind {
	case NodeCredentialKindRotate:
		if m.AckForMessageID != 0 || m.PreviousValidUntilMillis <= 0 || CredentialNodeID(m.Credential) != nodeID {
			return errors.New("invalid node credential rotation")
		}
	case NodeCredentialKindResult:
		if m.Credential != "" || m.PreviousValidUntilMillis != 0 || m.AckForMessageID == 0 {
			return errors.New("invalid node credential rotation result")
		}
	default:
		return errors.New("unknown node credential control kind")
	}
	return nil
}

type DeviceAuthRequest struct {
	RequestID uint64 `json:"request_id"`
	SourceIP  string `json:"source_ip"`
	Packet    []byte `json:"packet"`
}

type DeviceGrant struct {
	SessionID            uint64   `json:"session_id"`
	SessionEpoch         uint64   `json:"session_epoch"`
	DeviceID             int      `json:"device_id"`
	OwnerID              int      `json:"owner_id"`
	Username             string   `json:"username"`
	CallSign             string   `json:"callsign"`
	Nickname             string   `json:"nickname"`
	SSID                 byte     `json:"ssid"`
	DevModel             byte     `json:"dev_model"`
	DMRID                uint32   `json:"dmrid"`
	GroupID              int      `json:"group_id"`
	DomainID             uint64   `json:"domain_id"`
	RxGroupIDs           []int    `json:"rx_group_ids,omitempty"`
	RxDomainIDs          []uint64 `json:"rx_domain_ids,omitempty"`
	GhostSessionID       string   `json:"ghost_session_id,omitempty"`
	ClientInstanceID     string   `json:"client_instance_id,omitempty"`
	SessionTag           uint32   `json:"session_tag,omitempty"`
	GhostProtocolVersion uint16   `json:"ghost_protocol_version,omitempty"`
	SourceGroupV1        bool     `json:"source_group_v1,omitempty"`
	RecoveryTicket       string   `json:"recovery_ticket,omitempty"`
	DisableSend          bool     `json:"disable_send"`
	DisableRecv          bool     `json:"disable_recv"`
	ExpiresAtMillis      int64    `json:"expires_at_ms"`
}

func (g DeviceGrant) Route() DeviceRoute {
	return DeviceRoute{
		SessionID: g.SessionID, DeviceID: g.DeviceID, Username: g.Username, CallSign: g.CallSign, Nickname: g.Nickname,
		SSID: g.SSID, DevModel: g.DevModel, GroupID: g.GroupID, DomainID: g.DomainID,
		RxGroupIDs: append([]int(nil), g.RxGroupIDs...), RxDomainIDs: append([]uint64(nil), g.RxDomainIDs...),
		GhostSessionID: g.GhostSessionID, ClientInstanceID: g.ClientInstanceID, SessionTag: g.SessionTag,
		GhostProtocolVersion: g.GhostProtocolVersion, SourceGroupV1: g.SourceGroupV1,
		SessionEpoch: g.SessionEpoch, DisableSend: g.DisableSend, DisableRecv: g.DisableRecv,
	}
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
	RecoveryTicket  string `json:"recovery_ticket,omitempty"`
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

const MaxDeviceSessionConfirmBatch = 128

type DeviceSessionConfirmItem struct {
	SessionID        uint64 `json:"session_id"`
	SessionEpoch     uint64 `json:"session_epoch"`
	ControlSessionID uint64 `json:"control_session_id"`
	DeviceID         int    `json:"device_id"`
	OwnerID          int    `json:"owner_id"`
	SSID             byte   `json:"ssid"`
	DevModel         byte   `json:"dev_model,omitempty"`
	GhostSessionID   string `json:"ghost_session_id,omitempty"`
	ClientInstanceID string `json:"client_instance_id,omitempty"`
	RecoveryTicket   string `json:"recovery_ticket,omitempty"`
}

type DeviceSessionConfirmRequest struct {
	RequestID uint64                     `json:"request_id"`
	Sessions  []DeviceSessionConfirmItem `json:"sessions"`
}

func (m DeviceSessionConfirmRequest) Validate() error {
	if m.RequestID == 0 || len(m.Sessions) == 0 || len(m.Sessions) > MaxDeviceSessionConfirmBatch {
		return errors.New("invalid device session confirmation batch")
	}
	seen := make(map[uint64]struct{}, len(m.Sessions))
	seenDevices := make(map[int]struct{}, len(m.Sessions))
	seenOwners := make(map[string]struct{}, len(m.Sessions))
	for _, item := range m.Sessions {
		if item.SessionID == 0 || item.SessionEpoch == 0 || item.ControlSessionID == 0 || item.DeviceID < 0 || item.OwnerID <= 0 || item.SSID == 0 {
			return errors.New("invalid device session confirmation identity")
		}
		ghost := item.GhostSessionID != ""
		if item.DeviceID == 0 && (!ghost || item.DevModel == 0 || strings.TrimSpace(item.RecoveryTicket) == "" || len(item.RecoveryTicket) > 1024) {
			return errors.New("invalid ghost session confirmation identity")
		}
		if item.DeviceID > 0 && (ghost || item.ClientInstanceID != "" || item.RecoveryTicket != "") {
			return errors.New("physical session confirmation contains ghost identity")
		}
		if _, exists := seen[item.SessionID]; exists {
			return errors.New("duplicate device session confirmation identity")
		}
		seen[item.SessionID] = struct{}{}
		ownerKey := fmt.Sprintf("%d:%d", item.OwnerID, item.SSID)
		if item.ClientInstanceID != "" {
			ownerKey += ":" + item.ClientInstanceID
		}
		if item.DeviceID > 0 {
			if _, exists := seenDevices[item.DeviceID]; exists {
				return errors.New("duplicate device in session confirmation batch")
			}
			seenDevices[item.DeviceID] = struct{}{}
		}
		if _, exists := seenOwners[ownerKey]; exists {
			return errors.New("duplicate owner identity in session confirmation batch")
		}
		seenOwners[ownerKey] = struct{}{}
	}
	return nil
}

type DeviceSessionConfirmResult struct {
	SessionID    uint64       `json:"session_id"`
	SessionEpoch uint64       `json:"session_epoch"`
	Success      bool         `json:"success"`
	Error        string       `json:"error,omitempty"`
	Grant        *DeviceGrant `json:"grant,omitempty"`
}

type DeviceSessionConfirmResponse struct {
	RequestID uint64                       `json:"request_id"`
	Results   []DeviceSessionConfirmResult `json:"results"`
}

func (m DeviceSessionConfirmResponse) Validate() error {
	if m.RequestID == 0 || len(m.Results) == 0 || len(m.Results) > MaxDeviceSessionConfirmBatch {
		return errors.New("invalid device session confirmation response")
	}
	seen := make(map[uint64]struct{}, len(m.Results))
	for _, result := range m.Results {
		if result.SessionID == 0 || result.SessionEpoch == 0 || len(result.Error) > 128 {
			return errors.New("invalid device session confirmation result")
		}
		if _, exists := seen[result.SessionID]; exists {
			return errors.New("duplicate device session confirmation result")
		}
		seen[result.SessionID] = struct{}{}
		if result.Success {
			if result.Grant == nil || result.Error != "" || result.Grant.SessionID == 0 || result.Grant.SessionEpoch == 0 || result.Grant.ExpiresAtMillis <= 0 {
				return errors.New("invalid successful device session confirmation")
			}
			if result.Grant.GhostSessionID != "" && strings.TrimSpace(result.Grant.RecoveryTicket) == "" {
				return errors.New("ghost session confirmation is missing its recovery ticket")
			}
		} else if result.Grant != nil {
			return errors.New("failed device session confirmation contains a grant")
		}
	}
	return nil
}

const (
	SpeakerLeaseActionClaim   = "claim"
	SpeakerLeaseActionGrant   = "grant"
	SpeakerLeaseActionDeny    = "deny"
	SpeakerLeaseActionRelease = "release"
)

// SpeakerLeaseControl is carried only over the authenticated TLS control
// plane. Durations are relative so centre and edge clocks need not agree.
type SpeakerLeaseControl struct {
	Action           string `json:"action"`
	RequestID        uint64 `json:"request_id,omitempty"`
	SessionID        uint64 `json:"session_id"`
	SessionEpoch     uint64 `json:"session_epoch"`
	DomainID         uint64 `json:"domain_id"`
	LeaseID          uint64 `json:"lease_id,omitempty"`
	TTLMillis        int64  `json:"ttl_ms,omitempty"`
	RetryAfterMillis int64  `json:"retry_after_ms,omitempty"`
}

func (m SpeakerLeaseControl) Validate() error {
	if m.SessionID == 0 || m.SessionEpoch == 0 || m.DomainID == 0 {
		return errors.New("speaker lease identity is incomplete")
	}
	switch m.Action {
	case SpeakerLeaseActionClaim:
		if m.RequestID == 0 || m.TTLMillis != 0 || m.RetryAfterMillis != 0 {
			return errors.New("invalid speaker lease claim")
		}
	case SpeakerLeaseActionGrant:
		if m.RequestID == 0 || m.LeaseID == 0 || m.TTLMillis <= 0 || m.RetryAfterMillis != 0 {
			return errors.New("invalid speaker lease grant")
		}
	case SpeakerLeaseActionDeny:
		if m.RequestID == 0 || m.TTLMillis != 0 || m.RetryAfterMillis < 0 {
			return errors.New("invalid speaker lease denial")
		}
	case SpeakerLeaseActionRelease:
		if m.LeaseID == 0 || m.TTLMillis != 0 || m.RetryAfterMillis != 0 {
			return errors.New("invalid speaker lease release")
		}
	default:
		return errors.New("unknown speaker lease action")
	}
	return nil
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
	Begin         SnapshotBegin
	chunks        map[int][]byte
	receivedBytes int
	startedAt     time.Time
}

func NewSnapshotAssembler(begin SnapshotBegin) (*SnapshotAssembler, error) {
	if begin.SnapshotID == 0 || begin.ClusterEpoch == 0 || begin.TotalBytes < 0 || begin.TotalBytes > 64<<20 {
		return nil, errors.New("invalid snapshot begin")
	}
	expectedChunks := (begin.TotalBytes + SnapshotRawChunkSize - 1) / SnapshotRawChunkSize
	if expectedChunks == 0 {
		expectedChunks = 1
	}
	if len(begin.Checksum) != sha256.Size*2 {
		return nil, errors.New("invalid snapshot begin")
	}
	checksum, checksumErr := hex.DecodeString(begin.Checksum)
	if begin.Chunks != expectedChunks || checksumErr != nil || len(checksum) != sha256.Size {
		return nil, errors.New("invalid snapshot begin")
	}
	return &SnapshotAssembler{Begin: begin, chunks: make(map[int][]byte, begin.Chunks), startedAt: time.Now()}, nil
}
func (a *SnapshotAssembler) Add(chunk SnapshotChunk) error {
	if a == nil || chunk.SnapshotID != a.Begin.SnapshotID || chunk.Index < 0 || chunk.Index >= a.Begin.Chunks {
		return errors.New("invalid snapshot chunk")
	}
	start := chunk.Index * SnapshotRawChunkSize
	expectedBytes := SnapshotRawChunkSize
	if remaining := a.Begin.TotalBytes - start; remaining < expectedBytes {
		expectedBytes = remaining
	}
	if expectedBytes < 0 || len(chunk.Data) != expectedBytes {
		return errors.New("invalid snapshot chunk length")
	}
	if existing, exists := a.chunks[chunk.Index]; exists {
		if !bytes.Equal(existing, chunk.Data) {
			return errors.New("conflicting duplicate snapshot chunk")
		}
		return nil
	}
	if a.receivedBytes+len(chunk.Data) > a.Begin.TotalBytes {
		return errors.New("snapshot chunks exceed declared length")
	}
	a.chunks[chunk.Index] = append([]byte(nil), chunk.Data...)
	a.receivedBytes += len(chunk.Data)
	return nil
}
func (a *SnapshotAssembler) Commit(commit SnapshotCommit) (*Projection, error) {
	if a == nil || commit.SnapshotID != a.Begin.SnapshotID || len(a.chunks) != a.Begin.Chunks || a.receivedBytes != a.Begin.TotalBytes {
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
	InstanceID        string                    `json:"instance_id"`
	SentAtMillis      int64                     `json:"sent_at_ms"`
	Goroutines        int                       `json:"goroutines"`
	ConnectionCount   int                       `json:"connection_count"`
	Device            MetricsSnapshot           `json:"device"`
	Interconnect      MetricsSnapshot           `json:"interconnect"`
	ProjectionVersion uint64                    `json:"projection_version"`
	Protection        NodeProtectionSnapshot    `json:"protection"`
	ReceiverCache     EdgeReceiverCacheSnapshot `json:"receiver_cache"`
}

const (
	NodeDataBindRequest   = "request"
	NodeDataBindChallenge = "challenge"
	NodeDataBindProof     = "proof"
)

type NodeDataBind struct {
	Action    string `json:"action"`
	Challenge []byte `json:"challenge,omitempty"`
}

func EncodeJSON(value any) ([]byte, error) { return json.Marshal(value) }
func DecodeJSON(data []byte, value any) error {
	if len(data) == 0 {
		return errors.New("empty payload")
	}
	return json.Unmarshal(data, value)
}

const relayHeaderSize = 42

type RelayFrame struct {
	SessionID                 uint64
	SessionEpoch              uint64
	DomainID                  uint64
	RequiredProjectionVersion uint64
	SpeakerLeaseID            uint64
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
	binary.BigEndian.PutUint64(out[32:40], f.SpeakerLeaseID)
	binary.BigEndian.PutUint16(out[40:42], uint16(len(f.InnerPacket)))
	copy(out[42:], f.InnerPacket)
	return out, nil
}
func UnmarshalRelayFrame(data []byte) (RelayFrame, error) {
	var f RelayFrame
	if len(data) < relayHeaderSize {
		return f, errors.New("relay frame too short")
	}
	n := int(binary.BigEndian.Uint16(data[40:42]))
	if n < DraARLHeaderSize || n > 800 || relayHeaderSize+n != len(data) {
		return f, fmt.Errorf("invalid relay inner length %d", n)
	}
	f.SessionID = binary.BigEndian.Uint64(data[0:8])
	f.SessionEpoch = binary.BigEndian.Uint64(data[8:16])
	f.DomainID = binary.BigEndian.Uint64(data[16:24])
	f.RequiredProjectionVersion = binary.BigEndian.Uint64(data[24:32])
	f.SpeakerLeaseID = binary.BigEndian.Uint64(data[32:40])
	if f.SessionID == 0 || f.SessionEpoch == 0 || f.DomainID == 0 {
		return RelayFrame{}, errors.New("relay identity is incomplete")
	}
	f.InnerPacket = append([]byte(nil), data[42:]...)
	return f, nil
}
