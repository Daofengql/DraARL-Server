package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"draarl/internal/config"
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
)

// WeChatCode2SessionResponse 微信 code2Session 响应
type WeChatCode2SessionResponse struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

// weChatCode2Session 调用微信 code2Session 接口获取 openid
func weChatCode2Session(code string) (*WeChatCode2SessionResponse, error) {
	cfg := config.Get()
	if !cfg.IsWeChatEnabled() {
		return nil, fmt.Errorf("微信小程序功能未配置")
	}

	url := fmt.Sprintf("https://api.weixin.qq.com/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		cfg.WeChat.AppID, cfg.WeChat.AppSecret, code)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("请求微信接口失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result WeChatCode2SessionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return nil, fmt.Errorf("微信接口错误: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	if result.OpenID == "" {
		return nil, fmt.Errorf("微信返回 openid 为空")
	}

	return &result, nil
}

// WeChatMiniLogin 微信小程序快捷登录
func WeChatMiniLogin(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{Code: 400, Message: "请求参数错误"})
		return
	}

	cfg := config.Get()
	if !cfg.IsWeChatEnabled() {
		c.JSON(http.StatusServiceUnavailable, Response{Code: 503, Message: "微信小程序功能未配置"})
		return
	}

	// 调用微信接口获取 openid
	wxResp, err := weChatCode2Session(req.Code)
	if err != nil {
		log.Printf("[WeChat-Mini] code2Session 失败: %v", err)
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "微信登录失败: " + err.Error()})
		return
	}

	// 查找已绑定该 openid 的用户
	user := FindUserBySSOID(SSOProviderWechat, wxResp.OpenID)
	if user == nil {
		c.JSON(http.StatusNotFound, Response{Code: 404, Message: "该微信号未绑定账号，请先使用账号密码登录后绑定"})
		return
	}

	// 检查用户状态
	if user.Status != 1 {
		c.JSON(http.StatusForbidden, Response{Code: 403, Message: "用户已被禁用"})
		return
	}

	// 更新最后登录时间
	repo := gormdb.NewUserRepository()
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

	// 签发 JWT
	issued, err := issueAuthTokens(c, user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{Code: 500, Message: "生成令牌失败"})
		return
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 通过微信小程序登录成功，IP: %s", user.Name, user.CallSign, c.ClientIP()),
		"wechat_mini_login",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "登录成功",
		Data: gin.H{
			"token":              issued.AccessToken,
			"refresh_token":      issued.RefreshToken,
			"expires_in":         issued.AccessExpiresIn,
			"refresh_expires_in": issued.RefreshExpiresIn,
			"user":               buildLoginUserData(user),
		},
	})
}

// WeChatMiniBind 绑定微信小程序账号
func WeChatMiniBind(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{Code: 400, Message: "请求参数错误"})
		return
	}

	cfg := config.Get()
	if !cfg.IsWeChatEnabled() {
		c.JSON(http.StatusServiceUnavailable, Response{Code: 503, Message: "微信小程序功能未配置"})
		return
	}

	// 获取当前用户（带 ok 保护，避免类型断言 panic）
	usernameVal, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "未认证"})
		return
	}
	username, ok := usernameVal.(string)
	if !ok || username == "" {
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "未认证"})
		return
	}
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username)
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "用户不存在"})
		return
	}

	// 检查是否已绑定微信
	if HasSSOBinding(user.OpenID, SSOProviderWechat) {
		c.JSON(http.StatusBadRequest, Response{Code: 400, Message: "已绑定微信号，请先解绑"})
		return
	}

	// 调用微信接口获取 openid
	wxResp, err := weChatCode2Session(req.Code)
	if err != nil {
		log.Printf("[WeChat-Mini] code2Session 失败: %v", err)
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "获取微信信息失败: " + err.Error()})
		return
	}

	// 检查该 openid 是否已被其他用户绑定
	existingUser := FindUserBySSOID(SSOProviderWechat, wxResp.OpenID)
	if existingUser != nil && existingUser.ID != user.ID {
		c.JSON(http.StatusConflict, Response{Code: 409, Message: "该微信号已绑定其他账号"})
		return
	}

	// 添加绑定
	newOpenID := AddSSOBinding(user.OpenID, SSOProviderWechat, wxResp.OpenID)
	user.OpenID = newOpenID
	if err := repo.UpdateUser(user); err != nil {
		log.Printf("更新用户OpenID失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{Code: 500, Message: "绑定失败"})
		return
	}

	// 使缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 绑定微信小程序账号成功", user.Name, user.CallSign),
		"wechat_mini_bind",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, Response{Code: 200, Message: "绑定成功", Data: gin.H{"bound": true}})
}

// WeChatMiniUnbind 解绑微信小程序账号
func WeChatMiniUnbind(c *gin.Context) {
	usernameVal, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "未认证"})
		return
	}
	username, ok := usernameVal.(string)
	if !ok || username == "" {
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "未认证"})
		return
	}
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username)
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "用户不存在"})
		return
	}

	if !HasSSOBinding(user.OpenID, SSOProviderWechat) {
		c.JSON(http.StatusBadRequest, Response{Code: 400, Message: "未绑定微信账号"})
		return
	}

	newOpenID := RemoveSSOBinding(user.OpenID, SSOProviderWechat)
	if err := repo.UpdateUserOpenID(user.ID, newOpenID); err != nil {
		log.Printf("更新用户OpenID失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{Code: 500, Message: "解绑失败"})
		return
	}

	// 使缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 解除微信小程序账号绑定", user.Name, user.CallSign),
		"wechat_mini_unbind",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, Response{Code: 200, Message: "解绑成功"})
}

// WeChatMiniStatus 查询微信绑定状态
func WeChatMiniStatus(c *gin.Context) {
	usernameVal, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "未认证"})
		return
	}
	username, ok := usernameVal.(string)
	if !ok || username == "" {
		c.JSON(http.StatusUnauthorized, Response{Code: 401, Message: "未认证"})
		return
	}
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, Response{Code: 404, Message: "用户不存在"})
		return
	}

	bound := HasSSOBinding(user.OpenID, SSOProviderWechat)
	var wechatOpenID string
	if bound {
		wechatOpenID = GetSSOID(user.OpenID, SSOProviderWechat)
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "成功",
		Data: gin.H{
			"bound":  bound,
			"openid": wechatOpenID,
		},
	})
}
