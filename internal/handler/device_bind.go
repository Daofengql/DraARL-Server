package handler

import (
	"fmt"
	"log"
	"net/http"
	"sort"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/protocol"
	"draarl/internal/udphub"
	"draarl/pkg/crypto"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// ==========================================
// 设备端接口（公开接口，无需 JWT）
// ==========================================

// PreCheckRequest 设备预检查请求
type PreCheckRequest struct {
	MAC            string `json:"mac" binding:"required"`
	Username       string `json:"username" binding:"required"`
	DevicePassword string `json:"device_password" binding:"required"`
}

// PreCheckResponse 设备预检查响应
type PreCheckResponse struct {
	Status   string `json:"status"`              // authenticated | need_bind
	Message  string `json:"message,omitempty"`   // 提示信息
	CallSign string `json:"call_sign,omitempty"` // 认证成功时返回呼号
}

// PreCheck 设备上电后检查存储的账号密码是否有效
// POST /api/device/pre-check
func PreCheck(c *gin.Context) {
	var req PreCheckRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证 MAC 地址格式
	if !udphub.ValidateMAC(req.MAC) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "MAC 地址格式错误",
		})
		return
	}

	mac := udphub.NormalizeMAC(req.MAC)
	ip := c.ClientIP()

	// 查询用户
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(req.Username)
	if err != nil || user == nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": PreCheckResponse{
				Status:  "need_bind",
				Message: "请使用动态码绑定设备",
			},
		})
		return
	}

	// 检查用户状态
	if user.Status != 1 || user.ApprovalStatus != 1 {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": PreCheckResponse{
				Status:  "need_bind",
				Message: "请使用动态码绑定设备",
			},
		})
		return
	}

	// 检查设备密码是否已设置
	if user.DevicePassword == "" {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": PreCheckResponse{
				Status:  "need_bind",
				Message: "请先在平台设置设备密码",
			},
		})
		return
	}

	// AES 解密存储的密码并验证
	match, legacyPassword, err := crypto.VerifyDevicePassword(user.DevicePassword, req.DevicePassword)
	if err != nil {
		log.Printf("[DEVICE] 设备密码校验失败: user=%s err=%v", req.Username, err)
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": PreCheckResponse{
				Status:  "need_bind",
				Message: "请使用动态码绑定设备",
			},
		})
		return
	}

	// 验证密码
	if !match {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": PreCheckResponse{
				Status:  "need_bind",
				Message: "请使用动态码绑定设备",
			},
		})
		return
	}

	// 历史 bcrypt 数据兼容迁移：认证成功后自动迁移为 AES 可逆加密格式
	if legacyPassword {
		encryptedPassword, encErr := crypto.Encrypt(req.DevicePassword)
		if encErr != nil {
			log.Printf("[DEVICE] 历史设备密码迁移加密失败: user=%s err=%v", req.Username, encErr)
		} else if updateErr := repo.UpdateUserDevicePassword(user.ID, encryptedPassword); updateErr != nil {
			log.Printf("[DEVICE] 历史设备密码迁移写库失败: user=%s err=%v", req.Username, updateErr)
		} else {
			log.Printf("[DEVICE] 历史设备密码已迁移为 AES 存储: user=%s", req.Username)
		}
	}

	// 认证成功
	log.Printf("[DEVICE] 设备认证成功: MAC=%s, IP=%s, User=%s", mac, ip, req.Username)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": PreCheckResponse{
			Status:   "authenticated",
			CallSign: user.CallSign,
		},
	})
}

// RequestCodeRequest 请求动态码请求
type RequestCodeRequest struct {
	MAC string `json:"mac" binding:"required"`
}

// RequestCode 请求生成动态码
// POST /api/device/request-code
func RequestCode(c *gin.Context) {
	var req RequestCodeRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证 MAC 地址格式
	if !udphub.ValidateMAC(req.MAC) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "MAC 地址格式错误",
		})
		return
	}

	mac := udphub.NormalizeMAC(req.MAC)
	ip := c.ClientIP()

	// 生成动态码
	manager := udphub.GetPendingDeviceManager()
	if manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "服务未初始化",
		})
		return
	}

	device, err := manager.RequestCode(mac, ip)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	log.Printf("[DEVICE] 生成动态码: MAC=%s, Code=%s", mac, device.Code)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"dynamic_code": device.Code,
			"expires_in":   60,
		},
	})
}

// ConfirmBindRequest 确认绑定请求
type ConfirmBindRequest struct {
	MAC string `json:"mac" binding:"required"`
}

// ConfirmBind 设备确认绑定状态
// POST /api/device/confirm-bind
func ConfirmBind(c *gin.Context) {
	var req ConfirmBindRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证 MAC 地址格式
	if !udphub.ValidateMAC(req.MAC) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "MAC 地址格式错误",
		})
		return
	}

	mac := udphub.NormalizeMAC(req.MAC)

	// 查询绑定状态
	manager := udphub.GetPendingDeviceManager()
	if manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "服务未初始化",
		})
		return
	}

	device, err := manager.ConfirmBind(mac)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": err.Error(),
		})
		return
	}

	// 检查是否已绑定且配置就绪
	if device.Bound && device.ConfigReady && device.Config != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": gin.H{
				"status":          "ready",
				"username":        device.Config.Username,
				"device_password": device.Config.DevicePassword,
				"ssid":            device.Config.SSID,
				"dmr_id":          device.Config.DMRID,
			},
		})
		return
	}

	// 等待中
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"status":  "waiting",
			"message": "等待用户完成绑定",
		},
	})
}

// ==========================================
// Web 端接口（需要 JWT 认证）
// ==========================================

// BindRequest 绑定设备请求
type BindRequest struct {
	DynamicCode string `json:"dynamic_code" binding:"required"`
}

type DynamicBindReplaceableDevice struct {
	DeviceID     int    `json:"device_id"`
	Name         string `json:"name"`
	CallSign     string `json:"callsign"`
	SSID         int    `json:"ssid"`
	LastOnlineIP string `json:"last_online_ip,omitempty"`
	OnlineTime   string `json:"online_time,omitempty"`
}

func listPendingConfiguredDynamicBindSSIDs(manager *udphub.PendingDeviceManager, userID uint, excludeMAC string) map[int]struct{} {
	if manager == nil {
		return make(map[int]struct{})
	}
	return manager.ListConfiguredSSIDsByUser(userID, excludeMAC)
}

func listUserUsedDynamicBindSSIDs(userID int, manager *udphub.PendingDeviceManager, excludeMAC string) (map[int]struct{}, error) {
	repo := gormdb.NewDeviceRepository()
	devices, err := repo.ListDevicesByOwnerID(userID)
	if err != nil {
		return nil, err
	}

	used := make(map[int]struct{}, len(devices))
	for _, device := range devices {
		used[int(device.SSID)] = struct{}{}
	}

	for ssid := range listPendingConfiguredDynamicBindSSIDs(manager, uint(userID), excludeMAC) {
		used[ssid] = struct{}{}
	}

	return used, nil
}

func buildReplaceableDynamicBindDevices(devices []*gormdb.Device, pendingUsed map[int]struct{}, callSign string, isRuntimeActive func(ownerID int, ssid byte) bool) []DynamicBindReplaceableDevice {
	replaceable := make([]DynamicBindReplaceableDevice, 0, len(devices))
	for _, device := range devices {
		if device == nil || !protocol.IsValidNormalSSID(device.SSID) {
			continue
		}
		if device.ISOnline {
			continue
		}
		if isRuntimeActive != nil && isRuntimeActive(device.OwnerID, device.SSID) {
			continue
		}
		if _, exists := pendingUsed[int(device.SSID)]; exists {
			continue
		}

		replaceable = append(replaceable, DynamicBindReplaceableDevice{
			DeviceID:     device.ID,
			Name:         device.Name,
			CallSign:     callSign,
			SSID:         int(device.SSID),
			LastOnlineIP: device.LastOnlineIP,
			OnlineTime:   device.OnlineTime.Format("2006-01-02 15:04:05"),
		})
	}

	sort.Slice(replaceable, func(i, j int) bool {
		if replaceable[i].SSID != replaceable[j].SSID {
			return replaceable[i].SSID < replaceable[j].SSID
		}
		return replaceable[i].DeviceID < replaceable[j].DeviceID
	})

	return replaceable
}

func buildAvailableDynamicBindSSIDs(used map[int]struct{}) []int {
	available := make([]int, 0, 248)
	for ssid := 1; ssid <= 255; ssid++ {
		if !protocol.IsValidNormalSSID(byte(ssid)) {
			continue
		}
		if _, exists := used[ssid]; exists {
			continue
		}
		available = append(available, ssid)
	}
	return available
}

func buildDynamicBindOptions(user *gormdb.User, manager *udphub.PendingDeviceManager, excludeMAC string) ([]int, int, []DynamicBindReplaceableDevice, error) {
	repo := gormdb.NewDeviceRepository()
	devices, err := repo.ListDevicesByOwnerID(user.ID)
	if err != nil {
		return nil, 0, nil, err
	}

	pendingUsed := listPendingConfiguredDynamicBindSSIDs(manager, uint(user.ID), excludeMAC)
	used := make(map[int]struct{}, len(devices)+len(pendingUsed))
	for _, device := range devices {
		if device == nil {
			continue
		}
		used[int(device.SSID)] = struct{}{}
	}
	for ssid := range pendingUsed {
		used[ssid] = struct{}{}
	}

	availableSSIDs := buildAvailableDynamicBindSSIDs(used)
	recommendedSSID := 0
	if len(availableSSIDs) > 0 {
		recommendedSSID = availableSSIDs[0]
	}

	replaceableDevices := buildReplaceableDynamicBindDevices(devices, pendingUsed, user.CallSign, udphub.IsRuntimeNormalDeviceActive)
	return availableSSIDs, recommendedSSID, replaceableDevices, nil
}

// BindDevice 用户输入动态码绑定设备
// POST /api/device/bind
func BindDevice(c *gin.Context) {
	var req BindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 获取当前用户名
	username, _ := c.Get("username")

	// 查询用户
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 检查用户是否已审核通过
	if user.ApprovalStatus != 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "账号未审核通过，无法绑定设备",
		})
		return
	}

	// 检查设备密码是否已设置
	if user.DevicePassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请先设置设备密码",
		})
		return
	}

	// 绑定设备
	manager := udphub.GetPendingDeviceManager()
	if manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "服务未初始化",
		})
		return
	}

	device, err := manager.BindDevice(req.DynamicCode, uint(user.ID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	availableSSIDs, recommendedSSID, replaceableDevices, err := buildDynamicBindOptions(user, manager, device.MAC)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取动态绑定选项失败",
		})
		return
	}

	log.Printf("[DEVICE] 设备绑定成功: MAC=%s, User=%s", device.MAC, username)
	oplog.AddLog(
		fmt.Sprintf("绑定设备成功: mac=%s", device.MAC),
		"device_bind",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"device_mac":          device.MAC,
			"call_sign":           user.CallSign,
			"message":             "绑定成功，请配置设备参数",
			"available_ssids":     availableSSIDs,
			"recommended_ssid":    recommendedSSID,
			"replaceable_devices": replaceableDevices,
		},
	})
}

// SubmitConfigRequest 提交设备配置请求
type SubmitConfigRequest struct {
	DeviceMAC       string `json:"device_mac" binding:"required"`
	SSID            *int   `json:"ssid"`
	ReplaceDeviceID *int   `json:"replace_device_id,omitempty"`
}

func isValidDynamicBindSSID(ssid int) bool {
	if ssid < 0 || ssid > 255 {
		return false
	}
	return protocol.IsValidNormalSSID(byte(ssid))
}

func resolveDynamicBindSelectedSSID(user *gormdb.User, manager *udphub.PendingDeviceManager, excludeMAC string, req SubmitConfigRequest) (int, *gormdb.Device, error) {
	if req.ReplaceDeviceID != nil {
		if *req.ReplaceDeviceID <= 0 {
			return 0, nil, fmt.Errorf("无效的离线设备")
		}

		repo := gormdb.NewDeviceRepository()
		device, err := repo.GetDeviceByID(*req.ReplaceDeviceID)
		if err != nil {
			return 0, nil, fmt.Errorf("校验离线设备失败")
		}
		if device == nil || device.OwnerID != user.ID {
			return 0, nil, fmt.Errorf("所选离线设备已不存在，请重新选择")
		}
		if !protocol.IsValidNormalSSID(device.SSID) {
			return 0, nil, fmt.Errorf("所选设备不是可复用的普通设备")
		}
		if device.ISOnline || udphub.IsRuntimeNormalDeviceActive(user.ID, device.SSID) {
			return 0, nil, fmt.Errorf("所选设备当前在线，请先让旧设备离线")
		}

		pendingUsed := listPendingConfiguredDynamicBindSSIDs(manager, uint(user.ID), excludeMAC)
		if _, exists := pendingUsed[int(device.SSID)]; exists {
			return 0, nil, fmt.Errorf("所选离线设备已有其他待绑定配置，请稍后重试")
		}
		if req.SSID != nil && *req.SSID != int(device.SSID) {
			return 0, nil, fmt.Errorf("SSID 与所选离线设备不一致，请重新选择")
		}

		return int(device.SSID), device, nil
	}

	if req.SSID == nil {
		return 0, nil, fmt.Errorf("请选择新 SSID 或离线设备")
	}
	if !isValidDynamicBindSSID(*req.SSID) {
		return 0, nil, fmt.Errorf("SSID 必须在 1-99 或 106-254 范围内")
	}

	usedSSIDs, err := listUserUsedDynamicBindSSIDs(user.ID, manager, excludeMAC)
	if err != nil {
		return 0, nil, fmt.Errorf("校验 SSID 失败")
	}
	if _, exists := usedSSIDs[*req.SSID]; exists {
		return 0, nil, fmt.Errorf("SSID 已被当前用户占用，请选择其他 SSID")
	}

	return *req.SSID, nil, nil
}

// SubmitDeviceConfig 提交设备配置
// POST /api/device/submit-config
func SubmitDeviceConfig(c *gin.Context) {
	var req SubmitConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	if req.ReplaceDeviceID == nil {
		if req.SSID == nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "请选择新 SSID 或离线设备",
			})
			return
		}
		if !isValidDynamicBindSSID(*req.SSID) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "SSID 必须在 1-99 或 106-254 范围内",
			})
			return
		}
	}

	// 验证 MAC 地址格式
	if !udphub.ValidateMAC(req.DeviceMAC) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "MAC 地址格式错误",
		})
		return
	}

	mac := udphub.NormalizeMAC(req.DeviceMAC)

	// 获取当前用户信息
	username, _ := c.Get("username")

	// 查询用户
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	manager := udphub.GetPendingDeviceManager()
	if manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "服务未初始化",
		})
		return
	}

	selectedSSID, reusedDevice, err := resolveDynamicBindSelectedSSID(user, manager, mac, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	// 读取设备密码（兼容历史 bcrypt：不可逆，自动生成新密码并迁移）
	devicePassword := ""
	legacyPassword := false
	if user.DevicePassword != "" {
		devicePassword, legacyPassword, err = crypto.DecodeDevicePassword(user.DevicePassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "获取设备密码失败",
			})
			return
		}
	}

	if user.DevicePassword == "" || legacyPassword || devicePassword == "" {
		devicePassword = generateDevicePassword()
		encryptedPassword, encErr := crypto.Encrypt(devicePassword)
		if encErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "生成设备密码失败",
			})
			return
		}
		if updateErr := repo.UpdateUserDevicePassword(user.ID, encryptedPassword); updateErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "更新设备密码失败",
			})
			return
		}
		log.Printf("[DEVICE] 已为用户重建并迁移设备密码: user=%s legacy=%v", user.Name, legacyPassword)
	}

	// 设置设备配置
	config := &udphub.PendingDeviceConfig{
		Username:       user.Name,
		DevicePassword: devicePassword,
		SSID:           selectedSSID,
		DMRID:          user.DMRID,
	}

	if err := manager.SetDeviceConfig(mac, config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	reuseDeviceID := 0
	if reusedDevice != nil {
		reuseDeviceID = reusedDevice.ID
	}

	log.Printf("[DEVICE] 设备配置已保存: MAC=%s, User=%s, SSID=%d, ReuseDeviceID=%d", mac, username, selectedSSID, reuseDeviceID)
	oplog.AddLog(
		fmt.Sprintf("提交设备绑定配置: mac=%s, ssid=%d, dmr_id=%d, reuse_device_id=%d", mac, selectedSSID, user.DMRID, reuseDeviceID),
		"device_bind_config_submit",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"message": "配置已保存",
			"ssid":    selectedSSID,
			"udp_auth_info": gin.H{
				"username":        user.Name,
				"device_password": devicePassword,
			},
			"dmr_id": user.DMRID,
		},
	})
}
