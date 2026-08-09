package handler

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"draarl/internal/accesspoint"
	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	appjwt "draarl/pkg/jwt"
)

type deviceAccessPointTokenRequest struct {
	Username       string `json:"username" binding:"required"`
	DevicePassword string `json:"device_password" binding:"required"`
}

type publicAccessPoint struct {
	ID              string    `json:"id"`
	DisplayName     string    `json:"display_name"`
	UDPHost         string    `json:"udp_host"`
	UDPPort         int       `json:"udp_port"`
	Region          string    `json:"region,omitempty"`
	Network         string    `json:"network,omitempty"`
	Priority        int       `json:"priority"`
	HealthySampleAt time.Time `json:"healthy_sample_at"`
}

func IssueDeviceAccessPointToken(c *gin.Context) {
	setNoStore(c)
	settings, err := loadAccessDiscoveryConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取设备接入点配置失败"})
		return
	}
	var req deviceAccessPointTokenRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}
	username := strings.TrimSpace(req.Username)
	result := udphub.AuthenticateDevice(c.ClientIP(), username, req.DevicePassword)
	if result == nil || !result.Success || result.User == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "设备认证失败"})
		return
	}
	ttl := time.Duration(settings.TokenTTLSeconds) * time.Second
	token, expiresAt, err := appjwt.GenerateEdgeDiscoveryToken(result.User.Name, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "签发发现凭证失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "成功", "data": gin.H{"access_token": token, "token_type": "Bearer", "expires_at": expiresAt, "expires_in": int(time.Until(expiresAt).Seconds())}})
}

func ListAccessPoints(c *gin.Context) {
	settings, err := loadAccessDiscoveryConfig(c.Request.Context())
	if err != nil {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取设备接入点配置失败"})
		return
	}
	now := time.Now()
	healthTTL := time.Duration(settings.EdgeHealthTTLSeconds) * time.Second
	items := make([]publicAccessPoint, 0)
	if settings.Center.Enabled {
		if item, ok := centerAccessPoint(
			settings.Center.PublicID,
			settings.Center.DisplayName,
			settings.Center.UDPHost,
			settings.Center.UDPPort,
			settings.Center.Region,
			settings.Center.Network,
			settings.Center.Priority,
			now,
		); ok {
			items = append(items, item)
		}
	}
	nodes, err := gormdb.NewServerRepository().ListDiscoverableNodes()
	if err != nil {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "查询设备接入点失败"})
		return
	}
	if runtime := interconnect.ActiveCenterRuntime(); runtime != nil {
		for _, node := range nodes {
			if node.NodeID == nil {
				continue
			}
			if item, ok := publishedEdgeAccessPoint(node, runtime.Cluster.NodeStatus(*node.NodeID), now, healthTTL); ok {
				items = append(items, item)
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		if items[i].Region != items[j].Region {
			return items[i].Region < items[j].Region
		}
		return items[i].ID < items[j].ID
	})
	maxAge := settings.CacheMaxAgeSeconds
	c.Header("Cache-Control", "private, max-age="+strconv.Itoa(maxAge))
	c.Header("Vary", "Authorization")
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "成功", "data": gin.H{"items": items, "server_time": now, "cache_max_age": maxAge}})
}

func loadAccessDiscoveryConfig(ctx context.Context) (*gormdb.AccessDiscoveryConfig, error) {
	var (
		settings *gormdb.AccessDiscoveryConfig
		err      error
	)
	if configCache := cache.GetConfigCache(); configCache != nil {
		settings, err = configCache.GetAccessDiscoveryConfig(ctx)
	} else {
		settings, err = gormdb.GetSiteConfigRepo().GetAccessDiscoveryConfig()
	}
	if err != nil {
		return nil, err
	}
	return normalizeAccessDiscoveryConfig(settings)
}

func centerAccessPoint(id, displayName, host string, port int, region, network string, priority int, now time.Time) (publicAccessPoint, bool) {
	publicID, err := accesspoint.NormalizePublicID(id)
	if err != nil {
		return publicAccessPoint{}, false
	}
	label, err := accesspoint.NormalizeLabel(displayName, 100)
	if err != nil || label == "" {
		return publicAccessPoint{}, false
	}
	udpHost, err := accesspoint.NormalizeUDPHost(host)
	if err != nil || accesspoint.ValidateUDPPort(port) != nil {
		return publicAccessPoint{}, false
	}
	region, err = accesspoint.NormalizeLabel(region, 100)
	if err != nil {
		return publicAccessPoint{}, false
	}
	network, err = accesspoint.NormalizeLabel(network, 100)
	if err != nil {
		return publicAccessPoint{}, false
	}
	return publicAccessPoint{
		ID: publicID, DisplayName: label, UDPHost: udpHost, UDPPort: port,
		Region: region, Network: network, Priority: priority, HealthySampleAt: now,
	}, true
}

func publishedEdgeAccessPoint(node *gormdb.Server, status interconnect.NodeStatus, now time.Time, healthTTL time.Duration) (publicAccessPoint, bool) {
	if node == nil || node.NodeID == nil || node.PublicAccessID == nil || !node.PublicAccessEnabled || node.Status != 1 || node.NodeRegisteredAt == nil || !status.Online || status.LastHeartbeat == nil || status.LastHeartbeat.After(now.Add(time.Second)) || now.Sub(*status.LastHeartbeat) > healthTTL {
		return publicAccessPoint{}, false
	}
	host, err := accesspoint.NormalizeUDPHost(node.PublicUDPHost)
	if err != nil || accesspoint.ValidateUDPPort(node.PublicUDPPort) != nil {
		return publicAccessPoint{}, false
	}
	publicID, err := accesspoint.NormalizePublicID(*node.PublicAccessID)
	if err != nil {
		return publicAccessPoint{}, false
	}
	displayName, err := accesspoint.NormalizeLabel(node.DisplayName, 100)
	if err != nil || displayName == "" {
		return publicAccessPoint{}, false
	}
	region, err := accesspoint.NormalizeAdministrativeRegion(node.PublicRegion, 100)
	if err != nil {
		return publicAccessPoint{}, false
	}
	network, err := accesspoint.NormalizeLabel(node.PublicNetwork, 100)
	if err != nil {
		return publicAccessPoint{}, false
	}
	return publicAccessPoint{ID: publicID, DisplayName: displayName, UDPHost: host, UDPPort: node.PublicUDPPort, Region: region, Network: network, Priority: node.PublicPriority, HealthySampleAt: *status.LastHeartbeat}, true
}

func setNoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Pragma", "no-cache")
}
