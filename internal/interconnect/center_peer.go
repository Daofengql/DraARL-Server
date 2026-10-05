package interconnect

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"
)

const (
	CenterLinkDirectionForward       = "one_way_forward"
	CenterLinkDirectionReverse       = "one_way_reverse"
	CenterLinkDirectionBidirectional = "bidirectional"
	CenterLinkMediaAudio             = "audio"
	CenterPeerMaxAge                 = time.Second
	CenterPeerQueueSize              = 8
)

var peerIDPattern = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$")
var peerNamePattern = regexp.MustCompile("^Server-[A-Za-z0-9_.-]{1,25}$")

// IDs are application identities, independent of the transport's 32-byte
// NodeID and of ordinary device/user IDs.
type CenterPeerLink struct {
	LinkID            string
	LocalCenterID     string
	RemoteCenterID    string
	LocalGroupID      int
	RemoteGroupID     int
	InitiatorCenterID string
	Direction         string
	Enabled           bool
	Accepted          bool
	ForwardAudio      bool
	ForwardText       bool
	ForwardBroadcast  bool
	VirtualDeviceName string
	CredentialEpoch   uint32
}

func (l CenterPeerLink) Validate() error {
	for _, id := range []string{l.LinkID, l.LocalCenterID, l.RemoteCenterID, l.InitiatorCenterID} {
		if !peerIDPattern.MatchString(id) {
			return errors.New("中心及链路 ID 只能使用 1–64 位字母、数字、点、横线和下划线")
		}
	}
	if l.LocalCenterID == l.RemoteCenterID || l.InitiatorCenterID != l.LocalCenterID && l.InitiatorCenterID != l.RemoteCenterID {
		return errors.New("发起中心必须是两个独立中心之一")
	}
	if l.LocalGroupID <= 0 || l.RemoteGroupID <= 0 || l.LocalGroupID > 2147483647 || l.RemoteGroupID > 2147483647 {
		return errors.New("群组 ID 必须是有效正整数")
	}
	switch l.Direction {
	case CenterLinkDirectionForward, CenterLinkDirectionReverse, CenterLinkDirectionBidirectional:
	default:
		return errors.New("互联方向无效")
	}
	if !peerNamePattern.MatchString(l.VirtualDeviceName) {
		return errors.New("虚拟设备名应为 Server- 前缀加 1–25 位字母、数字、点、横线或下划线")
	}
	if l.ForwardText || l.ForwardBroadcast {
		return errors.New("当前版本仅支持音频互联，文本与自动播报暂未开放")
	}
	return nil
}

func (l CenterPeerLink) AllowsOrigin(origin string) bool {
	if !l.Enabled || !l.Accepted || !l.ForwardAudio || origin != l.LocalCenterID && origin != l.RemoteCenterID {
		return false
	}
	switch l.Direction {
	case CenterLinkDirectionForward:
		return origin == l.InitiatorCenterID
	case CenterLinkDirectionReverse:
		return origin != l.InitiatorCenterID
	case CenterLinkDirectionBidirectional:
		return true
	default:
		return false
	}
}
func (l CenterPeerLink) AllowsMedia(media string) bool {
	return media == CenterLinkMediaAudio && l.ForwardAudio
}

// A link has its own transport identity, so two mappings to the same peer
// cannot replace each other's TCP sessions.
func CenterPeerTransportID(linkID string) string {
	hash := sha256.Sum256([]byte(linkID))
	return "cp-" + hex.EncodeToString(hash[:])[:29]
}
func CenterPeerVirtualDeviceID(remote, link, direction string) string {
	return fmt.Sprintf("intercenter:%s:%s:%s", remote, link, direction)
}

// Payload contains Opus bytes only. No ordinary packet header, username,
// callsign, device ID, password or JWT is sent to the peer.
type CenterPeerFrame struct {
	OriginCenterID  string `json:"origin_center_id"`
	OriginLinkID    string `json:"origin_link_id"`
	MessageID       string `json:"message_id"`
	Sequence        uint64 `json:"sequence"`
	HopCount        byte   `json:"hop_count"`
	CreatedAt       int64  `json:"created_at_ms"`
	MediaType       string `json:"media_type"`
	OriginGroupID   int    `json:"origin_group_id"`
	TargetGroupID   int    `json:"target_group_id"`
	CredentialEpoch uint32 `json:"credential_epoch"`
	Payload         []byte `json:"payload"`
}

func NewCenterPeerFrame(l CenterPeerLink, media string, payload []byte, sequence uint64, now time.Time) (CenterPeerFrame, error) {
	if err := l.Validate(); err != nil {
		return CenterPeerFrame{}, err
	}
	if !l.AllowsOrigin(l.LocalCenterID) || !l.AllowsMedia(media) {
		return CenterPeerFrame{}, errors.New("outbound media not permitted")
	}
	f := CenterPeerFrame{OriginCenterID: l.LocalCenterID, OriginLinkID: l.LinkID, MessageID: fmt.Sprintf("%016x:%016x", randomUint64(), sequence), Sequence: sequence, CreatedAt: now.UnixMilli(), MediaType: media, OriginGroupID: l.LocalGroupID, TargetGroupID: l.RemoteGroupID, CredentialEpoch: l.CredentialEpoch, Payload: append([]byte(nil), payload...)}
	return f, f.Validate(now, CenterPeerMaxAge)
}

func (f CenterPeerFrame) Validate(now time.Time, maxAge time.Duration) error {
	if !peerIDPattern.MatchString(f.OriginCenterID) || !peerIDPattern.MatchString(f.OriginLinkID) || len(f.MessageID) != 33 || f.MessageID[16] != ':' {
		return errors.New("invalid peer frame identity")
	}
	if _, err := hex.DecodeString(f.MessageID[:16] + f.MessageID[17:]); err != nil {
		return errors.New("invalid peer message ID")
	}
	if f.Sequence == 0 || f.HopCount != 0 || f.OriginGroupID <= 0 || f.TargetGroupID <= 0 || f.CredentialEpoch == 0 || f.MediaType != CenterLinkMediaAudio || len(f.Payload) == 0 || len(f.Payload) > 710 {
		return errors.New("invalid peer media or routing metadata")
	}
	if maxAge <= 0 {
		maxAge = CenterPeerMaxAge
	}
	age := now.Sub(time.UnixMilli(f.CreatedAt))
	if f.CreatedAt <= 0 || age > maxAge || age < -maxAge {
		return errors.New("stale peer frame")
	}
	return nil
}

// A hello must match the receiver's local approval exactly. No group is
// discovered or created from peer-supplied metadata.
type CenterPeerHello struct {
	LinkID            string `json:"link_id"`
	CenterID          string `json:"center_id"`
	RemoteCenterID    string `json:"remote_center_id"`
	LocalGroupID      int    `json:"local_group_id"`
	RemoteGroupID     int    `json:"remote_group_id"`
	InitiatorCenterID string `json:"initiator_center_id"`
	Direction         string `json:"direction"`
	CredentialEpoch   uint32 `json:"credential_epoch"`
}

func (l CenterPeerLink) Hello() CenterPeerHello {
	return CenterPeerHello{l.LinkID, l.LocalCenterID, l.RemoteCenterID, l.LocalGroupID, l.RemoteGroupID, l.InitiatorCenterID, l.Direction, l.CredentialEpoch}
}
func (l CenterPeerLink) MatchesHello(h CenterPeerHello) bool {
	return h.LinkID == l.LinkID && h.CenterID == l.RemoteCenterID && h.RemoteCenterID == l.LocalCenterID && h.LocalGroupID == l.RemoteGroupID && h.RemoteGroupID == l.LocalGroupID && h.InitiatorCenterID == l.InitiatorCenterID && h.Direction == l.Direction && h.CredentialEpoch == l.CredentialEpoch
}
