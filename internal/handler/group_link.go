package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/repository"
	broadcastruntime "draarl/internal/broadcast/runtime"
	schedruntime "draarl/internal/broadcast/scheduler/runtime"
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/routesync"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	"github.com/gin-gonic/gin"
)

func canUseGroupAsLinkTarget(group *gormdb.Group) bool {
	return group != nil &&
		group.Status == 1 &&
		!group.IsVirtual &&
		isSupportedGroupType(group.Type)
}

func filterAvailableGroupLinkTargets(groups []*gormdb.Group, occupied map[int]struct{}) []*gormdb.Group {
	available := make([]*gormdb.Group, 0, len(groups))
	for _, group := range groups {
		if !canUseGroupAsLinkTarget(group) {
			continue
		}
		if _, exists := occupied[group.ID]; exists {
			continue
		}
		available = append(available, group)
	}
	return available
}

func writeVirtualGroupError(c *gin.Context, status int, errorCode, message string) {
	c.JSON(status, gin.H{"code": status, "error_code": errorCode, "message": message})
}

func writeVirtualGroupRepositoryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeVirtualGroupError(c, http.StatusNotFound, "virtual_group_not_found", "虚拟互联组或成员不存在")
	case errors.Is(err, repository.ErrVirtualGroupRequired):
		writeVirtualGroupError(c, http.StatusBadRequest, "virtual_group_required", "目标群组不是虚拟互联组")
	case errors.Is(err, repository.ErrInvalidEntityGroup):
		writeVirtualGroupError(c, http.StatusBadRequest, "invalid_virtual_group_member", "只能关联已启用的公开或私有实体群组")
	case errors.Is(err, repository.ErrInvalidPolicy):
		writeVirtualGroupError(c, http.StatusBadRequest, "virtual_broadcast_policy_required", "必须保存有效的自动播报策略")
	case errors.Is(err, repository.ErrPolicySourceNotMember):
		writeVirtualGroupError(c, http.StatusBadRequest, "virtual_broadcast_source_not_member", "保留的自动播报来源必须属于该虚拟组")
	case errors.Is(err, repository.ErrPolicySourceStillMember):
		writeVirtualGroupError(c, http.StatusConflict, "virtual_broadcast_source_must_change", "移除当前保留来源前必须先选择其他来源或全部暂停")
	case errors.Is(err, gormdb.ErrTargetGroupAlreadyLinked):
		writeVirtualGroupError(c, http.StatusConflict, "virtual_group_member_already_linked", "目标实体组已被虚拟互联组关联")
	default:
		log.Printf("[BROADCAST] virtual group coordination failed: %v", err)
		writeVirtualGroupError(c, http.StatusInternalServerError, "virtual_group_coordination_failed", "虚拟互联组更新失败")
	}
}

func waitForBroadcastTopologyMutation(c *gin.Context, mutation *repository.VirtualGroupMutation) bool {
	if mutation == nil || len(mutation.CancelRunIDs) == 0 {
		return true
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := broadcastruntime.CancelRunsAndWait(waitCtx, mutation.CancelRunIDs, schedruntime.ErrInterconnectChange); err != nil {
		log.Printf("[BROADCAST] timed out waiting for interconnect playback release: groups=%v runs=%v err=%v", mutation.CancelGroupIDs, mutation.CancelRunIDs, err)
		writeVirtualGroupError(c, http.StatusServiceUnavailable, "broadcast_interconnect_release_timeout", "互联配置已保存，但自动播报尚未安全释放；运行态拓扑未刷新")
		return false
	}
	if err := repository.Default().FinalizeOrphanedInterconnectRuns(waitCtx, mutation.CancelRunIDs, time.Now().UTC()); err != nil {
		log.Printf("[BROADCAST] failed to finalize orphaned interconnect runs: groups=%v runs=%v err=%v", mutation.CancelGroupIDs, mutation.CancelRunIDs, err)
		writeVirtualGroupError(c, http.StatusServiceUnavailable, "broadcast_interconnect_finalize_failed", "互联配置已保存，但自动播报执行状态尚未安全收尾；运行态拓扑未刷新")
		return false
	}
	return true
}

// CreateVirtualGroupRequest 创建虚拟互联组请求
type CreateVirtualGroupRequest struct {
	Name            string                              `json:"name" binding:"required"`
	Note            string                              `json:"note"`
	Status          int                                 `json:"status"`
	TargetGroupIDs  []int                               `json:"target_group_ids"`
	BroadcastPolicy *VirtualGroupBroadcastPolicyRequest `json:"broadcast_policy"`
}

type VirtualGroupBroadcastPolicyRequest struct {
	Mode                 string `json:"mode"`
	AllowedSourceGroupID *int   `json:"allowed_source_group_id"`
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

	if req.Status != 0 && req.Status != 1 {
		writeVirtualGroupError(c, http.StatusBadRequest, "invalid_virtual_group_status", "虚拟互联组状态只能是关闭或开启")
		return
	}
	policyRequest := req.BroadcastPolicy
	if policyRequest == nil {
		policyRequest = &VirtualGroupBroadcastPolicyRequest{Mode: model.PolicySuspendAll}
	}
	policyRequest.Mode = strings.TrimSpace(policyRequest.Mode)
	if policyRequest.Mode == "" {
		policyRequest.Mode = model.PolicySuspendAll
	}

	group := &gormdb.Group{
		Name:      req.Name,
		Type:      1, // 公开类型
		OwerID:    currentUser.ID,
		Status:    req.Status,
		IsVirtual: true,
		Note:      req.Note,
	}
	policy := &model.VirtualGroupBroadcastPolicy{
		Mode: policyRequest.Mode, AllowedSourceGroupID: policyRequest.AllowedSourceGroupID, UpdatedBy: currentUser.ID,
	}
	mutation, err := repository.Default().CreateVirtualGroup(c.Request.Context(), group, req.TargetGroupIDs, policy, time.Now().UTC())
	if err != nil {
		writeVirtualGroupRepositoryError(c, err)
		return
	}
	if !waitForBroadcastTopologyMutation(c, mutation) {
		return
	}

	// 使群组列表缓存失效
	if groupCache := cache.GetGroupCache(); groupCache != nil {
		_ = groupCache.InvalidateGroupList(c.Request.Context())
	}

	// 通知 udphub 刷新群组缓存
	udphub.RefreshGroupCache()
	udphub.RefreshGroupLinkCache()
	routesync.RefreshTopology()

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
			"id": group.ID, "name": group.Name, "is_virtual": group.IsVirtual,
			"broadcast_policy": policy,
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

	broadcastRepo := repository.Default()
	policy, err := broadcastRepo.GetPolicy(c.Request.Context(), group.ID)
	if err != nil {
		writeVirtualGroupRepositoryError(c, err)
		return
	}
	memberStats, err := broadcastRepo.ListPolicyMemberStats(c.Request.Context(), group.ID)
	if err != nil {
		writeVirtualGroupRepositoryError(c, err)
		return
	}
	allowedSourceName := ""
	if policy.AllowedSourceGroupID != nil {
		for _, stats := range memberStats {
			if stats.GroupID == *policy.AllowedSourceGroupID {
				allowedSourceName = stats.GroupName
				break
			}
		}
	}
	type virtualGroupDetail struct {
		*gormdb.Group
		BroadcastPolicy   *model.VirtualGroupBroadcastPolicy `json:"broadcast_policy"`
		AllowedSourceName string                             `json:"allowed_source_name,omitempty"`
		BroadcastMembers  []repository.MemberScheduleStats   `json:"broadcast_members"`
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 200, "message": "成功",
		"data": virtualGroupDetail{Group: group, BroadcastPolicy: policy, AllowedSourceName: allowedSourceName, BroadcastMembers: memberStats},
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

	fields := repository.VirtualGroupFields{Status: req.Status}
	if req.Name != "" {
		fields.Name = &req.Name
	}
	if req.Note != "" {
		fields.Note = &req.Note
	}
	mutation, err := repository.Default().UpdateVirtualGroup(c.Request.Context(), id, fields, time.Now().UTC())
	if err != nil {
		writeVirtualGroupRepositoryError(c, err)
		return
	}
	if fields.Name != nil {
		group.Name = *fields.Name
	}
	if fields.Note != nil {
		group.Note = *fields.Note
	}
	if fields.Status != nil {
		group.Status = *fields.Status
	}
	if !waitForBroadcastTopologyMutation(c, mutation) {
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
	routesync.RefreshTopology()

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

	mutation, err := repository.Default().SetVirtualGroupStatus(c.Request.Context(), id, 0, time.Now().UTC())
	if err != nil {
		writeVirtualGroupRepositoryError(c, err)
		return
	}
	if !waitForBroadcastTopologyMutation(c, mutation) {
		return
	}

	// 群组和全部关联关系由同一仓库事务删除，不能先在事务外删除关联。
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
	routesync.RefreshTopology()

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

	// 写入端必须独立校验目标，不能依赖候选列表阻止非法 ID 直提。
	targetGroup, err := groupRepo.GetGroupByID(req.TargetGroupID)
	if err != nil || targetGroup == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "目标群组不存在",
		})
		return
	}
	if !canUseGroupAsLinkTarget(targetGroup) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "只能关联已启用的公开或私有实体群组",
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

	mutation, err := repository.Default().AddVirtualGroupMember(c.Request.Context(), linkGroupID, req.TargetGroupID, time.Now().UTC())
	if err != nil {
		writeVirtualGroupRepositoryError(c, err)
		return
	}
	if !waitForBroadcastTopologyMutation(c, mutation) {
		return
	}

	// 通知 udphub 刷新互联缓存
	udphub.RefreshGroupLinkCache()
	routesync.RefreshTopology()

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

	mutation, err := repository.Default().RemoveVirtualGroupMember(c.Request.Context(), linkGroupID, targetGroupID, time.Now().UTC())
	if err != nil {
		writeVirtualGroupRepositoryError(c, err)
		return
	}
	if !waitForBroadcastTopologyMutation(c, mutation) {
		return
	}

	// 通知 udphub 刷新互联缓存
	udphub.RefreshGroupLinkCache()
	routesync.RefreshTopology()

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

func UpdateVirtualGroupBroadcastPolicy(c *gin.Context) {
	virtualGroupID, err := strconv.Atoi(c.Param("id"))
	if err != nil || virtualGroupID <= 0 {
		writeVirtualGroupError(c, http.StatusBadRequest, "invalid_virtual_group_id", "无效的群组ID")
		return
	}
	var req VirtualGroupBroadcastPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeVirtualGroupError(c, http.StatusBadRequest, "virtual_broadcast_policy_required", "必须提交有效的自动播报策略")
		return
	}
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	if !currentUser.HasRole("admin") {
		writeVirtualGroupError(c, http.StatusForbidden, "admin_required", "需要管理员权限")
		return
	}
	policy := &model.VirtualGroupBroadcastPolicy{
		VirtualGroupID: virtualGroupID, Mode: strings.TrimSpace(req.Mode),
		AllowedSourceGroupID: req.AllowedSourceGroupID, UpdatedBy: currentUser.ID,
	}
	broadcastRepo := repository.Default()
	mutation, err := broadcastRepo.UpdateVirtualGroupPolicy(c.Request.Context(), policy, time.Now().UTC())
	if err != nil {
		writeVirtualGroupRepositoryError(c, err)
		return
	}
	if !waitForBroadcastTopologyMutation(c, mutation) {
		return
	}
	memberStats, err := broadcastRepo.ListPolicyMemberStats(c.Request.Context(), virtualGroupID)
	if err != nil {
		writeVirtualGroupRepositoryError(c, err)
		return
	}
	oplog.AddLog(
		fmt.Sprintf("更新虚拟组自动播报策略: 虚拟组 ID %d, 模式 %s", virtualGroupID, policy.Mode),
		"virtual_group_broadcast_policy_update", currentUser.ID, currentUser.Name, currentUser.CallSign, c.ClientIP(),
	)
	c.JSON(http.StatusOK, gin.H{
		"code": 200, "message": "自动播报策略更新成功",
		"data": gin.H{"broadcast_policy": policy, "broadcast_members": memberStats},
	})
}

// GetAvailableTargetGroups 获取可关联的群组列表（仅管理员）。
// 已启用的公开和私有实体群组都可以作为互联目标；禁用、虚拟和已经被其他
// 互联组占用的实体群组不会返回。
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

	// 获取所有已启用实体群组（公开 + 私有，排除虚拟互联组）。
	repo := gormdb.NewGroupRepository()
	groups, err := repo.ListGroupsExcludeVirtual()
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

	availableGroups := filterAvailableGroupLinkTargets(groups, linkedTargetSet)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": availableGroups,
			"total": len(availableGroups),
		},
	})
}
