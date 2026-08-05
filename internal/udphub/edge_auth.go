package udphub

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

// ProxiedDeviceAuthResult is the centre-side result returned to an edge. It
// contains only a short-lived grant and the ordinary device response; no
// password or JWT is ever returned to the edge.
type ProxiedDeviceAuthResult struct {
	Success              bool
	Error                string
	ResponsePacket       []byte
	DeviceID             int
	OwnerID              int
	Username             string
	CallSign             string
	Nickname             string
	SSID                 byte
	DevModel             byte
	DMRID                uint32
	GroupID              int
	RxGroupIDs           []int
	GhostSessionID       string
	ClientInstanceID     string
	SessionTag           uint32
	GhostProtocolVersion uint16
	SourceGroupV1        bool
	DisableSend          bool
	DisableRecv          bool
}

type ProxiedGhostSessionHooks struct {
	ApplyRouting func(ghostSessionID string, ownerID int, ssid byte, clientInstanceID string, routing ghostsession.Routing) error
	Disconnect   func(ghostSessionID, reason string)
}

type ProxiedDeviceAuthOptions struct {
	AllowGhostMultiSession bool
	Endpoint               string
	Ghost                  ProxiedGhostSessionHooks
}

// AuthenticateProxiedDevice applies the same device authentication and
// registration rules as the centre UDP path, but returns a serialisable grant
// for an authenticated edge. It is intentionally called only after the centre
// has decoded the Type 0 DeviceAuthRequest.
func AuthenticateProxiedDevice(sourceIP string, wire []byte, optionList ...ProxiedDeviceAuthOptions) ProxiedDeviceAuthResult {
	var options ProxiedDeviceAuthOptions
	if len(optionList) > 0 {
		options = optionList[0]
	}
	packet, err := protocol.NewDraARLv1RoutingPacket(nil, wire)
	if err != nil {
		return ProxiedDeviceAuthResult{Error: "invalid_device_packet"}
	}
	defer protocol.ReleaseDraARLv1RoutingPacket(packet)
	if packet.Type == protocol.DraARLTypeJWTAuth {
		return authenticateProxiedJWT(sourceIP, packet, options)
	}
	if packet.Type != protocol.DraARLTypeHeartbeat {
		return ProxiedDeviceAuthResult{Error: "heartbeat_required"}
	}
	if sourceIP == "" {
		sourceIP = "edge"
	}
	authResult := AuthenticateDevice(sourceIP, packet.Username, packet.DevicePassword)
	if !authResult.Success || authResult.User == nil {
		return ProxiedDeviceAuthResult{Error: authResult.Error}
	}
	if existing := findDeviceByOwnerSSIDFromMemory(authResult.User.ID, packet.SSID); existing != nil && existing.CurrentEntryNodeID != "center" && shouldRejectNormalDeviceConflict(existing, packet.UDPAddr, "") {
		return ProxiedDeviceAuthResult{Error: "device_conflict_online"}
	}
	model := packet.DevModel
	if !protocol.IsValidClientReportedDevModel(model) {
		model = protocol.DraARLDevModelUnknown
	}
	newDevice := &models.Device{Username: authResult.User.Name, CallSign: authResult.CallSign, Nickname: authResult.User.NickName, SSID: packet.SSID, OwnerID: authResult.User.ID, CallSignSSID: fmt.Sprintf("%s-%d", authResult.CallSign, packet.SSID), DevModel: model, Priority: 100, Status: 0, LastOnlineIP: sourceIP}
	dev, err := addDevice(newDevice, func() int { return resolveAvailableNewDeviceDefaultGroup(authResult.User) })
	if err != nil || dev == nil {
		return ProxiedDeviceAuthResult{Error: "device_registration_failed"}
	}
	dev.Username, dev.CallSign, dev.Nickname, dev.DevModel = authResult.User.Name, authResult.CallSign, authResult.User.NickName, model
	if dev.GroupID > 0 {
		if gp, ok := GetGroupFromCache(dev.GroupID); ok {
			attachRuntimeDeviceToGroup(gp, dev)
		}
	}
	response := protocol.EncodeHeartbeatResponse(packet, authResult.CallSign)
	dev.ISOnline, dev.LastPacketTime, dev.OnlineTime = true, time.Now(), time.Now()
	indexRuntimeDevice(dev)
	return ProxiedDeviceAuthResult{Success: true, ResponsePacket: response, DeviceID: dev.ID, OwnerID: dev.OwnerID, Username: dev.Username, CallSign: dev.CallSign, Nickname: dev.Nickname, SSID: dev.SSID, DevModel: dev.DevModel, DMRID: dev.DMRID, GroupID: dev.GroupID, DisableSend: dev.DisableSend, DisableRecv: dev.DisableRecv}
}

func authenticateProxiedJWT(sourceIP string, packet *protocol.DraARLv1Packet, options ProxiedDeviceAuthOptions) ProxiedDeviceAuthResult {
	request, legacy, err := protocol.DecodeGhostAuthRequest(packet.DATA)
	if err != nil {
		return ProxiedDeviceAuthResult{Error: "invalid_authentication_payload"}
	}
	if !legacy {
		if !options.AllowGhostMultiSession {
			return ProxiedDeviceAuthResult{Error: "node_ghost_multi_session_unsupported"}
		}
		instanceID, normalizedLegacy, normalizeErr := ghostsession.NormalizeClientInstanceID(request.ClientInstanceID)
		if normalizeErr != nil || normalizedLegacy {
			return ProxiedDeviceAuthResult{Error: "invalid_client_instance_id"}
		}
		request.ClientInstanceID = instanceID
	}
	result := AuthenticateJWT(request.Token)
	if !result.Success || result.User == nil {
		return ProxiedDeviceAuthResult{Error: result.ErrorMsg}
	}
	if !protocol.IsGhostDevModel(packet.DevModel) {
		return ProxiedDeviceAuthResult{Error: "invalid_device_model"}
	}
	ssid := protocol.GetGhostSSID(packet.DevModel)
	fallbackGroupID := GetGhostDeviceGroupID(result.User.ID, packet.DevModel)
	routing, err := loadUDPGhostRouting(result.User, packet.DevModel, request.ClientInstanceID, legacy, request.Capabilities, fallbackGroupID)
	if err != nil {
		return ProxiedDeviceAuthResult{Error: "client_preference_unavailable"}
	}
	now := time.Now()
	endpoint := strings.TrimSpace(options.Endpoint)
	if endpoint == "" {
		endpoint = strings.TrimSpace(sourceIP)
	}
	registeredSessionID := ""
	controller := ghostsession.Controller{
		ApplyRouting: func(next ghostsession.Routing) error {
			if options.Ghost.ApplyRouting == nil || registeredSessionID == "" {
				return nil
			}
			return options.Ghost.ApplyRouting(registeredSessionID, result.User.ID, ssid, request.ClientInstanceID, next)
		},
		Disconnect: func(reason string) {
			if options.Ghost.Disconnect != nil && registeredSessionID != "" {
				options.Ghost.Disconnect(registeredSessionID, reason)
			}
		},
	}
	session, err := ghostsession.Global.Register(ghostsession.Registration{
		ClientInstanceID: request.ClientInstanceID, ReplaceExisting: false,
		OwnerID: result.User.ID, Username: result.User.Name, CallSign: result.CallSign, Nickname: result.User.NickName,
		DevModel: packet.DevModel, SSID: ssid, Transport: ghostsession.TransportEdge,
		Endpoint: endpoint, ProtocolVersion: request.Version, Capabilities: request.Capabilities,
		Routing: routing, Now: now,
	}, controller)
	if err != nil {
		code := "ghost_session_registration_failed"
		switch {
		case errors.Is(err, ghostsession.ErrInstanceAlreadyOnline):
			code = "ghost_device_already_online"
		case errors.Is(err, ghostsession.ErrSessionLimit):
			code = fmt.Sprintf("ghost_session_limit active=%d limit=%d", len(ghostsession.Global.ListOwner(result.User.ID)), ghostsession.MaxSessionsPerOwner())
		}
		return ProxiedDeviceAuthResult{Error: code}
	}
	registeredSessionID = session.SessionID
	if !legacy {
		refreshed, refreshErr := ghostsession.Global.RefreshRouting(session.SessionID, func(ghostsession.Session) (ghostsession.Routing, error) {
			return loadUDPGhostRouting(result.User, packet.DevModel, session.ClientInstanceID, false, session.Capabilities, fallbackGroupID)
		})
		if refreshErr != nil {
			ghostsession.Global.Remove(session.SessionID)
			return ProxiedDeviceAuthResult{Error: "client_preference_unavailable"}
		}
		session = refreshed
	}

	responseData := []byte{protocol.JWTAuthSuccess}
	if !legacy {
		responseData, err = protocol.EncodeGhostAuthSuccessData(protocol.GhostAuthSuccess{
			Version: protocol.GhostAuthPayloadVersion, SessionID: session.SessionID, SessionTag: session.SessionTag,
			ClientInstanceID: session.ClientInstanceID, TxGroupID: session.TxGroupID, RxGroupIDs: append([]int(nil), session.RxGroupIDs...),
		})
		if err != nil {
			ghostsession.Global.Remove(session.SessionID)
			return ProxiedDeviceAuthResult{Error: "authentication_response_failed"}
		}
	}
	response := encodeJWTAuthResponse(packet, result.CallSign, responseData)
	if !legacy {
		if tagged, ok := protocol.WithReservedUint32(response, session.SessionTag); ok {
			response = tagged
		}
	}
	return ProxiedDeviceAuthResult{
		Success: true, ResponsePacket: response, OwnerID: session.OwnerID, Username: session.Username,
		CallSign: session.CallSign, Nickname: session.Nickname, SSID: session.SSID, DevModel: session.DevModel,
		GroupID: session.TxGroupID, RxGroupIDs: append([]int(nil), session.RxGroupIDs...), GhostSessionID: session.SessionID,
		ClientInstanceID: session.ClientInstanceID, SessionTag: session.SessionTag, GhostProtocolVersion: session.ProtocolVersion,
		SourceGroupV1: session.HasCapability("source_group_v1"), DisableSend: session.DisableSend, DisableRecv: session.DisableRecv,
	}
}

func ConfirmProxiedGhostSession(itemID, edgeNodeID string, ownerID int, ssid, devModel byte, clientInstanceID string) (ghostsession.Session, error) {
	session, exists := ghostsession.Global.Get(strings.TrimSpace(itemID))
	edgeNodeID = strings.TrimSpace(edgeNodeID)
	if !exists || !session.Connected || session.Transport != ghostsession.TransportEdge || session.OwnerID != ownerID ||
		session.SSID != ssid || session.DevModel != devModel || session.ClientInstanceID != clientInstanceID ||
		edgeNodeID == "" || !strings.HasPrefix(session.Endpoint, edgeNodeID+"/") {
		return ghostsession.Session{}, ghostsession.ErrSessionNotFound
	}
	return session, nil
}

// DeviceSourceAddr is a small helper for callers that need a valid source IP.
func DeviceSourceAddr(addr *net.UDPAddr) string {
	if addr == nil || addr.IP == nil {
		return ""
	}
	return addr.IP.String()
}
