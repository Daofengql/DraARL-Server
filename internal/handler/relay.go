package handler

import (
	"log"
	"net/http"
	"strings"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"

	"github.com/gin-gonic/gin"
)

// PublicSearchRelays 公开搜索中继台（无需登录）
// GET /api/public/relays?location=广东省
func PublicSearchRelays(c *gin.Context) {
	location := c.Query("location")

	repo := gormdb.NewRelayRepository()
	relays, err := repo.SearchRelaysByLocation(location)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": relays,
		},
	})
}

// CreateRelay 创建中继台（仅管理员）
func CreateRelay(c *gin.Context) {
	username, _ := c.Get("username")

	var relay gormdb.Relay
	if err := c.ShouldBindJSON(&relay); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证必填字段
	if relay.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "名称为必填项",
		})
		return
	}
	if relay.DownFreq == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "接收频率为必填项",
		})
		return
	}
	if relay.UpFreq == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "发射频率为必填项",
		})
		return
	}
	// 验证位置至少到市级别（格式：省份 城市 [区县]）
	locationParts := strings.Fields(relay.Location)
	if len(locationParts) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "位置为必填项，至少需要选择到市级别",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取用户信息失败",
		})
		return
	}

	repo := gormdb.NewRelayRepository()
	if err := repo.CreateRelay(&relay); err != nil {
		log.Printf("创建中继台失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建中继台失败",
		})
		return
	}

	oplog.AddLog(relay.String(), "新增中继台成功", user.ID, user.Name, user.CallSign, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "新增中继台成功",
		"data":    relay,
	})
}

// UpdateRelay 更新中继台（仅管理员）
func UpdateRelay(c *gin.Context) {
	username, _ := c.Get("username")

	var relay gormdb.Relay
	if err := c.ShouldBindJSON(&relay); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证必填字段
	if relay.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "名称为必填项",
		})
		return
	}
	if relay.DownFreq == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "接收频率为必填项",
		})
		return
	}
	if relay.UpFreq == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "发射频率为必填项",
		})
		return
	}
	// 验证位置至少到市级别（格式：省份 城市 [区县]）
	locationParts := strings.Fields(relay.Location)
	if len(locationParts) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "位置为必填项，至少需要选择到市级别",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取用户信息失败",
		})
		return
	}

	repo := gormdb.NewRelayRepository()
	if err := repo.UpdateRelay(&relay); err != nil {
		log.Printf("更新中继台失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "修改中继台失败",
		})
		return
	}

	oplog.AddLog(relay.String(), "修改中继台成功", user.ID, user.Name, user.CallSign, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "修改中继台成功",
		"data":    relay,
	})
}

// DeleteRelay 删除中继台（仅管理员）
func DeleteRelay(c *gin.Context) {
	username, _ := c.Get("username")

	var req struct {
		ID int `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	user, _ := userRepo.GetUserByName(username.(string))

	repo := gormdb.NewRelayRepository()
	relay, _ := repo.GetRelayByID(req.ID)

	if err := repo.DeleteRelay(req.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除中继台失败",
		})
		return
	}

	if relay != nil && user != nil {
		oplog.AddLog(relay.String(), "中继台删除成功", user.ID, user.Name, user.CallSign, c.ClientIP())
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "中继台删除成功",
	})
}
