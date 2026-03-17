package websocket

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"nrllink/internal/gormdb"
	"nrllink/internal/protocol"
	"nrllink/internal/udphub"
	"nrllink/pkg/jwt"

	"github.com/gorilla/websocket"
)

// AuthType 认证类型
type AuthType int

const (
	AuthTypeNone    AuthType = iota // 未认证
	AuthTypeJWT                     // JWT 认证（幽灵设备）
	AuthTypeDevice                  // 设备密码认证（普通设备）
)

// AuthResult 认证结果
type AuthResult struct {
	Success     bool
	AuthType    AuthType
	UserID      int
	Username    string
	CallSign    string
	Nickname    string
	DeviceID    int
	SSID        byte
	Error       string
}

// WSPreAuthData 预认证数据（从 URL 参数或 Cookie 中提取）
type WSPreAuthData struct {
	Token    string // JWT Token
	Username string // 用户名（可选，用于设备认证）
	SSID     byte   // SSID（仅设备认证使用）
}

// ParsePreAuthData 从请求中解析预认证数据
func ParsePreAuthData(r *http.Request) *WSPreAuthData {
	data := &WSPreAuthData{}

	// 1. 尝试从 URL 参数获取 token
	data.Token = r.URL.Query().Get("token")

	// 2. 尝试从 URL 参数获取 ssid
	if ssidStr := r.URL.Query().Get("ssid"); ssidStr != "" {
		var ssid int
		if _, err := fmt.Sscanf(ssidStr, "%d", &ssid); err == nil {
			data.SSID = byte(ssid)
		}
	}

	// 3. 如果 URL 参数中没有 token，尝试从 Cookie 获取
	if data.Token == "" {
		if cookie, err := r.Cookie("token"); err == nil {
			data.Token = cookie.Value
		}
	}

	// 4. 设置默认 SSID
	if data.SSID == 0 {
		data.SSID = 10 // 默认 SSID
	}

	return data
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

	log.Printf("[WS-AUTH] JWT auth success: user-%d (%s)", user.ID, user.CallSign)
	return result
}

// AuthenticateDevice 进行设备密码认证（普通设备）
func AuthenticateDevice(username, password string, ssid byte) *AuthResult {
	result := &AuthResult{
		AuthType: AuthTypeDevice,
	}

	// 使用 udphub 包的认证逻辑
	// 注意：这里直接调用 udphub.AuthenticateDevice，它会处理密码验证
	authResult := udphub.AuthenticateDevice("", username, password)
	if !authResult.Success {
		result.Error = authResult.Error
		log.Printf("[WS-AUTH] Device auth failed: %s, error: %s", username, authResult.Error)
		return result
	}

	// 查找设备
	deviceRepo := gormdb.NewDeviceRepository()
	devices, err := deviceRepo.ListDevicesByOwnerID(authResult.User.ID)
	if err != nil {
		result.Error = "device_query_failed"
		log.Printf("[WS-AUTH] Device query failed: %v", err)
		return result
	}

	// 查找匹配 SSID 的设备
	var targetDevice *gormdb.Device
	for _, dev := range devices {
		if dev.SSID == ssid {
			targetDevice = dev
			break
		}
	}

	// 如果没有找到匹配的设备，使用第一个设备
	// 或者如果 SSID 为 0，使用第一个设备
	if targetDevice == nil && len(devices) > 0 {
		targetDevice = devices[0]
	}

	result.Success = true
	result.UserID = authResult.User.ID
	result.Username = username
	result.CallSign = authResult.CallSign
	result.SSID = ssid

	if targetDevice != nil {
		result.DeviceID = targetDevice.ID
		result.SSID = byte(targetDevice.SSID)
	}

	log.Printf("[WS-AUTH] Device auth success: %s-%d (device-%d)",
		result.CallSign, result.SSID, result.DeviceID)
	return result
}

// HandleAuthentication 处理 WebSocket 认证流程
func HandleAuthentication(conn *websocket.Conn, r *http.Request, manager *WSConnectionManager) (*WSDevice, *AuthResult) {
	preAuth := ParsePreAuthData(r)

	// 注册连接
	device := manager.RegisterConnection(conn)

	// 如果有 JWT Token，尝试 JWT 认证
	if preAuth.Token != "" {
		device.ConnState = StateAuthenticating
		authResult := AuthenticateJWT(preAuth.Token)

		if authResult.Success {
			// 【核心修改】JWT 认证的设备 SSID 统一为 105
			// 与 DevModel=105 (DraARLDevModelBrowser) 保持一致
			device.SSID = 105
			// 【核心修复】使用用户的 LastGroupID 恢复上次选中的群组
			// 如果用户没有 LastGroupID 或为 0，则使用默认公共群组 999
			device.GroupID = 999 // 默认公共群组
			if authResult.UserID > 0 {
				userRepo := gormdb.NewUserRepository()
				if user, err := userRepo.GetUserByID(authResult.UserID); err == nil && user != nil {
					if user.LastGroupID > 0 {
						device.GroupID = user.LastGroupID
						log.Printf("[WS-AUTH] 恢复用户 %d 的群组设置: %d", user.ID, user.LastGroupID)
					}
				}
			}
			manager.RegisterGhostDevice(device, authResult.UserID, authResult.Username, authResult.CallSign, authResult.Nickname, 105)

			// 【关键修复】同时创建 GhostDevice 并建立与 WSDevice 的关联
			// 这样 GetGhostDevice 才能获取到 GhostDevice，且 GhostDevice.Conn 指向 WSDevice
			GlobalGhostManager.CreateGhostDevice(device, authResult.UserID, authResult.Username, authResult.CallSign, authResult.Nickname, 105)

			return device, authResult
		}

		// JWT 认证失败，关闭连接
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, authResult.Error))
		manager.UnregisterDevice(device)
		return nil, authResult
	}

	// 没有 JWT Token，等待心跳包进行设备密码认证
	device.ConnState = StateAuthenticating
	device.SSID = preAuth.SSID

	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(manager.AuthTimeout))

	// 等待心跳包
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[WS-AUTH] Read error during auth: %v", err)
			manager.UnregisterDevice(device)
			return nil, &AuthResult{Error: "read_error"}
		}

		// 只处理二进制消息
		if messageType != websocket.BinaryMessage {
			continue
		}

		// 解析数据包
		packet, err := DecodeWSPacket(data)
		if err != nil {
			log.Printf("[WS-AUTH] Packet decode error: %v", err)
			continue
		}

		// 心跳包触发设备认证
		if packet.Type == protocol.DraARLTypeHeartbeat {
			device.Username = packet.Username
			device.DevicePassword = packet.DevicePassword
			device.SSID = packet.SSID
			device.DevModel = packet.DevModel

			authResult := AuthenticateDevice(packet.Username, packet.DevicePassword, packet.SSID)

			if authResult.Success {
				manager.RegisterNormalDevice(device, packet.Username, packet.SSID, authResult.DeviceID, authResult.CallSign)

				// 发送心跳响应（填充 CallSign）
				response := EncodeHeartbeatResponse(packet, authResult.CallSign)
				conn.WriteMessage(websocket.BinaryMessage, response)

				return device, authResult
			}

			// 认证失败，关闭连接
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, authResult.Error))
			manager.UnregisterDevice(device)
			return nil, authResult
		}
	}
}
