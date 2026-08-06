package websocket

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"
	"draarl/internal/groupaccess"
	"draarl/internal/models"
	"draarl/internal/protocol"
	"draarl/internal/udphub"
	"draarl/pkg/jwt"

	"github.com/gorilla/websocket"
)

// AuthType 认证类型
type AuthType int

const (
	AuthTypeNone AuthType = iota // 未认证
	AuthTypeJWT                  // JWT 认证（幽灵设备）

	wsTokenCookieName = "ws_token"
)

// AuthResult 认证结果
type AuthResult struct {
	Success          bool
	AuthType         AuthType
	UserID           int
	Username         string
	CallSign         string
	Nickname         string
	GroupID          int // 设备所属群组ID（从数据库读取）
	ClientInstanceID string
	LegacySession    bool
	ProtocolVersion  uint16
	Capabilities     []string
	Routing          ghostsession.Routing
	User             *gormdb.User
	Error            string
}

// WSPreAuthData 预认证数据（仅从 HttpOnly Cookie 中提取，避免 URL/JS 透传 token）
type WSPreAuthData struct {
	Token            string // JWT Token
	ClientInstanceID string
	ProtocolVersion  uint16
	Capabilities     []string
}

// ParsePreAuthData 从请求中解析预认证数据
func ParsePreAuthData(r *http.Request) *WSPreAuthData {
	data := &WSPreAuthData{}

	// 仅读取专用 ws_token（由后端 Set-Cookie 注入 HttpOnly）
	if cookie, err := r.Cookie(wsTokenCookieName); err == nil {
		data.Token = cookie.Value
	}
	data.ClientInstanceID = strings.TrimSpace(r.Header.Get("X-DraARL-Client-Instance-ID"))
	if data.ClientInstanceID == "" {
		data.ClientInstanceID = strings.TrimSpace(r.URL.Query().Get("client_instance_id"))
	}
	protocolVersion := strings.TrimSpace(r.Header.Get("X-DraARL-Protocol-Version"))
	if protocolVersion == "" {
		protocolVersion = strings.TrimSpace(r.URL.Query().Get("protocol_version"))
	}
	if parsed, err := strconv.ParseUint(protocolVersion, 10, 16); err == nil {
		data.ProtocolVersion = uint16(parsed)
	}
	capabilities := strings.TrimSpace(r.Header.Get("X-DraARL-Capabilities"))
	if capabilities == "" {
		capabilities = strings.TrimSpace(r.URL.Query().Get("capabilities"))
	}
	if capabilities != "" {
		data.Capabilities = strings.Split(capabilities, ",")
	}

	return data
}

// HandleAuthentication 处理 WebSocket 认证流程（仅支持 JWT 认证）
func RegisterAuthenticatedConnection(conn *websocket.Conn, manager *WSConnectionManager, authResult *AuthResult) (*WSDevice, error) {
	// 注册连接
	device := manager.RegisterConnection(conn)
	device.ConnState = StateAuthenticating
	if authResult == nil || !authResult.Success {
		manager.UnregisterDevice(device)
		return nil, errors.New("authentication result is not successful")
	}

	controller := ghostsession.Controller{
		ApplyRouting: func(routing ghostsession.Routing) error {
			if current, exists := manager.GetGhostSession(device.SessionID); exists && current == device {
				return applyAuthenticatedRouting(manager, device, routing, func(device *WSDevice, groupID int) bool {
					return udphub.AuthorizeCenterLocalWS(device, groupID)
				})
			}
			device.setRouting(routing)
			return nil
		},
		Disconnect: func(string) {
			manager.DisconnectDevice(device)
		},
	}
	session, err := ghostsession.Global.Register(ghostsession.Registration{
		ClientInstanceID: authResult.ClientInstanceID, OwnerID: authResult.UserID,
		Username: authResult.Username, CallSign: authResult.CallSign, Nickname: authResult.Nickname,
		DevModel: protocol.DraARLDevModelBrowser, SSID: fixedWebGhostSSID, Transport: ghostsession.TransportWebSocket,
		Endpoint: conn.RemoteAddr().String(), ProtocolVersion: authResult.ProtocolVersion,
		Capabilities: authResult.Capabilities, Routing: authResult.Routing,
	}, controller)
	if err != nil {
		manager.UnregisterDevice(device)
		return nil, err
	}
	device.SessionID = session.SessionID
	device.SessionTag = session.SessionTag
	device.ClientInstanceID = session.ClientInstanceID
	device.LegacySession = session.Legacy
	device.ProtocolVersion = session.ProtocolVersion
	device.Capabilities = append([]string(nil), session.Capabilities...)
	if !session.Legacy {
		registeredSessionID := session.SessionID
		clientInstanceID := session.ClientInstanceID
		refreshed, refreshErr := ghostsession.Global.RefreshRouting(registeredSessionID, func(ghostsession.Session) (ghostsession.Routing, error) {
			return loadWebSocketInstanceRouting(authResult.User, clientInstanceID, authResult.GroupID, authResult.Capabilities)
		})
		if refreshErr != nil {
			ghostsession.Global.Remove(registeredSessionID)
			manager.UnregisterDevice(device)
			return nil, refreshErr
		}
		session = refreshed
	}
	device.SSID = session.SSID
	device.DevModel = session.DevModel
	device.GroupID = session.TxGroupID
	device.RxGroupIDs = append([]int(nil), session.RxGroupIDs...)
	if err := manager.RegisterGhostDevice(device, session.OwnerID, session.Username, session.CallSign, session.Nickname, session.SSID); err != nil {
		ghostsession.Global.Remove(session.SessionID)
		manager.UnregisterDevice(device)
		return nil, err
	}
	log.Printf("[WS-AUTH] session authenticated: session=%s user=%d tx=%d rx=%v legacy=%v", ghostsession.ShortID(session.SessionID), session.OwnerID, session.TxGroupID, session.RxGroupIDs, session.Legacy)
	return device, nil
}

func applyAuthenticatedRouting(manager *WSConnectionManager, device *WSDevice, routing ghostsession.Routing, authorize func(*WSDevice, int) bool) error {
	previous := ghostsession.Routing{TxGroupID: device.GetGroupID(), RxGroupIDs: device.GetRxGroupIDs()}
	if err := manager.SetDeviceRouting(device, routing); err != nil {
		return err
	}
	if authorize == nil || authorize(device, routing.TxGroupID) {
		return nil
	}

	projectionErr := errors.New("center session route update failed")
	rollbackErr := manager.SetDeviceRouting(device, previous)
	if rollbackErr == nil && !authorize(device, previous.TxGroupID) {
		rollbackErr = errors.New("center session route rollback failed")
	}
	return errors.Join(projectionErr, rollbackErr)
}

func AuthenticateWebSocketRequest(r *http.Request) *AuthResult {
	preAuth := ParsePreAuthData(r)
	if preAuth.Token == "" {
		return &AuthResult{Error: "token_required"}
	}
	result := AuthenticateJWT(preAuth.Token)
	if !result.Success {
		return result
	}
	instanceID, legacy, err := ghostsession.NormalizeClientInstanceID(preAuth.ClientInstanceID)
	if err != nil {
		result.Success = false
		result.Error = "invalid_client_instance_id"
		return result
	}
	result.ClientInstanceID = instanceID
	result.LegacySession = legacy
	result.ProtocolVersion = preAuth.ProtocolVersion
	result.Capabilities = preAuth.Capabilities
	result.Routing = ghostsession.Routing{TxGroupID: result.GroupID, RxGroupIDs: []int{result.GroupID}}
	if legacy {
		return result
	}
	routing, err := loadWebSocketInstanceRouting(result.User, instanceID, result.GroupID, preAuth.Capabilities)
	if err != nil {
		result.Success = false
		result.Error = "client_preference_unavailable"
		return result
	}
	result.Routing = routing
	return result
}

func loadWebSocketInstanceRouting(user *gormdb.User, instanceID string, fallbackGroupID int, capabilities []string) (ghostsession.Routing, error) {
	if user == nil {
		return ghostsession.Routing{}, errors.New("authenticated user is required")
	}
	repository := gormdb.NewGhostClientPreferenceRepository()
	pref, err := repository.GetOrCreate(user.ID, protocol.DraARLDevModelBrowser, instanceID, fallbackGroupID)
	if err != nil || pref == nil {
		return ghostsession.Routing{}, errors.New("client preference unavailable")
	}
	routing, changed, err := sanitizePersistedRouting(user, ghostsession.Routing{
		TxGroupID: pref.TxGroupID, RxGroupIDs: pref.RxGroupIDs,
	}, models.GroupIDPublicMin)
	if err != nil {
		return ghostsession.Routing{}, err
	}
	if changed {
		if err := repository.ReplaceRouting(user.ID, protocol.DraARLDevModelBrowser, instanceID, routing.TxGroupID, routing.RxGroupIDs); err != nil {
			return ghostsession.Routing{}, err
		}
	}
	if !hasCapability(capabilities, "multi_receive_v1") || !hasCapability(capabilities, "source_group_v1") {
		routing.RxGroupIDs = []int{routing.TxGroupID}
	}
	return routing, nil
}

func sanitizePersistedRouting(user *gormdb.User, routing ghostsession.Routing, fallbackGroupID int) (ghostsession.Routing, bool, error) {
	return groupaccess.SanitizeRouting(gormdb.Get(), user, routing, fallbackGroupID, ghostsession.MaxSubscriptions())
}

func hasCapability(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), wanted) {
			return true
		}
	}
	return false
}

// AuthenticateJWT 进行 JWT 认证（幽灵设备）
func AuthenticateJWT(tokenString string) *AuthResult {
	result := &AuthResult{
		AuthType: AuthTypeJWT,
	}

	// 解析 JWT Token
	claims, err := jwt.ValidateAccessToken(tokenString)
	if err != nil {
		result.Error = "invalid_token"
		log.Printf("[WS-AUTH] JWT parse failed: %v", err)
		return result
	}

	// 获取用户信息
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(claims.Username)
	if err != nil || user == nil {
		result.Error = "user_not_found"
		log.Printf("[WS-AUTH] User not found: %s", claims.Username)
		return result
	}

	// 检查用户状态
	if user.Status != 1 {
		result.Error = "user_disabled"
		log.Printf("[WS-AUTH] User disabled: %s", claims.Username)
		return result
	}

	// 检查审核状态
	if user.ApprovalStatus != 1 {
		result.Error = "user_not_approved"
		log.Printf("[WS-AUTH] User not approved: %s", claims.Username)
		return result
	}

	result.Success = true
	result.UserID = user.ID
	result.User = user
	result.Username = user.Name
	result.CallSign = user.CallSign
	result.Nickname = user.NickName

	// 使用分平台群组偏好 (user_device_preferences 表)
	// DevModel=105 为 Web 浏览器端
	lastGroupID, err := repo.GetUserLastGroupID(user.ID, protocol.DraARLDevModelBrowser)
	if err != nil {
		log.Printf("[WS-AUTH] 获取用户 %d 的群组偏好失败: %v，使用默认群组", user.ID, err)
		lastGroupID = models.GroupIDPublicMin
	}
	if lastGroupID != models.GroupIDPublicMin {
		group, groupErr := gormdb.NewGroupRepository().GetGroupByID(lastGroupID)
		isVerifiedMember := false
		if groupErr == nil && group != nil && group.Status == 1 && !group.IsVirtual &&
			group.Type == 2 && !user.HasRole("admin") && group.OwerID != user.ID {
			member, memberErr := gormdb.NewGroupMemberRepository().GetVerifiedMemberByGroupAndUser(group.ID, user.ID)
			if memberErr != nil {
				groupErr = memberErr
			} else {
				isVerifiedMember = member != nil
			}
		}
		if groupErr != nil || !canUseGroupForWebGhost(user, lastGroupID, group, isVerifiedMember) {
			log.Printf("[WS-AUTH] 用户 %d 的群组偏好 %d 已失效，回退默认群组", user.ID, lastGroupID)
			lastGroupID = models.GroupIDPublicMin
		}
	}
	result.GroupID = lastGroupID

	log.Printf("[WS-AUTH] JWT auth success: user-%d (%s) group-%d", user.ID, user.CallSign, result.GroupID)
	return result
}

func canUseGroupForWebGhost(user *gormdb.User, groupID int, group *gormdb.Group, isVerifiedMember bool) bool {
	if user == nil || groupID <= 0 {
		return false
	}
	if groupID == models.GroupIDPublicMin {
		return true
	}
	if group == nil || group.ID != groupID {
		return false
	}
	return groupaccess.CanView(user, group, isVerifiedMember)
}
