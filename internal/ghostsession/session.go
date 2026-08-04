package ghostsession

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Transport string

const (
	TransportUDP       Transport = "udp"
	TransportWebSocket Transport = "websocket"
	TransportEdge      Transport = "edge"
)

var (
	ErrInvalidClientInstance = errors.New("invalid client instance id")
	ErrSessionNotFound       = errors.New("ghost session not found")
	ErrSessionLimit          = errors.New("ghost session limit reached")
	ErrSubscriptionLimit     = errors.New("ghost subscription limit reached")
	ErrInstanceAlreadyOnline = errors.New("ghost client instance already online")
)

type Routing struct {
	TxGroupID  int   `json:"tx_group_id"`
	RxGroupIDs []int `json:"rx_group_ids"`
}

func NormalizeRouting(routing Routing, maxSubscriptions int) (Routing, error) {
	if routing.TxGroupID <= 0 {
		return Routing{}, errors.New("transmit group is required")
	}
	set := make(map[int]struct{}, len(routing.RxGroupIDs))
	for _, groupID := range routing.RxGroupIDs {
		if groupID <= 0 {
			return Routing{}, errors.New("receive group must be positive")
		}
		set[groupID] = struct{}{}
	}
	if len(set) == 0 {
		return Routing{}, errors.New("at least one receive group is required")
	}
	if maxSubscriptions > 0 && len(set) > maxSubscriptions {
		return Routing{}, ErrSubscriptionLimit
	}
	normalized := Routing{TxGroupID: routing.TxGroupID, RxGroupIDs: make([]int, 0, len(set))}
	for groupID := range set {
		normalized.RxGroupIDs = append(normalized.RxGroupIDs, groupID)
	}
	sort.Ints(normalized.RxGroupIDs)
	return normalized, nil
}

func NormalizeClientInstanceID(raw string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true, nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil {
		return "", false, ErrInvalidClientInstance
	}
	return strings.ToLower(parsed.String()), false, nil
}

type Session struct {
	SessionID        string    `json:"session_id"`
	SessionTag       uint32    `json:"session_tag,omitempty"`
	ClientInstanceID string    `json:"client_instance_id,omitempty"`
	Legacy           bool      `json:"legacy"`
	OwnerID          int       `json:"owner_id"`
	Username         string    `json:"username"`
	CallSign         string    `json:"callsign"`
	Nickname         string    `json:"nickname"`
	DevModel         uint8     `json:"dev_model"`
	SSID             uint8     `json:"ssid"`
	Transport        Transport `json:"transport"`
	Endpoint         string    `json:"endpoint,omitempty"`
	ProtocolVersion  uint16    `json:"protocol_version"`
	Capabilities     []string  `json:"capabilities"`
	CreatedAt        time.Time `json:"created_at"`
	LastActivity     time.Time `json:"last_activity"`
	Connected        bool      `json:"connected"`
	TxGroupID        int       `json:"tx_group_id"`
	RxGroupIDs       []int     `json:"rx_group_ids"`
	DisableSend      bool      `json:"disable_send"`
	DisableRecv      bool      `json:"disable_recv"`
}

func (s Session) Routing() Routing {
	return Routing{TxGroupID: s.TxGroupID, RxGroupIDs: append([]int(nil), s.RxGroupIDs...)}
}

func cloneSession(session Session) Session {
	session.Capabilities = append([]string(nil), session.Capabilities...)
	session.RxGroupIDs = append([]int(nil), session.RxGroupIDs...)
	return session
}

func normalizeCapabilities(capabilities []string) []string {
	set := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability != "" {
			set[capability] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for capability := range set {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

func (s Session) HasCapability(capability string) bool {
	capability = strings.ToLower(strings.TrimSpace(capability))
	index := sort.SearchStrings(s.Capabilities, capability)
	return index < len(s.Capabilities) && s.Capabilities[index] == capability
}
