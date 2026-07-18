package handler

import (
	"fmt"
	"net/http"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"

	"github.com/gin-gonic/gin"
)

func canUseGroupAsDeviceDefault(user *gormdb.User, group *gormdb.Group, isVerifiedMember bool) bool {
	return group != nil && group.Status == 1 && canViewGroup(user, group, isVerifiedMember)
}

// GetDeviceDefaultGroup 返回当前用户的新普通设备默认群组。nil 表示新设备
// 只登记为未分组状态，不进入任何实时转发域。
func GetDeviceDefaultGroup(c *gin.Context) {
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}

	userRepo := gormdb.NewUserRepository()
	groupID, err := userRepo.GetUserDefaultDeviceGroupID(currentUser.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "读取默认群组失败"})
		return
	}
	if groupID > 0 {
		group, err := gormdb.NewGroupRepository().GetGroupByID(groupID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "校验默认群组失败"})
			return
		}
		isVerifiedMember := false
		if group != nil && group.Type == groupTypePrivate && !isAdminUser(currentUser) && group.OwerID != currentUser.ID {
			member, err := gormdb.NewGroupMemberRepository().GetVerifiedMemberByGroupAndUser(group.ID, currentUser.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "校验私有群组成员资格失败"})
				return
			}
			isVerifiedMember = member != nil
		}
		if !canUseGroupAsDeviceDefault(currentUser, group, isVerifiedMember) {
			// 群组已删除、禁用，或用户已失去私有群权限时清理悬空偏好。
			if err := userRepo.SetUserDefaultDeviceGroupID(currentUser.ID, 0); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "清理失效默认群组失败"})
				return
			}
			groupID = 0
		}
	}

	var responseGroupID any
	if groupID > 0 {
		responseGroupID = groupID
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "成功",
		"data": gin.H{
			"group_id": responseGroupID,
		},
	})
}

// UpdateDeviceDefaultGroup 设置当前用户的新普通设备默认群组。请求中的 null
// 或 0 都表示清空；此设置只影响之后首次登记的设备，不修改已有设备。
func UpdateDeviceDefaultGroup(c *gin.Context) {
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}

	var req struct {
		GroupID *int `json:"group_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数错误"})
		return
	}

	groupID := 0
	if req.GroupID != nil {
		groupID = *req.GroupID
	}
	if groupID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的群组ID"})
		return
	}

	if groupID > 0 {
		group, err := gormdb.NewGroupRepository().GetGroupByID(groupID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "校验默认群组失败"})
			return
		}
		if group == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "群组不存在"})
			return
		}
		isVerifiedMember := false
		if group.Type == groupTypePrivate && !isAdminUser(currentUser) && group.OwerID != currentUser.ID {
			member, err := gormdb.NewGroupMemberRepository().GetVerifiedMemberByGroupAndUser(group.ID, currentUser.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "校验私有群组成员资格失败"})
				return
			}
			isVerifiedMember = member != nil
		}
		if !canUseGroupAsDeviceDefault(currentUser, group, isVerifiedMember) {
			c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权将该群组设为设备默认群组"})
			return
		}
	}

	if err := gormdb.NewUserRepository().SetUserDefaultDeviceGroupID(currentUser.ID, groupID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "保存默认群组失败"})
		return
	}

	oplog.AddLog(
		fmt.Sprintf("设置新设备默认群组: group_id=%d", groupID),
		"device_default_group_update",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	var responseGroupID any
	if groupID > 0 {
		responseGroupID = groupID
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "保存成功",
		"data": gin.H{
			"group_id": responseGroupID,
		},
	})
}
