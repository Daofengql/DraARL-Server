package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/models"
	"draarl/internal/routesync"
	"draarl/internal/udphub"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	groupTypePublic  = 1
	groupTypePrivate = 2
)

func isSupportedGroupType(groupType int) bool {
	return groupType == groupTypePublic || groupType == groupTypePrivate
}

// GroupInfo 群组信息响应
type GroupInfo struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Type         int    `json:"type"`
	OwerID       int    `json:"ower_id"`
	MasterServer int    `json:"master_server"`
	SlaveServer  int    `json:"slave_server"`
	Status       int    `json:"status"`
	CreateTime   string `json:"create_time,omitempty"`
	UpdateTime   string `json:"update_time,omitempty"`
	Note         string `json:"note"`
}

// GetGroups 获取当前用户可见的群组列表（公开群组 + 已加入私有群组）。
func GetGroups(c *gin.Context) {
	getGroups(c, false)
}

// GetAdminGroups 获取管理员可管理的全部非虚拟群组。
func GetAdminGroups(c *gin.Context) {
	getGroups(c, true)
}

func getGroups(c *gin.Context, adminView bool) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	if adminView && !isAdminUser(currentUser) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    http.StatusForbidden,
			"message": "需要管理员权限",
		})
		return
	}

	uid := currentUser.ID
	likeKeyword := "%" + keyword + "%"
	offset := (page - 1) * pageSize

	countQuery := gormdb.Get().Table("public_groups g").
		Joins("LEFT JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = ? AND gm.is_verified = ?", uid, true).
		Where("(g.is_virtual = ? OR g.is_virtual IS NULL)", false).
		Where("g.type IN ?", []int{groupTypePublic, groupTypePrivate})
	if !adminView {
		countQuery = countQuery.Where("(g.type = ? OR g.ower_id = ? OR gm.user_id IS NOT NULL)", groupTypePublic, uid)
	}
	if keyword != "" {
		countQuery = countQuery.Where("CAST(g.id AS CHAR) LIKE ? OR g.name LIKE ?", likeKeyword, likeKeyword)
	}

	var total int64
	if err := countQuery.Distinct("g.id").Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询群组总数失败",
		})
		return
	}

	type groupListRow struct {
		ID            int       `gorm:"column:id"`
		Name          string    `gorm:"column:name"`
		Type          int       `gorm:"column:type"`
		OwerID        int       `gorm:"column:ower_id"`
		OwnerCallSign string    `gorm:"column:owner_callsign"`
		MasterServer  int       `gorm:"column:master_server"`
		SlaveServer   int       `gorm:"column:slave_server"`
		Status        int       `gorm:"column:status"`
		Note          string    `gorm:"column:note"`
		CreateTime    time.Time `gorm:"column:create_time"`
		UpdateTime    time.Time `gorm:"column:update_time"`
		OnlineCount   int       `gorm:"column:online_count"`
		TotalCount    int       `gorm:"column:total_count"`
		IsJoined      bool      `gorm:"column:is_joined"`
	}

	rows := make([]groupListRow, 0, pageSize)
	dataQuery := gormdb.Get().Table("public_groups g").
		Select(`
			g.id, g.name, g.type, g.ower_id, g.master_server, g.slave_server, g.status, g.note, g.create_time, g.update_time,
			COALESCE(u.callsign, '') AS owner_callsign,
			COALESCE(stats.online_count, 0) AS online_count,
			COALESCE(stats.total_count, 0) AS total_count,
			CASE
				WHEN g.type = 1 THEN true
				WHEN g.ower_id = ? THEN true
				WHEN gm.user_id IS NOT NULL THEN true
				ELSE false
			END AS is_joined
		`, uid).
		Joins("LEFT JOIN users u ON u.id = g.ower_id").
		Joins("LEFT JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = ? AND gm.is_verified = ?", uid, true).
		Joins(`
			LEFT JOIN (
				SELECT group_id,
					SUM(CASE WHEN is_online = 1 THEN 1 ELSE 0 END) AS online_count,
					COUNT(1) AS total_count
				FROM devices
				GROUP BY group_id
			) stats ON stats.group_id = g.id
		`).
		Where("(g.is_virtual = ? OR g.is_virtual IS NULL)", false).
		Where("g.type IN ?", []int{groupTypePublic, groupTypePrivate})
	if !adminView {
		dataQuery = dataQuery.Where("(g.type = ? OR g.ower_id = ? OR gm.user_id IS NOT NULL)", groupTypePublic, uid)
	}
	if keyword != "" {
		dataQuery = dataQuery.Where("CAST(g.id AS CHAR) LIKE ? OR g.name LIKE ?", likeKeyword, likeKeyword)
	}
	if err := dataQuery.
		Distinct().
		Order("g.id DESC").
		Offset(offset).
		Limit(pageSize).
		Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询群组列表失败",
		})
		return
	}

	uniqueRows := make([]groupListRow, 0, len(rows))
	seenGroupIDs := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		if _, exists := seenGroupIDs[row.ID]; exists {
			continue
		}
		seenGroupIDs[row.ID] = struct{}{}
		uniqueRows = append(uniqueRows, row)
	}

	resultItems := make([]gin.H, 0, len(uniqueRows))
	for _, row := range uniqueRows {
		resultItems = append(resultItems, gin.H{
			"id":            row.ID,
			"name":          row.Name,
			"type":          row.Type,
			"ower_id":       row.OwerID,
			"ower_callsign": row.OwnerCallSign,
			"master_server": row.MasterServer,
			"slave_server":  row.SlaveServer,
			"status":        row.Status,
			"note":          row.Note,
			"is_joined":     row.IsJoined,
			"is_owner":      row.OwerID == uid,
			"online_count":  row.OnlineCount,
			"total_count":   row.TotalCount,
			"create_time":   row.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time":   row.UpdateTime.Format("2006-01-02 15:04:05"),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items":     resultItems,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// GetGroup 获取单个群组
func GetGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	ctx := c.Request.Context()
	groupCache := cache.GetGroupCache()

	var group *gormdb.Group
	if groupCache != nil {
		group, err = groupCache.GetGroupByID(ctx, id)
	} else {
		repo := gormdb.NewGroupRepository()
		group, err = repo.GetGroupByID(id)
	}
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}
	currentUser, ok := requireGroupViewAccess(c, group)
	if !ok {
		return
	}

	isOwner := group.OwerID == currentUser.ID

	// Get owner callsign from user table
	var ownerCallSign string
	if group.OwerID > 0 {
		userRepo := gormdb.NewUserRepository()
		if owner, err := userRepo.GetUserByID(group.OwerID); err == nil && owner != nil {
			ownerCallSign = owner.CallSign
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
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
			"is_owner":      isOwner,
			"create_time":   group.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time":   group.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// CreateGroupRequest 创建群组请求
type CreateGroupRequest struct {
	Name     string `json:"name" binding:"required"`
	Type     int    `json:"type"`
	Password string `json:"password"`
	Note     string `json:"note"`
	Status   int    `json:"status"`
}

// CreateGroup 创建群组
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
type UpdateGroupRequest struct {
	ID       int     `json:"id"` // 兼容 POST /group/update
	Name     string  `json:"name"`
	Type     int     `json:"type"`
	Password string  `json:"password"`
	Note     *string `json:"note"`
	Status   *int    `json:"status"`
}

// UpdateGroup 更新群组
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
func GetGroupDevices(c *gin.Context) {
	groupIDStr := c.Param("id")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}
	groupRepo := gormdb.NewGroupRepository()
	group, err := groupRepo.GetGroupByID(groupID)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    http.StatusNotFound,
			"message": "群组不存在",
		})
		return
	}
	if group.IsVirtual || !isSupportedGroupType(group.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "虚拟群组不支持设备列表"})
		return
	}
	if _, ok := requireGroupViewAccess(c, group); !ok {
		return
	}

	ctx := c.Request.Context()
	deviceCache := cache.GetDeviceCache()

	var devicesRaw []*gormdb.Device
	if deviceCache != nil {
		devicesRaw, err = deviceCache.GetDevicesByGroupID(ctx, groupID)
	} else {
		repo := gormdb.NewDeviceRepository()
		devicesRaw, err = repo.ListDevicesByGroupID(groupID)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询设备列表失败",
		})
		return
	}

	// 批量获取所有者呼号（解决 N+1 查询问题）
	userRepo := gormdb.NewUserRepository()
	ownerIDs := make([]int, 0, len(devicesRaw))
	for _, d := range devicesRaw {
		if d.OwnerID > 0 {
			ownerIDs = append(ownerIDs, d.OwnerID)
		}
	}
	// 去重
	ownerIDSet := make(map[int]bool)
	uniqueOwnerIDs := make([]int, 0, len(ownerIDs))
	for _, id := range ownerIDs {
		if !ownerIDSet[id] {
			ownerIDSet[id] = true
			uniqueOwnerIDs = append(uniqueOwnerIDs, id)
		}
	}
	ownerCallSigns, _ := userRepo.GetUserBriefByIDs(uniqueOwnerIDs)

	// 转换为响应格式（收发控制只来自 devices 表）
	devices := make([]gin.H, 0, len(devicesRaw))
	for _, d := range devicesRaw {
		// 获取所有者呼号
		var callsign string
		if brief, ok := ownerCallSigns[d.OwnerID]; ok {
			callsign = brief.CallSign
		}

		devices = append(devices, gin.H{
			"id":             d.ID,
			"name":           d.Name,
			"callsign":       callsign,
			"owner_callsign": callsign,
			"ssid":           d.SSID,
			"dev_model":      d.DevModel,
			"group_id":       d.GroupID,
			"status":         d.Status,
			"priority":       d.Priority,
			"is_online":      d.ISOnline,
			"disable_send":   d.DisableSend,
			"disable_recv":   d.DisableRecv,
			"create_time":    d.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time":    d.UpdateTime.Format("2006-01-02 15:04:05"),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total": int64(len(devices)),
			"items": devices,
		},
	})
}

// UpdateGroupDeviceCommControlRequest 仅更新设备级禁发/禁收状态。
type UpdateGroupDeviceCommControlRequest struct {
	DisableSend *bool `json:"disable_send"`
	DisableRecv *bool `json:"disable_recv"`
}

// UpdateGroupDeviceCommControl 允许管理员或当前群主临时控制组内普通设备收发。
func UpdateGroupDeviceCommControl(c *gin.Context) {
	groupID, err := strconv.Atoi(c.Param("id"))
	if err != nil || groupID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的群组ID"})
		return
	}
	deviceID, err := strconv.Atoi(c.Param("deviceId"))
	if err != nil || deviceID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的设备ID"})
		return
	}

	var req UpdateGroupDeviceCommControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数错误"})
		return
	}
	if req.DisableSend == nil && req.DisableRecv == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "至少需要设置一项收发状态"})
		return
	}
	group, err := gormdb.NewGroupRepository().GetGroupByID(groupID)
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
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "虚拟群组不支持设备收发控制"})
		return
	}

	deviceRepo := gormdb.NewDeviceRepository()
	currentDevice, err := deviceRepo.GetDeviceByID(deviceID)
	if err != nil || currentDevice == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "设备不存在"})
		return
	}
	if !canManageGroupDeviceCommControl(currentUser, group, currentDevice) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "设备已不在该群组，请刷新设备列表"})
		return
	}
	before, after, err := deviceRepo.UpdateDeviceCommControlInGroup(
		deviceID,
		groupID,
		req.DisableSend,
		req.DisableRecv,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "设备不存在"})
		return
	}
	if errors.Is(err, gormdb.ErrDeviceNotInGroup) {
		c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "设备已不在该群组，请刷新设备列表"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "更新设备收发状态失败"})
		return
	}

	ctx := c.Request.Context()
	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		_ = deviceCache.InvalidateDevice(ctx, after.ID, after.OwnerID, after.SSID)
		_ = deviceCache.InvalidateDevicesByGroup(ctx, groupID)
		_ = deviceCache.InvalidateDeviceList(ctx)
	}
	udphub.SyncDeviceCommControlByID(after.ID, after.DisableSend, after.DisableRecv)
	routesync.PublishDevice(after.ID)

	ownerCallSign := ""
	if owner, ownerErr := gormdb.NewUserRepository().GetUserByID(after.OwnerID); ownerErr == nil && owner != nil {
		ownerCallSign = owner.CallSign
	}
	source := "group_owner"
	if isAdminUser(currentUser) {
		source = "admin"
	}
	oplog.AddLog(
		fmt.Sprintf(
			"群组设备收发控制: source=%s, group_id=%d, group_name=%q, device_id=%d, owner_id=%d, callsign_ssid=%s-%d, disable_send=%t->%t, disable_recv=%t->%t",
			source,
			groupID,
			group.Name,
			after.ID,
			after.OwnerID,
			ownerCallSign,
			after.SSID,
			before.DisableSend,
			after.DisableSend,
			before.DisableRecv,
			after.DisableRecv,
		),
		"group_device_comm_control",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "设备收发状态已更新",
		"data": gin.H{
			"device_id":    after.ID,
			"group_id":     groupID,
			"disable_send": after.DisableSend,
			"disable_recv": after.DisableRecv,
		},
	})
}

// GetRelays 获取中继台列表（管理员接口，支持按地区搜索）
func GetRelays(c *gin.Context) {
	location := c.Query("location")

	repo := gormdb.NewRelayRepository()
	var relays []*gormdb.Relay
	var err error

	if location != "" {
		// 管理员搜索不限制状态
		relays, err = repo.SearchRelaysByLocationAdmin(location)
	} else {
		relays, err = repo.ListRelays()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询中继台列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": relays,
		},
	})
}

// GetServers 获取服务器列表
func GetServers(c *gin.Context) {
	repo := gormdb.NewServerRepository()
	servers, err := repo.ListServers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询服务器列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": servers,
		},
	})
}

// SearchGroupsRequest 搜索群组请求
type SearchGroupsRequest struct {
	Keyword  string `json:"keyword"`
	Query    string `json:"query"` // 兼容旧电台客户端
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// SearchGroups 搜索群组
func SearchGroups(c *gin.Context) {
	var req SearchGroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		keyword = strings.TrimSpace(req.Query)
	}
	if keyword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请输入搜索关键词"})
		return
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}

	repo := gormdb.NewGroupRepository()
	groups, err := repo.SearchGroups(keyword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "搜索群组失败",
		})
		return
	}

	memberRepo := gormdb.NewGroupMemberRepository()
	verifiedGroupIDs := make(map[int]bool)
	if !isAdminUser(currentUser) {
		members, err := memberRepo.ListGroupsByUser(currentUser.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "查询群组成员关系失败"})
			return
		}
		for _, member := range members {
			verifiedGroupIDs[member.GroupID] = true
		}
	}

	// 搜索与普通列表、详情使用完全相同的可见性规则，避免通过旧搜索
	// 接口枚举未加入私有群组的名称和备注。
	visibleGroups := make([]*gormdb.Group, 0, len(groups))
	for _, group := range groups {
		if canViewGroup(currentUser, group, verifiedGroupIDs[group.ID]) {
			visibleGroups = append(visibleGroups, group)
		}
	}
	total := len(visibleGroups)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	groups = visibleGroups[start:end]

	// 批量获取所有者呼号（解决 N+1 查询问题）
	userRepo := gormdb.NewUserRepository()
	ownerIDs := make([]int, 0, len(groups))
	for _, g := range groups {
		if g.OwerID > 0 {
			ownerIDs = append(ownerIDs, g.OwerID)
		}
	}
	// 去重
	ownerIDSet := make(map[int]bool)
	uniqueOwnerIDs := make([]int, 0, len(ownerIDs))
	for _, id := range ownerIDs {
		if !ownerIDSet[id] {
			ownerIDSet[id] = true
			uniqueOwnerIDs = append(uniqueOwnerIDs, id)
		}
	}
	ownerCallSigns, _ := userRepo.GetUserBriefByIDs(uniqueOwnerIDs)

	// Reassemble response data with user status
	resultItems := make([]gin.H, 0, len(groups))
	for _, g := range groups {
		isJoined := g.Type == groupTypePublic || g.OwerID == currentUser.ID || verifiedGroupIDs[g.ID]

		// Get owner callsign
		var ownerCallSign string
		if brief, ok := ownerCallSigns[g.OwerID]; ok {
			ownerCallSign = brief.CallSign
		}

		resultItems = append(resultItems, gin.H{
			"id":               g.ID,
			"name":             g.Name,
			"type":             g.Type,
			"ower_id":          g.OwerID,
			"ower_callsign":    ownerCallSign,
			"master_server":    g.MasterServer,
			"slave_server":     g.SlaveServer,
			"status":           g.Status,
			"note":             g.Note,
			"require_password": false,
			"is_joined":        isJoined,
			"create_time":      g.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time":      g.UpdateTime.Format("2006-01-02 15:04:05"),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items":     resultItems,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// JoinGroupRequest 加入群组请求
type JoinGroupRequest struct {
	Password string `json:"password" binding:"required"`
}

// JoinGroup 加入群组（验证密码）
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

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": members,
			"total": len(members),
		},
	})
}

// KickDevice 踢出设备
func KickDevice(c *gin.Context) {
	groupIDStr := c.Param("id")
	deviceIDStr := c.Param("deviceId")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}
	deviceID, err := strconv.Atoi(deviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的设备ID",
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
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "虚拟群组不支持踢出设备"})
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

	// 检查设备是否属于该群组
	deviceRepo := gormdb.NewDeviceRepository()
	device, err := deviceRepo.GetDeviceByID(deviceID)
	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	if device.GroupID != groupID {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "设备不属于该群组",
		})
		return
	}

	// 踢出只移动指定设备；设备所有者的成员资格必须保留，因为同一用户
	// 可能仍有其他设备留在该群组。系统默认公共群组为 999。
	err = deviceRepo.UpdateDeviceFields(deviceID, map[string]interface{}{
		"group_id": models.GroupIDPublicMin,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "移动设备失败",
		})
		return
	}

	// 使设备详情、群组设备列表和默认群组设备列表缓存失效
	ctx := c.Request.Context()
	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		// 使用 OwnerID 作为缓存键
		_ = deviceCache.InvalidateDevice(ctx, deviceID, device.OwnerID, uint8(device.SSID))
		// 使原群组设备列表缓存失效
		_ = deviceCache.InvalidateDevicesByGroup(ctx, groupID)
		// 使默认群组设备列表缓存失效（设备移入默认群组）
		_ = deviceCache.InvalidateDevicesByGroup(ctx, models.GroupIDPublicMin)
		// 由于设备的 GroupID 发生了改变，必须使全局设备列表也主动失效
		_ = deviceCache.InvalidateDeviceList(ctx)
	}
	if err := udphub.ChangeDeviceGroupByID(deviceID, models.GroupIDPublicMin); err != nil {
		log.Printf("[WARN] Failed to update kicked device group in memory: %v", err)
	}
	routesync.PublishDevice(deviceID)

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("踢出设备: 设备ID %d 从群组 %s (ID: %d) 移出", deviceID, group.Name, groupID),
		"device_kick",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "踢出成功",
	})
}

// LeaveGroup 离开群组
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

	// 检查是否是群组创建者，创建者不能离开自己的群组
	if group.OwerID == currentUser.ID {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "群组创建者不能退出自己的群组",
		})
		return
	}

	// 成员资格删除与本人设备迁移必须处于同一事务。
	movedDevices, err := repo.LeaveGroupAndMoveDevices(id, currentUser.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "离开群组失败",
		})
		return
	}

	// 数据库写入完成后同步 UDP 运行时索引，避免设备仍在旧私有群组收发。
	for _, device := range movedDevices {
		if err := udphub.ChangeDeviceGroupByID(device.ID, models.GroupIDPublicMin); err != nil {
			log.Printf("[WARN] Failed to update leaving device group in memory: %v", err)
		}
	}
	for _, device := range movedDevices {
		routesync.PublishDevice(device.ID)
	}

	// 使设备缓存和群组设备列表缓存失效
	ctx := c.Request.Context()
	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		// 使移动的设备缓存失效
		for _, device := range movedDevices {
			_ = deviceCache.InvalidateDevice(ctx, device.ID, device.OwnerID, uint8(device.SSID))
		}
		// 使原群组和默认群组的设备列表缓存失效
		_ = deviceCache.InvalidateDevicesByGroup(ctx, id)
		if len(movedDevices) > 0 {
			_ = deviceCache.InvalidateDevicesByGroup(ctx, models.GroupIDPublicMin)
		}
		// 由于设备的 GroupID 发生了改变，必须使全局设备列表也主动失效
		_ = deviceCache.InvalidateDeviceList(ctx)
	}

	// 使群组成员缓存和用户群组列表缓存失效
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		_ = groupCache.InvalidateGroupMembers(ctx, id)
		_ = groupCache.InvalidateUserGroups(ctx, currentUser.ID)
	}

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
