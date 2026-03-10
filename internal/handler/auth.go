package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	gormdb "nrllink/internal/gormdb"
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
func hasRoleGORM(user *gormdb.User, role string) bool {
	// 解析 roles 字符串来判断角色
	if user.Roles == "" {
		return role == "user"
	}
	// 简单检查：如果 roles 包含 admin 字符串
	return user.Roles == "[\""+role+"\"]" || user.Roles == "["+role+"]" || user.Roles == "\""+role+"\""
}

// Login 用户登录
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("登录请求参数错误: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	log.Printf("登录请求: username=%s", req.Username)

	// 使用 GORM 查询用户
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(req.Username)
	if err != nil || user == nil {
		log.Printf("用户不存在: %s", req.Username)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户名或密码错误",
		})
		return
	}

	// 验证密码（支持 bcrypt 和���文向后兼容)
	// 验证密码（仅支持 bcrypt）
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		log.Printf("密码验证失败: %v", err)
		repo.IncrementLoginError(user.ID)
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
	repo.UpdateLastLogin(user.ID, c.ClientIP())

	// 生成 JWT token
	roles := user.GetRoles()
	token, err := jwt.GenerateToken(user.Name, roles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成令牌失败",
		})
		return
	}

	log.Printf("用户 %s 登录成功", user.Name)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "登录成功",
		"data": gin.H{
			"token": token,
			"user": gin.H{
				"id":       user.ID,
				"username": user.Name,
				"nickname": user.NickName,
				"isAdmin":  hasRoleGORM(user, "admin"),
			},
		},
	})
}

// Logout 用户登出
func Logout(c *gin.Context) {
	// JWT 是无状态的，客户端删除 token 即可
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

	repo := gormdb.NewUserRepository()

	// 检查用户名是否已存在
	existing, _ := repo.GetUserByName(req.Username)
	if existing != nil {
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

	user := &gormdb.User{
		Name:     req.Username,
		Password: string(hashedPassword),
		NickName: nickname,
		Status:   1,
		Roles:    "[\"user\"]",
	}

	if err := repo.CreateUser(user); err != nil {
		log.Printf("创建用户失败: %v", err)
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

	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username.(string))
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
			"isAdmin":  hasRoleGORM(user, "admin"),
			"status":   user.Status,
		},
	})
}

// GetUsers 获取用户列表（管理员）
func GetUsers(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	keyword := c.Query("keyword")

	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}

	// 检查用户是否为管理员
	username, _ := c.Get("username")
	repo := gormdb.NewUserRepository()
	currentUser, err := repo.GetUserByName(username.(string))
	if err != nil || currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	if !hasRoleGORM(currentUser, "admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "权限不足",
		})
		return
	}

	var users []*gormdb.User
	var total int64

	// 根据是否有关键字选择不同的查询方法
	if keyword != "" {
		users, total, err = repo.SearchUsers(keyword, limit, page)
	} else {
		users, total, err = repo.ListUsers(limit, page)
	}

	if err != nil {
		log.Printf("获取用户列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取用户列表失败",
		})
		return
	}

	// 转换为响应格式
	items := make([]gin.H, 0, len(users))
	for _, u := range users {
		items = append(items, gin.H{
			"id":       u.ID,
			"name":     u.Name,
			"nickname": u.NickName,
			"callsign": u.CallSign,
			"phone":    u.Phone,
			"status":   u.Status,
			"isAdmin":  hasRoleGORM(u, "admin"),
			"roles":    u.Roles,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total":     total,
			"items":     items,
			"page":      page,
			"page_size": limit,
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

	repo := gormdb.NewUserRepository()

	// 检查用户名是否已存在
	existing, _ := repo.GetUserByName(req.Username)
	if existing != nil {
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

	user := &gormdb.User{
		Name:     req.Username,
		Password: string(hashedPassword),
		NickName: nickname,
		Status:   1,
		Roles:    "[\"user\"]",
	}

	if err := repo.CreateUser(user); err != nil {
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
	id, err := strconv.Atoi(idStr)
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

	repo := gormdb.NewUserRepository()

	// 获取用户
	user, err := repo.GetUserByID(id)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 更新字段
	if req.Name != "" {
		user.Name = req.Name
	}
	if req.NickName != "" {
		user.NickName = req.NickName
	}
	if req.Status > 0 {
		user.Status = req.Status
	}
	if req.Roles != "" {
		user.Roles = req.Roles
	}

	if err := repo.UpdateUser(user); err != nil {
		log.Printf("更新用户失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新用户失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id": id,
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

	repo := gormdb.NewUserRepository()

	// 检查用户是否存在
	user, err := repo.GetUserByID(id)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	if err := repo.DeleteUser(id); err != nil {
		log.Printf("删除用户失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除用户失败",
		})
		return
	}

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
	DevNumber       int64 `json:"dev_number"`
	OnlineDevNumber int64 `json:"online_dev_number"`
	UserNumber      int64 `json:"user_number"`
}

// GetTotalStats 获取统计信息
func GetTotalStats(c *gin.Context) {
	userRepo := gormdb.NewUserRepository()
	deviceRepo := gormdb.NewDeviceRepository()

	// 获取真实统计数据
	userCount, _ := userRepo.UserCount()
	devCount, _ := deviceRepo.DeviceCount()

	stats := TotalStats{
		DevNumber:       devCount,
		OnlineDevNumber: 0, // 需要运行时状态
		UserNumber:      userCount,
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

	repo := gormdb.NewUserRepository()

	// 获取当前用户信息
	username, _ := c.Get("username")
	currentUser, err := repo.GetUserByName(username.(string))
	if err != nil || currentUser == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 获取目标用户（检查权限）
	targetUser, err := repo.GetUserByID(id)
	if err != nil || targetUser == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "目标用户不存在",
		})
		return
	}

	// 检查权限：只有管理员或用户本人可以修改密码
	isAdmin := hasRoleGORM(currentUser, "admin")
	isSelf := currentUser.ID == id

	if !isAdmin && !isSelf {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "权限不足",
		})
		return
	}

	// 如果是修改自己的密码，需要验证旧密码
	if isSelf && !isAdmin {
		if err := bcrypt.CompareHashAndPassword([]byte(currentUser.Password), []byte(req.OldPassword)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "原密码错误",
			})
			return
		}
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

	// 更新密码
	if err := repo.UpdateUserPassword(id, string(hashedPassword)); err != nil {
		log.Printf("更新密码失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码修改失败",
		})
		return
	}

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

	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByID(id)
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
			"id":         user.ID,
			"name":       user.Name,
			"nickname":   user.NickName,
			"callsign":   user.CallSign,
			"phone":      user.Phone,
			"status":     user.Status,
			"isAdmin":    hasRoleGORM(user, "admin"),
			"roles":      user.Roles,
			"avatar":     user.Avatar,
			"introduction": user.Introduction,
			"address":    user.Address,
			"sex":        user.Sex,
			"birthday":   user.Birthday,
		},
	})
}
