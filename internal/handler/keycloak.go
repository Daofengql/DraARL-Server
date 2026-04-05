package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"draarl/internal/config"
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/protocol"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
)

// SSO提供商常量
const (
	SSOProviderKeycloak = "ky"
	SSOProviderWechat   = "wx"
	SSOProviderGoogle   = "gg"
)

// state存储，用于OAuth防CSRF
var (
	stateStore = make(map[string]stateEntry)
	stateMutex sync.RWMutex

	loginCodeStore = make(map[string]loginCodeEntry)
	loginCodeMutex sync.RWMutex
)

type stateEntry struct {
	Action    string    // "login" 或 "bind"
	UserID    int       // 绑定操作时的用户ID
	ExpiresAt time.Time // 过期时间
}

type loginCodeEntry struct {
	UserID    int
	UserJSON  string
	ExpiresAt time.Time
}

// ============== SSO OpenID 辅助函数 ==============

// AddSSOBinding 添加SSO绑定到OpenID字段
func AddSSOBinding(openID string, provider, ssoID string) string {
	binding := fmt.Sprintf("%s:%s", provider, ssoID)

	if openID == "" {
		return binding
	}

	// 检查是否已存在该provider的绑定
	bindings := strings.Split(openID, ",")
	for i, b := range bindings {
		if strings.HasPrefix(b, provider+":") {
			bindings[i] = binding // 替换现有绑定
			return strings.Join(bindings, ",")
		}
	}

	// 添加新绑定
	return openID + "," + binding
}

// RemoveSSOBinding 从OpenID字段移除指定provider的绑定
func RemoveSSOBinding(openID string, provider string) string {
	if openID == "" {
		return ""
	}

	bindings := strings.Split(openID, ",")
	result := make([]string, 0, len(bindings))

	for _, b := range bindings {
		if !strings.HasPrefix(b, provider+":") {
			result = append(result, b)
		}
	}

	return strings.Join(result, ",")
}

// HasSSOBinding 检查是否已绑定指定provider
func HasSSOBinding(openID string, provider string) bool {
	return GetSSOID(openID, provider) != ""
}

// GetSSOID 获取指定provider的SSO ID
func GetSSOID(openID string, provider string) string {
	if openID == "" {
		return ""
	}

	prefix := provider + ":"
	bindings := strings.Split(openID, ",")

	for _, b := range bindings {
		if strings.HasPrefix(b, prefix) {
			return strings.TrimPrefix(b, prefix)
		}
	}

	return ""
}

// FindUserBySSOID 查找绑定指定SSO的用户
func FindUserBySSOID(provider, ssoID string) *gormdb.User {
	repo := gormdb.NewUserRepository()
	users, _, err := repo.ListUsers(1000, 1) // 获取用户列表
	if err != nil {
		return nil
	}

	targetBinding := fmt.Sprintf("%s:%s", provider, ssoID)

	for _, user := range users {
		if strings.Contains(user.OpenID, targetBinding) {
			return user
		}
	}

	return nil
}

// ============== State 管理 ==============

// generateState 生成随机state
func generateState() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// saveState 保存state
func saveState(state string, action string, userID int) {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	stateStore[state] = stateEntry{
		Action:    action,
		UserID:    userID,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}

	// 清理过期state
	go cleanExpiredStates()
}

// consumeState 消费state（验证后删除）
func consumeState(state string) *stateEntry {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	entry, exists := stateStore[state]
	if !exists {
		return nil
	}

	delete(stateStore, state)

	if time.Now().After(entry.ExpiresAt) {
		return nil
	}

	return &entry
}

// cleanExpiredStates 清理过期state
func cleanExpiredStates() {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	now := time.Now()
	for k, v := range stateStore {
		if now.After(v.ExpiresAt) {
			delete(stateStore, k)
		}
	}
}

// saveLoginCode 保存一次性 SSO 登录交换码
func saveLoginCode(userID int, userData gin.H) (string, error) {
	userDataJSON, err := json.Marshal(userData)
	if err != nil {
		return "", err
	}

	code := generateState()

	loginCodeMutex.Lock()
	defer loginCodeMutex.Unlock()

	loginCodeStore[code] = loginCodeEntry{
		UserID:    userID,
		UserJSON:  string(userDataJSON),
		ExpiresAt: time.Now().Add(2 * time.Minute),
	}

	go cleanExpiredLoginCodes()

	return code, nil
}

// consumeLoginCode 消费一次性 SSO 登录交换码
func consumeLoginCode(code string) *loginCodeEntry {
	loginCodeMutex.Lock()
	defer loginCodeMutex.Unlock()

	entry, exists := loginCodeStore[code]
	if !exists {
		return nil
	}

	delete(loginCodeStore, code)

	if time.Now().After(entry.ExpiresAt) {
		return nil
	}

	return &entry
}

// cleanExpiredLoginCodes 清理过期登录交换码
func cleanExpiredLoginCodes() {
	loginCodeMutex.Lock()
	defer loginCodeMutex.Unlock()

	now := time.Now()
	for k, v := range loginCodeStore {
		if now.After(v.ExpiresAt) {
			delete(loginCodeStore, k)
		}
	}
}

// ============== Keycloak OAuth 实现 ==============

// KeycloakTokenResponse Keycloak token响应
type KeycloakTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// KeycloakUserInfo Keycloak用户信息
type KeycloakUserInfo struct {
	Sub               string `json:"sub"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	Name              string `json:"name"`
}

// isKeycloakEnabled 检查Keycloak是否启用
func isKeycloakEnabled() bool {
	return config.Get().Keycloak.Enabled
}

// getKeycloakConfig 获取Keycloak配置
func getKeycloakConfig() *config.Configuration {
	return config.Get()
}

// ============== HTTP 处理器 ==============

// GetSSOLoginURL 获取SSO登录URL
func GetSSOLoginURL(c *gin.Context) {
	if !isKeycloakEnabled() {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "SSO未启用",
		})
		return
	}

	cfg := getKeycloakConfig()
	state := generateState()

	// 保存state用于登录
	saveState(state, "login", 0)

	// 构建授权URL
	authURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth?client_id=%s&redirect_uri=%s&response_type=code&state=%s",
		cfg.Keycloak.BaseURL,
		cfg.Keycloak.Realm,
		cfg.Keycloak.ClientID,
		url.QueryEscape(cfg.Keycloak.RedirectURI),
		state,
	)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "成功",
		Data: gin.H{
			"url": authURL,
		},
	})
}

// SSOCallback SSO回调处理（直接从Keycloak回调）
func SSOCallback(c *gin.Context) {
	if !isKeycloakEnabled() {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "SSO未启用",
		})
		return
	}

	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "缺少必要参数",
		})
		return
	}

	// 验证state
	entry := consumeState(state)
	if entry == nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的state参数",
		})
		return
	}

	// 获取Keycloak用户信息
	kcUserInfo, err := getKeycloakUserInfo(code)
	if err != nil {
		log.Printf("获取Keycloak用户信息失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取SSO用户信息失败",
		})
		return
	}

	cfg := getKeycloakConfig()
	// 前端地址（用于重定向）
	frontendURL := cfg.Web.FrontendURL
	if frontendURL == "" {
		frontendURL = "http://localhost:5173" // 默认前端地址
	}

	if entry.Action == "bind" {
		// 绑定操作
		result := handleBindCallbackRedirect(c, entry.UserID, kcUserInfo, frontendURL)
		c.Redirect(http.StatusFound, result)
	} else {
		// 登录操作
		result := handleLoginCallbackRedirect(c, kcUserInfo, frontendURL)
		c.Redirect(http.StatusFound, result)
	}
}

// ExchangeSSOCode 使用一次性交换码换取登录态数据（避免 URL 透传 token）
func ExchangeSSOCode(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	entry := consumeLoginCode(req.Code)
	if entry == nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效或已过期的登录交换码",
		})
		return
	}

	userData := gin.H{}
	if err := json.Unmarshal([]byte(entry.UserJSON), &userData); err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "解析登录数据失败",
		})
		return
	}

	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByID(entry.UserID)
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "用户不存在或会话已失效",
		})
		return
	}

	if user.Status != 1 || user.ApprovalStatus != 1 {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "用户状态异常，无法完成登录",
		})
		return
	}

	issued, err := issueAuthTokens(c, user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "生成令牌失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "成功",
		Data: gin.H{
			"token":              issued.AccessToken,
			"refresh_token":      issued.RefreshToken,
			"expires_in":         issued.AccessExpiresIn,
			"refresh_expires_in": issued.RefreshExpiresIn,
			"user":               userData,
		},
	})
}

// getKeycloakUserInfo 通过code获取Keycloak用户信息
func getKeycloakUserInfo(code string) (*KeycloakUserInfo, error) {
	cfg := getKeycloakConfig()

	// 用code换取token
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", cfg.Keycloak.BaseURL, cfg.Keycloak.Realm)

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", cfg.Keycloak.ClientID)
	data.Set("client_secret", cfg.Keycloak.ClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", cfg.Keycloak.RedirectURI)

	log.Printf("[SSO] 正在请求 Token 交换")

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		log.Printf("[SSO] Token request error: %v", err)
		return nil, fmt.Errorf("请求token失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[SSO] Token 响应状态: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		log.Printf("[SSO] Token 请求失败: status=%d, body_size=%d", resp.StatusCode, len(body))
		return nil, fmt.Errorf("token请求失败(status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp KeycloakTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("解析token响应失败: %w", err)
	}

	// 直接从 JWT access_token 解析用户信息
	userInfo, err := parseJWTClaims(tokenResp.AccessToken)
	if err != nil {
		log.Printf("[SSO] Failed to parse JWT claims: %v", err)
		return nil, fmt.Errorf("解析token失败: %w", err)
	}

	log.Printf("[SSO] User info from token: sub=%s, username=%s, email=%s", userInfo.Sub, userInfo.PreferredUsername, userInfo.Email)

	return userInfo, nil
}

// parseJWTClaims 解析 JWT token 获取用户信息
func parseJWTClaims(accessToken string) (*KeycloakUserInfo, error) {
	// JWT 格式: header.payload.signature
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT token format")
	}

	// 解码 payload (第二部分)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	// 定义 JWT claims 结构
	var claims struct {
		Sub               string `json:"sub"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		Nickname          string `json:"nickname"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	return &KeycloakUserInfo{
		Sub:               claims.Sub,
		PreferredUsername: claims.PreferredUsername,
		Email:             claims.Email,
		Name:              claims.Name,
	}, nil
}

// handleLoginCallback 处理登录回调
func handleLoginCallback(c *gin.Context, kcUserInfo *KeycloakUserInfo) {
	// 查找绑定该Keycloak账号的用户
	user := FindUserBySSOID(SSOProviderKeycloak, kcUserInfo.Sub)

	if user == nil {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "该SSO账号未绑定任何用户，请先使用账号密码登录后在个人中心绑定",
		})
		return
	}

	// 检查用户状态
	if user.Status != 1 {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "用户已被禁用",
		})
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

	issued, err := issueAuthTokens(c, user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "生成令牌失败",
		})
		return
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 通过SSO登录成功，IP: %s", user.Name, user.CallSign, c.ClientIP()),
		"sso_login",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	// 返回登录成功，前端需要处理重定向
	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "登录成功",
		Data: gin.H{
			"token":              issued.AccessToken,
			"refresh_token":      issued.RefreshToken,
			"expires_in":         issued.AccessExpiresIn,
			"refresh_expires_in": issued.RefreshExpiresIn,
			"user":               buildUserData(user),
		},
	})
}

// handleBindCallback 处理绑定回调
func handleBindCallback(c *gin.Context, userID int, kcUserInfo *KeycloakUserInfo) {
	repo := gormdb.NewUserRepository()

	// 检查该Keycloak账号是否已被其他用户绑定
	existingUser := FindUserBySSOID(SSOProviderKeycloak, kcUserInfo.Sub)
	if existingUser != nil && existingUser.ID != userID {
		c.JSON(http.StatusConflict, Response{
			Code:    409,
			Message: "该SSO账号已被其他用户绑定",
		})
		return
	}

	// 获取当前用户
	user, err := repo.GetUserByID(userID)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "用户不存在",
		})
		return
	}

	// 添加绑定
	newOpenID := AddSSOBinding(user.OpenID, SSOProviderKeycloak, kcUserInfo.Sub)
	user.OpenID = newOpenID

	if err := repo.UpdateUser(user); err != nil {
		log.Printf("更新用户OpenID失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "绑定失败",
		})
		return
	}

	// 使缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 绑定Keycloak账号成功", user.Name, user.CallSign),
		"sso_bind",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "绑定成功",
		Data: gin.H{
			"bound": true,
		},
	})
}

// handleLoginCallbackRedirect 处理登录回调并重定向到前端
func handleLoginCallbackRedirect(c *gin.Context, kcUserInfo *KeycloakUserInfo, frontendURL string) string {
	// 查找绑定该Keycloak账号的用户
	user := FindUserBySSOID(SSOProviderKeycloak, kcUserInfo.Sub)

	if user == nil {
		// 重定向到登录页，带上错误信息
		return fmt.Sprintf("%s/login?sso_error=%s", frontendURL, url.QueryEscape("该SSO账号未绑定任何用户，请先使用账号密码登录后在个人中心绑定"))
	}

	// 检查用户状态
	if user.Status != 1 {
		return fmt.Sprintf("%s/login?sso_error=%s", frontendURL, url.QueryEscape("用户已被禁用"))
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

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 通过SSO登录成功，IP: %s", user.Name, user.CallSign, c.ClientIP()),
		"sso_login",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	// 重定向到前端，携带一次性交换码（避免 URL 透传 token）
	userData := buildUserData(user)
	code, err := saveLoginCode(user.ID, userData)
	if err != nil {
		log.Printf("保存 SSO 登录交换码失败: %v", err)
		return fmt.Sprintf("%s/login?sso_error=%s", frontendURL, url.QueryEscape("登录会话创建失败"))
	}
	return fmt.Sprintf("%s/sso/callback?code=%s", frontendURL, url.QueryEscape(code))
}

// handleBindCallbackRedirect 处理绑定回调并重定向到前端
func handleBindCallbackRedirect(c *gin.Context, userID int, kcUserInfo *KeycloakUserInfo, frontendURL string) string {
	repo := gormdb.NewUserRepository()

	// 检查该Keycloak账号是否已被其他用户绑定
	existingUser := FindUserBySSOID(SSOProviderKeycloak, kcUserInfo.Sub)
	if existingUser != nil && existingUser.ID != userID {
		return fmt.Sprintf("%s/profile?sso_error=%s", frontendURL, url.QueryEscape("该SSO账号已被其他用户绑定"))
	}

	// 获取当前用户
	user, err := repo.GetUserByID(userID)
	if err != nil || user == nil {
		return fmt.Sprintf("%s/profile?sso_error=%s", frontendURL, url.QueryEscape("用户不存在"))
	}

	// 添加绑定
	newOpenID := AddSSOBinding(user.OpenID, SSOProviderKeycloak, kcUserInfo.Sub)
	user.OpenID = newOpenID

	if err := repo.UpdateUser(user); err != nil {
		log.Printf("更新用户OpenID失败: %v", err)
		return fmt.Sprintf("%s/profile?sso_error=%s", frontendURL, url.QueryEscape("绑定失败"))
	}

	// 使缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 绑定Keycloak账号成功", user.Name, user.CallSign),
		"sso_bind",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	return fmt.Sprintf("%s/profile?sso_success=%s", frontendURL, url.QueryEscape("SSO绑定成功"))
}

// GetSSOStatus 获取当前用户的SSO绑定状态
func GetSSOStatus(c *gin.Context) {
	username, _ := c.Get("username")

	// SSO状态是关键信息，直接从数据库获取，不使用缓存
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username.(string))

	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "用户不存在",
		})
		return
	}

	bound := HasSSOBinding(user.OpenID, SSOProviderKeycloak)
	var keycloakID string
	if bound {
		keycloakID = GetSSOID(user.OpenID, SSOProviderKeycloak)
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "成功",
		Data: gin.H{
			"bound":       bound,
			"keycloak_id": keycloakID,
		},
	})
}

// SSOBind 发起SSO绑定
func SSOBind(c *gin.Context) {
	if !isKeycloakEnabled() {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "SSO未启用",
		})
		return
	}

	// 获取当前用户
	username, _ := c.Get("username")
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "用户不存在",
		})
		return
	}

	cfg := getKeycloakConfig()
	state := generateState()

	// 保存state用于绑定
	saveState(state, "bind", user.ID)

	// 构建授权URL
	authURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth?client_id=%s&redirect_uri=%s&response_type=code&state=%s",
		cfg.Keycloak.BaseURL,
		cfg.Keycloak.Realm,
		cfg.Keycloak.ClientID,
		url.QueryEscape(cfg.Keycloak.RedirectURI),
		state,
	)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "成功",
		Data: gin.H{
			"url": authURL,
		},
	})
}

// SSOUnbind 解除SSO绑定
func SSOUnbind(c *gin.Context) {
	if !isKeycloakEnabled() {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "SSO未启用",
		})
		return
	}

	username, _ := c.Get("username")
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "用户不存在",
		})
		return
	}

	// 检查是否已绑定
	if !HasSSOBinding(user.OpenID, SSOProviderKeycloak) {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "未绑定Keycloak账号",
		})
		return
	}

	// 移除绑定
	newOpenID := RemoveSSOBinding(user.OpenID, SSOProviderKeycloak)

	if err := repo.UpdateUserOpenID(user.ID, newOpenID); err != nil {
		log.Printf("更新用户OpenID失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "解绑失败",
		})
		return
	}

	// 使缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户 %s (%s) 解除Keycloak账号绑定", user.Name, user.CallSign),
		"sso_unbind",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "解绑成功",
	})
}

// buildUserData 构建用户数据响应
func buildUserData(user *gormdb.User) gin.H {
	// 获取用户 Web 端的群组偏好
	userRepo := gormdb.NewUserRepository()
	lastGroupID, _ := userRepo.GetUserLastGroupID(user.ID, protocol.DraARLDevModelBrowser)

	return gin.H{
		"id":              user.ID,
		"username":        user.Name,
		"nickname":        user.NickName,
		"callsign":        user.CallSign,
		"role":            getRoleNameFromUser(user),
		"roles":           user.Roles,
		"status":          user.Status,
		"approval_status": user.ApprovalStatus,
		"avatar":          user.Avatar,
		"phone":           user.Phone,
		"address":         user.Address,
		"introduction":    user.Introduction,
		"sex":             user.Sex,
		"birthday":        user.Birthday,
		"isAdmin":         user.HasRole("admin"),
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
	}
}
