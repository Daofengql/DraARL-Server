package handler

import (
	"encoding/base64"
	"testing"
	"time"

	"draarl/internal/gormdb"
)

func TestMessageCursorRoundTrip(t *testing.T) {
	wantTime := time.Date(2026, 8, 5, 12, 34, 56, 789123000, time.FixedZone("test", 8*60*60))
	wantID := uint(12345)
	cursor, err := encodeMessageCursor(wantTime, wantID)
	if err != nil {
		t.Fatal(err)
	}
	gotTime, gotID, err := decodeMessageCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(wantTime) || gotID != wantID {
		t.Fatalf("cursor boundary=(%s,%d) want=(%s,%d)", gotTime, gotID, wantTime, wantID)
	}
}

func TestMessageCursorRejectsMalformedBoundaries(t *testing.T) {
	tests := []string{
		"not-base64!",
		base64.RawURLEncoding.EncodeToString([]byte(`{}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"start_time":"2026-08-05T00:00:00Z","id":0}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"start_time":"not-a-time","id":1}`)),
	}
	for _, cursor := range tests {
		if _, _, err := decodeMessageCursor(cursor); err == nil {
			t.Fatalf("decodeMessageCursor(%q) unexpectedly succeeded", cursor)
		}
	}
}

func TestParseMessageType(t *testing.T) {
	all, err := parseMessageType("all")
	if err != nil || all != nil {
		t.Fatalf("all filter=%v err=%v", all, err)
	}
	textType, err := parseMessageType("TEXT")
	if err != nil || textType == nil || *textType != gormdb.CommMessageTypeText {
		t.Fatalf("text filter=%v err=%v", textType, err)
	}
	if _, err := parseMessageType("image"); err == nil {
		t.Fatal("unsupported message type was accepted")
	}
}

func TestMessageResponsePrefersSenderSnapshot(t *testing.T) {
	userID := uint(7)
	record := &gormdb.MessageRecord{
		ID: 99, DeviceID: 0, DeviceSSID: 101, SourceGroupID: 3, SourceGroupName: "source",
		UserID: &userID, StartTime: time.Date(2026, 8, 5, 1, 2, 3, 0, time.UTC),
		MessageType: gormdb.CommMessageTypeText, TextContent: "snapshot text", Status: 2,
		SenderUsername: "sender-at-send-time", SenderCallSign: "BG7OLD", SenderNickname: "Old Nick", SenderDevModel: 101,
		CurrentUsername: "renamed-user", CurrentCallSign: "BG7NEW", CurrentNickname: "New Nick", CurrentDevModel: 105,
	}
	response := toMessageResponse(record, 8)
	if response.MessageType != "text" || response.TextContent != "snapshot text" {
		t.Fatalf("unexpected text response: %#v", response)
	}
	if response.Sender.Username != "sender-at-send-time" || response.Sender.CallSign != "BG7OLD" || response.Sender.Nickname != "Old Nick" {
		t.Fatalf("response did not preserve sender snapshot: %#v", response.Sender)
	}
	if response.Sender.DevModel != 101 || !response.Sender.IsGhost || response.RequestedGroupID != 8 || response.SourceGroupID != 3 {
		t.Fatalf("unexpected message routing metadata: %#v", response)
	}
}

func TestMessageResponseReadsLegacyTextPrefix(t *testing.T) {
	record := &gormdb.MessageRecord{
		ID: 1, DeviceID: 1, SourceGroupID: 2, StartTime: time.Now(),
		MessageType: gormdb.CommMessageTypeVoice, AudioPath: "text:legacy", Status: 2,
	}
	response := toMessageResponse(record, 2)
	if response.MessageType != "text" || response.TextContent != "legacy" || response.AudioURL != "" {
		t.Fatalf("legacy text response=%#v", response)
	}
}

func TestMessageResponsePreservesUnknownSnapshotDeviceModel(t *testing.T) {
	record := &gormdb.MessageRecord{
		ID: 2, DeviceID: 7, SourceGroupID: 2, StartTime: time.Now(), Status: 2,
		SenderUsername: "sender-at-send-time", SenderDevModel: 0, CurrentDevModel: 105,
	}
	response := toMessageResponse(record, 2)
	if response.Sender.DevModel != 0 {
		t.Fatalf("unknown sender device model changed to current model: %#v", response.Sender)
	}
}
