package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/interconnect"
	"draarl/internal/protocol"
)

const (
	ghostRecoveryTicketVersion = 1
	ghostRecoveryTicketMaxSize = 1024
)

var errInvalidGhostRecoveryTicket = errors.New("invalid ghost recovery ticket")

type ghostRecoveryTicketClaims struct {
	Version          uint8  `json:"v"`
	NodeID           string `json:"n"`
	ControlSessionID uint64 `json:"c"`
	GhostSessionID   string `json:"g"`
	SessionTag       uint32 `json:"t"`
	ClientInstanceID string `json:"i"`
	OwnerID          int    `json:"o"`
	SSID             uint8  `json:"s"`
	DevModel         uint8  `json:"d"`
	ExpiresAtMillis  int64  `json:"x"`
}

type ghostRecoveryTicketSigner struct {
	key []byte
}

func newGhostRecoveryTicketSigner(secret string) (*ghostRecoveryTicketSigner, error) {
	if len(secret) < 32 {
		return nil, errors.New("ghost recovery ticket secret must contain at least 32 bytes")
	}
	return &ghostRecoveryTicketSigner{key: append([]byte(nil), secret...)}, nil
}

func (s *ghostRecoveryTicketSigner) Sign(session ghostsession.Session, nodeID string, controlSessionID uint64, expiresAt time.Time) (string, error) {
	instanceID, err := ghostsession.NormalizeClientInstanceID(session.ClientInstanceID)
	if err != nil || strings.TrimSpace(session.SessionID) == "" || session.SessionTag == 0 || session.OwnerID <= 0 ||
		session.Transport != ghostsession.TransportEdge || protocol.GetGhostSSID(session.DevModel) != session.SSID ||
		strings.TrimSpace(nodeID) == "" || controlSessionID == 0 || !expiresAt.After(time.Now()) {
		return "", errInvalidGhostRecoveryTicket
	}
	claims := ghostRecoveryTicketClaims{
		Version: ghostRecoveryTicketVersion, NodeID: strings.TrimSpace(nodeID), ControlSessionID: controlSessionID,
		GhostSessionID: strings.ToLower(strings.TrimSpace(session.SessionID)), SessionTag: session.SessionTag,
		ClientInstanceID: instanceID, OwnerID: session.OwnerID, SSID: session.SSID, DevModel: session.DevModel,
		ExpiresAtMillis: expiresAt.UnixMilli(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signature := s.sign(payload)
	ticket := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature)
	if len(ticket) > ghostRecoveryTicketMaxSize {
		return "", errInvalidGhostRecoveryTicket
	}
	return ticket, nil
}

func (s *ghostRecoveryTicketSigner) Verify(ticket string, now time.Time) (ghostRecoveryTicketClaims, error) {
	if s == nil || len(s.key) == 0 || len(ticket) == 0 || len(ticket) > ghostRecoveryTicketMaxSize || strings.Count(ticket, ".") != 1 {
		return ghostRecoveryTicketClaims{}, errInvalidGhostRecoveryTicket
	}
	parts := strings.SplitN(ticket, ".", 2)
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ghostRecoveryTicketClaims{}, errInvalidGhostRecoveryTicket
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, s.sign(payload)) {
		return ghostRecoveryTicketClaims{}, errInvalidGhostRecoveryTicket
	}
	var claims ghostRecoveryTicketClaims
	if json.Unmarshal(payload, &claims) != nil || claims.Version != ghostRecoveryTicketVersion || claims.OwnerID <= 0 ||
		claims.SSID == 0 || claims.DevModel == 0 || claims.SessionTag == 0 || claims.ControlSessionID == 0 ||
		strings.TrimSpace(claims.NodeID) == "" || strings.TrimSpace(claims.GhostSessionID) == "" ||
		protocol.GetGhostSSID(claims.DevModel) != claims.SSID || now.UnixMilli() >= claims.ExpiresAtMillis {
		return ghostRecoveryTicketClaims{}, errInvalidGhostRecoveryTicket
	}
	instanceID, err := ghostsession.NormalizeClientInstanceID(claims.ClientInstanceID)
	if err != nil || instanceID != claims.ClientInstanceID {
		return ghostRecoveryTicketClaims{}, errInvalidGhostRecoveryTicket
	}
	return claims, nil
}

func (c ghostRecoveryTicketClaims) Matches(nodeID string, item interconnect.DeviceSessionConfirmItem) bool {
	return c.NodeID == strings.TrimSpace(nodeID) && c.ControlSessionID == item.ControlSessionID &&
		c.GhostSessionID == strings.ToLower(strings.TrimSpace(item.GhostSessionID)) && c.SessionTag > 0 &&
		c.ClientInstanceID == strings.ToLower(strings.TrimSpace(item.ClientInstanceID)) && c.OwnerID == item.OwnerID &&
		c.SSID == item.SSID && c.DevModel == item.DevModel
}

func (s *ghostRecoveryTicketSigner) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("draarl:ghost-recovery:v1\x00"))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}
