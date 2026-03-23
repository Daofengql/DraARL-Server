package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
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
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "当前用户没有权限设置此参数",
		})
		return
	}

	var server gormdb.Server
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 获取用户信息
	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取用户信息失败",
		})
		return
	}

	server.OwerID = strconv.Itoa(user.ID)
	server.OwerCallSign = user.CallSign

	repo := gormdb.NewServerRepository()
	if err := repo.CreateServer(&server); err != nil {
		log.Printf("创建服务器失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建服务器失败",
		})
		return
	}

	oplog.AddLog(server.String(), "新增服务器成功", user.ID, user.Name, user.CallSign, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "新增服务器成功",
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
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "当前用户没有权限设置此参数",
		})
		return
	}

	var server gormdb.Server
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 获取用户信息
	userRepo := gormdb.NewUserRepository()
	user, _ := userRepo.GetUserByName(username.(string))

	repo := gormdb.NewServerRepository()
	if err := repo.UpdateServer(&server); err != nil {
		log.Printf("更新服务器失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "服务器更新失败",
		})
		return
	}

	if user != nil {
		oplog.AddLog(server.String(), "修改服务器信息成功", user.ID, user.Name, user.CallSign, c.ClientIP())
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "服务器更新成功",
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

	// 获取用户信息
	userRepo := gormdb.NewUserRepository()
	user, _ := userRepo.GetUserByName(username.(string))

	repo := gormdb.NewServerRepository()
	server, _ := repo.GetServerByID(req.ID)

	if err := repo.DeleteServer(req.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "服务器删除失败",
		})
		return
	}

	if server != nil && user != nil {
		oplog.AddLog(server.String(), "服务器删除成功", user.ID, user.Name, user.CallSign, c.ClientIP())
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "服务器删除成功",
	})
}
