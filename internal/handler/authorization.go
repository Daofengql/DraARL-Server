package handler

import (
	"net/http"

	gormdb "draarl/internal/gormdb"
	"draarl/internal/groupaccess"

	"github.com/gin-gonic/gin"
)

// currentUserFromContext returns the database-backed user loaded by AuthMiddleware.
func currentUserFromContext(c *gin.Context) (*gormdb.User, bool) {
	value, exists := c.Get("user")
	if !exists {
		return nil, false
	}
	user, ok := value.(*gormdb.User)
	return user, ok && user != nil
}

func isAdminUser(user *gormdb.User) bool {
	return user != nil && user.HasRole("admin")
}

func canManageDevice(user *gormdb.User, device *gormdb.Device) bool {
	return user != nil && device != nil && (isAdminUser(user) || device.OwnerID == user.ID)
}

func canManageGroup(user *gormdb.User, group *gormdb.Group) bool {
	return user != nil && group != nil && (isAdminUser(user) || group.OwerID == user.ID)
}

func canManageGroupDeviceCommControl(user *gormdb.User, group *gormdb.Group, device *gormdb.Device) bool {
	return canManageGroup(user, group) && device != nil && !group.IsVirtual &&
		isSupportedGroupType(group.Type) && device.GroupID == group.ID
}

// canAdminSwitchLogin 只允许管理员切换到启用的其他账号。
// 主管理员（ID=1）具有额外身份特权，不能作为切换目标。
func canAdminSwitchLogin(actor, target *gormdb.User) bool {
	return isAdminUser(actor) && target != nil && target.ID != actor.ID &&
		target.ID != 1 && target.Status == 1
}

func canViewGroup(user *gormdb.User, group *gormdb.Group, isVerifiedMember bool) bool {
	return groupaccess.CanReceiveGroup(user, group, isVerifiedMember)
}

func requireCurrentUser(c *gin.Context) (*gormdb.User, bool) {
	user, ok := currentUserFromContext(c)
	if ok {
		return user, true
	}
	c.JSON(http.StatusUnauthorized, gin.H{
		"code":    http.StatusUnauthorized,
		"message": "用户不存在",
	})
	return nil, false
}

func requireGroupViewAccess(c *gin.Context, group *gormdb.Group) (*gormdb.User, bool) {
	user, ok := requireCurrentUser(c)
	if !ok {
		return nil, false
	}

	isVerifiedMember := false
	if group != nil && group.Type == groupTypePrivate && !isAdminUser(user) && group.OwerID != user.ID {
		isVerifiedMember = gormdb.NewGroupMemberRepository().IsVerifiedMember(group.ID, user.ID)
	}
	if !canViewGroup(user, group, isVerifiedMember) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    http.StatusForbidden,
			"message": "无权访问该群组",
		})
		return nil, false
	}
	return user, true
}
