package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"

	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
)

const messagesE2EEnabledEnv = "DRAARL_MESSAGES_E2E"

type messagesE2EListEnvelope struct {
	Data struct {
		Messages   []MessageResponse `json:"messages"`
		NextCursor string            `json:"next_cursor"`
		HasMore    bool              `json:"has_more"`
		ServerTime string            `json:"server_time"`
	} `json:"data"`
}

func TestGroupMessagesHTTPE2E(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(messagesE2EEnabledEnv)), "true") {
		t.Skip("set " + messagesE2EEnabledEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the HTTP E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("DRAARL_TEST_MYSQL_DSN is required")
	}
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 10, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatalf("initialize MySQL: %v", err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	db := gormdb.Get()
	if err := db.AutoMigrate(&gormdb.User{}, &gormdb.Group{}, &gormdb.Device{}, &gormdb.GroupMember{}, &gormdb.GroupLink{}, &gormdb.CommRecord{}, &gormdb.CommRecordDeliveryGroup{}); err != nil {
		t.Fatalf("migrate messages E2E tables: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	viewer := &gormdb.User{Name: "message-viewer-" + suffix, Email: "message-viewer-" + suffix + "@example.invalid", CallSign: "MV" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	sender := &gormdb.User{Name: "message-sender-current-" + suffix, Email: "message-sender-" + suffix + "@example.invalid", CallSign: "MS" + suffix[len(suffix)-8:], NickName: "Current Sender", Roles: "user", Status: 1, ApprovalStatus: 1}
	for _, user := range []*gormdb.User{viewer, sender} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}

	publicGroup := &gormdb.Group{Name: "message-public-" + suffix, Type: groupTypePublic, OwerID: viewer.ID, Status: 1}
	privateGroup := &gormdb.Group{Name: "message-private-" + suffix, Type: groupTypePrivate, OwerID: sender.ID, Status: 1}
	relatedGroup := &gormdb.Group{Name: "message-related-" + suffix, Type: groupTypePublic, OwerID: sender.ID, Status: 1}
	unrelatedGroup := &gormdb.Group{Name: "message-unrelated-" + suffix, Type: groupTypePublic, OwerID: sender.ID, Status: 1}
	linkGroup := &gormdb.Group{Name: "message-link-" + suffix, Type: groupTypePublic, OwerID: viewer.ID, Status: 1, IsVirtual: true}
	groups := []*gormdb.Group{publicGroup, privateGroup, relatedGroup, unrelatedGroup, linkGroup}
	for _, group := range groups {
		if err := db.Create(group).Error; err != nil {
			t.Fatal(err)
		}
	}

	publicID := uint(publicGroup.ID)
	privateID := uint(privateGroup.ID)
	relatedID := uint(relatedGroup.ID)
	unrelatedID := uint(unrelatedGroup.ID)
	senderID := uint(sender.ID)
	baseTime := time.Now().UTC().Truncate(time.Millisecond)
	newRecord := func(groupID *uint, messageType uint8, text string, at time.Time, status int) *gormdb.CommRecord {
		return &gormdb.CommRecord{
			DeviceID: 0, DeviceSSID: 101, GroupID: groupID, UserID: &senderID,
			StartTime: at, EndTime: at, Status: status, MessageType: messageType, TextContent: text,
			SenderUsername: "message-sender-at-send-time", SenderCallSign: "BG7OLD",
			SenderNickname: "Historical Sender", SenderDevModel: 101,
		}
	}
	oldest := newRecord(&publicID, gormdb.CommMessageTypeText, "oldest", baseTime.Add(-time.Second), 2)
	sameTimeText := newRecord(&publicID, gormdb.CommMessageTypeText, "same-time-text", baseTime, 2)
	sameTimeVoice := newRecord(&publicID, gormdb.CommMessageTypeVoice, "", baseTime, 2)
	sameTimeVoice.UserID = nil
	sameTimeVoice.DeviceSSID = 255
	sameTimeVoice.SenderUsername = "system-broadcast"
	sameTimeVoice.SenderCallSign = "AUTO"
	sameTimeVoice.SenderNickname = "自动播报"
	sameTimeVoice.SenderDevModel = 0
	sameTimeVoice.IsAutoBroadcast = true
	incomplete := newRecord(&publicID, gormdb.CommMessageTypeText, "incomplete", baseTime.Add(time.Second), 0)
	privateMessage := newRecord(&privateID, gormdb.CommMessageTypeText, "private", baseTime, 2)
	unrelatedMessage := newRecord(&unrelatedID, gormdb.CommMessageTypeText, "unrelated", baseTime, 2)
	initialRecords := []*gormdb.CommRecord{oldest, sameTimeText, sameTimeVoice, incomplete, privateMessage, unrelatedMessage}
	if err := gormdb.CreateCommRecordsWithDeliveryGroups(db, initialRecords, 100); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		groupIDs := []int{publicGroup.ID, privateGroup.ID, relatedGroup.ID, unrelatedGroup.ID}
		_ = db.Where("group_id IN ?", groupIDs).Delete(&gormdb.CommRecord{}).Error
		_ = db.Where("link_group_id = ?", linkGroup.ID).Delete(&gormdb.GroupLink{}).Error
		_ = db.Where("group_id IN ?", groupIDs).Delete(&gormdb.GroupMember{}).Error
		ids := make([]int, len(groups))
		for i, group := range groups {
			ids[i] = group.ID
		}
		_ = db.Delete(&gormdb.Group{}, ids).Error
		_ = db.Delete(&gormdb.User{}, []int{viewer.ID, sender.ID}).Error
	})

	request := func(name, pattern, path string, handler gin.HandlerFunc, want int) []byte {
		t.Helper()
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.GET(pattern, func(c *gin.Context) {
			c.Set("user", viewer)
			handler(c)
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want {
			t.Fatalf("%s status=%d want=%d body=%s", name, response.Code, want, response.Body.String())
		}
		return response.Body.Bytes()
	}
	listPattern := "/api/groups/:id/messages"
	detailPattern := "/api/groups/:id/messages/:message_id"
	listPath := fmt.Sprintf("/api/groups/%d/messages", publicGroup.ID)

	body := request("first cursor page", listPattern, listPath+"?limit=1", GetGroupMessages, http.StatusOK)
	var firstPage messagesE2EListEnvelope
	if err := json.Unmarshal(body, &firstPage); err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Data.Messages) != 1 || firstPage.Data.Messages[0].ID != sameTimeVoice.ID || !firstPage.Data.HasMore || firstPage.Data.NextCursor == "" || firstPage.Data.ServerTime == "" {
		t.Fatalf("unexpected first page: %s", body)
	}
	if firstPage.Data.Messages[0].Sender.Username != "system-broadcast" || firstPage.Data.Messages[0].Sender.CallSign != "AUTO" || !firstPage.Data.Messages[0].IsAutoBroadcast {
		t.Fatalf("sender snapshot was not used: %#v", firstPage.Data.Messages[0].Sender)
	}

	newer := newRecord(&publicID, gormdb.CommMessageTypeText, "inserted-after-page-one", baseTime.Add(2*time.Second), 2)
	if err := gormdb.CreateCommRecordsWithDeliveryGroups(db, []*gormdb.CommRecord{newer}, 1); err != nil {
		t.Fatal(err)
	}
	body = request("second cursor page", listPattern, listPath+"?limit=1&cursor="+firstPage.Data.NextCursor, GetGroupMessages, http.StatusOK)
	var secondPage messagesE2EListEnvelope
	if err := json.Unmarshal(body, &secondPage); err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Data.Messages) != 1 || secondPage.Data.Messages[0].ID != sameTimeText.ID {
		t.Fatalf("same-timestamp cursor boundary was unstable: %s", body)
	}
	if secondPage.Data.Messages[0].ID == newer.ID || secondPage.Data.Messages[0].ID == firstPage.Data.Messages[0].ID {
		t.Fatalf("cursor page included a new or duplicate message: %s", body)
	}

	body = request("text filter", listPattern, listPath+"?message_type=text", GetGroupMessages, http.StatusOK)
	var textPage messagesE2EListEnvelope
	if err := json.Unmarshal(body, &textPage); err != nil {
		t.Fatal(err)
	}
	for _, message := range textPage.Data.Messages {
		if message.MessageType != "text" {
			t.Fatalf("text filter returned %#v", message)
		}
	}
	body = request("voice filter", listPattern, listPath+"?message_type=voice", GetGroupMessages, http.StatusOK)
	var voicePage messagesE2EListEnvelope
	if err := json.Unmarshal(body, &voicePage); err != nil {
		t.Fatal(err)
	}
	if len(voicePage.Data.Messages) != 1 || voicePage.Data.Messages[0].ID != sameTimeVoice.ID || voicePage.Data.Messages[0].MessageType != "voice" {
		t.Fatalf("unexpected voice filter response: %s", body)
	}

	request("public message detail", detailPattern, fmt.Sprintf("/api/groups/%d/messages/%d", publicGroup.ID, sameTimeText.ID), GetGroupMessage, http.StatusOK)
	request("cross-group detail", detailPattern, fmt.Sprintf("/api/groups/%d/messages/%d", publicGroup.ID, unrelatedMessage.ID), GetGroupMessage, http.StatusNotFound)
	request("invalid cursor", listPattern, listPath+"?cursor=invalid!", GetGroupMessages, http.StatusBadRequest)
	request("invalid type", listPattern, listPath+"?message_type=image", GetGroupMessages, http.StatusBadRequest)

	privatePath := fmt.Sprintf("/api/groups/%d/messages", privateGroup.ID)
	request("private non-member", listPattern, privatePath, GetGroupMessages, http.StatusForbidden)
	membership := &gormdb.GroupMember{GroupID: privateGroup.ID, UserID: viewer.ID, IsVerified: true}
	if err := db.Create(membership).Error; err != nil {
		t.Fatal(err)
	}
	request("private verified member", listPattern, privatePath, GetGroupMessages, http.StatusOK)
	if err := db.Delete(membership).Error; err != nil {
		t.Fatal(err)
	}
	request("private removed member", listPattern, privatePath, GetGroupMessages, http.StatusForbidden)

	links := []*gormdb.GroupLink{
		{LinkGroupID: linkGroup.ID, TargetGroupID: publicGroup.ID},
		{LinkGroupID: linkGroup.ID, TargetGroupID: relatedGroup.ID},
	}
	if err := db.Create(links).Error; err != nil {
		t.Fatal(err)
	}
	relatedMessage := newRecord(&relatedID, gormdb.CommMessageTypeText, "related", baseTime.Add(3*time.Second), 2)
	relatedMessage.DeliveryGroupIDs = []uint{publicID, relatedID}
	if err := gormdb.CreateCommRecordsWithDeliveryGroups(db, []*gormdb.CommRecord{relatedMessage}, 1); err != nil {
		t.Fatal(err)
	}
	body = request("linked group history", listPattern, listPath, GetGroupMessages, http.StatusOK)
	var linkedPage messagesE2EListEnvelope
	if err := json.Unmarshal(body, &linkedPage); err != nil {
		t.Fatal(err)
	}
	foundRelated := false
	for _, message := range linkedPage.Data.Messages {
		if message.ID == relatedMessage.ID {
			foundRelated = message.SourceGroupID == relatedID && message.RequestedGroupID == publicID
		}
	}
	if !foundRelated {
		t.Fatalf("linked message source/requested group metadata missing: %s", body)
	}
	if err := db.Model(linkGroup).Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	body = request("disabled link historical snapshot", listPattern, listPath, GetGroupMessages, http.StatusOK)
	if !strings.Contains(string(body), fmt.Sprintf(`"id":%d`, relatedMessage.ID)) {
		t.Fatalf("delivery snapshot did not preserve related history: %s", body)
	}
}
