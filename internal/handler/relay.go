package handler

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	gormdb "nrllink/internal/gormdb"
	oplog "nrllink/internal/log"
)

// CreateRelay 创建中继台
func CreateRelay(c *gin.Context) {
	username, _ := c.Get("username")

	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取用户信息失败",
		})
		return
	}

	var relay gormdb.Relay
	if err := c.ShouldBindJSON(&relay); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 设置所有者呼号为当前用户
	relay.OwerCallSign = user.CallSign

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
	})
}

// UpdateRelay 更新中继台
func UpdateRelay(c *gin.Context) {
	username, _ := c.Get("username")
	roles, _ := c.Get("roles")

	var relay gormdb.Relay
	if err := c.ShouldBindJSON(&relay); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
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

	// 检查是否是 admin
	isAdmin := false
	if roleList, ok := roles.([]string); ok {
		for _, r := range roleList {
			if r == "admin" {
				isAdmin = true
				break
			}
		}
	}

	// 获取原有中继台信息
	repo := gormdb.NewRelayRepository()
	oldRelay, err := repo.GetRelayByID(relay.ID)
	if err != nil || oldRelay == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "中继台不存在",
		})
		return
	}

	// 检查是否是所有者
	if !isAdmin && user.CallSign != oldRelay.OwerCallSign {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "当前用户没有权限设置此参数",
		})
		return
	}

	// 保留原有所有者
	relay.OwerCallSign = oldRelay.OwerCallSign

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
	})
}

// DeleteRelay 删除中继台
func DeleteRelay(c *gin.Context) {
	username, _ := c.Get("username")
	roles, _ := c.Get("roles")

	// 检查是否是 admin
	isAdmin := false
	if roleList, ok := roles.([]string); ok {
		for _, r := range roleList {
			if r == "admin" {
				isAdmin = true
				break
			}
		}
	}

	if !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "当前用户没有权限设置此参数",
		})
		return
	}

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

	if relay != nil {
		oplog.AddLog(relay.String(), "中继台删除成功", user.ID, user.Name, user.CallSign, c.ClientIP())
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "中继台删除成功",
	})
}
