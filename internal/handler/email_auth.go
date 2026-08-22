package handler

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"draarl/internal/email"
	"draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// SendCodeRequest 发送验证码请求
type SendCodeRequest struct {
	Email       string `json:"email" binding:"required,email"`
	Purpose     string `json:"purpose" binding:"required"` // register, login, reset_password
	CaptchaID   string `json:"captcha_id" binding:"required"`
	CaptchaCode string `json:"captcha_code" binding:"required"`
}

// SendCodeResponse 发送验证码响应
type SendCodeResponse struct {
	SessionID string `json:"session_id"`
	ExpiresIn int    `json:"expires_in"` // 秒
}

type verificationEmailLookup func(string) (*gormdb.User, error)

func checkVerificationRecipient(purpose email.Purpose, address string, lookup verificationEmailLookup) (int, string, error) {
	if purpose == email.PurposeChangeEmail {
		return 0, "", nil
	}
	user, err := lookup(address)
	if err != nil {
		return http.StatusServiceUnavailable, "服务暂时不可用，请稍后再试", err
	}
	switch purpose {
	case email.PurposeRegister:
		if user != nil {
			return http.StatusConflict, "该邮箱已被注册", nil
		}
	case email.PurposeLogin, email.PurposeResetPassword:
		if user == nil {
			return http.StatusNotFound, "该邮箱未注册", nil
		}
	}
	return 0, "", nil
}

// SendVerificationCode 发送邮箱验证码
func SendVerificationCode(c *gin.Context) {
	var req SendCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证图片验证码
	if !VerifyCaptchaCode(req.CaptchaID, req.CaptchaCode) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "图片验证码错误或已过期",
		})
		return
	}

	// 获取客户端 IP
	clientIP := c.ClientIP()

	mgr := email.GetVerificationManager()
	// 验证 purpose
	var purpose email.Purpose
	switch req.Purpose {
	case "register":
		purpose = email.PurposeRegister
	case "login":
		purpose = email.PurposeLogin
	case "reset_password":
		purpose = email.PurposeResetPassword
	case "change_email":
		purpose = email.PurposeChangeEmail
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的验证码用途",
		})
		return
	}

	// Consume the source-IP budget before account-existence checks. Otherwise an
	// attacker can enumerate registered addresses without paying the send limit.
	if allowed, errMsg := allowVerificationEmailSend(mgr, clientIP); !allowed {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"code":    429,
			"message": errMsg,
		})
		return
	}

	// Account checks deliberately happen after the IP budget is consumed so
	// register/login/reset probing is subject to the same bounded policy.
	repo := gormdb.NewUserRepository()
	if status, message, err := checkVerificationRecipient(purpose, req.Email, repo.GetUserByEmail); status != 0 {
		if err != nil {
			log.Printf("查询验证码邮箱失败: purpose=%s err=%v", purpose, err)
		}
		c.JSON(status, gin.H{"code": status, "message": message})
		return
	}

	// 创建验证会话并发送验证码
	session, err := mgr.CreateSession(req.Email, purpose)
	if err != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"code":    429,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "验证码已发送",
		"data": SendCodeResponse{
			SessionID: session.SessionID,
			ExpiresIn: 600, // 10分钟
		},
	})
}

// EmailLoginRequest 邮箱验证码登录请求
type EmailLoginRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
}

// EmailLogin 邮箱验证码登录
func EmailLogin(c *gin.Context) {
	var req EmailLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证验证码
	mgr := email.GetVerificationManager()
	session, err := mgr.Verify(req.SessionID, req.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	// 验证用途是否正确
	if session.Purpose != email.PurposeLogin {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "验证码用途不正确",
		})
		return
	}

	// 获取用户
	repo := gormdb.NewUserRepository()
	user, _ := repo.GetUserByEmail(session.Email)
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
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
	// Successful email authentication also proves control of the account. Clear
	// password-failure state so an earlier brute-force window cannot block the
	// user after a valid second-factor login.
	loginGuardClear(user.Name)

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

	// 删除验证会话
	mgr.DeleteSession(req.SessionID)

	// 生成 JWT token
	issued, err := issueAuthTokens(c, user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成令牌失败",
		})
		return
	}

	// 记录登录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 邮箱验证码登录成功，IP: %s", user.Name, user.CallSign, c.ClientIP()),
		"email_login",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "登录成功",
		"data": gin.H{
			"token":              issued.AccessToken,
			"refresh_token":      issued.RefreshToken,
			"expires_in":         issued.AccessExpiresIn,
			"refresh_expires_in": issued.RefreshExpiresIn,
			"user":               buildUserResponse(user),
		},
	})
}

// ResetPasswordRequest 重置密码请求
type ResetPasswordRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ResetPassword 重置密码
func ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}
	if err := validateNewUserPassword(req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}

	// 验证验证码
	mgr := email.GetVerificationManager()
	session, err := mgr.Verify(req.SessionID, req.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	// 验证用途是否正确
	if session.Purpose != email.PurposeResetPassword {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "验证码用途不正确",
		})
		return
	}

	// 获取用户
	repo := gormdb.NewUserRepository()
	user, _ := repo.GetUserByEmail(session.Email)
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
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
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码重置失败",
		})
		return
	}
	loginGuardClear(user.Name)
	revokeUserRefreshSessions(user.ID, "password_reset")
	clearRefreshTokenCookie(c)
	clearWSTokenCookie(c)

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 删除验证会话
	mgr.DeleteSession(req.SessionID)

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 通过邮箱重置密码成功，IP: %s", user.Name, user.CallSign, c.ClientIP()),
		"password_reset",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密码重置成功",
	})
}

// VerifyEmailRequest 验证邮箱请求（用于注册）
type VerifyEmailRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
}

// VerifyEmail 验证邮箱（用于注册流程）
func VerifyEmail(c *gin.Context) {
	var req VerifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证验证码
	mgr := email.GetVerificationManager()
	session, err := mgr.Verify(req.SessionID, req.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
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

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "邮箱验证成功",
		"data": gin.H{
			"email":      session.Email,
			"session_id": session.SessionID,
		},
	})
}

// buildUserResponse 构建用户响应
func buildUserResponse(user *gormdb.User) gin.H {
	return gin.H{
		"id":              user.ID,
		"username":        user.Name,
		"email":           user.Email,
		"email_verified":  user.EmailVerified,
		"nickname":        user.NickName,
		"callsign":        user.CallSign,
		"role":            getRoleName(user.GetRoles()),
		"roles":           user.GetRoles(),
		"status":          user.Status,
		"approval_status": user.ApprovalStatus,
		"isAdmin":         hasRoleGORM(user, "admin"),
		"last_login_time": func() string {
			if user.LastLoginTime != nil {
				return user.LastLoginTime.Format("2006-01-02 15:04:05")
			}
			return ""
		}(),
		"last_login_ip":          user.LastLoginIP,
		"last_login_ip_location": getIPLocation(user.LastLoginIP),
		"login_err_times":        user.LoginErrTimes,
	}
}

// ChangeEmailRequest 修改邮箱请求
type ChangeEmailRequest struct {
	OldSessionID string `json:"old_session_id"`                    // 旧邮箱验证会话ID（有邮箱时必填）
	OldCode      string `json:"old_code"`                          // 旧邮箱验证码（有邮箱时必填）
	NewSessionID string `json:"new_session_id" binding:"required"` // 新邮箱验证会话ID
	NewCode      string `json:"new_code" binding:"required"`       // 新邮箱验证码
}

// ChangeEmail 修改用户邮箱（需要登录）
// 如果用户有已验证的邮箱，需要先验证旧邮箱，再验证新邮箱
func ChangeEmail(c *gin.Context) {
	var req ChangeEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 获取当前用户
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

	mgr := email.GetVerificationManager()

	// 如果用户有已验证的邮箱，需要验证旧邮箱
	if user.Email != "" && user.EmailVerified {
		if req.OldSessionID == "" || req.OldCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "请先验证当前邮箱",
			})
			return
		}

		// 验证旧邮箱验证码
		oldSession, err := mgr.Verify(req.OldSessionID, req.OldCode)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "当前邮箱验证码错误或已过期",
			})
			return
		}

		// 验证用途是否正确
		if oldSession.Purpose != email.PurposeChangeEmail {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "当前邮箱验证码用途不正确",
			})
			return
		}

		// 验证旧邮箱是否匹配
		if oldSession.Email != user.Email {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "当前邮箱地址不匹配",
			})
			return
		}

		// 删除旧邮箱验证会话
		mgr.DeleteSession(req.OldSessionID)
	}

	// 验证新邮箱验证码
	newSession, err := mgr.Verify(req.NewSessionID, req.NewCode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "新邮箱验证码错误或已过期",
		})
		return
	}

	// 验证用途是否正确
	if newSession.Purpose != email.PurposeChangeEmail {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "新邮箱验证码用途不正确",
		})
		return
	}

	// 检查新邮箱是否与旧邮箱相同
	if user.Email != "" && newSession.Email == user.Email {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "新邮箱不能与当前邮箱相同",
		})
		return
	}

	// 检查新邮箱是否已被其他用户使用
	existingUser, _ := repo.GetUserByEmail(newSession.Email)
	if existingUser != nil && existingUser.ID != user.ID {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "该邮箱已被其他用户使用",
		})
		return
	}

	// 更新邮箱
	if err := repo.UpdateUserEmail(user.ID, newSession.Email); err != nil {
		log.Printf("更新邮箱失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新邮箱失败",
		})
		return
	}

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 删除新邮箱验证会话
	mgr.DeleteSession(req.NewSessionID)

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 修改邮箱为 %s，IP: %s", user.Name, user.CallSign, newSession.Email, c.ClientIP()),
		"email_change",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "邮箱修改成功",
		"data": gin.H{
			"email": newSession.Email,
		},
	})
}
