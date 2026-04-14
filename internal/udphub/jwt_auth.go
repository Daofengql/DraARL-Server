package udphub

import (
	"log"
	"net"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/internal/protocol"
	"draarl/pkg/jwt"
)

// ==========================================
// UDP JWT 认证处理
// 处理 Type=1 的 JWT 认证包
// ==========================================

// JWTAuthResult JWT 认证结果
type JWTAuthResult struct {
	Success   bool
	User      *gormdb.User
	CallSign  string
	GroupID   uint
	ErrorCode byte
	ErrorMsg  string
}

// HandleJWTAuthPacket 处理 JWT 认证包 (Type=1)
// 流程:
// 1. 从 packet.DATA 提取 JWT Token
// 2. 调用 jwt.ParseToken() 验证 Token
// 3. 验证失败 → 发送错误响应，返回
// 4. 从 Token 获取 username
// 5. 查询数据库获取用户信息
// 6. 检查用户状态 (Status, ApprovalStatus)
// 7. 验证 DevModel 是否为有效的 UDP 幽灵设备型号 (101-104)
// 8. 计算 SSID: ssid = GetGhostSSID(packet.DevModel)
// 9. 获取用户该平台的群组偏好
// 10. 创建 UDPGhostDevice 结构
// 11. 调用 GlobalUDPGhostManager.Register() 注册
// 12. 发送成功响应
func HandleJWTAuthPacket(packet *protocol.DraARLv1Packet, realAddr *net.UDPAddr, conn *net.UDPConn) {
	// 1. 提取 JWT Token
	token := string(packet.DATA)
	if token == "" {
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "Token is empty")
		return
	}

	// 2. 验证 Token
	claims, err := jwt.ParseToken(token)
	if err != nil {
		log.Printf("[UDP-JWT] Token 解析失败: %v (地址: %v)", err, realAddr)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidToken, "Invalid or expired token")
		return
	}

	// 3. 验证 DevModel 是否为有效的 UDP 幽灵设备型号 (101-104)
	// 注意: 105 (Web) 使用 WebSocket，不在此范围内
	if !protocol.IsGhostDevModel(packet.DevModel) {
		log.Printf("[UDP-JWT] 无效的设备型号: %d (用户: %s, 地址: %v)",
			packet.DevModel, claims.Username, realAddr)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthInvalidDevModel, "Invalid device model for UDP")
		return
	}

	// 4. 查询用户信息
	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(claims.Username)
	if err != nil || user == nil {
		log.Printf("[UDP-JWT] 用户不存在: %s (地址: %v)", claims.Username, realAddr)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthUserNotFound, "User not found")
		return
	}

	// 5. 检查用户状态
	if user.Status != 1 {
		log.Printf("[UDP-JWT] 用户已禁用: %s (状态: %d)", claims.Username, user.Status)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthUserDisabled, "User is disabled")
		return
	}

	// 6. 检查审核状态
	if user.ApprovalStatus != 1 {
		log.Printf("[UDP-JWT] 用户未审核: %s (审核状态: %d)", claims.Username, user.ApprovalStatus)
		sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthUserNotApproved, "User is not approved")
		return
	}

	ssid := protocol.GetGhostSSID(packet.DevModel)
	if existingGhost := GlobalUDPGhostManager.Get(user.Name, ssid); existingGhost != nil {
		if existingGhost.ISOnline && isRecentlyActiveDevice(existingGhost) && !sameUDPAddr(existingGhost.UDPAddr, packet.UDPAddr) {
			log.Printf("[UDP-JWT] 幽灵设备冲突: user=%s dev_model=%d old_addr=%v new_addr=%v",
				user.Name, packet.DevModel, existingGhost.UDPAddr, packet.UDPAddr)
			sendJWTAuthResponse(packet, conn, false, "", protocol.JWTAuthGhostDeviceConflict, "Ghost device already online")
			return
		}

		if existingGhost.ISOnline && sameUDPAddr(existingGhost.UDPAddr, packet.UDPAddr) {
			existingGhost.LastPacketTime = time.Now()
			existingGhost.CallSign = user.CallSign
			existingGhost.OwnerID = user.ID
			existingGhost.CallSignSSID = protocol.GetCallSignSSID(user.CallSign, ssid)
			sendJWTAuthResponse(packet, conn, true, user.CallSign, protocol.JWTAuthSuccess, "")
			log.Printf("[UDP-JWT] 设备重连复用认证态: %s (地址: %v)", getDeviceKey(user.Name, ssid), realAddr)
			return
		}
	}

	// 8. 获取用户该平台的群组偏好
	groupID, err := userRepo.GetUserLastGroupID(user.ID, packet.DevModel)
	if err != nil {
		log.Printf("[UDP-JWT] 获取群组偏好失败: %v (用户: %s)", err, claims.Username)
		groupID = models.GroupIDPublicMin // 使用默认群组
	}

	// 9. 验证群组是否存在且未禁用
	if groupID > 0 {
		if gp, exists := GetGroupFromCache(int(groupID)); !exists || gp.Status != 1 {
			log.Printf("[UDP-JWT] 群组无效或已禁用: %d (用户: %s)", groupID, claims.Username)
			groupID = models.GroupIDPublicMin // 回退到默认群组
		}
	}

	// 10. 创建 UDP 幽灵设备
	now := time.Now()
	ghostDevice := &models.Device{
		Username:       user.Name,
		CallSign:       user.CallSign,
		SSID:           ssid,
		OwnerID:        user.ID,
		CallSignSSID:   protocol.GetCallSignSSID(user.CallSign, ssid),
		DevModel:       packet.DevModel,
		GroupID:        int(groupID),
		Priority:       100,
		Status:         0,
		ISOnline:       true,
		UDPAddr:        packet.UDPAddr,
		LastPacketTime: now,
		OnlineTime:     now,
	}

	// 11. 注册设备（在准入前已完成冲突检查）
	GlobalUDPGhostManager.Register(ghostDevice)

	// 12. 发送成功响应
	sendJWTAuthResponse(packet, conn, true, user.CallSign, protocol.JWTAuthSuccess, "")

	log.Printf("[UDP-JWT] 认证成功: %s (%s-%d) 群组: %d 地址: %v",
		user.Name, user.CallSign, ssid, groupID, realAddr)
}

// sendJWTAuthResponse 发送 JWT 认证响应包
func sendJWTAuthResponse(packet *protocol.DraARLv1Packet, conn *net.UDPConn,
	success bool, callSign string, errorCode byte, errorMsg string) {

	var data []byte
	var responseCallSign string

	if success {
		data = []byte{protocol.JWTAuthSuccess} // 状态码 0 = 成功
		responseCallSign = callSign
	} else {
		data = append([]byte{errorCode}, []byte(errorMsg)...)
		responseCallSign = ""
	}

	// 计算服务器分配的 SSID (等于 DevModel)
	ssid := protocol.GetGhostSSID(packet.DevModel)
	if ssid == 0 {
		ssid = packet.DevModel // 如果不是幽灵设备，使用原始 DevModel
	}

	// 组装响应数据包
	response := protocol.EncodeDraARLv1(
		packet.Username,            // 回显用户名
		"",                         // password 空
		ssid,                       // 服务器分配的 SSID
		protocol.DraARLTypeJWTAuth, // Type=1
		packet.DevModel,            // 回显设备型号
		0,                          // dmrid
		responseCallSign,           // 呼号
		data,                       // DATA 区域
	)

	conn.WriteToUDP(response, packet.UDPAddr)
}

// AuthenticateJWT 进行 JWT 认证（供外部调用）
func AuthenticateJWT(token string) *JWTAuthResult {
	result := &JWTAuthResult{}

	// 解析 Token
	claims, err := jwt.ParseToken(token)
	if err != nil {
		result.ErrorCode = protocol.JWTAuthInvalidToken
		result.ErrorMsg = "Invalid or expired token"
		return result
	}

	// 查询用户
	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(claims.Username)
	if err != nil || user == nil {
		result.ErrorCode = protocol.JWTAuthUserNotFound
		result.ErrorMsg = "User not found"
		return result
	}

	// 检查用户状态
	if user.Status != 1 {
		result.ErrorCode = protocol.JWTAuthUserDisabled
		result.ErrorMsg = "User is disabled"
		return result
	}

	// 检查审核状态
	if user.ApprovalStatus != 1 {
		result.ErrorCode = protocol.JWTAuthUserNotApproved
		result.ErrorMsg = "User is not approved"
		return result
	}

	result.Success = true
	result.User = user
	result.CallSign = user.CallSign
	return result
}

// GetGhostDeviceGroupID 获取幽灵设备的群组 ID
// 优先从 user_device_preferences 读取，如果没有或为 0 则返回默认群组
func GetGhostDeviceGroupID(userID int, devModel byte) int {
	userRepo := gormdb.NewUserRepository()
	groupID, err := userRepo.GetUserLastGroupID(userID, devModel)
	if err != nil || groupID == 0 {
		return models.GroupIDPublicMin // 默认公共群组
	}

	// 验证群组是否存在且未禁用
	if gp, exists := GetGroupFromCache(groupID); exists && gp.Status == 1 {
		return groupID
	}

	return models.GroupIDPublicMin
}
