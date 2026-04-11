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
// 后台设备配置 API（管理员权限）
// ============================================================

// AdminGetDeviceConfig 获取任意设备配置
// GET /api/admin/devices/:id/config
func AdminGetDeviceConfig(c *gin.Context) {
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

// AdminUpdateDeviceConfig 更新任意设备配置
// PUT /api/admin/devices/:id/config
func AdminUpdateDeviceConfig(c *gin.Context) {
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
	if req.RxToneMode != nil {
		configs["rx_tone_mode"] = *req.RxToneMode
	}
	if req.RxToneValue != nil {
		configs["rx_tone_value"] = *req.RxToneValue
	}
	if req.TxToneMode != nil {
		configs["tx_tone_mode"] = *req.TxToneMode
	}
	if req.TxToneValue != nil {
		configs["tx_tone_value"] = *req.TxToneValue
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
	if req.RFGuardEnabled != nil {
		configs[udphub.ConfigKeyRFGuardEnabled] = *req.RFGuardEnabled
	}
	if req.RFGuardSingleTxLimitS != nil {
		configs[udphub.ConfigKeyRFGuardSingleTxLimitS] = *req.RFGuardSingleTxLimitS
	}
	if req.RFGuardWindowS != nil {
		configs[udphub.ConfigKeyRFGuardWindowS] = *req.RFGuardWindowS
	}
	if req.RFGuardMaxTxInWindowS != nil {
		configs[udphub.ConfigKeyRFGuardMaxTxInWindowS] = *req.RFGuardMaxTxInWindowS
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

	operatorID := 0
	operatorName := ""
	operatorCallSign := ""
	if u, exists := c.Get("user"); exists {
		if user, ok := u.(*gormdb.User); ok && user != nil {
			operatorID = user.ID
			operatorName = user.Name
			operatorCallSign = user.CallSign
		}
	}
	configKeys := make([]string, 0, len(configs))
	for k := range configs {
		configKeys = append(configKeys, k)
	}
	oplog.AddLog(
		fmt.Sprintf("管理员更新设备配置: device_id=%d, config_keys=%v", deviceID, configKeys),
		"admin_device_config_update",
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

// AdminSyncDeviceConfig 强制同步配置到设备
// POST /api/admin/devices/:id/config/sync
func AdminSyncDeviceConfig(c *gin.Context) {
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

	// 尝试发送配置到设备
	if err := udphub.SendConfigToDeviceByID(deviceID, paramConfigs); err != nil {
		operatorID := 0
		operatorName := ""
		operatorCallSign := ""
		if u, exists := c.Get("user"); exists {
			if user, ok := u.(*gormdb.User); ok && user != nil {
				operatorID = user.ID
				operatorName = user.Name
				operatorCallSign = user.CallSign
			}
		}
		oplog.AddLog(
			fmt.Sprintf("管理员同步设备配置: device_id=%d, result=deferred_offline, config_count=%d", deviceID, len(paramConfigs)),
			"admin_device_config_sync",
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

	operatorID := 0
	operatorName := ""
	operatorCallSign := ""
	if u, exists := c.Get("user"); exists {
		if user, ok := u.(*gormdb.User); ok && user != nil {
			operatorID = user.ID
			operatorName = user.Name
			operatorCallSign = user.CallSign
		}
	}
	oplog.AddLog(
		fmt.Sprintf("管理员同步设备配置: device_id=%d, result=sent, config_count=%d", deviceID, len(paramConfigs)),
		"admin_device_config_sync",
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
