package handler

import (
	"fmt"
	"log"
	"net/http"

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

	if manager != nil {
		for ssid := range manager.ListConfiguredSSIDsByUser(uint(userID), excludeMAC) {
			used[ssid] = struct{}{}
		}
	}

	return used, nil
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

	usedSSIDs, err := listUserUsedDynamicBindSSIDs(user.ID, manager, device.MAC)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取可分配 SSID 失败",
		})
		return
	}
	availableSSIDs := buildAvailableDynamicBindSSIDs(usedSSIDs)
	recommendedSSID := 0
	if len(availableSSIDs) > 0 {
		recommendedSSID = availableSSIDs[0]
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
			"device_mac":       device.MAC,
			"call_sign":        user.CallSign,
			"message":          "绑定成功，请配置设备参数",
			"available_ssids":  availableSSIDs,
			"recommended_ssid": recommendedSSID,
		},
	})
}

// SubmitConfigRequest 提交设备配置请求
type SubmitConfigRequest struct {
	DeviceMAC string `json:"device_mac" binding:"required"`
	SSID      *int   `json:"ssid" binding:"required"`
}

func isValidDynamicBindSSID(ssid int) bool {
	if ssid < 0 || ssid > 255 {
		return false
	}
	return protocol.IsValidNormalSSID(byte(ssid))
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

	if req.SSID == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
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

	usedSSIDs, err := listUserUsedDynamicBindSSIDs(user.ID, manager, mac)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "校验 SSID 失败",
		})
		return
	}
	if _, exists := usedSSIDs[*req.SSID]; exists {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "SSID 已被当前用户占用，请选择其他 SSID",
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
		SSID:           *req.SSID,
		DMRID:          user.DMRID,
	}

	if err := manager.SetDeviceConfig(mac, config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	log.Printf("[DEVICE] 设备配置已保存: MAC=%s, User=%s, SSID=%d", mac, username, *req.SSID)
	oplog.AddLog(
		fmt.Sprintf("提交设备绑定配置: mac=%s, ssid=%d, dmr_id=%d", mac, *req.SSID, user.DMRID),
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
			"udp_auth_info": gin.H{
				"username":        user.Name,
				"device_password": devicePassword,
			},
			"dmr_id": user.DMRID,
		},
	})
}
