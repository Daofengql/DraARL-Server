package interconnect

import (
	"time"
)

type DeviceAuthHandler func(session *NodeSession, request DeviceAuthRequest) (DeviceAuthResponse, error)

type DeviceActivationHandler func(session *NodeSession, grant *DeviceGrant) error

type DeviceSessionConfirmHandler func(session *NodeSession, sessions []DeviceSessionConfirmItem) ([]DeviceSessionConfirmResult, error)

type DeviceConfigHandler func(deviceID int, kind string, data []byte) ([][]byte, error)

type AcceptedRelayHandler func(AcceptedRelay)

type GhostSessionRenewHandler func(sessionID, nodeID string, controlSessionID uint64, now, expiresAt time.Time) (string, error)

type AcceptedRelay struct {
	SessionID uint64
	DeviceID  int
	OwnerID   int
	Username  string
	CallSign  string
	Nickname  string
	SSID      byte
	DevModel  byte
	GroupID   int
	Type      byte
	Payload   []byte
}

const defaultDeviceGrantTTL = 2 * time.Minute
