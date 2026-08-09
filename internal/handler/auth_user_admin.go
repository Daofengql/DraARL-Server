package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	broadcastrepository "draarl/internal/broadcast/repository"
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/models"
	"draarl/internal/routesync"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	"draarl/pkg/minio"

	"github.com/gin-gonic/gin"
)

func GetUsers(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	keyword := c.Query("keyword")

	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
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
	if req.Status == 0 {
		reconcileOwnerGhostSessions(id)
		routesync.RevokeOwner(id, "user_disabled")
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
	ownedGroups, err := repo.ListOwnedGroups(id)
	if err != nil {
		log.Printf("删除用户前读取群组失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取用户群组失败"})
		return
	}

	// Virtual groups must go through the same state transition as the normal
	// delete path. This restores surviving members' future schedules and waits
	// for every active domain broadcast before the topology is removed.
	for _, group := range ownedGroups {
		if !group.IsVirtual || group.Status != 1 {
			continue
		}
		mutation, transitionErr := broadcastrepository.Default().SetVirtualGroupStatus(c.Request.Context(), group.ID, 0, time.Now().UTC())
		if transitionErr != nil {
			log.Printf("删除用户前关闭虚拟群组 %d 失败: %v", group.ID, transitionErr)
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "停止虚拟群组自动播报失败"})
			return
		}
		if !waitForBroadcastTopologyMutation(c, mutation) {
			return
		}
		group.Status = 0
	}

	entityGroupIDs := make([]int, 0, len(ownedGroups))
	for _, group := range ownedGroups {
		if group.IsVirtual {
			continue
		}
		if group.Status == 1 {
			if updateErr := gormdb.NewGroupRepository().UpdateGroupFields(group.ID, map[string]interface{}{"status": 0}); updateErr != nil {
				log.Printf("删除用户前停用实体群组 %d 失败: %v", group.ID, updateErr)
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "停止实体群组自动播报失败"})
				return
			}
			group.Status = 0
		}
		// Removing an entity group must repair any surviving virtual group's
		// selected-source policy before the group foreign key disappears.
		if _, ok := prepareEntityGroupBroadcastDeletion(c, currentUserModel.ID, group.ID); !ok {
			return
		}
		entityGroupIDs = append(entityGroupIDs, group.ID)
	}
	if len(entityGroupIDs) != 0 {
		udphub.RefreshGroupCache()
		routesync.RefreshTopology()
		if !cancelBroadcastGroupsAfterMutation(c, entityGroupIDs, "group_unavailable") {
			return
		}
	}

	cascadeResult, err := repo.DeleteUserWithCascade(id)
	if err != nil {
		log.Printf("删除用户失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除用户失败",
		})
		return
	}
	reconcileOwnerGhostSessions(id)
	routesync.RevokeOwner(id, "user_deleted")
	for _, device := range cascadeResult.DeletedDevices {
		udphub.RemoveRuntimeDevice(device.OwnerID, device.SSID)
		routesync.RevokeDevice(device.ID, "user_deleted")
	}
	for _, device := range cascadeResult.MovedDevices {
		if err := udphub.ChangeDeviceGroupByID(device.ID, models.GroupIDPublicMin); err != nil {
			log.Printf("[WARN] 删除群主后同步设备 %d 默认群组失败: %v", device.ID, err)
		}
	}

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), targetUser.ID, targetUser.Name)
	}
	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		for _, device := range cascadeResult.DeletedDevices {
			_ = deviceCache.InvalidateDevice(c.Request.Context(), device.ID, device.OwnerID, uint8(device.SSID))
			if device.GroupID > 0 {
				_ = deviceCache.InvalidateDevicesByGroup(c.Request.Context(), device.GroupID)
			}
		}
		for _, device := range cascadeResult.MovedDevices {
			_ = deviceCache.InvalidateDevice(c.Request.Context(), device.ID, device.OwnerID, uint8(device.SSID))
		}
		_ = deviceCache.InvalidateDevicesByGroup(c.Request.Context(), models.GroupIDPublicMin)
		_ = deviceCache.InvalidateDeviceList(c.Request.Context())
	}
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		for _, groupID := range cascadeResult.OwnedGroupIDs {
			_ = groupCache.InvalidateGroup(c.Request.Context(), groupID)
		}
		_ = groupCache.InvalidateGroupList(c.Request.Context())
	}
	udphub.RefreshGroupCache()
	udphub.RefreshGroupLinkCache()
	for _, device := range cascadeResult.MovedDevices {
		routesync.PublishDevice(device.ID)
	}
	routesync.RefreshTopology()
	cleanupPending := cleanupDeletedBroadcastObjects(c, cascadeResult.BroadcastObjectKeys)

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
			"id":                        id,
			"broadcast_cleanup_pending": cleanupPending,
		},
	})
}

// UpdateUserStatusRequest 更新用户状态请求
