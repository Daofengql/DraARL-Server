package websocket

import (
	"log"
	"net/http"

	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/internal/protocol"
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
	Success  bool
	AuthType AuthType
	UserID   int
	Username string
	CallSign string
	Nickname string
	GroupID  int // 设备所属群组ID（从数据库读取）
	Error    string
}

// WSPreAuthData 预认证数据（仅从 HttpOnly Cookie 中提取，避免 URL/JS 透传 token）
type WSPreAuthData struct {
	Token string // JWT Token
}

// ParsePreAuthData 从请求中解析预认证数据
func ParsePreAuthData(r *http.Request) *WSPreAuthData {
	data := &WSPreAuthData{}

	// 仅读取专用 ws_token（由后端 Set-Cookie 注入 HttpOnly）
	if cookie, err := r.Cookie(wsTokenCookieName); err == nil {
		data.Token = cookie.Value
	}

	return data
}

// HandleAuthentication 处理 WebSocket 认证流程（仅支持 JWT 认证）
func HandleAuthentication(conn *websocket.Conn, r *http.Request, manager *WSConnectionManager) (*WSDevice, *AuthResult) {
	preAuth := ParsePreAuthData(r)

	// 注册连接
	device := manager.RegisterConnection(conn)

	// 必须提供 JWT Token
	if preAuth.Token == "" {
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "token_required"))
		manager.UnregisterDevice(device)
		return nil, &AuthResult{Error: "token_required"}
	}

	// JWT 认证
	device.ConnState = StateAuthenticating
	authResult := AuthenticateJWT(preAuth.Token)

	if authResult.Success {
		// JWT 认证的设备 SSID 和 DevModel 统一为 105（Web 浏览器）
		// 注意：不同平台客户端（100-104）应通过心跳包更新 DevModel
		device.SSID = fixedWebGhostSSID
		device.DevModel = protocol.DraARLDevModelBrowser
		device.GroupID = authResult.GroupID
		log.Printf("[WS-AUTH] JWT 认证成功: 用户 %d (%s), 群组 %d", authResult.UserID, authResult.CallSign, authResult.GroupID)

		manager.RegisterGhostDevice(device, authResult.UserID, authResult.Username, authResult.CallSign, authResult.Nickname, fixedWebGhostSSID)

		// 同时创建 GhostDevice 并建立与 WSDevice 的关联
		GlobalGhostManager.CreateGhostDevice(device, authResult.UserID, authResult.Username, authResult.CallSign, authResult.Nickname, fixedWebGhostSSID)

		return device, authResult
	}

	// JWT 认证失败，关闭连接
	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, authResult.Error))
	manager.UnregisterDevice(device)
	return nil, authResult
}

// AuthenticateJWT 进行 JWT 认证（幽灵设备）
func AuthenticateJWT(tokenString string) *AuthResult {
	result := &AuthResult{
		AuthType: AuthTypeJWT,
	}

	// 解析 JWT Token
	claims, err := jwt.ParseToken(tokenString)
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
	result.GroupID = lastGroupID

	log.Printf("[WS-AUTH] JWT auth success: user-%d (%s) group-%d", user.ID, user.CallSign, result.GroupID)
	return result
}
