package udphub

import (
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"
	"draarl/internal/groupaccess"
	"draarl/internal/models"
	"draarl/internal/protocol"
	"draarl/pkg/jwt"
)

type JWTAuthResult struct {
	Success   bool
	User      *gormdb.User
	CallSign  string
	GroupID   uint
	ErrorCode byte
	ErrorMsg  string
}

// HandleJWTAuthPacket accepts only versioned, instance-bound ghost sessions.
func HandleJWTAuthPacket(packet *protocol.DraARLv1Packet, realAddr *net.UDPAddr, conn *net.UDPConn) {
	request, err := protocol.DecodeGhostAuthRequest(packet.DATA)
	if err != nil {
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "ghost_protocol_upgrade_required")
		return
	}
	instanceID, normalizeErr := ghostsession.NormalizeClientInstanceID(request.ClientInstanceID)
	if normalizeErr != nil {
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "invalid_client_instance_id")
		return
	}
	request.ClientInstanceID = instanceID
	if err := ghostsession.ValidateCapabilities(request.Capabilities); err != nil {
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "ghost_capabilities_required")
		return
	}

	result := AuthenticateJWT(request.Token)
	if !result.Success || result.User == nil {
		log.Printf("[UDP-JWT] authentication failed: model=%d err=%s", packet.DevModel, result.ErrorMsg)
		sendJWTAuthResponse(packet, conn, false, "", result.ErrorCode, result.ErrorMsg)
		return
	}
	if !protocol.IsGhostDevModel(packet.DevModel) {
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidDevModel, "Invalid device model for UDP")
		return
	}

	user := result.User
	ssid := protocol.GetGhostSSID(packet.DevModel)

	fallbackGroupID := GetGhostDeviceGroupID(user.ID, packet.DevModel)
	routing, err := loadUDPGhostRouting(user, packet.DevModel, request.ClientInstanceID, fallbackGroupID)
	if err != nil {
		log.Printf("[UDP-JWT] load routing failed: user=%d model=%d err=%v", user.ID, packet.DevModel, err)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "client_preference_unavailable")
		return
	}

	now := time.Now()
	device := &models.Device{
		Username: user.Name, CallSign: user.CallSign, Nickname: user.NickName,
		SSID: ssid, OwnerID: user.ID, CallSignSSID: protocol.GetCallSignSSID(user.CallSign, ssid),
		DevModel: packet.DevModel, GroupID: routing.TxGroupID, Priority: 100, Status: 0,
		ISOnline: true, UDPAddr: packet.UDPAddr, LastPacketTime: now, OnlineTime: now,
		ClientInstanceID: request.ClientInstanceID, GhostRxGroupIDs: append([]int(nil), routing.RxGroupIDs...),
		GhostProtocolVersion: request.Version, GhostCapabilities: append([]string(nil), request.Capabilities...),
	}

	controller := ghostsession.Controller{
		ApplyRouting: func(next ghostsession.Routing) error {
			if device.GhostSessionID != "" && GlobalUDPGhostManager.GetSession(device.GhostSessionID) == device {
				return applyAuthenticatedUDPGhostRouting(GlobalUDPGhostManager, device, next, ActivateCenterLocalDevice)
			}
			device.GroupID = next.TxGroupID
			device.GhostRxGroupIDs = append([]int(nil), next.RxGroupIDs...)
			return nil
		},
		Disconnect: func(string) {
			removed := GlobalUDPGhostManager.RemoveSession(device.GhostSessionID)
			if removed != nil {
				RevokeCenterLocalDevice(removed)
			}
		},
	}
	session, err := ghostsession.Global.Register(ghostsession.Registration{
		ClientInstanceID: request.ClientInstanceID,
		OwnerID:          user.ID, Username: user.Name, CallSign: user.CallSign, Nickname: user.NickName,
		DevModel: packet.DevModel, SSID: ssid, Transport: ghostsession.TransportUDP,
		Endpoint: udpEndpointString(packet.UDPAddr), ProtocolVersion: device.GhostProtocolVersion,
		Capabilities: request.Capabilities, Routing: routing, Now: now,
	}, controller)
	if err != nil {
		code := protocol.JWTAuthInvalidToken
		message := "ghost_session_registration_failed"
		switch {
		case errors.Is(err, ghostsession.ErrSessionLimit):
			message = fmt.Sprintf("ghost_session_limit active=%d limit=%d", len(ghostsession.Global.ListOwner(user.ID)), ghostsession.MaxSessionsPerOwner())
		}
		sendJWTAuthResponse(packet, conn, false, "", code, message)
		return
	}
	device.GhostSessionID = session.SessionID
	device.GhostSessionTag = session.SessionTag
	device.ClientInstanceID = session.ClientInstanceID
	device.GhostCapabilities = append([]string(nil), session.Capabilities...)
	device.GroupID = session.TxGroupID
	device.GhostRxGroupIDs = append([]int(nil), session.RxGroupIDs...)

	// Reload after registration so an API update racing with authentication
	// cannot be overwritten by a stale pre-auth preference snapshot.
	refreshed, refreshErr := ghostsession.Global.RefreshRouting(session.SessionID, func(ghostsession.Session) (ghostsession.Routing, error) {
		return loadUDPGhostRouting(user, packet.DevModel, session.ClientInstanceID, fallbackGroupID)
	})
	if refreshErr != nil {
		ghostsession.Global.Remove(session.SessionID)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "client_preference_unavailable")
		return
	}
	session = refreshed
	device.GroupID = session.TxGroupID
	device.GhostRxGroupIDs = append([]int(nil), session.RxGroupIDs...)

	if _, err := GlobalUDPGhostManager.RegisterSession(device); err != nil {
		ghostsession.Global.Remove(session.SessionID)
		log.Printf("[UDP-JWT] publish session failed: session=%s err=%v", ghostsession.ShortID(session.SessionID), err)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "ghost_session_registration_failed")
		return
	}
	if err := ActivateCenterLocalDevice(device); err != nil {
		GlobalUDPGhostManager.RemoveSession(session.SessionID)
		ghostsession.Global.Remove(session.SessionID)
		log.Printf("[UDP-JWT] center activation failed: session=%s err=%v", ghostsession.ShortID(session.SessionID), err)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "center_session_activation_failed")
		return
	}

	sendJWTAuthSessionResponse(packet, conn, user.Name, user.CallSign, protocol.GhostAuthSuccess{
		Version: protocol.GhostAuthPayloadVersion, SessionID: session.SessionID, SessionTag: session.SessionTag,
		ClientInstanceID: session.ClientInstanceID, TxGroupID: session.TxGroupID,
		RxGroupIDs: append([]int(nil), session.RxGroupIDs...),
	})
	log.Printf("[UDP-JWT] authenticated: session=%s user=%s model=%d tx=%d rx=%v",
		ghostsession.ShortID(session.SessionID), user.Name, packet.DevModel, session.TxGroupID, session.RxGroupIDs)
}

func applyAuthenticatedUDPGhostRouting(manager *UDPGhostManager, device *models.Device, next ghostsession.Routing, project func(*models.Device) error) error {
	if manager == nil || device == nil || manager.GetSession(device.GhostSessionID) != device {
		return ghostsession.ErrSessionNotFound
	}
	previous := ghostsession.Routing{
		TxGroupID: device.GroupID, RxGroupIDs: append([]int(nil), device.GhostRxGroupIDs...),
	}
	if err := manager.SetSessionRouting(device.GhostSessionID, next); err != nil {
		return err
	}
	if project == nil {
		return nil
	}
	projectionErr := project(device)
	if projectionErr == nil {
		return nil
	}

	rollbackErr := manager.SetSessionRouting(device.GhostSessionID, previous)
	if rollbackErr == nil {
		rollbackErr = project(device)
	}
	return errors.Join(projectionErr, rollbackErr)
}

func udpEndpointString(addr *net.UDPAddr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

func loadUDPGhostRouting(user *gormdb.User, devModel byte, instanceID string, fallbackGroupID int) (ghostsession.Routing, error) {
	if user == nil {
		return ghostsession.Routing{}, errors.New("authenticated user is required")
	}
	routing := ghostsession.Routing{TxGroupID: fallbackGroupID, RxGroupIDs: []int{fallbackGroupID}}
	repository := gormdb.NewGhostClientPreferenceRepository()
	preference, err := repository.GetOrCreate(user.ID, devModel, instanceID, fallbackGroupID)
	if err != nil || preference == nil {
		return ghostsession.Routing{}, errors.New("client preference unavailable")
	}
	routing = ghostsession.Routing{TxGroupID: preference.TxGroupID, RxGroupIDs: preference.RxGroupIDs}
	routing, changed, err := groupaccess.SanitizeRouting(gormdb.Get(), user, routing, models.GroupIDPublicMin, ghostsession.MaxSubscriptions())
	if err != nil {
		return ghostsession.Routing{}, err
	}
	if changed {
		if err := repository.ReplaceRouting(user.ID, devModel, instanceID, routing.TxGroupID, routing.RxGroupIDs); err != nil {
			return ghostsession.Routing{}, err
		}
	}
	return routing, nil
}

func sendJWTAuthResponse(packet *protocol.DraARLv1Packet, conn *net.UDPConn, success bool, callSign string, errorCode byte, errorMsg string) {
	data := []byte{protocol.JWTAuthSuccess}
	responseCallSign := callSign
	if !success {
		data = append([]byte{errorCode}, []byte(errorMsg)...)
		responseCallSign = ""
	}
	response := encodeJWTAuthResponse(packet, packet.Username, responseCallSign, data)
	if conn != nil && packet != nil && packet.UDPAddr != nil {
		_, _ = conn.WriteToUDP(response, packet.UDPAddr)
	}
}

func sendJWTAuthSessionResponse(packet *protocol.DraARLv1Packet, conn *net.UDPConn, username, callSign string, success protocol.GhostAuthSuccess) {
	data, err := protocol.EncodeGhostAuthSuccessData(success)
	if err != nil {
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "authentication_response_failed")
		return
	}
	response := encodeJWTAuthResponse(packet, username, callSign, data)
	if tagged, ok := protocol.WithReservedUint32(response, success.SessionTag); ok {
		response = tagged
	}
	if conn != nil && packet != nil && packet.UDPAddr != nil {
		_, _ = conn.WriteToUDP(response, packet.UDPAddr)
	}
}

func encodeJWTAuthResponse(packet *protocol.DraARLv1Packet, username, callSign string, data []byte) []byte {
	ssid := protocol.GetGhostSSID(packet.DevModel)
	if ssid == 0 {
		ssid = packet.DevModel
	}
	return protocol.EncodeDraARLv1(username, "", ssid, protocol.DraARLTypeJWTAuth, packet.DevModel, 0, callSign, data)
}

func AuthenticateJWT(token string) *JWTAuthResult {
	result := &JWTAuthResult{}
	claims, err := jwt.ValidateAccessToken(token)
	if err != nil {
		result.ErrorCode, result.ErrorMsg = protocol.JWTAuthInvalidToken, "Invalid or expired token"
		return result
	}
	user, err := gormdb.NewUserRepository().GetUserByName(claims.Username)
	if err != nil || user == nil {
		result.ErrorCode, result.ErrorMsg = protocol.JWTAuthUserNotFound, "User not found"
		return result
	}
	if user.Status != 1 {
		result.ErrorCode, result.ErrorMsg = protocol.JWTAuthUserDisabled, "User is disabled"
		return result
	}
	if user.ApprovalStatus != 1 {
		result.ErrorCode, result.ErrorMsg = protocol.JWTAuthUserNotApproved, "User is not approved"
		return result
	}
	result.Success, result.User, result.CallSign = true, user, user.CallSign
	return result
}

func GetGhostDeviceGroupID(userID int, devModel byte) int {
	groupID, err := gormdb.NewUserRepository().GetUserLastGroupID(userID, devModel)
	if err != nil || groupID == 0 {
		return models.GroupIDPublicMin
	}
	if group, exists := GetGroupFromCache(groupID); exists && group.Status == 1 {
		return groupID
	}
	return models.GroupIDPublicMin
}
