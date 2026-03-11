package handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	gormdb "nrllink/internal/gormdb"
	"nrllink/pkg/jwt"
	"nrllink/pkg/minio"
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
	CallSign string `json:"callsign" binding:"required"`
	Phone    string `json:"phone" binding:"required"`
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

	// 构建用户数据
	userData := gin.H{
		"id":              user.ID,
		"username":        user.Name,
		"nickname":        user.NickName,
		"callsign":        user.CallSign,
		"role":            getRoleName(roles),
		"roles":           roles,
		"status":          user.Status,
		"approval_status":  user.ApprovalStatus,
		"avatar":          user.Avatar,
		"avatar_thumb":    user.AvatarThumb,
		"phone":           user.Phone,
		"address":         user.Address,
		"introduction":    user.Introduction,
		"sex":             user.Sex,
		"birthday":        user.Birthday,
		"isAdmin":         hasRoleGORM(user, "admin"),
		"dmrid":           user.DMRID,
		"mdcid":           user.MDCID,
		"alarm_msg":       user.AlarmMsg,
		"last_login_time": func() string {
			if user.LastLoginTime != nil {
				return user.LastLoginTime.Format("2006-01-02 15:04:05")
			}
			return ""
		}(),
		"last_login_ip":   user.LastLoginIP,
		"login_err_times": user.LoginErrTimes,
		"created_at":      user.CreateTime.Format("2006-01-02 15:04:05"),
		"updated_at":      user.UpdateTime.Format("2006-01-02 15:04:05"),
	}

	// 处理头像URL
	if avatarVal, ok := userData["avatar"].(string); ok && avatarVal != "" && !strings.HasPrefix(avatarVal, "http") {
		userData["avatar"] = minio.GetFileURL(avatarVal)
	}
	if avatarThumbVal, ok := userData["avatar_thumb"].(string); ok && avatarThumbVal != "" && !strings.HasPrefix(avatarThumbVal, "http") {
		userData["avatar_thumb"] = minio.GetFileURL(avatarThumbVal)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "登录成功",
		"data": gin.H{
			"token": token,
			"user":  userData,
		},
	})
}

// getRoleName 从角色列表中获取主要角色名称
func getRoleName(roles []string) string {
	for _, role := range roles {
		if role == "admin" {
			return "admin"
		}
	}
	return "user"
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

	// 检查呼号是否已存在
	if req.CallSign != "" {
		existingCallSign, _ := repo.GetUserByCallSign(req.CallSign)
		if existingCallSign != nil {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "呼号已被使用",
			})
			return
		}
	}

	// 检查手机号是否已存在
	if req.Phone != "" {
		existingPhone, _ := repo.GetUserByPhone(req.Phone)
		if existingPhone != nil {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "手机号已被注册",
			})
			return
		}
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
		Name:           req.Username,
		Password:       string(hashedPassword),
		NickName:       nickname,
		CallSign:       req.CallSign,
		Phone:          req.Phone,
		Status:         1,
		ApprovalStatus: 0, // 待审核状态
		Roles:          "[\"user\"]",
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
		"message": "注册成功，请等待管理员审核",
		"data": gin.H{
			"id":              user.ID,
			"username":        user.Name,
			"nickname":        user.NickName,
			"approval_status":  user.ApprovalStatus,
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

	// 处理头像URL
	avatarURL := user.Avatar
	if avatarURL != "" && !strings.HasPrefix(avatarURL, "http") {
		avatarURL = minio.GetFileURL(avatarURL)
	}
	avatarThumbURL := user.AvatarThumb
	if avatarThumbURL != "" && !strings.HasPrefix(avatarThumbURL, "http") {
		avatarThumbURL = minio.GetFileURL(avatarThumbURL)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":             user.ID,
			"username":       user.Name,
			"nickname":       user.NickName,
			"callsign":       user.CallSign,
			"phone":          user.Phone,
			"address":        user.Address,
			"introduction":   user.Introduction,
			"avatar":         avatarURL,
			"avatar_thumb":   avatarThumbURL,
			"sex":            user.Sex,
			"birthday":       user.Birthday,
			"role":           getRoleNameFromUser(user),
			"roles":          user.Roles,
			"isAdmin":        hasRoleGORM(user, "admin"),
			"status":         user.Status,
			"approval_status": user.ApprovalStatus,
			"review_note":    user.ReviewNote,
			"dmrid":          user.DMRID,
			"mdcid":          user.MDCID,
			"alarm_msg":      user.AlarmMsg,
			"last_login_time": func() string {
				if user.LastLoginTime != nil {
					return user.LastLoginTime.Format("2006-01-02 15:04:05")
				}
				return ""
			}(),
			"last_login_ip":  user.LastLoginIP,
			"login_err_times": user.LoginErrTimes,
			"created_at":     user.CreateTime.Format("2006-01-02 15:04:05"),
			"updated_at":     user.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// getRoleNameFromUser 从用户获取角色名称
func getRoleNameFromUser(user *gormdb.User) string {
	if user.Roles == "" {
		return "user"
	}
	// 检查是否包含 admin
	if hasRoleGORM(user, "admin") {
		return "admin"
	}
	return "user"
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
		// 处理头像URL：如果数据库中存的是完整URL则直接使用，否则拼接
		avatarURL := u.Avatar
		if avatarURL != "" && !strings.HasPrefix(avatarURL, "http") {
			avatarURL = minio.GetFileURL(avatarURL)
		}
		avatarThumbURL := u.AvatarThumb
		if avatarThumbURL != "" && !strings.HasPrefix(avatarThumbURL, "http") {
			avatarThumbURL = minio.GetFileURL(avatarThumbURL)
		}

		items = append(items, gin.H{
			"id":           u.ID,
			"username":     u.Name,
			"nickname":     u.NickName,
			"callsign":     u.CallSign,
			"phone":        u.Phone,
			"address":      u.Address,
			"status":       u.Status,
			"role":         getRoleNameFromUser(u),
			"isAdmin":      hasRoleGORM(u, "admin"),
			"roles":        u.Roles,
			"avatar":       avatarURL,
			"avatar_thumb": avatarThumbURL,
			"created_at":   u.CreateTime.Format("2006-01-02 15:04:05"),
			"updated_at":   u.UpdateTime.Format("2006-01-02 15:04:05"),
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
	Username string `json:"username"` // 前端使用 username
	NickName string `json:"nickname"`
	CallSign string `json:"callsign"` // 前端使用 callsign
	Phone    string `json:"phone"`
	Address  string `json:"address"`
	Status   int    `json:"status"`
	Roles    string `json:"roles"`
	Role     string `json:"role"` // 前端使用 role
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
	if req.Username != "" {
		user.Name = req.Username
	}
	if req.NickName != "" {
		user.NickName = req.NickName
	}
	if req.CallSign != "" {
		user.CallSign = req.CallSign
	}
	if req.Phone != "" {
		user.Phone = req.Phone
	}
	if req.Address != "" {
		user.Address = req.Address
	}
	if req.Status > 0 {
		user.Status = req.Status
	}
	if req.Roles != "" {
		user.Roles = req.Roles
	}
	if req.Role != "" {
		// 前端发送 role (单数)，转换为 roles JSON 数组格式
		if req.Role == "admin" {
			user.Roles = `["admin"]`
		} else {
			user.Roles = `["user"]`
		}
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

	// 不允许删除ID为1的主管理员
	if id == 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "主管理员不能被删除",
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

// UpdateUserStatusRequest 更新用户状态请求
type UpdateUserStatusRequest struct {
	Status int `json:"status" binding:"required"`
}

// UpdateUserStatus 更新用户状态（禁用/启用）
func UpdateUserStatus(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	// 不允许修改ID为1的主管理员状态
	if id == 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "主管理员不能被禁用",
		})
		return
	}

	var req UpdateUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
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

	// 更新用户状态
	if err := repo.UpdateUserStatus(id, req.Status); err != nil {
		log.Printf("更新用户状态失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新用户状态失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id":     id,
			"status": req.Status,
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
	TotalDevices  int64 `json:"total_devices"`
	OnlineDevices int64 `json:"online_devices"`
	TotalUsers    int64 `json:"total_users"`
	TotalGroups   int64 `json:"total_groups"`
}

// GetTotalStats 获取统计信息
func GetTotalStats(c *gin.Context) {
	userRepo := gormdb.NewUserRepository()
	deviceRepo := gormdb.NewDeviceRepository()

	// 获取真实统计数据
	userCount, _ := userRepo.UserCount()
	devCount, _ := deviceRepo.DeviceCount()

	stats := TotalStats{
		TotalDevices:  devCount,
		OnlineDevices: 0, // 需要运行时状态
		TotalUsers:    userCount,
		TotalGroups:   0,
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    stats,
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

	// 处理头像URL
	avatarURL := user.Avatar
	if avatarURL != "" && !strings.HasPrefix(avatarURL, "http") {
		avatarURL = minio.GetFileURL(avatarURL)
	}
	avatarThumbURL := user.AvatarThumb
	if avatarThumbURL != "" && !strings.HasPrefix(avatarThumbURL, "http") {
		avatarThumbURL = minio.GetFileURL(avatarThumbURL)
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
			"avatar":     avatarURL,
			"avatar_thumb": avatarThumbURL,
			"introduction": user.Introduction,
			"address":    user.Address,
			"sex":        user.Sex,
			"birthday":   user.Birthday,
		},
	})
}

// UpdateProfileRequest 更新个人资料请求
type UpdateProfileRequest struct {
	NickName     string `json:"nickname"`
	CallSign     string `json:"callsign"`
	Phone        string `json:"phone"`
	Address      string `json:"address"`
	Introduction string `json:"introduction"`
	Avatar       string `json:"avatar"`
	Sex          *int   `json:"sex"`       // 使用指针，允许不更新
	Birthday     string `json:"birthday"`
	DMRID        *int   `json:"dmrid"`      // 允许更新 DMRID
	MDCID        string `json:"mdcid"`      // 允许更新 MDCID
	AlarmMsg     *bool  `json:"alarm_msg"`  // 允许更新报警消息设置
}

// UpdateProfile 更新当前用户个人资料
func UpdateProfile(c *gin.Context) {
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

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

	// 更新字段
	if req.NickName != "" {
		user.NickName = req.NickName
	}
	if req.CallSign != "" {
		user.CallSign = req.CallSign
	}
	if req.Phone != "" {
		user.Phone = req.Phone
	}
	if req.Address != "" {
		user.Address = req.Address
	}
	if req.Introduction != "" {
		user.Introduction = req.Introduction
	}
	if req.Avatar != "" {
		user.Avatar = req.Avatar
	}
	// 性别字段处理：
	// - 0 = 保密
	// - 1 = 男
	// - 2 = 女
	if req.Sex != nil {
		user.Sex = *req.Sex
	}
	if req.Birthday != "" {
		user.Birthday = req.Birthday
	}
	if req.DMRID != nil {
		user.DMRID = *req.DMRID
	}
	if req.MDCID != "" {
		user.MDCID = req.MDCID
	}
	if req.AlarmMsg != nil {
		user.AlarmMsg = *req.AlarmMsg
	}

	if err := repo.UpdateUser(user); err != nil {
		log.Printf("更新个人资料失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新个人资料失败",
		})
		return
	}

	// 处理头像URL
	avatarURL := user.Avatar
	if avatarURL != "" && !strings.HasPrefix(avatarURL, "http") {
		avatarURL = minio.GetFileURL(avatarURL)
	}
	avatarThumbURL := user.AvatarThumb
	if avatarThumbURL != "" && !strings.HasPrefix(avatarThumbURL, "http") {
		avatarThumbURL = minio.GetFileURL(avatarThumbURL)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id":              user.ID,
			"username":        user.Name,
			"nickname":        user.NickName,
			"callsign":        user.CallSign,
			"phone":           user.Phone,
			"address":         user.Address,
			"introduction":    user.Introduction,
			"avatar":          avatarURL,
			"avatar_thumb":    avatarThumbURL,
			"sex":             user.Sex,
			"birthday":        user.Birthday,
			"dmrid":           user.DMRID,
			"mdcid":           user.MDCID,
			"alarm_msg":       user.AlarmMsg,
			"role":            getRoleNameFromUser(user),
			"roles":           user.Roles,
			"status":          user.Status,
			"isAdmin":         hasRoleGORM(user, "admin"),
			"last_login_time": func() string {
				if user.LastLoginTime != nil {
					return user.LastLoginTime.Format("2006-01-02 15:04:05")
				}
				return ""
			}(),
			"last_login_ip":   user.LastLoginIP,
			"login_err_times": user.LoginErrTimes,
			"created_at":      user.CreateTime.Format("2006-01-02 15:04:05"),
			"updated_at":      user.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// ChangeOwnPasswordRequest 修改自己的密码请求
type ChangeOwnPasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ChangeOwnPassword 修改自己的密码
func ChangeOwnPassword(c *gin.Context) {
	var req ChangeOwnPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

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

	// 更新密码
	if err := repo.UpdateUserPassword(user.ID, string(hashedPassword)); err != nil {
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
	})
}
