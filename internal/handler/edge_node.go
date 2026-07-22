package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
	oplog "draarl/internal/log"
	"draarl/internal/udphub"
)

type createEdgeNodeRequest struct {
	DisplayName string `json:"display_name" binding:"required"`
	Note        string `json:"note"`
}

type updateEdgeNodeRequest struct {
	DisplayName *string `json:"display_name"`
	Note        *string `json:"note"`
	Status      *int    `json:"status"`
}

type edgeNodeView struct {
	ID                       int                     `json:"id"`
	NodeID                   string                  `json:"node_id"`
	DisplayName              string                  `json:"display_name"`
	Note                     string                  `json:"note"`
	Status                   int                     `json:"status"`
	RegisteredAt             *time.Time              `json:"registered_at,omitempty"`
	RegistrationExpiresAt    *time.Time              `json:"registration_expires_at,omitempty"`
	CredentialEpoch          uint32                  `json:"credential_epoch"`
	LastSeenAt               *time.Time              `json:"last_seen_at,omitempty"`
	PersistedConnectionCount int                     `json:"persisted_connection_count"`
	Runtime                  interconnect.NodeStatus `json:"runtime"`
	CreateTime               time.Time               `json:"create_time"`
	UpdateTime               time.Time               `json:"update_time"`
}

func edgeNodeToView(node *gormdb.Server) edgeNodeView {
	view := edgeNodeView{ID: node.ID, DisplayName: node.DisplayName, Note: node.Note, Status: node.Status, RegisteredAt: node.NodeRegisteredAt, RegistrationExpiresAt: node.NodeRegistrationExpiresAt, CredentialEpoch: node.NodeCredentialEpoch, LastSeenAt: node.NodeLastSeenAt, PersistedConnectionCount: node.NodeConnectionCount, CreateTime: node.CreateTime, UpdateTime: node.UpdateTime}
	if node.NodeID != nil {
		view.NodeID = *node.NodeID
	}
	if runtime := interconnect.ActiveCenterRuntime(); runtime != nil && view.NodeID != "" {
		view.Runtime = runtime.Cluster.NodeStatus(view.NodeID)
	} else {
		view.Runtime.NodeID = view.NodeID
	}
	return view
}

func ListEdgeNodes(c *gin.Context) {
	nodes, err := gormdb.NewServerRepository().ListServers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "查询边缘节点失败"})
		return
	}
	items := make([]edgeNodeView, 0, len(nodes))
	for _, node := range nodes {
		if node.NodeID != nil {
			items = append(items, edgeNodeToView(node))
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "成功", "data": gin.H{"items": items}})
}

func CreateEdgeNode(c *gin.Context) {
	var req createEdgeNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.DisplayName) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "节点昵称不能为空"})
		return
	}
	nodeID, err := interconnect.NewNodeID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成节点身份失败"})
		return
	}
	credential, err := interconnect.NewRegistrationCredential(nodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成注册凭据失败"})
		return
	}
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)
	if cfg := config.TryGet(); cfg != nil && cfg.Interconnect.RegistrationTokenTTL > 0 {
		expiresAt = now.Add(time.Duration(cfg.Interconnect.RegistrationTokenTTL) * time.Second)
	}
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	node := &gormdb.Server{Name: strings.TrimSpace(req.DisplayName), DisplayName: strings.TrimSpace(req.DisplayName), Note: strings.TrimSpace(req.Note), NodeID: &nodeID, NodeRegistrationTokenHash: interconnect.HashCredential(credential), NodeRegistrationExpiresAt: &expiresAt, Status: 1, ServerType: 3, OwerID: strconv.Itoa(currentUser.ID), OwerCallSign: currentUser.CallSign}
	if err := gormdb.NewServerRepository().CreateServer(node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建边缘节点失败"})
		return
	}
	oplog.AddLog("创建边缘节点: "+nodeID, "edge_node_create", currentUser.ID, currentUser.Name, currentUser.CallSign, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "注册凭据仅显示一次", "data": gin.H{"node": edgeNodeToView(node), "registration_token": credential}})
}

func UpdateEdgeNode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效节点ID"})
		return
	}
	var req updateEdgeNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}
	updates := make(map[string]interface{})
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "节点昵称不能为空"})
			return
		}
		updates["display_name"], updates["name"] = name, name
	}
	if req.Note != nil {
		updates["note"] = strings.TrimSpace(*req.Note)
	}
	if req.Status != nil {
		if *req.Status != 0 && *req.Status != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "节点状态只能为0或1"})
			return
		}
		updates["status"] = *req.Status
	}
	repo := gormdb.NewServerRepository()
	node, err := repo.GetServerByID(id)
	if err != nil || node == nil || node.NodeID == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "边缘节点不存在"})
		return
	}
	if len(updates) > 0 {
		if err := repo.UpdateServerFields(id, updates); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新边缘节点失败"})
			return
		}
	}
	if req.Status != nil && *req.Status == 0 {
		if runtime := interconnect.ActiveCenterRuntime(); runtime != nil {
			runtime.Control.Disconnect(*node.NodeID)
		}
		clearEdgeNodeDeviceEntries(repo, *node.NodeID)
	}
	currentUser, _ := requireCurrentUser(c)
	if currentUser != nil {
		oplog.AddLog("更新边缘节点: "+*node.NodeID, "edge_node_update", currentUser.ID, currentUser.Name, currentUser.CallSign, c.ClientIP())
	}
	updated, _ := repo.GetServerByID(id)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功", "data": edgeNodeToView(updated)})
}

func RotateEdgeNodeCredential(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效节点ID"})
		return
	}
	repo := gormdb.NewServerRepository()
	node, err := repo.GetServerByID(id)
	if err != nil || node == nil || node.NodeID == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "边缘节点不存在"})
		return
	}
	credential, err := interconnect.NewRegistrationCredential(*node.NodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成注册凭据失败"})
		return
	}
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)
	if cfg := config.TryGet(); cfg != nil && cfg.Interconnect.RegistrationTokenTTL > 0 {
		expiresAt = now.Add(time.Duration(cfg.Interconnect.RegistrationTokenTTL) * time.Second)
	}
	if err := repo.UpdateServerFields(id, map[string]interface{}{"node_token_hash": "", "node_registration_token_hash": interconnect.HashCredential(credential), "node_registration_expires_at": expiresAt}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "轮换节点凭据失败"})
		return
	}
	if runtime := interconnect.ActiveCenterRuntime(); runtime != nil {
		runtime.Control.Disconnect(*node.NodeID)
	}
	clearEdgeNodeDeviceEntries(repo, *node.NodeID)
	currentUser, _ := requireCurrentUser(c)
	if currentUser != nil {
		oplog.AddLog("轮换边缘节点凭据: "+*node.NodeID, "edge_node_rotate", currentUser.ID, currentUser.Name, currentUser.CallSign, c.ClientIP())
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "注册凭据仅显示一次", "data": gin.H{"registration_token": credential, "expires_at": expiresAt}})
}

func clearEdgeNodeDeviceEntries(repo *gormdb.ServerRepository, nodeID string) {
	affected, err := repo.ClearCurrentEntryForNode(nodeID)
	if err != nil {
		return
	}
	for _, device := range affected {
		udphub.ClearRuntimeDeviceEntryIfNode(device.ID, nodeID)
	}
}
