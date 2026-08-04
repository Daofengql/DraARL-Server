package udphub

import (
	"errors"
	"log"
	"net"
	"strings"
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

// HandleJWTAuthPacket accepts both the historical raw-JWT payload and the
// versioned session payload. Only the latter enables independent multi-device
// sessions and session-tag packet authentication.
func HandleJWTAuthPacket(packet *protocol.DraARLv1Packet, realAddr *net.UDPAddr, conn *net.UDPConn) {
	request, legacy, err := protocol.DecodeGhostAuthRequest(packet.DATA)
	if err != nil {
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "Invalid authentication payload")
		return
	}
	if !legacy {
		instanceID, normalizedLegacy, normalizeErr := ghostsession.NormalizeClientInstanceID(request.ClientInstanceID)
		if normalizeErr != nil || normalizedLegacy {
			sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "Invalid client instance id")
			return
		}
		request.ClientInstanceID = instanceID
	}

	result := AuthenticateJWT(request.Token)
	if !result.Success || result.User == nil {
		log.Printf("[UDP-JWT] authentication failed: addr=%v err=%s", realAddr, result.ErrorMsg)
		sendJWTAuthResponse(packet, conn, false, "", result.ErrorCode, result.ErrorMsg)
		return
	}
	if !protocol.IsGhostDevModel(packet.DevModel) {
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidDevModel, "Invalid device model for UDP")
		return
	}

	user := result.User
	ssid := protocol.GetGhostSSID(packet.DevModel)
	if legacy {
		if existing := GlobalUDPGhostManager.Get(user.Name, ssid); existing != nil {
			if isRecentlyActiveDevice(existing) && !sameUDPAddr(existing.UDPAddr, packet.UDPAddr) {
				log.Printf("[UDP-JWT] legacy ghost conflict: user=%s model=%d old=%v new=%v", user.Name, packet.DevModel, existing.UDPAddr, packet.UDPAddr)
				sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthGhostDeviceConflict, "Ghost device already online")
				return
			}
			if isRecentlyActiveDevice(existing) && existing.GhostSessionID != "" {
				if session, exists := ghostsession.Global.Get(existing.GhostSessionID); exists && session.Transport == ghostsession.TransportUDP {
					refreshAuthenticatedGhost(existing, user, packet.UDPAddr)
					GlobalUDPGhostManager.UpdateSessionActivity(existing.GhostSessionID, time.Now())
					if err := ActivateCenterLocalDevice(existing); err != nil {
						sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "center_session_activation_failed")
						return
					}
					sendJWTAuthResponse(packet, conn, true, user.CallSign, protocol.JWTAuthSuccess, "")
					return
				}
			}
			if existing.GhostSessionID != "" {
				GlobalUDPGhostManager.RemoveSession(existing.GhostSessionID)
				ghostsession.Global.Remove(existing.GhostSessionID)
			} else {
				GlobalUDPGhostManager.Remove(existing.Username, existing.SSID)
			}
			RevokeCenterLocalDevice(existing)
		}
	}

	fallbackGroupID := GetGhostDeviceGroupID(user.ID, packet.DevModel)
	routing, err := loadUDPGhostRouting(user, packet.DevModel, request.ClientInstanceID, legacy, request.Capabilities, fallbackGroupID)
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
		GhostCapabilities: append([]string(nil), request.Capabilities...),
	}
	if !legacy {
		device.GhostProtocolVersion = request.Version
	}

	controller := ghostsession.Controller{
		ApplyRouting: func(next ghostsession.Routing) error {
			if device.GhostSessionID != "" && GlobalUDPGhostManager.GetSession(device.GhostSessionID) == device {
				return GlobalUDPGhostManager.SetSessionRouting(device.GhostSessionID, next)
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
		ClientInstanceID: request.ClientInstanceID, ReplaceExisting: legacy,
		OwnerID: user.ID, Username: user.Name, CallSign: user.CallSign, Nickname: user.NickName,
		DevModel: packet.DevModel, SSID: ssid, Transport: ghostsession.TransportUDP,
		Endpoint: udpEndpointString(packet.UDPAddr), ProtocolVersion: device.GhostProtocolVersion,
		Capabilities: request.Capabilities, Routing: routing, Now: now,
	}, controller)
	if err != nil {
		code := protocol.JWTAuthInvalidToken
		message := "ghost_session_registration_failed"
		if errors.Is(err, ghostsession.ErrInstanceAlreadyOnline) {
			code, message = protocol.JWTAuthGhostDeviceConflict, "Ghost device already online"
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
	if !legacy {
		refreshed, refreshErr := ghostsession.Global.RefreshRouting(session.SessionID, func(ghostsession.Session) (ghostsession.Routing, error) {
			return loadUDPGhostRouting(user, packet.DevModel, session.ClientInstanceID, false, session.Capabilities, fallbackGroupID)
		})
		if refreshErr != nil {
			ghostsession.Global.Remove(session.SessionID)
			sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "client_preference_unavailable")
			return
		}
		session = refreshed
		device.GroupID = session.TxGroupID
		device.GhostRxGroupIDs = append([]int(nil), session.RxGroupIDs...)
	}

	if _, err := GlobalUDPGhostManager.RegisterSession(device); err != nil {
		ghostsession.Global.Remove(session.SessionID)
		log.Printf("[UDP-JWT] publish session failed: session=%s err=%v", session.SessionID, err)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "ghost_session_registration_failed")
		return
	}
	if err := ActivateCenterLocalDevice(device); err != nil {
		GlobalUDPGhostManager.RemoveSession(session.SessionID)
		ghostsession.Global.Remove(session.SessionID)
		log.Printf("[UDP-JWT] center activation failed: session=%s err=%v", session.SessionID, err)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "center_session_activation_failed")
		return
	}

	if legacy {
		sendJWTAuthResponse(packet, conn, true, user.CallSign, protocol.JWTAuthSuccess, "")
	} else {
		sendJWTAuthSessionResponse(packet, conn, user.CallSign, protocol.GhostAuthSuccess{
			Version: protocol.GhostAuthPayloadVersion, SessionID: session.SessionID, SessionTag: session.SessionTag,
			ClientInstanceID: session.ClientInstanceID, TxGroupID: session.TxGroupID,
			RxGroupIDs: append([]int(nil), session.RxGroupIDs...),
		})
	}
	log.Printf("[UDP-JWT] authenticated: session=%s user=%s model=%d tx=%d rx=%v legacy=%v addr=%v",
		session.SessionID, user.Name, packet.DevModel, session.TxGroupID, session.RxGroupIDs, legacy, realAddr)
}

func udpEndpointString(addr *net.UDPAddr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

func refreshAuthenticatedGhost(device *models.Device, user *gormdb.User, addr *net.UDPAddr) {
	if device == nil || user == nil {
		return
	}
	device.Username = user.Name
	device.CallSign = user.CallSign
	device.Nickname = user.NickName
	device.OwnerID = user.ID
	device.CallSignSSID = protocol.GetCallSignSSID(user.CallSign, device.SSID)
	device.UDPAddr = addr
	device.LastPacketTime = time.Now()
	device.ISOnline = true
}

func loadUDPGhostRouting(user *gormdb.User, devModel byte, instanceID string, legacy bool, capabilities []string, fallbackGroupID int) (ghostsession.Routing, error) {
	if user == nil {
		return ghostsession.Routing{}, errors.New("authenticated user is required")
	}
	routing := ghostsession.Routing{TxGroupID: fallbackGroupID, RxGroupIDs: []int{fallbackGroupID}}
	if legacy {
		sanitized, _, err := groupaccess.SanitizeRouting(gormdb.Get(), user, routing, models.GroupIDPublicMin, ghostsession.DefaultMaxSubscriptions)
		if err != nil {
			return ghostsession.Routing{}, err
		}
		sanitized.RxGroupIDs = []int{sanitized.TxGroupID}
		return sanitized, nil
	}

	repository := gormdb.NewGhostClientPreferenceRepository()
	preference, err := repository.GetOrCreate(user.ID, devModel, instanceID, fallbackGroupID)
	if err != nil || preference == nil {
		return ghostsession.Routing{}, errors.New("client preference unavailable")
	}
	routing = ghostsession.Routing{TxGroupID: preference.TxGroupID, RxGroupIDs: preference.RxGroupIDs}
	routing, changed, err := groupaccess.SanitizeRouting(gormdb.Get(), user, routing, models.GroupIDPublicMin, ghostsession.DefaultMaxSubscriptions)
	if err != nil {
		return ghostsession.Routing{}, err
	}
	if changed {
		if err := repository.ReplaceRouting(user.ID, devModel, instanceID, routing.TxGroupID, routing.RxGroupIDs); err != nil {
			return ghostsession.Routing{}, err
		}
	}
	if !ghostCapability(capabilities, "multi_receive_v1") || !ghostCapability(capabilities, "source_group_v1") {
		routing.RxGroupIDs = []int{routing.TxGroupID}
	}
	return routing, nil
}

func ghostCapability(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), wanted) {
			return true
		}
	}
	return false
}

func sendJWTAuthResponse(packet *protocol.DraARLv1Packet, conn *net.UDPConn, success bool, callSign string, errorCode byte, errorMsg string) {
	data := []byte{protocol.JWTAuthSuccess}
	responseCallSign := callSign
	if !success {
		data = append([]byte{errorCode}, []byte(errorMsg)...)
		responseCallSign = ""
	}
	response := encodeJWTAuthResponse(packet, responseCallSign, data)
	if conn != nil && packet != nil && packet.UDPAddr != nil {
		_, _ = conn.WriteToUDP(response, packet.UDPAddr)
	}
}

func sendJWTAuthSessionResponse(packet *protocol.DraARLv1Packet, conn *net.UDPConn, callSign string, success protocol.GhostAuthSuccess) {
	data, err := protocol.EncodeGhostAuthSuccessData(success)
	if err != nil {
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "authentication_response_failed")
		return
	}
	response := encodeJWTAuthResponse(packet, callSign, data)
	if tagged, ok := protocol.WithReservedUint32(response, success.SessionTag); ok {
		response = tagged
	}
	if conn != nil && packet != nil && packet.UDPAddr != nil {
		_, _ = conn.WriteToUDP(response, packet.UDPAddr)
	}
}

func encodeJWTAuthResponse(packet *protocol.DraARLv1Packet, callSign string, data []byte) []byte {
	ssid := protocol.GetGhostSSID(packet.DevModel)
	if ssid == 0 {
		ssid = packet.DevModel
	}
	return protocol.EncodeDraARLv1(packet.Username, "", ssid, protocol.DraARLTypeJWTAuth, packet.DevModel, 0, callSign, data)
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
