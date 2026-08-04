package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"draarl/internal/gormdb"
	minio_local "draarl/pkg/minio"

	"github.com/gin-gonic/gin"
)

type MessageSenderResponse struct {
	UserID   *uint  `json:"user_id"`
	Username string `json:"username"`
	CallSign string `json:"callsign"`
	Nickname string `json:"nickname"`
	SSID     uint8  `json:"ssid"`
	DevModel int    `json:"dev_model"`
	IsGhost  bool   `json:"is_ghost"`
}

type MessageResponse struct {
	ID               uint                  `json:"id"`
	MessageType      string                `json:"message_type"`
	SourceGroupID    uint                  `json:"source_group_id"`
	SourceGroupName  string                `json:"source_group_name"`
	RequestedGroupID uint                  `json:"requested_group_id"`
	Sender           MessageSenderResponse `json:"sender"`
	SentAt           string                `json:"sent_at"`
	EndTime          string                `json:"end_time,omitempty"`
	DurationMs       int                   `json:"duration_ms"`
	TextContent      string                `json:"text_content,omitempty"`
	AudioURL         string                `json:"audio_url,omitempty"`
	AudioSize        int64                 `json:"audio_size,omitempty"`
	AudioFormat      string                `json:"audio_format,omitempty"`
	Status           int                   `json:"status"`
}

type messageCursorPayload struct {
	StartTime string `json:"start_time"`
	ID        uint   `json:"id"`
}

func encodeMessageCursor(startTime time.Time, id uint) (string, error) {
	payload, err := json.Marshal(messageCursorPayload{StartTime: startTime.UTC().Format(time.RFC3339Nano), ID: id})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeMessageCursor(encoded string) (time.Time, uint, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("decode cursor: %w", err)
	}
	var payload messageCursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return time.Time{}, 0, fmt.Errorf("parse cursor: %w", err)
	}
	startTime, err := time.Parse(time.RFC3339Nano, payload.StartTime)
	if err != nil || payload.ID == 0 {
		return time.Time{}, 0, fmt.Errorf("invalid cursor boundary")
	}
	return startTime, payload.ID, nil
}

func parseMessageType(value string) (*uint8, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return nil, nil
	case "voice":
		value := gormdb.CommMessageTypeVoice
		return &value, nil
	case "text":
		value := gormdb.CommMessageTypeText
		return &value, nil
	default:
		return nil, fmt.Errorf("unsupported message type")
	}
}

func requireMessageScope(c *gin.Context) (uint, []int, bool) {
	groupParam := c.Param("group_id")
	if groupParam == "" {
		groupParam = c.Param("id")
	}
	groupID, err := strconv.ParseUint(groupParam, 10, 32)
	if err != nil || groupID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的群组ID"})
		return 0, nil, false
	}
	group, err := gormdb.NewGroupRepository().GetGroupByID(int(groupID))
	if err != nil {
		log.Printf("[MESSAGES] 查询群组失败 group=%d err=%v", groupID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "查询群组失败"})
		return 0, nil, false
	}
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "群组不存在"})
		return 0, nil, false
	}
	if _, allowed := requireGroupViewAccess(c, group); !allowed {
		return 0, nil, false
	}
	groupIDs, err := gormdb.NewMessageRepository().VisibleGroupIDs(int(groupID))
	if err != nil {
		log.Printf("[MESSAGES] 查询互联群组失败 group=%d err=%v", groupID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "查询互联群组失败"})
		return 0, nil, false
	}
	return uint(groupID), groupIDs, true
}

func effectiveMessageType(record *gormdb.MessageRecord) uint8 {
	if record.MessageType == gormdb.CommMessageTypeText || strings.HasPrefix(record.AudioPath, "text:") {
		return gormdb.CommMessageTypeText
	}
	return gormdb.CommMessageTypeVoice
}

func toMessageResponse(record *gormdb.MessageRecord, requestedGroupID uint) MessageResponse {
	messageType := effectiveMessageType(record)
	hasSenderSnapshot := record.SenderUsername != "" || record.SenderCallSign != "" ||
		record.SenderNickname != "" || record.SenderDevModel != 0
	username := record.SenderUsername
	if username == "" {
		username = record.CurrentUsername
	}
	callSign := record.SenderCallSign
	if callSign == "" {
		callSign = record.CurrentCallSign
	}
	nickname := record.SenderNickname
	if nickname == "" {
		nickname = record.CurrentNickname
	}
	if nickname == "" {
		nickname = callSign
	}
	devModel := record.SenderDevModel
	if !hasSenderSnapshot {
		devModel = record.CurrentDevModel
	}

	response := MessageResponse{
		ID:               record.ID,
		SourceGroupID:    record.SourceGroupID,
		SourceGroupName:  record.SourceGroupName,
		RequestedGroupID: requestedGroupID,
		Sender: MessageSenderResponse{
			UserID: record.UserID, Username: username, CallSign: callSign, Nickname: nickname,
			SSID: record.DeviceSSID, DevModel: devModel, IsGhost: record.DeviceID == 0,
		},
		SentAt:     record.StartTime.UTC().Format(time.RFC3339Nano),
		DurationMs: record.DurationMs,
		Status:     record.Status,
	}
	if !record.EndTime.IsZero() {
		response.EndTime = record.EndTime.UTC().Format(time.RFC3339Nano)
	}
	if messageType == gormdb.CommMessageTypeText {
		response.MessageType = "text"
		response.TextContent = record.TextContent
		if response.TextContent == "" && strings.HasPrefix(record.AudioPath, "text:") {
			response.TextContent = strings.TrimPrefix(record.AudioPath, "text:")
		}
	} else {
		response.MessageType = "voice"
		response.AudioSize = record.AudioSize
		if record.AudioPath != "" {
			response.AudioURL = minio_local.GetFileURL(record.AudioPath)
			response.AudioFormat = "draarl-opus-raw"
		}
	}
	return response
}

func GetGroupMessages(c *gin.Context) {
	requestedGroupID, groupIDs, ok := requireMessageScope(c)
	if !ok {
		return
	}
	messageType, err := parseMessageType(c.DefaultQuery("message_type", "all"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的消息类型"})
		return
	}
	limit := 50
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 || parsed > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "limit必须在1到100之间"})
			return
		}
		limit = parsed
	}

	query := gormdb.MessageQuery{GroupIDs: groupIDs, Type: messageType, Limit: limit}
	if encodedCursor := c.Query("cursor"); encodedCursor != "" {
		beforeTime, beforeID, err := decodeMessageCursor(encodedCursor)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的消息游标"})
			return
		}
		query.BeforeTime = &beforeTime
		query.BeforeID = beforeID
	}

	records, hasMore, err := gormdb.NewMessageRepository().List(query)
	if err != nil {
		log.Printf("[MESSAGES] 查询消息列表失败 group=%d err=%v", requestedGroupID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "查询消息失败"})
		return
	}
	messages := make([]MessageResponse, len(records))
	for i := range records {
		messages[i] = toMessageResponse(&records[i], requestedGroupID)
	}
	nextCursor := ""
	if hasMore && len(records) > 0 {
		nextCursor, err = encodeMessageCursor(records[len(records)-1].StartTime, records[len(records)-1].ID)
		if err != nil {
			log.Printf("[MESSAGES] 生成消息游标失败 group=%d err=%v", requestedGroupID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "生成消息游标失败"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK, "message": "成功",
		"data": gin.H{
			"messages": messages, "next_cursor": nextCursor, "has_more": hasMore,
			"server_time": time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
}

func GetGroupMessage(c *gin.Context) {
	requestedGroupID, groupIDs, ok := requireMessageScope(c)
	if !ok {
		return
	}
	messageID, err := strconv.ParseUint(c.Param("message_id"), 10, 32)
	if err != nil || messageID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的消息ID"})
		return
	}
	record, err := gormdb.NewMessageRepository().GetByID(uint(messageID), groupIDs)
	if err != nil {
		log.Printf("[MESSAGES] 查询消息详情失败 group=%d message=%d err=%v", requestedGroupID, messageID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "查询消息失败"})
		return
	}
	if record == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "消息不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "成功", "data": toMessageResponse(record, requestedGroupID)})
}
