package handler

import (
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"time"

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

func buildLoginUserData(user *gormdb.User) gin.H {
	roles := user.GetRoles()
	lastGroupID, _ := gormdb.NewUserRepository().GetUserLastGroupID(user.ID, protocol.DraARLDevModelBrowser)

	return gin.H{
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
		"last_group_id":   lastGroupID,
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
}

// Logout 用户登出

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

	if !VerifyCaptchaCode(req.CaptchaID, req.CaptchaCode) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "图片验证码错误或已过期",
		})
		return
	}

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

	userData := buildLoginUserData(user)

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
