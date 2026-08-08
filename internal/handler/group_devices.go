package handler

import (
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/models"
	"draarl/internal/routesync"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"log"
	"net/http"
	"strconv"
)

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
