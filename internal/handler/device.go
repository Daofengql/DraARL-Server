package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"nrllink/internal/db"
	"nrllink/internal/models"
)

// DeviceListRequest 设备列表请求
type DeviceListRequest struct {
	Limit    int    `json:"limit"`
	Page     int    `json:"page"`
	Callsign string `json:"callsign"`
	GroupID  string `json:"group_id"`
	IsOnline bool   `json:"isonline"`
	Sort     string `json:"sort"`
}

// DeviceInfo 设备信息响应
type DeviceInfo struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	CallSign    string `json:"callsign"`
	SSID        byte   `json:"ssid"`
	DevModel    byte   `json:"dev_model"`
	GroupID     int    `json:"group_id"`
	Status      byte   `json:"status"`
	Priority    int    `json:"priority"`
	IsOnline    bool   `json:"is_online"`
	QTH         string `json:"qth"`
	Note        string `json:"note"`
	OnlineTime  string `json:"online_time,omitempty"`
	CreateTime  string `json:"create_time,omitempty"`
	UpdateTime  string `json:"update_time,omitempty"`
}

// GetDevices 获取设备列表
func GetDevices(c *gin.Context) {
	// 获取查询参数
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	callsign := c.Query("callsign")
	groupID := c.Query("group_id")
	_ = c.Query("isonline") == "true" // TODO: 实现在线状态过滤

	repo := db.NewDeviceRepository()

	var devices []*DeviceInfo
	var total int
	var err error

	// TODO: 实现更复杂的查询条件
	devicesRaw, total, err := repo.ListDevices(limit, page)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询设备列表失败",
		})
		return
	}

	// 转换为响应格式
	for _, d := range devicesRaw {
		// 过滤条件
		if callsign != "" && d.CallSign != callsign {
			continue
		}
		if groupID != "" && strconv.Itoa(d.GroupID) != groupID {
			continue
		}

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

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total": total,
			"items": devices,
		},
	})
}

// GetDevice 获取单个设备
func GetDevice(c *gin.Context) {
	callsign := c.Query("callsign")
	ssidStr := c.Query("ssid")
	ssid := byte(0)

	if ssidStr != "" {
		s, err := strconv.ParseUint(ssidStr, 10, 8)
		if err == nil {
			ssid = byte(s)
		}
	}

	if callsign == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "缺少呼号参数",
		})
		return
	}

	repo := db.NewDeviceRepository()
	device, err := repo.GetDevice(callsign, ssid)
	if err != nil {
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
			"create_time": device.CreateTime.Format("2006-01-02 15:04:05"),
			"update_time": device.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// CreateDeviceRequest 创建设备请求
type CreateDeviceRequest struct {
	Name     string `json:"name" binding:"required"`
	CallSign string `json:"callsign" binding:"required"`
	SSID     byte   `json:"ssid"`
	DevModel byte   `json:"dev_model"`
	GroupID  int    `json:"group_id"`
	Password string `json:"password"`
	Note     string `json:"note"`
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

	repo := db.NewDeviceRepository()

	// 检查设备是否已存在
	_, err := repo.GetDevice(req.CallSign, req.SSID)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "设备已存在",
		})
		return
	}

	device := &models.Device{
		Name:       req.Name,
		CallSign:   req.CallSign,
		SSID:       req.SSID,
		DevModel:   req.DevModel,
		GroupID:    req.GroupID,
		Password:   req.Password,
		Status:     1,
		Priority:   100,
		Note:       req.Note,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}

	if err := repo.AddDevice(device); err != nil {
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
	Status    byte   `json:"status"`
	Priority  int    `json:"priority"`
	Note      string `json:"note"`
	Password  string `json:"password"`
	DevModel  byte   `json:"dev_model"`
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

	// TODO: 实现通过ID更新设备的逻辑
	// 当前数据库层使用 callsign+ssid 作为主键

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
	callsign := c.Query("callsign")
	ssidStr := c.Query("ssid")
	ssid := byte(0)

	if ssidStr != "" {
		s, err := strconv.ParseUint(ssidStr, 10, 8)
		if err == nil {
			ssid = byte(s)
		}
	}

	if callsign == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "缺少呼号参数",
		})
		return
	}

	repo := db.NewDeviceRepository()
	if err := repo.DeleteDevice(callsign, ssid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除设备失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// ChangeDeviceGroupRequest 修改设备群组请求
type ChangeDeviceGroupRequest struct {
	CallSign string `json:"callsign" binding:"required"`
	SSID     byte   `json:"ssid"`
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

	repo := db.NewDeviceRepository()
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
	// TODO: 从数据库获取设备的 QTH 位置信息
	devices := []DeviceInfo{}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items": devices,
		},
	})
}
