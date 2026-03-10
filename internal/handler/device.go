package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	gormdb "nrllink/internal/gormdb"
)

// DeviceListRequest 设备列表请求
type DeviceListRequest struct {
	Limit    int    `json:"limit"`
	Page     int    `json:"page"`
	CallSign string `json:"callsign"`
	GroupID  string `json:"group_id"`
	IsOnline bool   `json:"isonline"`
	Sort     string `json:"sort"`
}

// DeviceInfo 设备信息响应
type DeviceInfo struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	CallSign   string `json:"callsign"`
	SSID       uint8  `json:"ssid"`
	DevModel   int    `json:"dev_model"`
	GroupID    int    `json:"group_id"`
	Status     int8   `json:"status"`
	Priority   int    `json:"priority"`
	IsOnline   bool   `json:"is_online"`
	QTH        string `json:"qth"`
	Note       string `json:"note"`
	OnlineTime string `json:"online_time,omitempty"`
	CreateTime string `json:"create_time,omitempty"`
	UpdateTime string `json:"update_time,omitempty"`
}

// GetDevices 获取设备列表
func GetDevices(c *gin.Context) {
	// 获取查询参数
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	callsign := c.Query("callsign")
	groupID := c.Query("group_id")
	_ = c.Query("isonline") == "true" // TODO: 实现在线状态过滤

	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}

	repo := gormdb.NewDeviceRepository()

	var devices []*gormdb.Device
	var total int64
	var err error

	// 根据查询条件选择不同的查询方法
	if callsign != "" {
		// 按呼号搜索
		devicesRaw, _ := repo.ListDevicesByCallSign(callsign)
		total = int64(len(devicesRaw))
		// 手动分页
		start := (page - 1) * limit
		end := start + limit
		if start >= len(devicesRaw) {
			devices = []*gormdb.Device{}
		} else if end > len(devicesRaw) {
			devices = devicesRaw[start:]
		} else {
			devices = devicesRaw[start:end]
		}
	} else if groupID != "" {
		// 按群组过滤
		gid, _ := strconv.Atoi(groupID)
		devicesRaw, _ := repo.ListDevicesByGroupID(gid)
		total = int64(len(devicesRaw))
		// 手动分页
		start := (page - 1) * limit
		end := start + limit
		if start >= len(devicesRaw) {
			devices = []*gormdb.Device{}
		} else if end > len(devicesRaw) {
			devices = devicesRaw[start:]
		} else {
			devices = devicesRaw[start:end]
		}
	} else {
		// 获取所有设备
		devices, total, err = repo.ListDevices(limit, page)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "查询设备列表失败",
			})
			return
		}
	}

	// 转换为响应格式
	items := make([]*DeviceInfo, 0, len(devices))
	for _, d := range devices {
		items = append(items, &DeviceInfo{
			ID:         d.ID,
			Name:       d.Name,
			CallSign:   d.CallSign,
			SSID:       d.SSID,
			DevModel:   d.DevModel,
			GroupID:    d.GroupID,
			Status:     d.Status,
			Priority:   d.Priority,
			IsOnline:   d.ISOnline,
			Note:       d.Note,
			CreateTime: d.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime: d.UpdateTime.Format("2006-01-02 15:04:05"),
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

// GetDevice 获取单个设备
func GetDevice(c *gin.Context) {
	callsign := c.Query("callsign")
	ssidStr := c.Query("ssid")
	ssid := uint8(0)

	if ssidStr != "" {
		s, err := strconv.ParseUint(ssidStr, 10, 8)
		if err == nil {
			ssid = uint8(s)
		}
	}

	idStr := c.Query("id")

	repo := gormdb.NewDeviceRepository()
	var device *gormdb.Device
	var err error

	// 优先使用ID查询
	if idStr != "" {
		id, _ := strconv.Atoi(idStr)
		device, err = repo.GetDeviceByID(id)
	} else if callsign != "" {
		device, err = repo.GetDeviceByCallSignSSID(callsign, ssid)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "缺少设备标识",
		})
		return
	}

	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":         device.ID,
			"name":       device.Name,
			"callsign":   device.CallSign,
			"ssid":       device.SSID,
			"dev_model":  device.DevModel,
			"group_id":   device.GroupID,
			"status":     device.Status,
			"priority":   device.Priority,
			"is_online":  device.ISOnline,
			"note":       device.Note,
			"create_time": device.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time": device.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// CreateDeviceRequest 创建设备请求
type CreateDeviceRequest struct {
	Name     string `json:"name" binding:"required"`
	CallSign string `json:"callsign" binding:"required"`
	SSID     uint8  `json:"ssid"`
	DevModel int    `json:"dev_model"`
	GroupID  int    `json:"group_id"`
	Password string `json:"password"`
	Note     string `json:"note"`
	Priority int    `json:"priority"`
}

// CreateDevice 创建设备
func CreateDevice(c *gin.Context) {
	var req CreateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	repo := gormdb.NewDeviceRepository()

	// 检查设备是否已存在
	existing, _ := repo.GetDeviceByCallSignSSID(req.CallSign, req.SSID)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "设备已存在",
		})
		return
	}

	device := &gormdb.Device{
		Name:       req.Name,
		CallSign:   req.CallSign,
		SSID:       req.SSID,
		DevModel:   req.DevModel,
		GroupID:    req.GroupID,
		Password:   req.Password,
		Status:     1,
		Priority:   req.Priority,
		Note:       req.Note,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}

	if device.Priority == 0 {
		device.Priority = 100
	}

	if err := repo.CreateDevice(device); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建设备失败",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "创建成功",
		"data": gin.H{
			"id": device.ID,
		},
	})
}

// UpdateDeviceRequest 更新设备请求
type UpdateDeviceRequest struct {
	Name      string `json:"name"`
	GroupID   int    `json:"group_id"`
	Status    int8   `json:"status"`
	Priority  int    `json:"priority"`
	Note      string `json:"note"`
	Password  string `json:"password"`
	DevModel  int    `json:"dev_model"`
}

// UpdateDevice 更新设备
func UpdateDevice(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的设备ID",
		})
		return
	}

	var req UpdateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	repo := gormdb.NewDeviceRepository()

	// 检查设备是否存在
	device, err := repo.GetDeviceByID(id)
	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "设备不存在",
		})
		return
	}

	// 更新字段
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
		device.Name = req.Name
	}
	if req.GroupID > 0 {
		updates["group_id"] = req.GroupID
		device.GroupID = req.GroupID
	}
	if req.Status > 0 {
		updates["status"] = req.Status
		device.Status = req.Status
	}
	if req.Priority > 0 {
		updates["priority"] = req.Priority
		device.Priority = req.Priority
	}
	if req.Note != "" {
		updates["note"] = req.Note
		device.Note = req.Note
	}
	if req.Password != "" {
		updates["password"] = req.Password
		device.Password = req.Password
	}
	if req.DevModel > 0 {
		updates["dev_model"] = req.DevModel
		device.DevModel = req.DevModel
	}

	if err := repo.UpdateDeviceFields(id, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新设备失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id": id,
		},
	})
}

// DeleteDevice 删除设备
func DeleteDevice(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err == nil {
		// 通过ID删除
		repo := gormdb.NewDeviceRepository()
		if err := repo.DeleteDeviceByID(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "删除设备失败",
			})
			return
		}
	} else {
		// 通过呼号和SSID删除（兼容旧接口）
		callsign := c.Query("callsign")
		ssidStr := c.Query("ssid")
		ssid := uint8(0)

		if ssidStr != "" {
			s, err := strconv.ParseUint(ssidStr, 10, 8)
			if err == nil {
				ssid = uint8(s)
			}
		}

		if callsign == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "缺少设备标识",
			})
			return
		}

		repo := gormdb.NewDeviceRepository()
		if err := repo.DeleteDevice(callsign, ssid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "删除设备失败",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// ChangeDeviceGroupRequest 修改设备群组请求
type ChangeDeviceGroupRequest struct {
	CallSign string `json:"callsign" binding:"required"`
	SSID     uint8  `json:"ssid"`
	GroupID  int    `json:"group_id" binding:"required"`
}

// ChangeDeviceGroup 修改设备群组
func ChangeDeviceGroup(c *gin.Context) {
	var req ChangeDeviceGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	repo := gormdb.NewDeviceRepository()
	if err := repo.ChangeDeviceGroup(req.CallSign, req.SSID, req.GroupID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "修改设备群组失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "修改成功",
	})
}

// GetDeviceQTHs 获取设备位置列表
func GetDeviceQTHs(c *gin.Context) {
	repo := gormdb.NewDeviceRepository()
	devicesRaw, _, err := repo.ListDevices(1000, 1)
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
			"id":       d.ID,
			"name":     d.Name,
			"callsign": d.CallSign,
			"ssid":     d.SSID,
			"qth":      "", // TODO: 需要添加 QTH 字段到设备表
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": devices,
		},
	})
}
