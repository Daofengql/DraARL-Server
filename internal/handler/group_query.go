package handler

import (
	gormdb "draarl/internal/gormdb"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
	"time"
)

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
		// Ghost devices are session-only runtime entities and are therefore not
		// represented by the SQL aggregate over devices. Add the de-duplicated
		// live session count for this group to the physical-device count.
		onlineCount := row.OnlineCount + udphub.GetOnlineGhostCountByGroup(row.ID)
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
			"online_count":  onlineCount,
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
