package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"nrllink/internal/db"
	"nrllink/internal/log"
	"nrllink/internal/models"
)

// CreateServer 创建服务器
func CreateServer(c *gin.Context) {
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

	var server models.Server
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "新增服务器失败,json格式错误",
			},
		})
		return
	}

	// 获取用户信息
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

	server.OwerID = user.ID
	server.OwerCallSign = user.CallSign

	repo := db.NewServerRepository()
	if err := repo.AddServer(&server); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "新增服务器失败",
			},
		})
		return
	}

	log.AddLog(server.String(), "新增服务器成功", user.ID, user.Name, user.CallSign, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"isok":    0,
			"message": "新增服务器成功",
		},
	})
}

// UpdateServer 更新服务器
func UpdateServer(c *gin.Context) {
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
				"message": "当前用户没有权限设置此参数",
			},
		})
		return
	}

	var server models.Server
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "账号操作失败",
			},
		})
		return
	}

	// 获取用户信息
	user, err := db.GetUserByUsername(username.(string))

	repo := db.NewServerRepository()
	if err := repo.UpdateServer(&server); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "服务器更新失败",
			},
		})
		return
	}

	if err == nil {
		log.AddLog(server.String(), "修改服务器信息成功", user.ID, user.Name, user.CallSign, c.ClientIP())
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"message": "服务器更新成功",
		},
	})
}

// DeleteServer 删除服务器
func DeleteServer(c *gin.Context) {
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
				"message": "服务器删除失败",
			},
		})
		return
	}

	// 获取用户信息
	user, err := db.GetUserByUsername(username.(string))

	repo := db.NewServerRepository()
	server, _ := repo.GetServer(req.ID)

	if err := repo.DeleteServer(req.ID); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "服务器删除失败",
			},
		})
		return
	}

	if server != nil && err == nil {
		log.AddLog(server.String(), "服务器删除成功", user.ID, user.Name, user.CallSign, c.ClientIP())
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"isok":    0,
			"message": "服务器删除成功",
		},
	})
}
