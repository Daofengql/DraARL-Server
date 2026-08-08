package handler

import (
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/models"
	"draarl/internal/routesync"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"strconv"
)

func CreateGroup(c *gin.Context) {
	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 1. 获取当前登录用户 (从上下文中提取)
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

	groupType := req.Type
	if groupType == 0 {
		groupType = groupTypePublic
	}
	if !isSupportedGroupType(groupType) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "群组类型仅支持公开(1)或私有(2)",
		})
		return
	}

	repo := gormdb.NewGroupRepository()
	group := &gormdb.Group{
		Name:     req.Name,
		Type:     groupType,
		Password: req.Password,
		OwerID:   currentUser.ID,
		Note:     req.Note,
		Status:   1,
	}

	// 群组与群主的已验证成员资格必须原子创建，避免只留下半个群组。
	if err := repo.CreateGroupWithOwnerMember(group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建群组失败",
		})
		return
	}

	// 使群组列表缓存失效（新创建群组后列表应更新）
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		_ = groupCache.InvalidateGroupList(c.Request.Context())
	}
	udphub.RefreshGroupCache()

	// 记录审计日志
	groupTypeStr := "公开群组"
	if groupType == groupTypePrivate {
		groupTypeStr = "私有群组"
	}
	oplog.AddLog(
		fmt.Sprintf("创建群组: %s (类型: %s, ID: %d)", req.Name, groupTypeStr, group.ID),
		"group_create",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "创建成功",
		"data": gin.H{
			"id": group.ID,
		},
	})
}

// UpdateGroupRequest 更新群组请求

func UpdateGroup(c *gin.Context) {
	var req UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	idStr := c.Param("id")
	if idStr == "" {
		idStr = c.Query("id")
	}
	id := req.ID
	if idStr != "" {
		parsedID, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的群组ID"})
			return
		}
		id = parsedID
	}
	if id <= 0 || (req.ID > 0 && idStr != "" && req.ID != id) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效或不一致的群组ID"})
		return
	}

	repo := gormdb.NewGroupRepository()

	// 先获取现有群组
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
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
	if group.IsVirtual || !isSupportedGroupType(group.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请使用互联管理接口修改虚拟群组"})
		return
	}

	// 更新字段
	if req.Name != "" {
		group.Name = req.Name
	}
	if req.Type != 0 {
		if !isSupportedGroupType(req.Type) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "群组类型仅支持公开(1)或私有(2)",
			})
			return
		}
		group.Type = req.Type
	}
	if req.Password != "" {
		group.Password = req.Password
	}
	if req.Note != nil {
		group.Note = *req.Note
	}
	if req.Status != nil {
		group.Status = *req.Status
	}

	if err := repo.UpdateGroup(group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新群组失败",
		})
		return
	}

	// 使群组详情缓存和列表缓存统统主动失效
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		// 失效群组详情
		_ = groupCache.InvalidateGroup(c.Request.Context(), id)
		// 主动使群组的公开列表和所有分页列表失效
		_ = groupCache.InvalidateGroupList(c.Request.Context())
	}
	udphub.RefreshGroupCache()
	routesync.RefreshTopology()
	reconcileGhostSessionsForGroup(id)

	// Get owner callsign from user table
	var ownerCallSign string
	if group.OwerID > 0 {
		userRepo := gormdb.NewUserRepository()
		if owner, err := userRepo.GetUserByID(group.OwerID); err == nil && owner != nil {
			ownerCallSign = owner.CallSign
		}
	}

	// 记录审计日志 - 获取当前用户
	oplog.AddLog(
		fmt.Sprintf("更新群组: %s (ID: %d)", group.Name, group.ID),
		"group_update",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id":            group.ID,
			"name":          group.Name,
			"type":          group.Type,
			"ower_id":       group.OwerID,
			"ower_callsign": ownerCallSign,
			"master_server": group.MasterServer,
			"slave_server":  group.SlaveServer,
			"status":        group.Status,
			"note":          group.Note,
			"create_time":   group.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time":   group.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// DeleteGroup 删除群组

func DeleteGroup(c *gin.Context) {
	idStr := c.Param("id")
	if idStr == "" {
		idStr = c.Query("id")
	}
	if idStr == "" {
		var req struct {
			ID int `json:"id"`
		}
		if err := c.ShouldBindJSON(&req); err == nil && req.ID > 0 {
			idStr = strconv.Itoa(req.ID)
		}
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	repo := gormdb.NewGroupRepository()

	// 先获取群组信息用于审计日志
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "群组不存在"})
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
	if group.IsVirtual || !isSupportedGroupType(group.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请使用互联管理接口删除虚拟群组"})
		return
	}
	deviceRepo := gormdb.NewDeviceRepository()
	movedDevices, err := deviceRepo.ListDevicesByGroupID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "查询群组设备失败"})
		return
	}

	if err := repo.DeleteGroupWithCascade(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除群组失败",
		})
		return
	}
	for _, device := range movedDevices {
		if err := udphub.ChangeDeviceGroupByID(device.ID, models.GroupIDPublicMin); err != nil {
			log.Printf("[WARN] Failed to update deleted-group device in memory: %v", err)
		}
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("删除群组: %s (ID: %d)", group.Name, id),
		"group_delete",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	// 使群组详情缓存和列表缓存统统失效
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		_ = groupCache.InvalidateGroup(c.Request.Context(), id)
		// 彻底清空相关的群组列表
		_ = groupCache.InvalidateGroupList(c.Request.Context())
	}
	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		for _, device := range movedDevices {
			_ = deviceCache.InvalidateDevice(c.Request.Context(), device.ID, device.OwnerID, uint8(device.SSID))
		}
		_ = deviceCache.InvalidateDevicesByGroup(c.Request.Context(), id)
		_ = deviceCache.InvalidateDevicesByGroup(c.Request.Context(), models.GroupIDPublicMin)
		_ = deviceCache.InvalidateDeviceList(c.Request.Context())
	}
	udphub.RefreshGroupCache()
	udphub.RefreshGroupLinkCache()
	reconcileGhostSessionsForGroup(id)
	for _, device := range movedDevices {
		routesync.PublishDevice(device.ID)
	}
	routesync.RefreshTopology()

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// GetGroupDevices 获取群组设备列表
