package ghostsession

import (
	"errors"
	"fmt"
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
	ErrInvalidRouting        = errors.New("invalid ghost routing")
	ErrSessionNotFound       = errors.New("ghost session not found")
	ErrSessionConflict       = errors.New("ghost session identity conflict")
	ErrSessionLimit          = errors.New("ghost session limit reached")
	ErrSubscriptionLimit     = errors.New("ghost subscription limit reached")
	ErrRequiredCapabilities  = errors.New("required ghost capabilities are missing")
	ErrPTTActive             = errors.New("ghost session PTT is active")
)

func StableErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrSessionLimit):
		return "ghost_session_limit"
	case errors.Is(err, ErrSubscriptionLimit):
		return "subscription_limit"
	case errors.Is(err, ErrRequiredCapabilities):
		return "ghost_capabilities_required"
	case errors.Is(err, ErrInvalidClientInstance):
		return "invalid_client_instance_id"
	case errors.Is(err, ErrInvalidRouting):
		return "invalid_routing"
	case errors.Is(err, ErrSessionNotFound):
		return "session_not_found"
	case errors.Is(err, ErrSessionConflict):
		return "session_conflict"
	case errors.Is(err, ErrPTTActive):
		return "ptt_active"
	default:
		return "ghost_session_registration_failed"
	}
}

const (
	CapabilityMultiReceiveV1 = "multi_receive_v1"
	CapabilitySourceGroupV1  = "source_group_v1"
	PTTHoldTimeout           = 900 * time.Millisecond
)

type Routing struct {
	TxGroupID  int   `json:"tx_group_id"`
	RxGroupIDs []int `json:"rx_group_ids"`
}

func NormalizeRouting(routing Routing, maxSubscriptions int) (Routing, error) {
	if routing.TxGroupID <= 0 {
		return Routing{}, fmt.Errorf("%w: transmit group is required", ErrInvalidRouting)
	}
	set := make(map[int]struct{}, len(routing.RxGroupIDs)+1)
	for _, groupID := range routing.RxGroupIDs {
		if groupID <= 0 {
			return Routing{}, fmt.Errorf("%w: receive group must be positive", ErrInvalidRouting)
		}
		set[groupID] = struct{}{}
	}
	// A sender must also receive its selected transmit channel. Adding it here
	// keeps every protocol and API path on the same invariant.
	set[routing.TxGroupID] = struct{}{}
	if maxSubscriptions > 0 && len(set) > maxSubscriptions {
		return Routing{}, fmt.Errorf("%w: requested=%d limit=%d", ErrSubscriptionLimit, len(set), maxSubscriptions)
	}
	normalized := Routing{TxGroupID: routing.TxGroupID, RxGroupIDs: make([]int, 0, len(set))}
	for groupID := range set {
		normalized.RxGroupIDs = append(normalized.RxGroupIDs, groupID)
	}
	sort.Ints(normalized.RxGroupIDs)
	return normalized, nil
}

func NormalizeClientInstanceID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidClientInstance
	}
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil {
		return "", ErrInvalidClientInstance
	}
	return strings.ToLower(parsed.String()), nil
}

func ValidateCapabilities(capabilities []string) error {
	normalized := normalizeCapabilities(capabilities)
	for _, required := range []string{CapabilityMultiReceiveV1, CapabilitySourceGroupV1} {
		index := sort.SearchStrings(normalized, required)
		if index >= len(normalized) || normalized[index] != required {
			return fmt.Errorf("%w: %s", ErrRequiredCapabilities, required)
		}
	}
	return nil
}

type Session struct {
	SessionID        string    `json:"session_id"`
	SessionTag       uint32    `json:"session_tag,omitempty"`
	ClientInstanceID string    `json:"client_instance_id"`
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
