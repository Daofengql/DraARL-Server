package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"
	"draarl/internal/protocol"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestRadioSessionListAndDeleteAreOwnerScoped(t *testing.T) {
	previousGlobal := ghostsession.Global
	ghostsession.Global = ghostsession.NewRegistry(8, 16)
	t.Cleanup(func() { ghostsession.Global = previousGlobal })
	owner := &gormdb.User{ID: 7, Name: "owner"}
	other := &gormdb.User{ID: 8, Name: "other"}
	disconnected := false
	register := func(user *gormdb.User, controller ghostsession.Controller) ghostsession.Session {
		t.Helper()
		session, err := ghostsession.Global.Register(ghostsession.Registration{
			ClientInstanceID: uuid.NewString(), OwnerID: user.ID, Username: user.Name,
			DevModel: protocol.DraARLDevModelBrowser, SSID: protocol.SSIDGhostWeb,
			Transport: ghostsession.TransportWebSocket,
			Routing:   ghostsession.Routing{TxGroupID: 1001, RxGroupIDs: []int{1001}},
		}, controller)
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	owned := register(owner, ghostsession.Controller{Disconnect: func(string) { disconnected = true }})
	foreign := register(other, ghostsession.Controller{})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user", owner) })
	router.GET("/api/radio/sessions", GetRadioSessions)
	router.DELETE("/api/radio/sessions/:session_id", DeleteRadioSession)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/radio/sessions", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data []RadioSessionResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Data[0].SessionID != owned.SessionID {
		t.Fatalf("owner-scoped sessions=%#v", envelope.Data)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/radio/sessions/"+foreign.SessionID, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign delete status=%d body=%s", response.Code, response.Body.String())
	}
	if _, exists := ghostsession.Global.Get(foreign.SessionID); !exists {
		t.Fatal("foreign session was deleted")
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/radio/sessions/"+owned.SessionID, nil))
	if response.Code != http.StatusOK || !disconnected {
		t.Fatalf("owned delete status=%d disconnected=%v body=%s", response.Code, disconnected, response.Body.String())
	}
	if _, exists := ghostsession.Global.Get(owned.SessionID); exists {
		t.Fatal("owned session survived delete")
	}
}

func TestAdminRadioSessionListIsRedactedAndCanDisconnectAnyOwner(t *testing.T) {
	previousGlobal := ghostsession.Global
	ghostsession.Global = ghostsession.NewRegistry(8, 16)
	t.Cleanup(func() { ghostsession.Global = previousGlobal })
	admin := &gormdb.User{ID: 1, Name: "admin", CallSign: "ADMIN", Roles: "admin"}
	instanceID := uuid.NewString()
	disconnected := false
	session, err := ghostsession.Global.Register(ghostsession.Registration{
		ClientInstanceID: instanceID, OwnerID: 42, Username: "remote-user", CallSign: "BG7TEST",
		DevModel: protocol.DraARLDevModelAndroid, SSID: protocol.SSIDGhostAndroid,
		Transport: ghostsession.TransportUDP, Endpoint: "203.0.113.7:60050",
		Routing: ghostsession.Routing{TxGroupID: 1001, RxGroupIDs: []int{1001, 1002}},
	}, ghostsession.Controller{Disconnect: func(string) { disconnected = true }})
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user", admin) })
	router.GET("/api/admin/radio/sessions", AdminGetRadioSessions)
	router.DELETE("/api/admin/radio/sessions/:session_id", AdminDeleteRadioSession)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/radio/sessions", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("admin list status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data []AdminRadioSessionResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Data[0].SessionID != session.SessionID || envelope.Data[0].OwnerID != 42 {
		t.Fatalf("admin sessions=%#v", envelope.Data)
	}
	if envelope.Data[0].ClientInstanceHint != instanceID[:8] {
		t.Fatalf("instance hint=%q", envelope.Data[0].ClientInstanceHint)
	}
	body := response.Body.String()
	if strings.Contains(body, instanceID) || strings.Contains(body, "203.0.113.7") || strings.Contains(body, "session_tag") {
		t.Fatalf("admin session response leaked sensitive fields: %s", body)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/admin/radio/sessions/"+session.SessionID, nil))
	if response.Code != http.StatusOK || !disconnected {
		t.Fatalf("admin delete status=%d disconnected=%v body=%s", response.Code, disconnected, response.Body.String())
	}
}
