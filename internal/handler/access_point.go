package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"draarl/internal/accesspoint"
	"draarl/internal/gormdb"
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
	var req deviceAccessPointTokenRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}
	username := strings.TrimSpace(req.Username)
	result := udphub.AuthenticateDevice(c.ClientIP(), username, req.DevicePassword)
	if result == nil || !result.Success || result.User == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "设备认证失败"})
		return
	}
	ttl := time.Duration(gormdb.AccessDiscoveryTokenTTLSeconds) * time.Second
	token, expiresAt, err := appjwt.GenerateDeviceDiscoveryToken(result.User.ID, result.User.Name, result.User.SessionVersion, ttl)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "签发发现凭证失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "成功", "data": gin.H{"access_token": token, "token_type": "Bearer", "expires_at": expiresAt, "expires_in": int(time.Until(expiresAt).Seconds())}})
}

func ListAccessPoints(c *gin.Context) {
	settings, err := accessDiscoveryForRequest(c)
	if err != nil {
		c.Header("Cache-Control", "no-store")
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "读取设备接入点配置失败"})
		return
	}
	now := time.Now()
	items := make([]publicAccessPoint, 0)
	site, err := gormdb.GetSiteConfigRepo().WithContext(c.Request.Context()).GetSystemInfoConfig()
	if err != nil {
		setNoStore(c)
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "读取站点名称失败"})
		return
	}
	if item, ok := localAccessPoint(settings.NodeID, site.Name, settings.UDPHost, settings.UDPPort, now); ok {
		items = append(items, item)
	}
	maxAge := gormdb.AccessDiscoveryCacheMaxAgeSeconds
	c.Header("Cache-Control", "private, max-age="+strconv.Itoa(maxAge))
	c.Header("Vary", "Authorization")
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "成功", "data": gin.H{"items": items, "server_time": now, "cache_max_age": maxAge}})
}

func accessDiscoveryForRequest(c *gin.Context) (*gormdb.AccessDiscoveryConfig, error) {
	settings, err := loadAccessDiscoveryConfig(c.Request.Context())
	if err != nil {
		return nil, err
	}
	return defaultAccessDiscoveryHost(settings, c.Request.Host)
}

// Resolve the default at request time, so different HTTP API hostnames never
// share a cached host. Explicit public/NAT endpoints always take precedence.
func defaultAccessDiscoveryHost(settings *gormdb.AccessDiscoveryConfig, requestHost string) (*gormdb.AccessDiscoveryConfig, error) {
	result := *settings
	if result.UDPHost == "" {
		apiURL := url.URL{Host: requestHost}
		host, err := accesspoint.NormalizeUDPHost(apiURL.Hostname())
		if err != nil {
			return nil, err
		}
		result.UDPHost = host
	}
	return &result, nil
}

func loadAccessDiscoveryConfig(ctx context.Context) (*gormdb.AccessDiscoveryConfig, error) {
	var (
		settings *gormdb.AccessDiscoveryConfig
		err      error
	)
	if configCache := cache.GetConfigCache(); configCache != nil {
		settings, err = configCache.GetAccessDiscoveryConfig(ctx)
	} else {
		settings, err = gormdb.GetSiteConfigRepo().WithContext(ctx).GetAccessDiscoveryConfig()
	}
	if err != nil {
		return nil, err
	}
	return normalizeAccessDiscoveryConfig(settings)
}

func setNoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Pragma", "no-cache")
}

func localAccessPoint(id, displayName, host string, port int, now time.Time) (publicAccessPoint, bool) {
	publicID, err := accesspoint.NormalizePublicID(id)
	if err != nil {
		return publicAccessPoint{}, false
	}
	// Site names are independently validated by the system settings API.
	label := strings.TrimSpace(displayName)
	if label == "" {
		label = publicID
	}
	udpHost, err := accesspoint.NormalizeUDPHost(host)
	if err != nil || accesspoint.ValidateUDPPort(port) != nil {
		return publicAccessPoint{}, false
	}
	// These compatibility fields no longer participate in node selection.
	return publicAccessPoint{ID: publicID, DisplayName: label, UDPHost: udpHost, UDPPort: port, Priority: 100, HealthySampleAt: now}, true
}
