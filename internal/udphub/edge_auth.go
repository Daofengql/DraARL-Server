package udphub

import (
	"fmt"
	"net"
	"time"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

// ProxiedDeviceAuthResult is the centre-side result returned to an edge. It
// contains only a short-lived grant and the ordinary device response; no
// password or JWT is ever returned to the edge.
type ProxiedDeviceAuthResult struct {
	Success        bool
	Error          string
	ResponsePacket []byte
	DeviceID       int
	OwnerID        int
	Username       string
	CallSign       string
	Nickname       string
	SSID           byte
	DevModel       byte
	DMRID          uint32
	GroupID        int
	DisableSend    bool
	DisableRecv    bool
}

// AuthenticateProxiedDevice applies the same device authentication and
// registration rules as the centre UDP path, but returns a serialisable grant
// for an authenticated edge. It is intentionally called only after the centre
// has decoded the Type 0 DeviceAuthRequest.
func AuthenticateProxiedDevice(sourceIP string, wire []byte) ProxiedDeviceAuthResult {
	packet, err := protocol.NewDraARLv1RoutingPacket(nil, wire)
	if err != nil {
		return ProxiedDeviceAuthResult{Error: "invalid_device_packet"}
	}
	defer protocol.ReleaseDraARLv1RoutingPacket(packet)
	if packet.Type == protocol.DraARLTypeJWTAuth {
		return authenticateProxiedJWT(sourceIP, packet)
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

func authenticateProxiedJWT(sourceIP string, packet *protocol.DraARLv1Packet) ProxiedDeviceAuthResult {
	result := AuthenticateJWT(string(packet.DATA))
	if !result.Success || result.User == nil {
		return ProxiedDeviceAuthResult{Error: result.ErrorMsg}
	}
	ssid := protocol.GetGhostSSID(packet.DevModel)
	if ssid == 0 {
		return ProxiedDeviceAuthResult{Error: "invalid_device_model"}
	}
	groupID := GetGhostDeviceGroupID(result.User.ID, packet.DevModel)
	now := time.Now()
	ghost := GlobalUDPGhostManager.Get(result.User.Name, ssid)
	if ghost == nil {
		ghost = &models.Device{Username: result.User.Name, CallSign: result.CallSign, Nickname: result.User.NickName, SSID: ssid, OwnerID: result.User.ID, DevModel: packet.DevModel, GroupID: groupID, ISOnline: true, LastPacketTime: now, OnlineTime: now}
		GlobalUDPGhostManager.Register(ghost)
	} else {
		ghost.ISOnline, ghost.LastPacketTime, ghost.UDPAddr = true, now, packet.UDPAddr
		ghost.Username, ghost.CallSign, ghost.Nickname = result.User.Name, result.CallSign, result.User.NickName
	}
	response := protocol.EncodeDraARLv1(packet.Username, "", ssid, protocol.DraARLTypeJWTAuth, packet.DevModel, 0, result.CallSign, []byte{protocol.JWTAuthSuccess})
	_ = sourceIP
	return ProxiedDeviceAuthResult{Success: true, ResponsePacket: response, DeviceID: ghost.ID, OwnerID: ghost.OwnerID, Username: ghost.Username, CallSign: ghost.CallSign, Nickname: ghost.Nickname, SSID: ghost.SSID, DevModel: ghost.DevModel, DMRID: ghost.DMRID, GroupID: ghost.GroupID, DisableSend: ghost.DisableSend, DisableRecv: ghost.DisableRecv}
}

// DeviceSourceAddr is a small helper for callers that need a valid source IP.
func DeviceSourceAddr(addr *net.UDPAddr) string {
	if addr == nil || addr.IP == nil {
		return ""
	}
	return addr.IP.String()
}
