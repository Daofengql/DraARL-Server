package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"nrllink/internal/db"
	"nrllink/internal/models"
)

// GroupInfo 群组信息响应
type GroupInfo struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Type               int    `json:"type"`
	CallSign           string `json:"callsign"`
	Password           string `json:"password,omitempty"`
	AllowCallSignSSID  string `json:"allow_callsign_ssid"`
	OwerID             int    `json:"ower_id"`
	OwerCallSign       string `json:"ower_callsign"`
	DevList            string `json:"devlist"`
	MasterServer       int    `json:"master_server"`
	SlaveServer        int    `json:"slave_server"`
	Status             int    `json:"status"`
	CreateTime         string `json:"create_time,omitempty"`
	UpdateTime         string `json:"update_time,omitempty"`
	Note               string `json:"note"`
}

// GetGroups 获取群组列表
func GetGroups(c *gin.Context) {
	repo := db.NewGroupRepository()

	groupsRaw, err := repo.ListPublicGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询群组列表失败",
		})
		return
	}

	groups := make([]*GroupInfo, 0, len(groupsRaw))
	for _, g := range groupsRaw {
		// 将 []int 转换为逗号分隔的字符串
		devListStr := ""
		if len(g.DevList) > 0 {
			for i, id := range g.DevList {
				if i > 0 {
					devListStr += ","
				}
				devListStr += strconv.Itoa(id)
			}
		}

		group := &GroupInfo{
			ID:                g.ID,
			Name:              g.Name,
			Type:              g.Type,
			CallSign:          g.CallSign,
			AllowCallSignSSID: g.AllowCallSignSSID,
			OwerID:            g.OwerID,
			OwerCallSign:      g.OwerCallSign,
			DevList:           devListStr,
			MasterServer:      g.MasterServer,
			SlaveServer:       g.SlaveServer,
			Status:            g.Status,
			CreateTime:        g.CreateTime,
			UpdateTime:        g.UpdateTime,
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

	repo := db.NewGroupRepository()
	group, err := repo.GetPublicGroup(id)
	if err != nil {
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
			DevList:           func() string {
				if len(group.DevList) > 0 {
					s := ""
					for i, id := range group.DevList {
						if i > 0 {
							s += ","
						}
						s += strconv.Itoa(id)
					}
					return s
				}
				return ""
			}(),
			MasterServer:      group.MasterServer,
			SlaveServer:       group.SlaveServer,
			Status:            group.Status,
			CreateTime:        group.CreateTime,
			UpdateTime:        group.UpdateTime,
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

	repo := db.NewGroupRepository()
	group := &models.Group{
		Name:              req.Name,
		Type:              req.Type,
		CallSign:          req.CallSign,
		Password:          req.Password,
		AllowCallSignSSID: req.AllowCallSignSSID,
		OwerCallSign:      req.OwerCallSign,
		Status:            1,
		DevMap:            make(map[int]*models.Device),
	}

	if err := repo.AddPublicGroup(group); err != nil {
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

	repo := db.NewGroupRepository()

	// 先获取现有群组
	group, err := repo.GetPublicGroup(id)
	if err != nil {
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

	if err := repo.UpdatePublicGroup(group); err != nil {
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

	repo := db.NewGroupRepository()
	if err := repo.DeletePublicGroup(id); err != nil {
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

	repo := db.NewDeviceRepository()
	devicesRaw, total, err := repo.ListDevices(1000, 1) // 获取所有设备
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询设备列表失败",
		})
		return
	}

	// 过滤出属于该群组的设备
	devices := make([]*DeviceInfo, 0)
	for _, d := range devicesRaw {
		if d.GroupID == groupID {
			dev := &DeviceInfo{
				ID:         d.ID,
				Name:       d.Name,
				CallSign:   d.CallSign,
				SSID:       d.SSID,
				DevModel:   d.DevModel,
				GroupID:    d.GroupID,
				Status:     d.Status,
				Priority:   d.Priority,
				IsOnline:   d.ISOnline,
				CreateTime: d.CreateTime.Format("2006-01-02 15:04:05"),
				UpdateTime: d.UpdateTime.Format("2006-01-02 15:04:05"),
			}
			devices = append(devices, dev)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total": total,
			"items": devices,
		},
	})
}

// GetRelays 获取中继台列表
func GetRelays(c *gin.Context) {
	repo := db.NewRelayRepository()
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
	repo := db.NewServerRepository()
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
