package handler

import (
	"net/http"

	"draarl/internal/buildinfo"
	"draarl/internal/gormdb"
	"draarl/internal/udphub"

	"github.com/gin-gonic/gin"
)

func GetTotalStats(c *gin.Context) {
	userRepo := gormdb.NewUserRepository()
	deviceRepo := gormdb.NewDeviceRepository()
	groupRepo := gormdb.NewGroupRepository()

	// 获取真实统计数据
	userCount, _ := userRepo.UserCount()
	devCount, _ := deviceRepo.DeviceCount()
	groupCount, _ := groupRepo.GroupCount()
	onlineCount, _ := deviceRepo.OnlineDeviceCount()
	// Ghost devices are runtime sessions and intentionally do not occupy a
	// persistent row in devices. Add their live session count to the existing
	// physical-device database count without changing the entity-device rule.
	onlineCount += int64(udphub.GetOnlineGhostCount())

	stats := TotalStats{
		TotalDevices:  devCount,
		OnlineDevices: onlineCount,
		TotalUsers:    userCount,
		TotalGroups:   groupCount,
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    stats,
	})
}

// UpdateUserPasswordRequest 修改密码请求

func GetPlatformInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"name":     "DraARL 麟链",
			"logourl":  "",
			"language": "zh-CN",
			"version":  buildinfo.VersionString(),
			"icp":      "",
			"mail":     "",
			"callsign": "",
		},
	})
}

// TotalStats 统计信息
