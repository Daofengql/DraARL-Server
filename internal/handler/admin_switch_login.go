package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"draarl/internal/gormdb"
	oplog "draarl/internal/log"

	"github.com/gin-gonic/gin"
)

// AdminSwitchLogin 由管理员直接切换为目标普通用户登录。
// 签发的令牌只包含目标用户本人的身份和权限，不保留管理员身份或返回令牌。
func AdminSwitchLogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Pragma", "no-cache")

	actor, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	// 即使路由已经挂载 RequireAdmin，也在处理器内再次校验，避免以后调整路由时放宽权限。
	if !isAdminUser(actor) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    http.StatusForbidden,
			"message": "仅管理员可以切换登录用户",
		})
		return
	}

	targetID, err := strconv.Atoi(c.Param("id"))
	if err != nil || targetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "无效的用户ID",
		})
		return
	}

	target, err := gormdb.NewUserRepository().GetUserByID(targetID)
	if err != nil || target == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    http.StatusNotFound,
			"message": "目标用户不存在",
		})
		return
	}
	if target.ID == actor.ID {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "当前已登录该账号",
		})
		return
	}
	if target.ID == 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    http.StatusForbidden,
			"message": "不允许切换登录到主管理员账号",
		})
		return
	}
	if target.Status != 1 {
		c.JSON(http.StatusConflict, gin.H{
			"code":    http.StatusConflict,
			"message": "目标用户已被禁用",
		})
		return
	}
	if !canAdminSwitchLogin(actor, target) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    http.StatusForbidden,
			"message": "不允许切换到该用户",
		})
		return
	}

	issued, err := issueAuthTokens(c, target)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"message": "签发目标用户登录态失败",
		})
		return
	}

	// issueAuthTokens 写入的是响应 Cookie；此处读取的仍是请求携带的原管理员
	// refresh token，因此只吊销当前管理员这一条会话，不影响管理员的其他设备。
	revokeCurrentRefreshToken(c, "admin_switch_login")

	oplog.AddLog(
		fmt.Sprintf(
			"管理员切换登录用户: admin_id=%d, admin_name=%s, target_id=%d, target_name=%s, target_callsign=%s, IP: %s",
			actor.ID, actor.Name, target.ID, target.Name, target.CallSign, c.ClientIP(),
		),
		"admin_switch_login",
		actor.ID,
		actor.Name,
		actor.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "已切换为目标用户登录",
		"data": gin.H{
			"token":              issued.AccessToken,
			"refresh_token":      issued.RefreshToken,
			"expires_in":         issued.AccessExpiresIn,
			"refresh_expires_in": issued.RefreshExpiresIn,
			"user":               buildLoginUserData(target),
		},
	})
}
