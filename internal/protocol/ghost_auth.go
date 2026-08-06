package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const GhostAuthPayloadVersion uint16 = 1

var ErrInvalidGhostAuthPayload = errors.New("invalid ghost authentication payload")

type GhostAuthRequest struct {
	Version          uint16   `json:"version"`
	Token            string   `json:"token"`
	ClientInstanceID string   `json:"client_instance_id"`
	Capabilities     []string `json:"capabilities,omitempty"`
}

type GhostAuthSuccess struct {
	Version          uint16 `json:"version"`
	SessionID        string `json:"session_id"`
	SessionTag       uint32 `json:"session_tag"`
	ClientInstanceID string `json:"client_instance_id"`
	TxGroupID        int    `json:"tx_group_id"`
	RxGroupIDs       []int  `json:"rx_group_ids"`
}

func DecodeGhostAuthRequest(data []byte) (GhostAuthRequest, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return GhostAuthRequest{}, ErrInvalidGhostAuthPayload
	}

	var request GhostAuthRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Version != GhostAuthPayloadVersion ||
		strings.TrimSpace(request.Token) == "" || strings.TrimSpace(request.ClientInstanceID) == "" {
		return GhostAuthRequest{}, ErrInvalidGhostAuthPayload
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return GhostAuthRequest{}, ErrInvalidGhostAuthPayload
	}
	request.Token = strings.TrimSpace(request.Token)
	request.ClientInstanceID = strings.TrimSpace(request.ClientInstanceID)
	return request, nil
}

func EncodeGhostAuthSuccessData(success GhostAuthSuccess) ([]byte, error) {
	if success.Version == 0 {
		success.Version = GhostAuthPayloadVersion
	}
	payload, err := json.Marshal(success)
	if err != nil {
		return nil, err
	}
	return append([]byte{JWTAuthSuccess}, payload...), nil
}

func DecodeGhostAuthSuccessData(data []byte) (GhostAuthSuccess, error) {
	if len(data) < 2 || data[0] != JWTAuthSuccess {
		return GhostAuthSuccess{}, ErrInvalidGhostAuthPayload
	}
	var success GhostAuthSuccess
	if err := json.Unmarshal(data[1:], &success); err != nil || success.Version != GhostAuthPayloadVersion || success.SessionID == "" || success.SessionTag == 0 {
		return GhostAuthSuccess{}, ErrInvalidGhostAuthPayload
	}
	return success, nil
}

func ReservedUint32(reserved []byte) uint32 {
	if len(reserved) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(reserved[:4])
}

func WithReservedUint32(data []byte, value uint32) ([]byte, bool) {
	if len(data) < DraARLv1HeaderSize {
		return nil, false
	}
	result := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(result[DraARLv1ReservedOffset:DraARLv1ReservedOffset+4], value)
	return result, true
}
