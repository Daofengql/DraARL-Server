package handler

import (
	"fmt"
	"net/http"
	"strconv"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/udphub"

	"github.com/gin-gonic/gin"
)

// ============================================================
// 前台设备配置 API（需要二次校验设备归属）
// ============================================================

// GetDeviceConfig 获取设备配置
// GET /api/devices/:id/config
func GetDeviceConfig(c *gin.Context) {
	// 获取设备 ID
	deviceIDStr := c.Param("id")
	deviceID, err := strconv.Atoi(deviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的设备 ID",
		})
		return
	}

	// 获取当前用户 ID
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	// 验证设备归属
	repo := gormdb.NewDeviceRepository()
	device, err := repo.GetDeviceByID(deviceID)
	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	// 检查设备归属（必须为设备所有者或管理员）
	if device.OwnerID != userID {
		username, _ := c.Get("username")
		if !isAdmin(username.(string)) {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "无权访问此设备的配置",
			})
			return
		}
	}

	// 获取设备配置
	configs, err := udphub.GetDeviceConfigsFromDB(deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取设备配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": configs,
	})
}

// UpdateDeviceConfigRequest 更新设备配置请求
type UpdateDeviceConfigRequest struct {
	RxFreq      *string `json:"rx_freq"`      // 接收频率 (Hz)
	TxFreq      *string `json:"tx_freq"`      // 发射频率 (Hz)
	RxCtcss     *string `json:"rx_ctcss"`     // 接收亚音 (Hz, 0=关闭)
	TxCtcss     *string `json:"tx_ctcss"`     // 发射亚音 (Hz, 0=关闭)
	SqlLevel    *string `json:"sql_level"`    // 静噪等级 (0-9)
	PowerLevel  *string `json:"power_level"`  // 功率等级 (1=低, 2=中, 3=高)
	TxBandwidth *string `json:"tx_bandwidth"` // 发射带宽 (1=窄带, 2=宽带)
}

// UpdateDeviceConfig 更新设备配置
// PUT /api/devices/:id/config
func UpdateDeviceConfig(c *gin.Context) {
	// 获取设备 ID
	deviceIDStr := c.Param("id")
	deviceID, err := strconv.Atoi(deviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的设备 ID",
		})
		return
	}

	// 获取当前用户 ID
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	// 验证设备归属
	repo := gormdb.NewDeviceRepository()
	device, err := repo.GetDeviceByID(deviceID)
	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	// 检查设备归属（必须为设备所有者或管理员）
	if device.OwnerID != userID {
		username, _ := c.Get("username")
		if !isAdmin(username.(string)) {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "无权修改此设备的配置",
			})
			return
		}
	}

	// 解析请求
	var req UpdateDeviceConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的请求参数",
		})
		return
	}

	// 构建配置 map（只包含非空字段）
	configs := make(map[string]string)
	if req.RxFreq != nil {
		configs["rx_freq"] = *req.RxFreq
	}
	if req.TxFreq != nil {
		configs["tx_freq"] = *req.TxFreq
	}
	if req.RxCtcss != nil {
		configs["rx_ctcss"] = *req.RxCtcss
	}
	if req.TxCtcss != nil {
		configs["tx_ctcss"] = *req.TxCtcss
	}
	if req.SqlLevel != nil {
		configs["sql_level"] = *req.SqlLevel
	}
	if req.PowerLevel != nil {
		configs["power_level"] = *req.PowerLevel
	}
	if req.TxBandwidth != nil {
		configs["tx_bandwidth"] = *req.TxBandwidth
	}

	if len(configs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "未提供任何配置项",
		})
		return
	}

	// 保存配置到数据库（如果设备在线会自动下发）
	if err := udphub.SaveDeviceConfigsToDB(deviceID, configs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存配置失败",
		})
		return
	}

	// 记录审计日志
	operatorID, operatorName, operatorCallSign := getDeviceConfigAuditOperator(c, userID)
	configKeys := make([]string, 0, len(configs))
	for k := range configs {
		configKeys = append(configKeys, k)
	}
	oplog.AddLog(
		fmt.Sprintf("更新设备配置: device_id=%d, owner_id=%d, config_keys=%v", deviceID, device.OwnerID, configKeys),
		"device_config_update",
		operatorID,
		operatorName,
		operatorCallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "配置已保存",
		"data":    configs,
	})
}

// SyncDeviceConfig 立即同步配置到设备
// POST /api/devices/:id/config/sync
func SyncDeviceConfig(c *gin.Context) {
	// 获取设备 ID
	deviceIDStr := c.Param("id")
	deviceID, err := strconv.Atoi(deviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的设备 ID",
		})
		return
	}

	// 获取当前用户 ID
	userID, ok := getUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	// 验证设备归属
	repo := gormdb.NewDeviceRepository()
	device, err := repo.GetDeviceByID(deviceID)
	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	// 检查设备归属（必须为设备所有者或管理员）
	if device.OwnerID != userID {
		username, _ := c.Get("username")
		if !isAdmin(username.(string)) {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "无权同步此设备的配置",
			})
			return
		}
	}

	// 获取设备配置
	configs, err := udphub.GetDeviceConfigsFromDB(deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取设备配置失败",
		})
		return
	}

	if len(configs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "设备没有保存的配置",
		})
		return
	}

	// 过滤掉 timestamp，只下发设备参数
	paramConfigs := make(map[string]string)
	for k, v := range configs {
		if k != "timestamp" {
			paramConfigs[k] = v
		}
	}

	operatorID, operatorName, operatorCallSign := getDeviceConfigAuditOperator(c, userID)

	// 尝试发送配置到设备
	if err := udphub.SendConfigToDeviceByID(deviceID, paramConfigs); err != nil {
		oplog.AddLog(
			fmt.Sprintf("同步设备配置: device_id=%d, owner_id=%d, result=deferred_offline, config_count=%d", deviceID, device.OwnerID, len(paramConfigs)),
			"device_config_sync",
			operatorID,
			operatorName,
			operatorCallSign,
			c.ClientIP(),
		)
		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "设备离线，配置将在设备上线时自动同步",
		})
		return
	}

	oplog.AddLog(
		fmt.Sprintf("同步设备配置: device_id=%d, owner_id=%d, result=sent, config_count=%d", deviceID, device.OwnerID, len(paramConfigs)),
		"device_config_sync",
		operatorID,
		operatorName,
		operatorCallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "配置已发送到设备",
	})
}

// isAdmin 检查用户是否为管理员
func isAdmin(username string) bool {
	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username)
	if err != nil || user == nil {
		return false
	}
	return user.HasRole("admin")
}

func getDeviceConfigAuditOperator(c *gin.Context, fallbackUserID int) (int, string, string) {
	username, _ := c.Get("username")
	usernameStr, _ := username.(string)
	if usernameStr == "" {
		return fallbackUserID, "", ""
	}

	user, err := gormdb.NewUserRepository().GetUserByName(usernameStr)
	if err != nil || user == nil {
		return fallbackUserID, usernameStr, ""
	}

	return user.ID, user.Name, user.CallSign
}
