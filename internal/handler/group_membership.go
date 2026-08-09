package handler

import (
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/models"
	"draarl/internal/routesync"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"log"
	"net/http"
	"strconv"
	"time"
)

func JoinGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	var req JoinGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 检查群组是否存在
	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}
	if group.IsVirtual || !isSupportedGroupType(group.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "虚拟群组不支持加入"})
		return
	}

	// 检查群组类型（Type=2 才需要密码）
	if group.Type != 2 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该群组不需要密码验证",
		})
		return
	}

	// 验证密码是否正确
	if group.Password != req.Password {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    401,
			"message": "密码错误",
		})
		return
	}

	// 检查群组是否被禁用
	if group.Status != 1 {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "群组已禁用",
		})
		return
	}

	// 获取当前用户
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	memberRepo := gormdb.NewGroupMemberRepository()

	// 检查用户是否已加入
	member, err := memberRepo.GetMemberByGroupAndUser(id, currentUser.ID)
	var isJoined bool
	if err == nil {
		isJoined = member != nil
	} else {
		// 兼容旧数据
		isJoined = memberRepo.IsVerifiedMember(group.ID, currentUser.ID)
	}

	var groupMember gormdb.GroupMember
	if isJoined {
		// 已加入，更新最后验证时间
		err = memberRepo.UpdateMemberVerification(id, currentUser.ID, true)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "更新验证时间失败",
			})
			return
		}
	} else {
		// 未加入，创建成员记录
		groupMember = gormdb.GroupMember{
			GroupID:    id,
			UserID:     currentUser.ID,
			IsVerified: true,
			JoinTime:   time.Now(),
			LastVerify: time.Now(),
		}
		err = memberRepo.CreateMember(&groupMember)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "加入群组失败",
			})
			return
		}
	}

	// 使群组成员缓存和用户群组列表缓存失效
	ctx := c.Request.Context()
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		_ = groupCache.InvalidateGroupMembers(ctx, id)
		_ = groupCache.InvalidateUserGroups(ctx, currentUser.ID)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("加入群组: %s (ID: %d)", group.Name, id),
		"group_join",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "加入成功",
		"data": gin.H{
			"group_id":    id,
			"is_verified": true,
			"join_time":   time.Now().Format("2006-01-02 15:04:05"),
		},
	})
}

// GetGroupMembers 获取群组成员列表

func GetGroupMembers(c *gin.Context) {
	groupIDStr := c.Param("id")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(groupID)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}
	if group.IsVirtual || !isSupportedGroupType(group.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "虚拟群组不支持成员列表"})
		return
	}

	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}

	if !canManageGroup(currentUser, group) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    http.StatusForbidden,
			"message": "需要管理员或群组创建者权限",
		})
		return
	}

	// 查询成员列表
	memberRepo := gormdb.NewGroupMemberRepository()
	members, err := memberRepo.ListVerifiedMembersByGroup(groupID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询成员列表失败",
		})
		return
	}
	userIDs := make([]int, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID)
	}
	usersByID := make(map[int]gormdb.User, len(userIDs))
	deviceCounts := make(map[int]int64, len(userIDs))
	if len(userIDs) > 0 {
		var users []gormdb.User
		if err := gormdb.Get().Select("id", "name", "callsign", "nickname").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "查询成员用户失败"})
			return
		}
		for _, user := range users {
			usersByID[user.ID] = user
		}
		var counts []struct {
			OwnerID int
			Count   int64
		}
		if err := gormdb.Get().Model(&gormdb.Device{}).
			Select("owner_id, COUNT(*) AS count").
			Where("group_id = ? AND owner_id IN ?", groupID, userIDs).
			Group("owner_id").Scan(&counts).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "查询成员设备失败"})
			return
		}
		for _, count := range counts {
			deviceCounts[count.OwnerID] = count.Count
		}
	}
	items := make([]GroupMemberInfo, 0, len(members))
	for _, member := range members {
		user := usersByID[member.UserID]
		items = append(items, GroupMemberInfo{
			ID: member.ID, GroupID: member.GroupID, UserID: member.UserID,
			Username: user.Name, CallSign: user.CallSign, Nickname: user.NickName,
			IsVerified: member.IsVerified, JoinTime: member.JoinTime, LastVerify: member.LastVerify,
			DeviceCount: deviceCounts[member.UserID],
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": items,
			"total": len(items),
		},
	})
}

// RemoveGroupMember removes one private-group membership and immediately
// reconciles every physical and ghost route owned by that user.

func RemoveGroupMember(c *gin.Context) {
	groupID, err := strconv.Atoi(c.Param("id"))
	if err != nil || groupID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的群组ID"})
		return
	}
	targetUserID, err := strconv.Atoi(c.Param("userId"))
	if err != nil || targetUserID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的用户ID"})
		return
	}

	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(groupID)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "群组不存在"})
		return
	}
	if group.IsVirtual || group.Type != groupTypePrivate {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "仅私有群组支持移除成员"})
		return
	}
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	if !canManageGroup(currentUser, group) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "需要管理员或群组创建者权限"})
		return
	}
	if targetUserID == group.OwerID {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "不能移除群组创建者"})
		return
	}
	targetUser, err := gormdb.NewUserRepository().GetUserByID(targetUserID)
	if err != nil || targetUser == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "用户不存在"})
		return
	}
	movedDevices, err := repo.RemoveGroupMemberAndMoveDevices(groupID, targetUserID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "群组成员不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "移除成员失败"})
		return
	}
	syncRemovedGroupMembership(c, groupID, targetUserID, movedDevices)

	oplog.AddLog(
		fmt.Sprintf("移除群组成员: %s (ID: %d) 从 %s (ID: %d) 移除", targetUser.Name, targetUser.ID, group.Name, group.ID),
		"group_member_remove", currentUser.ID, currentUser.Name, currentUser.CallSign, c.ClientIP(),
	)
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK, "message": "移除成功",
		"data": gin.H{"group_id": groupID, "user_id": targetUserID, "moved_device_count": len(movedDevices)},
	})
}

// KickDevice 踢出设备

func LeaveGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	// 检查群组是否存在
	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	// 检查是否是私有群组
	if group.Type != 2 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该群组不支持离开",
		})
		return
	}

	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}

	// 检查是否是群组创建者，创建者不能离开自己的群组
	if group.OwerID == currentUser.ID {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "群组创建者不能退出自己的群组",
		})
		return
	}

	// 成员资格删除与本人设备迁移必须处于同一事务。
	movedDevices, err := repo.RemoveGroupMemberAndMoveDevices(id, currentUser.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    http.StatusNotFound,
				"message": "尚未加入该群组",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "离开群组失败",
		})
		return
	}

	syncRemovedGroupMembership(c, id, currentUser.ID, movedDevices)

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("离开群组: %s (ID: %d)", group.Name, id),
		"group_leave",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "离开成功",
	})
}

func syncRemovedGroupMembership(c *gin.Context, groupID, userID int, movedDevices []*gormdb.Device) {
	for _, device := range movedDevices {
		if err := udphub.ChangeDeviceGroupByID(device.ID, models.GroupIDPublicMin); err != nil {
			log.Printf("[WARN] Failed to update removed member device group in memory: %v", err)
		}
		routesync.PublishDevice(device.ID)
	}
	reconcileOwnerGhostSessions(userID)

	ctx := c.Request.Context()
	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		for _, device := range movedDevices {
			_ = deviceCache.InvalidateDevice(ctx, device.ID, device.OwnerID, uint8(device.SSID))
		}
		_ = deviceCache.InvalidateDevicesByGroup(ctx, groupID)
		if len(movedDevices) > 0 {
			_ = deviceCache.InvalidateDevicesByGroup(ctx, models.GroupIDPublicMin)
		}
		_ = deviceCache.InvalidateDeviceList(ctx)
	}
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		_ = groupCache.InvalidateGroupMembers(ctx, groupID)
		_ = groupCache.InvalidateUserGroups(ctx, userID)
	}
}
