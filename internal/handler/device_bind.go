package handler

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	gormdb "draarl/internal/gormdb"
	"draarl/internal/udphub"
	"draarl/pkg/crypto"
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
	Status    string `json:"status"`              // authenticated | need_bind
	Message   string `json:"message,omitempty"`   // 提示信息
	CallSign  string `json:"call_sign,omitempty"` // 认证成功时返回呼号
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
	storedPassword, err := crypto.Decrypt(user.DevicePassword)
	if err != nil {
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
	if storedPassword != req.DevicePassword {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": PreCheckResponse{
				Status:  "need_bind",
				Message: "请使用动态码绑定设备",
			},
		})
		return
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

	log.Printf("[DEVICE] 设备绑定成功: MAC=%s, User=%s", device.MAC, username)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"device_mac": device.MAC,
			"call_sign":  user.CallSign,
			"message":    "绑定成功，请配置设备参数",
		},
	})
}

// SubmitConfigRequest 提交设备配置请求
type SubmitConfigRequest struct {
	DeviceMAC string `json:"device_mac" binding:"required"`
	SSID      int    `json:"ssid" binding:"required,min=1,max=99"`
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

	// 解密设备密码
	devicePassword, err := crypto.Decrypt(user.DevicePassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取设备密码失败",
		})
		return
	}

	// 设置设备配置
	manager := udphub.GetPendingDeviceManager()
	if manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "服务未初始化",
		})
		return
	}

	config := &udphub.PendingDeviceConfig{
		Username:       user.Name,
		DevicePassword: devicePassword,
		SSID:           req.SSID,
		DMRID:          user.DMRID,
	}

	if err := manager.SetDeviceConfig(mac, config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	log.Printf("[DEVICE] 设备配置已保存: MAC=%s, User=%s, SSID=%d", mac, username, req.SSID)
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
