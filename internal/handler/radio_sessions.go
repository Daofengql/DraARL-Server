package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"
	"draarl/internal/groupaccess"
	oplog "draarl/internal/log"
	"draarl/internal/models"

	"github.com/gin-gonic/gin"
)

type RadioSessionResponse struct {
	SessionID        string                 `json:"session_id"`
	ClientInstanceID string                 `json:"client_instance_id,omitempty"`
	Platform         uint8                  `json:"dev_model"`
	SSID             uint8                  `json:"ssid"`
	Transport        ghostsession.Transport `json:"transport"`
	ProtocolVersion  uint16                 `json:"protocol_version"`
	Capabilities     []string               `json:"capabilities"`
	OnlineSince      string                 `json:"online_since"`
	LastActivity     string                 `json:"last_activity"`
	TxGroupID        int                    `json:"tx_group_id"`
	RxGroupIDs       []int                  `json:"rx_group_ids"`
	DisableSend      bool                   `json:"disable_send"`
	DisableRecv      bool                   `json:"disable_recv"`
}

type UpdateSessionRoutingRequest struct {
	TxGroupID  int   `json:"tx_group_id" binding:"required"`
	RxGroupIDs []int `json:"rx_group_ids" binding:"required"`
}

type AdminRadioSessionResponse struct {
	SessionID          string                 `json:"session_id"`
	ClientInstanceHint string                 `json:"client_instance_hint"`
	OwnerID            int                    `json:"owner_id"`
	Username           string                 `json:"username"`
	CallSign           string                 `json:"callsign"`
	Platform           uint8                  `json:"dev_model"`
	SSID               uint8                  `json:"ssid"`
	Transport          ghostsession.Transport `json:"transport"`
	OnlineSince        string                 `json:"online_since"`
	LastActivity       string                 `json:"last_activity"`
	TxGroupID          int                    `json:"tx_group_id"`
	RxGroupIDs         []int                  `json:"rx_group_ids"`
	DisableSend        bool                   `json:"disable_send"`
	DisableRecv        bool                   `json:"disable_recv"`
}

var (
	errRoutingGroupNotFound  = errors.New("one or more groups do not exist")
	errRoutingGroupForbidden = errors.New("one or more groups are not accessible")
	errAmbiguousSession      = errors.New("multiple matching ghost sessions are online")
)

func toRadioSessionResponse(session ghostsession.Session) RadioSessionResponse {
	return RadioSessionResponse{
		SessionID: session.SessionID, ClientInstanceID: session.ClientInstanceID,
		Platform: session.DevModel, SSID: session.SSID, Transport: session.Transport,
		ProtocolVersion: session.ProtocolVersion, Capabilities: append([]string(nil), session.Capabilities...),
		OnlineSince: session.CreatedAt.UTC().Format(time.RFC3339Nano), LastActivity: session.LastActivity.UTC().Format(time.RFC3339Nano),
		TxGroupID: session.TxGroupID, RxGroupIDs: append([]int(nil), session.RxGroupIDs...),
		DisableSend: session.DisableSend, DisableRecv: session.DisableRecv,
	}
}

func GetRadioSessions(c *gin.Context) {
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	sessions := ghostsession.Global.ListOwner(currentUser.ID)
	result := make([]RadioSessionResponse, len(sessions))
	for i, session := range sessions {
		result[i] = toRadioSessionResponse(session)
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "成功", "data": result})
}

func adminRadioSessionResponse(session ghostsession.Session) AdminRadioSessionResponse {
	hint := ghostsession.ShortID(session.ClientInstanceID)
	return AdminRadioSessionResponse{
		SessionID: session.SessionID, ClientInstanceHint: hint, OwnerID: session.OwnerID,
		Username: session.Username, CallSign: session.CallSign,
		Platform: session.DevModel, SSID: session.SSID, Transport: session.Transport,
		OnlineSince:  session.CreatedAt.UTC().Format(time.RFC3339Nano),
		LastActivity: session.LastActivity.UTC().Format(time.RFC3339Nano),
		TxGroupID:    session.TxGroupID, RxGroupIDs: append([]int(nil), session.RxGroupIDs...),
		DisableSend: session.DisableSend, DisableRecv: session.DisableRecv,
	}
}

func AdminGetRadioSessions(c *gin.Context) {
	sessions := ghostsession.Global.List()
	result := make([]AdminRadioSessionResponse, len(sessions))
	for i := range sessions {
		result[i] = adminRadioSessionResponse(sessions[i])
	}
	c.Header("Cache-Control", "no-store, max-age=0")
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "成功", "data": result})
}

func AdminDeleteRadioSession(c *gin.Context) {
	actor, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if err := ghostsession.Global.Disconnect(sessionID, "admin_disconnected_session"); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "error": "session_not_found", "message": "会话不存在"})
		return
	}
	oplog.AddLog("管理员断开幽灵会话: "+ghostsession.ShortID(sessionID), "ghost_session_disconnect", actor.ID, actor.Name, actor.CallSign, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "会话已断开"})
}

func ownerPlatformSessions(ownerID int, devModel uint8) []ghostsession.Session {
	all := ghostsession.Global.ListOwner(ownerID)
	result := make([]ghostsession.Session, 0, len(all))
	for _, session := range all {
		if session.DevModel == devModel {
			result = append(result, session)
		}
	}
	return result
}

func resolveOwnedPlatformSession(ownerID int, devModel uint8, sessionID string) (ghostsession.Session, bool, error) {
	if sessionID != "" {
		session, exists := ghostsession.Global.Get(sessionID)
		if !exists || session.OwnerID != ownerID || session.DevModel != devModel {
			return ghostsession.Session{}, false, ghostsession.ErrSessionNotFound
		}
		return session, true, nil
	}
	sessions := ownerPlatformSessions(ownerID, devModel)
	switch len(sessions) {
	case 0:
		return ghostsession.Session{}, false, nil
	case 1:
		return sessions[0], true, nil
	default:
		return ghostsession.Session{}, false, errAmbiguousSession
	}
}

func validateSessionRoutingAccess(user *gormdb.User, routing ghostsession.Routing) (ghostsession.Routing, error) {
	normalized, err := ghostsession.NormalizeRouting(routing, ghostsession.MaxSubscriptions())
	if err != nil {
		return ghostsession.Routing{}, err
	}
	groupIDs := normalized.RxGroupIDs
	var groups []*gormdb.Group
	if err := gormdb.Get().Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
		return ghostsession.Routing{}, err
	}
	if len(groups) != len(groupIDs) {
		return ghostsession.Routing{}, errRoutingGroupNotFound
	}
	viewable, err := groupaccess.ViewableGroupIDs(gormdb.Get(), user, groupIDs)
	if err != nil {
		return ghostsession.Routing{}, err
	}
	if len(viewable) != len(groupIDs) {
		return ghostsession.Routing{}, errRoutingGroupForbidden
	}
	return normalized, nil
}

func persistSessionRouting(session ghostsession.Session, routing ghostsession.Routing) error {
	return gormdb.NewGhostClientPreferenceRepository().ReplaceRouting(
		session.OwnerID, session.DevModel, session.ClientInstanceID, routing.TxGroupID, routing.RxGroupIDs,
	)
}

func reconcileGhostSessionsForGroup(groupID int) {
	owners := make(map[int]struct{})
	for _, session := range ghostsession.Global.ListByGroup(groupID) {
		owners[session.OwnerID] = struct{}{}
	}
	for ownerID := range owners {
		reconcileOwnerGhostSessions(ownerID)
	}
}

func reconcileOwnerGhostSessions(ownerID int) {
	user, err := gormdb.NewUserRepository().GetUserByID(ownerID)
	if err != nil || user == nil || user.Status != 1 {
		for _, session := range ghostsession.Global.ListOwner(ownerID) {
			_ = ghostsession.Global.DisconnectOwned(ownerID, session.SessionID, "user_or_permissions_unavailable")
		}
		return
	}
	for _, session := range ghostsession.Global.ListOwner(ownerID) {
		routing, changed, err := groupaccess.SanitizeRouting(
			gormdb.Get(), user, session.Routing(), models.GroupIDPublicMin, ghostsession.MaxSubscriptions(),
		)
		if err == nil && !changed {
			continue
		}
		if err == nil {
			_, err = ghostsession.Global.UpdateRoutingPersisted(session.SessionID, routing, func(current ghostsession.Session, next ghostsession.Routing) error {
				return persistSessionRouting(current, next)
			})
		}
		if err != nil {
			log.Printf("[RADIO] disconnecting session after routing reconciliation failure session=%s err=%v", ghostsession.ShortID(session.SessionID), err)
			_ = ghostsession.Global.DisconnectOwned(ownerID, session.SessionID, "routing_permission_revoked")
		}
	}
}

func updateOwnedSessionRouting(user *gormdb.User, sessionID string, requested ghostsession.Routing) (ghostsession.Session, error) {
	session, exists := ghostsession.Global.Get(sessionID)
	if !exists || user == nil || session.OwnerID != user.ID {
		return ghostsession.Session{}, ghostsession.ErrSessionNotFound
	}
	policyRouting, err := ghostsession.Global.ValidateRoutingForSession(sessionID, requested)
	if err != nil {
		return ghostsession.Session{}, err
	}
	if policyRouting.TxGroupID != session.TxGroupID && ghostsession.Global.IsPTTActive(sessionID, time.Now()) {
		return ghostsession.Session{}, ghostsession.ErrPTTActive
	}
	routing, err := validateSessionRoutingAccess(user, policyRouting)
	if err != nil {
		return ghostsession.Session{}, err
	}
	return ghostsession.Global.UpdateRoutingPersisted(sessionID, routing, func(current ghostsession.Session, next ghostsession.Routing) error {
		if err := persistSessionRouting(current, next); err != nil {
			log.Printf("[RADIO] persist routing failed session=%s err=%v", ghostsession.ShortID(sessionID), err)
			return err
		}
		return nil
	})
}

func UpdateRadioSessionRouting(c *gin.Context) {
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	var request UpdateSessionRoutingRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的会话路由"})
		return
	}
	updated, err := updateOwnedSessionRouting(currentUser, c.Param("session_id"), ghostsession.Routing{
		TxGroupID: request.TxGroupID, RxGroupIDs: request.RxGroupIDs,
	})
	if err != nil {
		writeSessionRoutingError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "会话路由已更新", "data": toRadioSessionResponse(updated)})
}

func writeSessionRoutingError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "routing_update_failed"
	switch {
	case errors.Is(err, ghostsession.ErrInvalidRouting):
		status, code = http.StatusBadRequest, "invalid_routing"
	case errors.Is(err, ghostsession.ErrSessionNotFound):
		status, code = http.StatusNotFound, "session_not_found"
	case errors.Is(err, errAmbiguousSession):
		status, code = http.StatusConflict, "ambiguous_session"
	case errors.Is(err, ghostsession.ErrSubscriptionLimit):
		status, code = http.StatusUnprocessableEntity, "subscription_limit"
	case errors.Is(err, ghostsession.ErrPTTActive):
		status, code = http.StatusConflict, "ptt_active"
	case errors.Is(err, errRoutingGroupNotFound):
		status, code = http.StatusNotFound, "group_not_found"
	case errors.Is(err, errRoutingGroupForbidden):
		status, code = http.StatusForbidden, "group_forbidden"
	}
	message := err.Error()
	if status == http.StatusInternalServerError {
		message = "更新会话路由失败"
	}
	c.JSON(status, gin.H{"code": status, "error": code, "message": message})
}

func DeleteRadioSession(c *gin.Context) {
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	if err := ghostsession.Global.DisconnectOwned(currentUser.ID, c.Param("session_id"), "user_disconnected_session"); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "error": "session_not_found", "message": "会话不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "会话已断开"})
}
