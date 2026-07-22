// Package interconnect implements the private Type 0 protocol used between a
// DraARL centre and authenticated edge nodes.  It deliberately lives outside
// the ordinary device protocol package: Type 0 must never be accepted by a
// public device UDP/WebSocket decoder.
package interconnect

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	NodeMagic                = "DraN"
	NodeProtocolVersion byte = 1
	NodeIDSize               = 32
	NodeAuthTagSize          = sha256.Size
	NodeHeaderSize           = 89
	NodeMaxDatagramSize      = 1400
)

// Type 0 subtypes.  Values are part of the private protocol and are not
// ordinary DraARLv1 packet types.
const (
	SubtypeNodeEnroll          byte = 0x01
	SubtypeNodeAuth            byte = 0x03
	SubtypeNodeHeartbeat       byte = 0x05
	SubtypeRouteSnapshotBegin  byte = 0x10
	SubtypeRouteSnapshotChunk  byte = 0x11
	SubtypeRouteSnapshotCommit byte = 0x12
	SubtypeRouteDelta          byte = 0x13
	SubtypeRouteAck            byte = 0x14
	SubtypeRouteResyncRequest  byte = 0x15
	SubtypeDeviceAuth          byte = 0x20
	SubtypeDeviceSessionReport byte = 0x22
	SubtypeDeviceSessionRevoke byte = 0x23
	SubtypeDeviceConfig        byte = 0x24
	SubtypeSpeakerLease        byte = 0x28
	SubtypeRelayUpstream       byte = 0x30
	SubtypeRelayDownstream     byte = 0x31
)

const (
	FlagAck uint16 = 1 << iota
	FlagEncrypted
	FlagChunked
	FlagControl
)

// Envelope is the authenticated Type 0 node envelope.  NodeID is limited to
// NodeIDSize bytes on the wire; a NodeID must be stable and opaque.
type Envelope struct {
	Version           byte
	Subtype           byte
	Flags             uint16
	ClusterEpoch      uint64
	ProjectionVersion uint64
	SourceNodeID      string
	NodeSessionID     uint64
	MessageID         uint64
	SentAtMillis      int64
	HopCount          byte
	KeyEpoch          uint32
	Payload           []byte
	AuthTag           []byte
}

func NewEnvelope(subtype byte, sourceNode string, sessionID, messageID uint64, payload []byte) Envelope {
	return Envelope{Version: NodeProtocolVersion, Subtype: subtype, SourceNodeID: sourceNode, NodeSessionID: sessionID, MessageID: messageID, SentAtMillis: time.Now().UnixMilli(), Payload: payload}
}

func (e Envelope) validateForEncode() error {
	if e.Version == 0 {
		e.Version = NodeProtocolVersion
	}
	if len(e.SourceNodeID) > NodeIDSize {
		return fmt.Errorf("node id exceeds %d bytes", NodeIDSize)
	}
	if len(e.Payload) > NodeMaxDatagramSize-NodeHeaderSize-NodeAuthTagSize {
		return errors.New("node payload exceeds datagram limit")
	}
	if e.HopCount > 2 {
		return errors.New("hop count exceeds limit")
	}
	return nil
}

// Marshal returns the complete wire datagram.  HMAC-SHA256 covers all header
// bytes except the tag followed by the payload.
func (e Envelope) Marshal(key []byte) ([]byte, error) {
	if err := e.validateForEncode(); err != nil {
		return nil, err
	}
	out := make([]byte, NodeHeaderSize+len(e.Payload)+NodeAuthTagSize)
	copy(out[:4], NodeMagic)
	out[4] = e.Version
	out[5] = e.Subtype
	binary.BigEndian.PutUint16(out[6:8], e.Flags)
	binary.BigEndian.PutUint64(out[8:16], e.ClusterEpoch)
	binary.BigEndian.PutUint64(out[16:24], e.ProjectionVersion)
	copy(out[24:24+NodeIDSize], e.SourceNodeID)
	binary.BigEndian.PutUint64(out[56:64], e.NodeSessionID)
	binary.BigEndian.PutUint64(out[64:72], e.MessageID)
	binary.BigEndian.PutUint64(out[72:80], uint64(e.SentAtMillis))
	out[80] = e.HopCount
	binary.BigEndian.PutUint32(out[81:85], e.KeyEpoch)
	binary.BigEndian.PutUint32(out[85:89], uint32(len(e.Payload)))
	copy(out[NodeHeaderSize:], e.Payload)
	tag := hmac.New(sha256.New, key)
	_, _ = tag.Write(out[:NodeHeaderSize])
	_, _ = tag.Write(e.Payload)
	copy(out[NodeHeaderSize+len(e.Payload):], tag.Sum(nil))
	return out, nil
}

func Unmarshal(data, key []byte) (Envelope, error) {
	var e Envelope
	if len(data) < NodeHeaderSize+NodeAuthTagSize {
		return e, errors.New("node packet too short")
	}
	if string(data[:4]) != NodeMagic {
		return e, errors.New("invalid node packet magic")
	}
	e.Version, e.Subtype = data[4], data[5]
	if e.Version != NodeProtocolVersion {
		return e, fmt.Errorf("unsupported node protocol version %d", e.Version)
	}
	e.Flags = binary.BigEndian.Uint16(data[6:8])
	e.ClusterEpoch = binary.BigEndian.Uint64(data[8:16])
	e.ProjectionVersion = binary.BigEndian.Uint64(data[16:24])
	for i, b := range data[24 : 24+NodeIDSize] {
		if b == 0 {
			e.SourceNodeID = string(data[24 : 24+i])
			break
		}
		if i == NodeIDSize-1 {
			e.SourceNodeID = string(data[24 : 24+NodeIDSize])
		}
	}
	e.NodeSessionID = binary.BigEndian.Uint64(data[56:64])
	e.MessageID = binary.BigEndian.Uint64(data[64:72])
	e.SentAtMillis = int64(binary.BigEndian.Uint64(data[72:80]))
	e.HopCount = data[80]
	e.KeyEpoch = binary.BigEndian.Uint32(data[81:85])
	payloadLen := int(binary.BigEndian.Uint32(data[85:89]))
	if payloadLen < 0 || payloadLen > NodeMaxDatagramSize-NodeHeaderSize-NodeAuthTagSize || NodeHeaderSize+payloadLen+NodeAuthTagSize != len(data) {
		return e, errors.New("invalid node payload length")
	}
	e.Payload = append([]byte(nil), data[NodeHeaderSize:NodeHeaderSize+payloadLen]...)
	e.AuthTag = append([]byte(nil), data[NodeHeaderSize+payloadLen:]...)
	tag := hmac.New(sha256.New, key)
	_, _ = tag.Write(data[:NodeHeaderSize])
	_, _ = tag.Write(e.Payload)
	if !hmac.Equal(e.AuthTag, tag.Sum(nil)) {
		return Envelope{}, errors.New("invalid node packet authentication tag")
	}
	if e.HopCount > 2 {
		return Envelope{}, errors.New("hop count exceeds limit")
	}
	return e, nil
}

func (e Envelope) Expired(now time.Time, maxAge time.Duration) bool {
	if maxAge <= 0 {
		maxAge = 2 * time.Second
	}
	sent := time.UnixMilli(e.SentAtMillis)
	age := now.Sub(sent)
	return age < -maxAge || age > maxAge
}
