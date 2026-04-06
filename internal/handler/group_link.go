package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	"github.com/gin-gonic/gin"
)

// CreateVirtualGroupRequest 创建虚拟互联组请求
type CreateVirtualGroupRequest struct {
	Name   string `json:"name" binding:"required"`
	Note   string `json:"note"`
	Status int    `json:"status"`
}

// CreateVirtualGroup 创建虚拟互联组（仅管理员）
func CreateVirtualGroup(c *gin.Context) {
	var req CreateVirtualGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 获取当前登录用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是管理员
	if !currentUser.HasRole("admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 创建虚拟互联组
	group := &gormdb.Group{
		Name:      req.Name,
		Type:      1, // 公开类型
		OwerID:    currentUser.ID,
		Status:    req.Status,
		IsVirtual: true,
		Note:      req.Note,
	}

	repo := gormdb.NewGroupRepository()
	if err := repo.CreateGroup(group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建虚拟互联组失败",
		})
		return
	}

	// 使群组列表缓存失效
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		_ = groupCache.InvalidateGroupList(c.Request.Context())
	}

	// 通知 udphub 刷新群组缓存
	udphub.RefreshGroupCache()

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("创建虚拟互联组: %s (ID: %d, 状态: %d)", group.Name, group.ID, group.Status),
		"virtual_group_create",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "创建成功",
		"data": gin.H{
			"id":         group.ID,
			"name":       group.Name,
			"is_virtual": group.IsVirtual,
		},
	})
}

// GetVirtualGroups 获取所有虚拟互联组列表（仅管理员）
func GetVirtualGroups(c *gin.Context) {
	// 获取当前登录用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是管理员
	if !currentUser.HasRole("admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 获取所有虚拟互联组
	repo := gormdb.NewGroupRepository()
	groups, err := repo.ListGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取群组列表失败",
		})
		return
	}

	// 过滤出虚拟互联组
	virtualGroups := make([]*gormdb.Group, 0)
	for _, g := range groups {
		if g.IsVirtual {
			virtualGroups = append(virtualGroups, g)
		}
	}

	// 获取每个互联组的关联群组数量
	linkRepo := gormdb.NewGroupLinkRepository()
	type virtualGroupWithCount struct {
		*gormdb.Group
		TargetCount int64 `json:"target_count"`
	}

	result := make([]virtualGroupWithCount, 0, len(virtualGroups))
	for _, vg := range virtualGroups {
		count, _ := linkRepo.GetLinkCount(vg.ID)
		result = append(result, virtualGroupWithCount{
			Group:       vg,
			TargetCount: count,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": result,
			"total": len(result),
		},
	})
}

// GetVirtualGroup 获取虚拟互联组详情（仅管理员）
func GetVirtualGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	// 获取当前登录用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是管理员
	if !currentUser.HasRole("admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 获取群组
	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	// 检查是否是虚拟互联组
	if !group.IsVirtual {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该群组不是虚拟互联组",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    group,
	})
}

// UpdateVirtualGroup 更新虚拟互联组（仅管理员）
func UpdateVirtualGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	var req struct {
		Name   string `json:"name"`
		Note   string `json:"note"`
		Status *int   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 获取当前登录用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是管理员
	if !currentUser.HasRole("admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 获取群组
	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	// 检查是否是虚拟互联组
	if !group.IsVirtual {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该群组不是虚拟互联组",
		})
		return
	}

	// 更新字段
	if req.Name != "" {
		group.Name = req.Name
	}
	if req.Note != "" {
		group.Note = req.Note
	}
	if req.Status != nil {
		group.Status = *req.Status
	}

	if err := repo.UpdateGroup(group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新失败",
		})
		return
	}

	// 使群组缓存失效
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		_ = groupCache.InvalidateGroup(c.Request.Context(), id)
		_ = groupCache.InvalidateGroupList(c.Request.Context())
	}

	// 通知 udphub 刷新群组缓存和互联路由缓存
	udphub.RefreshGroupCache()
	udphub.RefreshGroupLinkCache() // 状态变更后立即刷新互联路由，确保转发立刻生效

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("更新虚拟互联组: %s (ID: %d, 状态: %d)", group.Name, group.ID, group.Status),
		"virtual_group_update",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data":    group,
	})
}

// DeleteVirtualGroup 删除虚拟互联组（仅管理员）
func DeleteVirtualGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	// 获取当前登录用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是管理员
	if !currentUser.HasRole("admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 获取群组
	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	// 检查是否是虚拟互联组
	if !group.IsVirtual {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该群组不是虚拟互联组",
		})
		return
	}

	// 删除所有关联关系
	linkRepo := gormdb.NewGroupLinkRepository()
	if err := linkRepo.DeleteLinksByLinkGroup(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除关联关系失败",
		})
		return
	}

	// 删除群组
	if err := repo.DeleteGroupWithCascade(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除群组失败",
		})
		return
	}

	// 使群组缓存失效
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		_ = groupCache.InvalidateGroup(c.Request.Context(), id)
		_ = groupCache.InvalidateGroupList(c.Request.Context())
	}

	// 通知 udphub 刷新群组缓存和互联缓存
	udphub.RefreshGroupCache()
	udphub.RefreshGroupLinkCache()

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("删除虚拟互联组: %s (ID: %d)", group.Name, id),
		"virtual_group_delete",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// GetGroupLinkTargets 获取互联组的关联群组列表（仅管理员）
func GetGroupLinkTargets(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	// 获取当前登录用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是管理员
	if !currentUser.HasRole("admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 获取关联群组信息
	linkRepo := gormdb.NewGroupLinkRepository()
	links, err := linkRepo.GetLinkWithGroupInfo(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取关联群组失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": links,
			"total": len(links),
		},
	})
}

// AddGroupLinkTarget 添加关联群组（仅管理员）
func AddGroupLinkTarget(c *gin.Context) {
	idStr := c.Param("id")
	linkGroupID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	var req struct {
		TargetGroupID int `json:"target_group_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 获取当前登录用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是管理员
	if !currentUser.HasRole("admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 验证互联组是否存在且是虚拟组
	groupRepo := gormdb.NewGroupRepository()
	linkGroup, err := groupRepo.GetGroupByID(linkGroupID)
	if err != nil || linkGroup == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "互联组不存在",
		})
		return
	}
	if !linkGroup.IsVirtual {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "目标群组必须是虚拟互联组",
		})
		return
	}

	// 验证目标群组是否存在且不是虚拟组
	targetGroup, err := groupRepo.GetGroupByID(req.TargetGroupID)
	if err != nil || targetGroup == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "目标群组不存在",
		})
		return
	}
	if targetGroup.IsVirtual {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "不能关联另一个虚拟互联组",
		})
		return
	}

	// 检查是否已存在关联
	linkRepo := gormdb.NewGroupLinkRepository()
	exists, _ := linkRepo.LinkExists(linkGroupID, req.TargetGroupID)
	if exists {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "该关联关系已存在",
		})
		return
	}

	// 业务约束：同一个实体组不能被多个虚拟互联组同时关联
	// 否则会导致互联拓扑重叠，产生不可预期的跨组扩散风险。
	existingLinks, err := linkRepo.GetLinksByTargetGroup(req.TargetGroupID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "检查目标群组关联状态失败",
		})
		return
	}
	for _, link := range existingLinks {
		if link.LinkGroupID == linkGroupID {
			continue
		}
		conflictGroup, _ := groupRepo.GetGroupByID(link.LinkGroupID)
		conflictName := fmt.Sprintf("ID=%d", link.LinkGroupID)
		if conflictGroup != nil {
			conflictName = fmt.Sprintf("%s (ID: %d)", conflictGroup.Name, conflictGroup.ID)
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": fmt.Sprintf("目标群组已被虚拟互联组 %s 关联，每个实体组只能加入一个虚拟互联组", conflictName),
		})
		return
	}

	// 添加关联
	if err := linkRepo.AddLink(linkGroupID, req.TargetGroupID); err != nil {
		if errors.Is(err, gormdb.ErrTargetGroupAlreadyLinked) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "目标群组已被其他虚拟互联组关联，每个实体组只能加入一个虚拟互联组",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "添加关联失败",
		})
		return
	}

	// 通知 udphub 刷新互联缓存
	udphub.RefreshGroupLinkCache()

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("添加群组互联: 虚拟组 %s (ID: %d) <- 目标组 %s (ID: %d)", linkGroup.Name, linkGroupID, targetGroup.Name, req.TargetGroupID),
		"group_link_add",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	// 获取关联数量用于提示
	count, _ := linkRepo.GetLinkCount(linkGroupID)

	response := gin.H{
		"code":    200,
		"message": "添加成功",
		"data": gin.H{
			"link_group_id":   linkGroupID,
			"target_group_id": req.TargetGroupID,
			"target_count":    count,
		},
	}

	// 如果关联群组超过5个，添加温馨提示
	if count > 5 {
		response["warning"] = "关联群组较多可能会增加服务器转发负担，请根据实际需求添加"
	}

	c.JSON(http.StatusOK, response)
}

// RemoveGroupLinkTarget 移除关联群组（仅管理员）
func RemoveGroupLinkTarget(c *gin.Context) {
	idStr := c.Param("id")
	linkGroupID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	targetIDStr := c.Param("targetId")
	targetGroupID, err := strconv.Atoi(targetIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的目标群组ID",
		})
		return
	}

	// 获取当前登录用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是管理员
	if !currentUser.HasRole("admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 移除关联
	linkRepo := gormdb.NewGroupLinkRepository()
	if err := linkRepo.RemoveLink(linkGroupID, targetGroupID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "移除关联失败",
		})
		return
	}

	// 通知 udphub 刷新互联缓存
	udphub.RefreshGroupLinkCache()

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("移除群组互联: 虚拟组 ID %d <- 目标组 ID %d", linkGroupID, targetGroupID),
		"group_link_remove",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "移除成功",
	})
}

// GetAvailableTargetGroups 获取可关联的群组列表（仅管理员）
// 返回所有非虚拟的公开群组，供管理员选择关联
func GetAvailableTargetGroups(c *gin.Context) {
	// 获取当前登录用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 检查是否是管理员
	if !currentUser.HasRole("admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 获取所有公开群组（非虚拟）
	repo := gormdb.NewGroupRepository()
	groups, err := repo.ListPublicGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取群组列表失败",
		})
		return
	}

	// 获取已被互联占用的实体组
	linkRepo := gormdb.NewGroupLinkRepository()
	linkedTargetIDs, err := linkRepo.GetLinkedTargetGroupIDs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取已关联目标群组失败",
		})
		return
	}
	linkedTargetSet := make(map[int]struct{}, len(linkedTargetIDs))
	for _, id := range linkedTargetIDs {
		linkedTargetSet[id] = struct{}{}
	}

	// 过滤掉虚拟组和已被占用的实体组
	availableGroups := make([]*gormdb.Group, 0)
	for _, g := range groups {
		if g.IsVirtual {
			continue
		}
		if _, occupied := linkedTargetSet[g.ID]; occupied {
			continue
		}
		availableGroups = append(availableGroups, g)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": availableGroups,
			"total": len(availableGroups),
		},
	})
}
