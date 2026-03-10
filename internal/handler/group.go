package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	gormdb "nrllink/internal/gormdb"
)

// GroupInfo 群组信息响应
type GroupInfo struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Type              int    `json:"type"`
	CallSign          string `json:"callsign"`
	Password          string `json:"password,omitempty"`
	AllowCallSignSSID string `json:"allow_callsign_ssid"`
	OwerID            int    `json:"ower_id"`
	OwerCallSign      string `json:"ower_callsign"`
	DevList           string `json:"devlist"`
	MasterServer      int    `json:"master_server"`
	SlaveServer       int    `json:"slave_server"`
	Status            int    `json:"status"`
	CreateTime        string `json:"create_time,omitempty"`
	UpdateTime        string `json:"update_time,omitempty"`
	Note              string `json:"note"`
}

// GetGroups 获取群组列表
func GetGroups(c *gin.Context) {
	repo := gormdb.NewGroupRepository()

	groupsRaw, err := repo.ListGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询群组列表失败",
		})
		return
	}

	groups := make([]*GroupInfo, 0, len(groupsRaw))
	for _, g := range groupsRaw {
		group := &GroupInfo{
			ID:                g.ID,
			Name:              g.Name,
			Type:              g.Type,
			CallSign:          g.CallSign,
			AllowCallSignSSID: g.AllowCallSignSSID,
			OwerID:            g.OwerID,
			OwerCallSign:      g.OwerCallSign,
			DevList:           g.DevList,
			MasterServer:      g.MasterServer,
			SlaveServer:       g.SlaveServer,
			Status:            g.Status,
			CreateTime:        g.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:        g.UpdateTime.Format("2006-01-02 15:04:05"),
			Note:              g.Note,
		}
		groups = append(groups, group)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": groups,
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

	repo := gormdb.NewGroupRepository()
	group, err := repo.GetGroupByID(id)
	if err != nil || group == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "群组不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": &GroupInfo{
			ID:                group.ID,
			Name:              group.Name,
			Type:              group.Type,
			CallSign:          group.CallSign,
			AllowCallSignSSID: group.AllowCallSignSSID,
			OwerID:            group.OwerID,
			OwerCallSign:      group.OwerCallSign,
			DevList:           group.DevList,
			MasterServer:      group.MasterServer,
			SlaveServer:       group.SlaveServer,
			Status:            group.Status,
			CreateTime:        group.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:        group.UpdateTime.Format("2006-01-02 15:04:05"),
			Note:              group.Note,
		},
	})
}

// CreateGroupRequest 创建群组请求
type CreateGroupRequest struct {
	Name              string `json:"name" binding:"required"`
	Type              int    `json:"type"`
	CallSign          string `json:"callsign"`
	Password          string `json:"password"`
	AllowCallSignSSID string `json:"allow_callsign_ssid"`
	OwerCallSign      string `json:"ower_callsign"`
	Note              string `json:"note"`
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

	repo := gormdb.NewGroupRepository()
	group := &gormdb.Group{
		Name:              req.Name,
		Type:              req.Type,
		CallSign:          req.CallSign,
		Password:          req.Password,
		AllowCallSignSSID: req.AllowCallSignSSID,
		OwerCallSign:      req.OwerCallSign,
		Status:            1,
		DevList:           "",
	}

	if err := repo.CreateGroup(group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建群组失败",
		})
		return
	}

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
	Name              string `json:"name"`
	Type              int    `json:"type"`
	CallSign          string `json:"callsign"`
	Password          string `json:"password"`
	AllowCallSignSSID string `json:"allow_callsign_ssid"`
	Note              string `json:"note"`
}

// UpdateGroup 更新群组
func UpdateGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	var req UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
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

	// 更新字段
	if req.Name != "" {
		group.Name = req.Name
	}
	if req.Type > 0 {
		group.Type = req.Type
	}
	if req.CallSign != "" {
		group.CallSign = req.CallSign
	}
	if req.Password != "" {
		group.Password = req.Password
	}
	if req.AllowCallSignSSID != "" {
		group.AllowCallSignSSID = req.AllowCallSignSSID
	}
	if req.Note != "" {
		group.Note = req.Note
	}

	if err := repo.UpdateGroup(group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新群组失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
	})
}

// DeleteGroup 删除群组
func DeleteGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的群组ID",
		})
		return
	}

	repo := gormdb.NewGroupRepository()
	if err := repo.DeleteGroup(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除群组失败",
		})
		return
	}

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

	repo := gormdb.NewDeviceRepository()
	devicesRaw, err := repo.ListDevicesByGroupID(groupID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询设备列表失败",
		})
		return
	}

	// 转换为响应格式
	devices := make([]gin.H, 0, len(devicesRaw))
	for _, d := range devicesRaw {
		devices = append(devices, gin.H{
			"id":         d.ID,
			"name":       d.Name,
			"callsign":   d.CallSign,
			"ssid":       d.SSID,
			"dev_model":  d.DevModel,
			"group_id":   d.GroupID,
			"status":     d.Status,
			"priority":   d.Priority,
			"is_online":  d.ISOnline,
			"create_time": d.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time": d.UpdateTime.Format("2006-01-02 15:04:05"),
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

// GetRelays 获取中继台列表
func GetRelays(c *gin.Context) {
	repo := gormdb.NewRelayRepository()
	relays, err := repo.ListRelays()
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
