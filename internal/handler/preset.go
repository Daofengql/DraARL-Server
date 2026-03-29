package handler

import (
	"log"
	"net/http"
	"strconv"

	gormdb "draarl/internal/gormdb"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RadioPresetRequest 创建/更新电台预设请求
type RadioPresetRequest struct {
	Name      string `json:"name" binding:"required"` // 预设名称
	Radio     string `json:"radio"`                   // 电台型号
	Antenna   string `json:"antenna"`                 // 天线类型
	Power     *int   `json:"power"`                   // 功率 (W)
	QTH       string `json:"qth"`                       // QTH位置
	SortOrder int    `json:"sort_order"`                // 排序权重
}

// GetRadioPresets 获取当前用户的电台预设列表
func GetRadioPresets(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	var presets []gormdb.UserRadioPreset
	if err := gormdb.Get().Where("user_id = ?", userID).Order("sort_order ASC, id ASC").Find(&presets).Error; err != nil {
		log.Printf("获取电台预设失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取电台预设失败",
		})
		return
	}

	// 转换为响应格式
	items := make([]gin.H, 0, len(presets))
	for _, p := range presets {
		items = append(items, presetToJSON(p))
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    items,
	})
}

// CreateRadioPreset 创建电台预设
func CreateRadioPreset(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	var req RadioPresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	preset := &gormdb.UserRadioPreset{
		UserID:    uint(userID.(int)),
		Name:      req.Name,
		Radio:     req.Radio,
		Antenna:   req.Antenna,
		Power:     req.Power,
		QTH:       req.QTH,
		SortOrder: req.SortOrder,
	}

	if err := gormdb.Get().Create(preset).Error; err != nil {
		log.Printf("创建电台预设失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建电台预设失败",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "创建成功",
		"data":    presetToJSON(*preset),
	})
}

// UpdateRadioPreset 更新电台预设（用户只能更新自己的)
func UpdateRadioPreset(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的ID",
		})
		return
	}

	var req RadioPresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 查询预设，确保是当前用户的
	var preset gormdb.UserRadioPreset
	if err := gormdb.Get().Where("id = ? AND user_id = ?", id, userID).First(&preset).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "预设不存在或无权修改",
			})
			return
		}
		log.Printf("获取电台预设失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取电台预设失败",
		})
		return
	}

	// 更新字段
	preset.Name = req.Name
	preset.Radio = req.Radio
	preset.Antenna = req.Antenna
	preset.Power = req.Power
	preset.QTH = req.QTH
	preset.SortOrder = req.SortOrder

    if err := gormdb.Get().Save(&preset).Error; err != nil {
		log.Printf("更新电台预设失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新电台预设失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data":    presetToJSON(preset),
	})
}

// DeleteRadioPreset 删除电台预设(用户只能删除自己的)
func DeleteRadioPreset(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的ID",
		})
		return
	}

	// 删除预设，确保是当前用户的
	result := gormdb.Get().Where("id = ? AND user_id = ?", id, userID).Delete(&gormdb.UserRadioPreset{})
	if result.Error != nil {
		log.Printf("删除电台预设失败: %v", result.Error)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除电台预设失败",
		})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "预设不存在或无权删除",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// ReorderPresetsRequest 批量更新排序请求
type ReorderPresetsRequest struct {
	Orders []struct {
		ID    uint `json:"id" binding:"required"`
		Order int  `json:"order" binding:"required"`
	} `json:"orders" binding:"required"`
}

// ReorderRadioPresets 批量更新预设排序
func ReorderRadioPresets(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	var req ReorderPresetsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 使用事务批量更新
	err := gormdb.Get().Transaction(func(tx *gorm.DB) error {
		for _, item := range req.Orders {
			result := tx.Model(&gormdb.UserRadioPreset{}).
				Where("id = ? AND user_id = ?", item.ID, userID).
				Update("sort_order", item.Order)
			if result.Error != nil {
				return result.Error
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("更新预设排序失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新排序失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "排序更新成功",
	})
}

// presetToJSON 转换为JSON响应格式
func presetToJSON(p gormdb.UserRadioPreset) gin.H {
	return gin.H{
		"id":         p.ID,
		"user_id":    p.UserID,
		"name":       p.Name,
		"radio":      p.Radio,
		"antenna":    p.Antenna,
		"power":      p.Power,
		"qth":        p.QTH,
		"sort_order": p.SortOrder,
		"created_at": p.CreatedAt.Format("2006-01-02 15:04:05"),
		"updated_at": p.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}
