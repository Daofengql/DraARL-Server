package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"nrllink/internal/db"
	"nrllink/internal/models"
	"nrllink/pkg/jwt"
)

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	NickName string `json:"nickname"`
}

// UserResponse 用户响应（用于中间件传递）
type UserResponse struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	CallSign string   `json:"callsign"`
	Roles    []string `json:"roles"`
}

// hasRole 检查用户是否有指定角色
func hasRole(user *models.User, role string) bool {
	for _, r := range user.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// Login 用户登录
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 查询用户
	user, err := db.GetUserByUsername(req.Username)
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户名或密码错误",
		})
		return
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		// 增加登录错误次数
		db.UpdateLoginError(user.ID)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户名或密码错误",
		})
		return
	}

	// 检查用户状态
	if user.Status != 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "用户已被禁用",
		})
		return
	}

	// 更新最后登录时间
	db.UpdateLastLogin(user.ID, c.ClientIP())

	// 生成 JWT token - 使用用户已有的角色
	token, err := jwt.GenerateToken(user.Name, user.Roles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成令牌失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "登录成功",
		"data": gin.H{
			"token": token,
			"user": gin.H{
				"id":       user.ID,
				"username": user.Name,
				"nickname": user.NickName,
				"isAdmin":  hasRole(user, "admin"),
			},
		},
	})
}

// Logout 用户登出
func Logout(c *gin.Context) {
	// JWT 是无状态的，客户端删除 token 即可
	// 后端可以实现 token 黑名单（Redis）
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "登出成功",
	})
}

// Register 用户注册
func Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 检查用户名是否已存在
	existing, err := db.GetUserByUsername(req.Username)
	if err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "用户名已存在",
		})
		return
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	// 创建用户
	nickname := req.NickName
	if nickname == "" {
		nickname = req.Username
	}

	user := &models.User{
		Name:     req.Username,
		Password: string(hashedPassword),
		NickName: nickname,
		Status:   1,
		Roles:    []string{"user"},
	}

	if err := db.CreateUser(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建用户失败",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "注册成功",
		"data": gin.H{
			"id":       user.ID,
			"username": user.Name,
			"nickname": user.NickName,
		},
	})
}

// GetCurrentUser 获取当前用户信息
func GetCurrentUser(c *gin.Context) {
	username, _ := c.Get("username")

	user, err := db.GetUserByUsername(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":       user.ID,
			"username": user.Name,
			"nickname": user.NickName,
			"isAdmin":  hasRole(user, "admin"),
			"status":   user.Status,
		},
	})
}

// GetUsers 获取用户列表
func GetUsers(c *gin.Context) {
	_, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))
	_, _ = strconv.Atoi(c.DefaultQuery("page", "1"))

	// TODO: 实现用户列表查询
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total": 0,
			"items": []interface{}{},
		},
	})
}

// CreateUser 创建用户（管理员）
func CreateUser(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 检查用户名是否已存在
	existing, err := db.GetUserByUsername(req.Username)
	if err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "用户名已存在",
		})
		return
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	nickname := req.NickName
	if nickname == "" {
		nickname = req.Username
	}

	user := &models.User{
		Name:     req.Username,
		Password: string(hashedPassword),
		NickName: nickname,
		Status:   1,
		Roles:    []string{"user"},
	}

	if err := db.CreateUser(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建用户失败",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "创建成功",
		"data": gin.H{
			"id": user.ID,
		},
	})
}

// UpdateUserRequest 更新用户请求
type UpdateUserRequest struct {
	Name     string `json:"name"`
	NickName string `json:"nickname"`
	Status   int    `json:"status"`
	Roles    string `json:"roles"`
}

// UpdateUser 更新用户
func UpdateUser(c *gin.Context) {
	idStr := c.Param("id")
	_, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// TODO: 实现更新用户的数据库操作
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id": idStr,
		},
	})
}

// DeleteUser 删除用户
func DeleteUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	// TODO: 实现删除用户的数据库操作
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
		"data": gin.H{
			"id": id,
		},
	})
}

// GetPlatformInfo 获取平台信息
func GetPlatformInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"name":     "NRL 火链",
			"logourl":  "",
			"language": "zh-CN",
			"version":  "v2.0.0",
			"icp":      "",
			"mail":     "",
			"callsign": "",
		},
	})
}

// TotalStats 统计信息
type TotalStats struct {
	DevNumber        int `json:"dev_number"`
	OnlineDevNumber  int `json:"online_dev_number"`
	UserNumber       int `json:"user_number"`
}

// GetTotalStats 获取统计信息
func GetTotalStats(c *gin.Context) {
	// TODO: 实现真实的统计数据
	stats := TotalStats{
		DevNumber:       0,
		OnlineDevNumber: 0,
		UserNumber:      0,
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": []interface{}{stats},
		},
	})
}

// UpdateUserPasswordRequest 修改密码请求
type UpdateUserPasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// UpdateUserPassword 修改用户密码
func UpdateUserPassword(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	var req UpdateUserPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 获取当前用户信息
	username, _ := c.Get("username")
	user, err := db.GetUserByUsername(username.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "原密码错误",
		})
		return
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	// TODO: 实现更新密码的数据库操作
	_ = hashedPassword // 暂时忽略未使用警告
	// db.UpdateUserPassword(id, string(hashedPassword))

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密码修改成功",
		"data": gin.H{
			"id": id,
		},
	})
}

// GetUserDetail 获取用户详情
func GetUserDetail(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	// TODO: 实现通过ID获取用户详情
	// 目前使用 GetUserByUsername 需要用户名

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id": id,
		},
	})
}
