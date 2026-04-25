package handler

import (
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"draarl/internal/buildinfo"
	"draarl/internal/email"
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/protocol"
	"draarl/pkg/cache"
	appcrypto "draarl/pkg/crypto"
	"draarl/pkg/minio"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// generateDevicePassword 生成随机设备准入密码
// 8位随机字符串，仅包含大小写字母和数字
func generateDevicePassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	// 使用 crypto/rand 生成安全的随机数
	randBytes := make([]byte, 8)
	rand.Read(randBytes)
	for i := range b {
		b[i] = charset[int(randBytes[i])%len(charset)]
	}
	return string(b)
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Username  string `json:"username" binding:"required"`
	Password  string `json:"password" binding:"required"`
	CallSign  string `json:"callsign" binding:"required"`
	Phone     string `json:"phone"` // 手机号可选
	NickName  string `json:"nickname"`
	Email     string `json:"email" binding:"required,email"` // 邮箱必填
	SessionID string `json:"session_id"`                     // 邮箱验证会话ID
	EmailCode string `json:"email_code"`                     // 邮箱验证码
}

// UserResponse 用户响应（用于中间件传递）
type UserResponse struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	CallSign string   `json:"callsign"`
	Roles    []string `json:"roles"`
}

// hasRoleGORM 检查用户是否有指定角色（单角色系统）
func hasRoleGORM(user *gormdb.User, role string) bool {
	if user.Roles == "" {
		return role == "user"
	}
	return user.Roles == role
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

	// 使用 GORM 查询用户（支持用户名或邮箱）
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByNameOrEmail(req.Username)
	if err != nil || user == nil {
		log.Printf("用户不存在: %s", req.Username)
		// 记录登录失败审计日志（用户不存在）
		oplog.AddLog(
			fmt.Sprintf("登录失败: 用户名 %s 不存在, IP: %s", req.Username, c.ClientIP()),
			"login_failed",
			0,
			req.Username,
			"",
			c.ClientIP(),
		)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户名或密码错误",
		})
		return
	}
	// 验证密码（仅支持 bcrypt）
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		log.Printf("密码验证失败: %v", err)
		if err := repo.IncrementLoginError(user.ID); err != nil {
			log.Printf("增加登录错误次数失败: %v", err)
		}
		// 记录登录失败审计日志（密码错误）
		oplog.AddLog(
			fmt.Sprintf("登录失败: 用户 %s (%s) 密码错误, IP: %s", user.Name, user.CallSign, c.ClientIP()),
			"login_failed",
			user.ID,
			user.Name,
			user.CallSign,
			c.ClientIP(),
		)
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
	clientIP := c.ClientIP()
	if err := repo.UpdateLastLogin(user.ID, clientIP); err != nil {
		log.Printf("更新最后登录时间失败: %v", err)
	} else {
		now := time.Now()
		user.LastLoginTime = &now
		user.LastLoginIP = clientIP
		user.LoginErrTimes = 0
		if userCache := cache.GetUserCache(); userCache != nil {
			_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
		}
	}

	// 生成 JWT token
	roles := user.GetRoles()
	issued, err := issueAuthTokens(c, user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成令牌失败",
		})
		return
	}

	log.Printf("用户 %s 登录成功", user.Name)

	// 记录登录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 登录成功，IP: %s", user.Name, user.CallSign, c.ClientIP()),
		"login",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	// 获取用户 Web 端的群组偏好
	userRepo := gormdb.NewUserRepository()
	lastGroupID, _ := userRepo.GetUserLastGroupID(user.ID, protocol.DraARLDevModelBrowser)

	// 构建用户数据
	userData := gin.H{
		"id":              user.ID,
		"username":        user.Name,
		"nickname":        user.NickName,
		"callsign":        user.CallSign,
		"role":            getRoleName(roles),
		"roles":           roles,
		"status":          user.Status,
		"approval_status": user.ApprovalStatus,
		"avatar":          minio.GetAvatarURL(user.Avatar),
		"avatar_thumb":    minio.GetAvatarThumbURL(user.Avatar),
		"phone":           user.Phone,
		"address":         user.Address,
		"introduction":    user.Introduction,
		"sex":             user.Sex,
		"birthday":        user.Birthday,
		"isAdmin":         hasRoleGORM(user, "admin"),
		"dmrid":           user.DMRID,
		"mdcid":           user.MDCID,
		"alarm_msg":       user.AlarmMsg,
		"last_group_id":   lastGroupID, // 用户最后选中的群组（从设备偏好表获取）
		"last_login_time": func() string {
			if user.LastLoginTime != nil {
				return user.LastLoginTime.Format("2006-01-02 15:04:05")
			}
			return ""
		}(),
		"last_login_ip":          user.LastLoginIP,
		"last_login_ip_location": getIPLocation(user.LastLoginIP),
		"login_err_times":        user.LoginErrTimes,
		"created_at":             user.CreateTime.Format("2006-01-02 15:04:05"),
		"updated_at":             user.UpdateTime.Format("2006-01-02 15:04:05"),
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "登录成功",
		"data": gin.H{
			"token":              issued.AccessToken,
			"refresh_token":      issued.RefreshToken,
			"expires_in":         issued.AccessExpiresIn,
			"refresh_expires_in": issued.RefreshExpiresIn,
			"user":               userData,
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
	// 获取当前用户信息用于审计日志
	if username, exists := c.Get("username"); exists {
		repo := gormdb.NewUserRepository()
		if user, err := repo.GetUserByName(username.(string)); err == nil && user != nil {
			oplog.AddLog(
				fmt.Sprintf("用户 %s (%s) 登出，IP: %s", user.Name, user.CallSign, c.ClientIP()),
				"logout",
				user.ID,
				user.Name,
				user.CallSign,
				c.ClientIP(),
			)
		}
	}

	// JWT 是无状态的，客户端删除 token 即可
	revokeCurrentRefreshToken(c, "logout")
	clearRefreshTokenCookie(c)
	clearWSTokenCookie(c)

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
	req.CallSign = gormdb.NormalizeCallSign(req.CallSign)

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
		available, err := repo.IsCallSignAvailable(req.CallSign, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "校验呼号失败",
			})
			return
		}
		if !available {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "呼号已被使用",
			})
			return
		}
	}

	// 检查手机号是否已存在（手机号可选）
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

	// 检查邮箱是否已被注册
	existingEmail, _ := repo.GetUserByEmail(req.Email)
	if existingEmail != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "该邮箱已被注册",
		})
		return
	}

	registrationConfig, err := gormdb.GetSiteConfigRepo().GetRegistrationConfig()
	if err != nil {
		log.Printf("获取注册配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取注册配置失败",
		})
		return
	}

	emailVerified := false
	if registrationConfig.RequireEmailVerification {
		if req.SessionID == "" || req.EmailCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "请完成邮箱验证",
			})
			return
		}

		mgr := email.GetVerificationManager()
		session, err := mgr.Verify(req.SessionID, req.EmailCode)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "邮箱验证码错误或已过期",
			})
			return
		}
		// 验证用途是否正确
		if session.Purpose != email.PurposeRegister {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "验证码用途不正确",
			})
			return
		}
		// 验证邮箱是否匹配
		if session.Email != req.Email {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "邮箱地址不匹配",
			})
			return
		}
		mgr.DeleteSession(req.SessionID)
		emailVerified = true
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

	// 生成设备准入密码
	devicePassword := generateDevicePassword()
	encryptedDevicePassword, err := appcrypto.Encrypt(devicePassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "设备密码加密失败",
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
		DevicePassword: encryptedDevicePassword,
		NickName:       nickname,
		CallSign:       req.CallSign,
		Phone:          req.Phone,
		Email:          req.Email,
		EmailVerified:  emailVerified,
		Status:         1,
		ApprovalStatus: 0, // 待审核状态
		Roles:          "user",
	}

	if err := repo.CreateUser(user); err != nil {
		if err == gormdb.ErrCallSignConflict {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "呼号已被使用",
			})
			return
		}
		log.Printf("创建用户失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建用户失败",
		})
		return
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户注册成功: %s (%s)", user.Name, user.CallSign),
		"register",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "注册成功，请等待管理员审核",
		"data": gin.H{
			"id":              user.ID,
			"username":        user.Name,
			"nickname":        user.NickName,
			"approval_status": user.ApprovalStatus,
			"device_password": devicePassword, // 仅显示一次
		},
	})
}

// GetCurrentUser 获取当前用户信息
func GetCurrentUser(c *gin.Context) {
	username, _ := c.Get("username")

	var user *gormdb.User
	var err error

	// 尝试从缓存获取用户信息
	userCache := cache.GetUserCache()
	if userCache != nil {
		user, err = userCache.GetUserByName(c.Request.Context(), username.(string))
	} else {
		// 缓存不可用，直接从数据库查询
		repo := gormdb.NewUserRepository()
		user, err = repo.GetUserByName(username.(string))
	}

	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 获取用户 Web 端的群组偏好
	userRepo := gormdb.NewUserRepository()
	lastGroupID, _ := userRepo.GetUserLastGroupID(user.ID, protocol.DraARLDevModelBrowser)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":              user.ID,
			"username":        user.Name,
			"nickname":        user.NickName,
			"callsign":        user.CallSign,
			"phone":           user.Phone,
			"email":           user.Email,
			"email_verified":  user.EmailVerified,
			"address":         user.Address,
			"introduction":    user.Introduction,
			"avatar":          minio.GetAvatarURL(user.Avatar),
			"avatar_thumb":    minio.GetAvatarThumbURL(user.Avatar),
			"sex":             user.Sex,
			"birthday":        user.Birthday,
			"role":            getRoleNameFromUser(user),
			"roles":           user.Roles,
			"isAdmin":         hasRoleGORM(user, "admin"),
			"status":          user.Status,
			"approval_status": user.ApprovalStatus,
			"review_note":     user.ReviewNote,
			"dmrid":           user.DMRID,
			"mdcid":           user.MDCID,
			"alarm_msg":       user.AlarmMsg,
			"last_group_id":   lastGroupID, // 从设备偏好表获取
			"last_login_time": func() string {
				if user.LastLoginTime != nil {
					return user.LastLoginTime.Format("2006-01-02 15:04:05")
				}
				return ""
			}(),
			"last_login_ip":          user.LastLoginIP,
			"last_login_ip_location": getIPLocation(user.LastLoginIP),
			"login_err_times":        user.LoginErrTimes,
			"created_at":             user.CreateTime.Format("2006-01-02 15:04:05"),
			"updated_at":             user.UpdateTime.Format("2006-01-02 15:04:05"),
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
			"avatar":       minio.GetAvatarURL(u.Avatar),
			"avatar_thumb": minio.GetAvatarThumbURL(u.Avatar),
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

	// 获取当前操作用户
	currentUser, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}
	currentUserModel := currentUser.(*gormdb.User)

	repo := gormdb.NewUserRepository()

	// 获取目标用户
	user, err := repo.GetUserByID(id)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	oldName := user.Name

	// 只有主管理员（ID=1）可以修改 ID=1 的用户信息
	if id == 1 && currentUserModel.ID != 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "只有主管理员可以修改主管理员信息",
		})
		return
	}

	// 检查是否在修改角色
	newRole := ""
	if req.Roles != "" {
		newRole = req.Roles
	}
	if req.Role != "" {
		if req.Role == "admin" {
			newRole = "admin"
		} else {
			newRole = "user"
		}
	}

	// 主管理员（ID=1）不能修改自己的角色，以防止系统失去管理员
	if id == 1 && currentUserModel.ID == 1 && newRole != "" && newRole != user.Roles {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "主管理员不能修改自己的角色",
		})
		return
	}

	// 主管理员（ID=1）不能修改自己的状态，以防止被禁用
	if id == 1 && currentUserModel.ID == 1 && req.Status > 0 && req.Status != user.Status {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "主管理员不能修改自己的状态",
		})
		return
	}

	// 如果在修改角色，只有主管理员（ID=1）可以操作
	if newRole != "" && currentUserModel.ID != 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "只有主管理员可以修改用户角色",
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
		user.Roles = newRole
	}

	if err := repo.UpdateUser(user); err != nil {
		if err == gormdb.ErrCallSignConflict {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "呼号已被使用",
			})
			return
		}
		log.Printf("更新用户失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新用户失败",
		})
		return
	}

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, oldName)
		if user.Name != oldName {
			_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
		}
	}

	// 获取当前操作用户信息
	if username, exists := c.Get("username"); exists {
		if currentUser, err := repo.GetUserByName(username.(string)); err == nil && currentUser != nil {
			oplog.AddLog(
				fmt.Sprintf("更新用户信息: %s (%s)", user.Name, user.CallSign),
				"user_update",
				currentUser.ID,
				currentUser.Name,
				currentUser.CallSign,
				c.ClientIP(),
			)
		}
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

	// 获取当前操作用户
	currentUser, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}
	currentUserModel := currentUser.(*gormdb.User)

	repo := gormdb.NewUserRepository()

	// 检查目标用户是否存在
	targetUser, err := repo.GetUserByID(id)
	if err != nil || targetUser == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 不能删除管理员用户（只有主管理员可以删除其他管理员）
	if targetUser.Roles == "admin" && currentUserModel.ID != 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "不能删除管理员用户",
		})
		return
	}

	if err := repo.DeleteUserWithCascade(id); err != nil {
		log.Printf("删除用户失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除用户失败",
		})
		return
	}

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), targetUser.ID, targetUser.Name)
	}

	// 获取当前操作用户信息
	if username, exists := c.Get("username"); exists {
		if currentUser, err := repo.GetUserByName(username.(string)); err == nil && currentUser != nil {
			oplog.AddLog(
				fmt.Sprintf("删除用户成功: %s (%s)", targetUser.Name, targetUser.CallSign),
				"user_delete",
				currentUser.ID,
				currentUser.Name,
				currentUser.CallSign,
				c.ClientIP(),
			)
		}
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
	Status int `json:"status"`
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

	// 验证 status 值的有效性（0: 禁用, 1: 启用）
	if req.Status != 0 && req.Status != 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "状态值必须为 0（禁用）或 1（启用）",
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

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 获取当前操作用户信息
	if username, exists := c.Get("username"); exists {
		if currentUser, err := repo.GetUserByName(username.(string)); err == nil && currentUser != nil {
			statusText := "禁用"
			if req.Status == 1 {
				statusText = "启用"
			}
			oplog.AddLog(
				fmt.Sprintf("%s用户: %s (%s)", statusText, user.Name, user.CallSign),
				"user_status",
				currentUser.ID,
				currentUser.Name,
				currentUser.CallSign,
				c.ClientIP(),
			)
		}
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
			"name":     "DraARL 麟链",
			"logourl":  "",
			"language": "zh-CN",
			"version":  buildinfo.VersionString(),
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
	groupRepo := gormdb.NewGroupRepository()

	// 获取真实统计数据
	userCount, _ := userRepo.UserCount()
	devCount, _ := deviceRepo.DeviceCount()
	groupCount, _ := groupRepo.GroupCount()
	onlineCount, _ := deviceRepo.OnlineDeviceCount()

	stats := TotalStats{
		TotalDevices:  devCount,
		OnlineDevices: onlineCount,
		TotalUsers:    userCount,
		TotalGroups:   groupCount,
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

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), targetUser.ID, targetUser.Name)
	}

	// 记录审计日志
	if isAdmin {
		oplog.AddLog(
			fmt.Sprintf("管理员重置用户密码: %s (%s)", targetUser.Name, targetUser.CallSign),
			"password_reset",
			currentUser.ID,
			currentUser.Name,
			currentUser.CallSign,
			c.ClientIP(),
		)
	} else {
		oplog.AddLog(
			fmt.Sprintf("用户修改自己的密码: %s (%s)", currentUser.Name, currentUser.CallSign),
			"password_change",
			currentUser.ID,
			currentUser.Name,
			currentUser.CallSign,
			c.ClientIP(),
		)
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

	ctx := c.Request.Context()
	userCache := cache.GetUserCache()

	var user *gormdb.User
	if userCache != nil {
		user, err = userCache.GetUserByID(ctx, id)
	} else {
		repo := gormdb.NewUserRepository()
		user, err = repo.GetUserByID(id)
	}
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
			"id":           user.ID,
			"name":         user.Name,
			"nickname":     user.NickName,
			"callsign":     user.CallSign,
			"phone":        user.Phone,
			"status":       user.Status,
			"isAdmin":      hasRoleGORM(user, "admin"),
			"roles":        user.Roles,
			"avatar":       minio.GetAvatarURL(user.Avatar),
			"avatar_thumb": minio.GetAvatarThumbURL(user.Avatar),
			"introduction": user.Introduction,
			"address":      user.Address,
			"sex":          user.Sex,
			"birthday":     user.Birthday,
		},
	})
}

// GetUserPublicInfo 获取用户公开信息（任何登录用户可访问）
func GetUserPublicInfo(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	// 检查用户是否已登录
	_, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	ctx := c.Request.Context()
	userCache := cache.GetUserCache()

	var user *gormdb.User
	if userCache != nil {
		user, err = userCache.GetUserByID(ctx, id)
	} else {
		repo := gormdb.NewUserRepository()
		user, err = repo.GetUserByID(id)
	}
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 只返回公开信息
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":           user.ID,
			"username":     user.Name,
			"avatar":       minio.GetAvatarURL(user.Avatar),
			"avatar_thumb": minio.GetAvatarThumbURL(user.Avatar),
			"callsign":     user.CallSign,
			"phone":        user.Phone,
			"address":      user.Address,
			"created_at":   user.CreateTime,
			"status":       user.Status,
		},
	})
}

// GetUserPublicInfoByName 通过用户名获取用户公开信息（任何登录用户名访问）
func GetUserPublicInfoByName(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户名",
		})
		return
	}

	// 检查用户是否已登录
	_, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	ctx := c.Request.Context()
	userCache := cache.GetUserCache()

	var user *gormdb.User
	var err error
	if userCache != nil {
		user, err = userCache.GetUserByName(ctx, username)
	} else {
		repo := gormdb.NewUserRepository()
		user, err = repo.GetUserByName(username)
	}
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 只返回公开信息
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":           user.ID,
			"username":     user.Name,
			"nickname":     user.NickName,
			"avatar":       minio.GetAvatarURL(user.Avatar),
			"avatar_thumb": minio.GetAvatarThumbURL(user.Avatar),
			"callsign":     user.CallSign,
			"phone":        user.Phone,
			"address":      user.Address,
			"created_at":   user.CreateTime,
			"status":       user.Status,
		},
	})
}

// UpdateProfileRequest 更新个人资料请求
type UpdateProfileRequest struct {
	NickName     string `json:"nickname"`
	Phone        string `json:"phone"`
	Address      string `json:"address"`
	Introduction string `json:"introduction"`
	Sex          *int   `json:"sex"` // 使用指针，允许不更新
	Birthday     string `json:"birthday"`
	DMRID        *int   `json:"dmrid"`     // 允许更新 DMRID
	MDCID        string `json:"mdcid"`     // 允许更新 MDCID
	AlarmMsg     *bool  `json:"alarm_msg"` // 允许更新报警消息设置
	// 注意：Avatar 字段已移除，头像更新请使用专门的上传接口
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
	if req.Phone != "" {
		user.Phone = req.Phone
	}
	if req.Address != "" {
		user.Address = req.Address
	}
	if req.Introduction != "" {
		user.Introduction = req.Introduction
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

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户更新个人资料: %s (%s)", user.Name, user.CallSign),
		"profile_update",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id":           user.ID,
			"username":     user.Name,
			"nickname":     user.NickName,
			"callsign":     user.CallSign,
			"phone":        user.Phone,
			"address":      user.Address,
			"introduction": user.Introduction,
			"avatar":       minio.GetAvatarURL(user.Avatar),
			"avatar_thumb": minio.GetAvatarThumbURL(user.Avatar),
			"sex":          user.Sex,
			"birthday":     user.Birthday,
			"dmrid":        user.DMRID,
			"mdcid":        user.MDCID,
			"alarm_msg":    user.AlarmMsg,
			"role":         getRoleNameFromUser(user),
			"roles":        user.Roles,
			"status":       user.Status,
			"isAdmin":      hasRoleGORM(user, "admin"),
			"last_login_time": func() string {
				if user.LastLoginTime != nil {
					return user.LastLoginTime.Format("2006-01-02 15:04:05")
				}
				return ""
			}(),
			"last_login_ip":          user.LastLoginIP,
			"last_login_ip_location": getIPLocation(user.LastLoginIP),
			"login_err_times":        user.LoginErrTimes,
			"created_at":             user.CreateTime.Format("2006-01-02 15:04:05"),
			"updated_at":             user.UpdateTime.Format("2006-01-02 15:04:05"),
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

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户修改自己的密码: %s (%s)", user.Name, user.CallSign),
		"password_change",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密码修改成功",
	})
}
