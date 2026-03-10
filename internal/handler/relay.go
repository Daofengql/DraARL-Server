package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"nrllink/internal/db"
	"nrllink/internal/log"
	"nrllink/internal/models"
)

// CreateRelay 创建中继台
func CreateRelay(c *gin.Context) {
	username, _ := c.Get("username")

	var relay models.Relay
	if err := c.ShouldBindJSON(&relay); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "新增中继台失败,json格式错误",
			},
		})
		return
	}

	// 获取用户完整信息
	user, err := db.GetUserByUsername(username.(string))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "获取用户信息失败",
			},
		})
		return
	}

	// 设置所有者呼号为当前用户
	relay.OwerCallSign = user.CallSign

	repo := db.NewRelayRepository()
	if err := repo.AddRelay(&relay); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "新增中继台失败",
			},
		})
		return
	}

	log.AddLog(relay.String(), "新增中继台成功", user.ID, user.Name, user.CallSign, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"isok":    0,
			"message": "新增中继台成功",
		},
	})
}

// UpdateRelay 更新中继台
func UpdateRelay(c *gin.Context) {
	username, _ := c.Get("username")
	roles, _ := c.Get("roles")

	var relay models.Relay
	if err := c.ShouldBindJSON(&relay); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "修改中继台失败",
			},
		})
		return
	}

	// 获取用户完整信息
	user, err := db.GetUserByUsername(username.(string))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "获取用户信息失败",
			},
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
	repo := db.NewRelayRepository()
	oldRelay, err := repo.GetRelay(relay.ID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "中继台不存在",
			},
		})
		return
	}

	// 检查是否是所有者
	if !isAdmin && user.CallSign != oldRelay.OwerCallSign {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "当前���户没有权限设置此参数",
			},
		})
		return
	}

	// 保留原有所有者
	relay.OwerCallSign = oldRelay.OwerCallSign

	if err := repo.UpdateRelay(&relay); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "修改中继台失败",
			},
		})
		return
	}

	log.AddLog(relay.String(), "修改中继台成功", user.ID, user.Name, user.CallSign, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"message": "修改中继台成功",
		},
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
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "当前用户没有权限设置此参数",
			},
		})
		return
	}

	var req struct {
		ID int `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "中继台删除失败",
			},
		})
		return
	}

	// 获取用户完整信息
	user, err := db.GetUserByUsername(username.(string))

	repo := db.NewRelayRepository()
	relay, _ := repo.GetRelay(req.ID)

	if err := repo.DeleteRelay(req.ID); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "中继台删除失败",
			},
		})
		return
	}

	if relay != nil && err == nil {
		log.AddLog(relay.String(), "中继台删除成功", user.ID, user.Name, user.CallSign, c.ClientIP())
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"isok":    0,
			"message": "中继台删除成功",
		},
	})
}
