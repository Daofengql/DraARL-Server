package handler

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/broadcast/media"
	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/repository"
	broadcastruntime "draarl/internal/broadcast/runtime"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/udphub"
	jwtutil "draarl/pkg/jwt"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
)

const broadcastAPIE2EEnabledEnv = "DRAARL_BROADCAST_API_E2E"

type broadcastHTTPResult struct {
	Status int
	Body   []byte
}

func TestBroadcastManagementHTTPE2E(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(broadcastAPIE2EEnabledEnv)), "true") {
		t.Skip("set " + broadcastAPIE2EEnabledEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the broadcast API E2E")
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
	if err := db.AutoMigrate(
		&gormdb.User{}, &gormdb.Group{}, &gormdb.GroupLink{}, &gormdb.CommRecord{}, &gormdb.CommRecordDeliveryGroup{},
		&gormdb.ClientResource{}, &gormdb.ClientResourceRelease{}, &gormdb.ClientResourceArtifact{}, &gormdb.FirmwareRelease{},
		&model.BroadcastAudio{}, &model.BroadcastSchedule{},
		&model.VirtualGroupBroadcastPolicy{}, &model.BroadcastRun{},
	); err != nil {
		t.Fatalf("migrate broadcast API E2E tables: %v", err)
	}

	oldConfig := config.Config
	t.Cleanup(func() { config.Config = oldConfig })
	cfg := &config.Configuration{}
	cfg.JWT.Secret = "broadcast-api-e2e-jwt-secret-2026"
	cfg.Broadcast = config.BroadcastConfig{
		Enabled: true, QuietWindowSeconds: 5, MaxAudioDurationSeconds: 30,
		MaxUploadBytes: 2 * 1024 * 1024, TranscodeTimeoutSeconds: 30,
		ScanIntervalMS: 500, RecoveryWindowSeconds: 10, ClaimBatchSize: 20,
		FFmpegPath: "ffmpeg", FFprobePath: "ffprobe",
	}
	cfg.Storage.ActiveProfile = "broadcast-e2e"
	storageRoot := t.TempDir()
	cfg.Storage.Profiles = map[string]config.StorageProfile{
		cfg.Storage.ActiveProfile: {Driver: storage.DriverLocal, Local: config.LocalStorageConfig{RootPath: storageRoot, BaseURL: "/files"}},
	}
	config.Config = cfg
	if err := jwtutil.SetSecret(cfg.JWT.Secret); err != nil {
		t.Fatalf("set JWT secret: %v", err)
	}
	if err := storage.Init(cfg); err != nil {
		t.Fatalf("initialize local storage: %v", err)
	}
	if err := media.InitProcessor(cfg); err != nil {
		t.Fatalf("start media processor: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := media.StopProcessor(ctx); err != nil {
			t.Errorf("stop media processor: %v", err)
		}
	}()

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	newUser := func(prefix, role string) *gormdb.User {
		return &gormdb.User{Name: prefix + "-" + suffix, Email: prefix + "-" + suffix + "@example.invalid", CallSign: strings.ToUpper(prefix) + suffix[len(suffix)-6:], Roles: role, Status: 1, ApprovalStatus: 1}
	}
	owner := newUser("broadcast-owner", "user")
	other := newUser("broadcast-other", "user")
	admin := newUser("broadcast-admin", "admin")
	for _, user := range []*gormdb.User{owner, other, admin} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	groupA := &gormdb.Group{Name: "broadcast-a-" + suffix, Type: groupTypePublic, OwerID: owner.ID, Status: 1}
	groupB := &gormdb.Group{Name: "broadcast-b-" + suffix, Type: groupTypePrivate, OwerID: other.ID, Status: 1}
	virtual := &gormdb.Group{Name: "broadcast-v-" + suffix, Type: groupTypePublic, OwerID: admin.ID, Status: 0, IsVirtual: true}
	for _, group := range []*gormdb.Group{groupA, groupB, virtual} {
		if err := db.Create(group).Error; err != nil {
			t.Fatal(err)
		}
	}

	repo := repository.Default()
	t.Cleanup(func() {
		var audios []model.BroadcastAudio
		_ = db.Where("group_id IN ?", []int{groupA.ID, groupB.ID}).Find(&audios).Error
		for _, audio := range audios {
			_ = storage.Delete(context.Background(), audio.OriginalObjectKey)
			if audio.PlaybackObjectKey != "" {
				_ = storage.Delete(context.Background(), audio.PlaybackObjectKey)
			}
		}
		var recordIDs []uint
		_ = db.Model(&gormdb.CommRecord{}).Where("group_id IN ?", []int{groupA.ID, groupB.ID}).Pluck("id", &recordIDs).Error
		if len(recordIDs) != 0 {
			_ = db.Where("record_id IN ?", recordIDs).Delete(&gormdb.CommRecordDeliveryGroup{}).Error
			_ = db.Where("id IN ?", recordIDs).Delete(&gormdb.CommRecord{}).Error
		}
		_ = db.Where("source_group_id IN ?", []int{groupA.ID, groupB.ID}).Delete(&model.BroadcastRun{}).Error
		_ = db.Where("group_id IN ?", []int{groupA.ID, groupB.ID}).Delete(&model.BroadcastSchedule{}).Error
		_ = db.Where("group_id IN ?", []int{groupA.ID, groupB.ID}).Delete(&model.BroadcastAudio{}).Error
		_ = db.Where("link_group_id = ?", virtual.ID).Delete(&gormdb.GroupLink{}).Error
		_ = db.Delete(&gormdb.Group{}, []int{groupA.ID, groupB.ID, virtual.ID}).Error
		_ = db.Delete(&gormdb.User{}, []int{owner.ID, other.ID, admin.ID}).Error
	})

	wav := makeBroadcastTestWAV(850 * time.Millisecond)
	upload := func(actor *gormdb.User, groupID int, name string) *model.BroadcastAudio {
		t.Helper()
		body, contentType := makeBroadcastMultipart(t, name+".wav", name, wav)
		result := performBroadcastHandlerRequest(t, actor, http.MethodPost, "/groups/:id/broadcast-audios", fmt.Sprintf("/groups/%d/broadcast-audios", groupID), body, contentType, UploadBroadcastAudio)
		requireBroadcastStatus(t, result, http.StatusAccepted)
		var envelope struct {
			Data broadcastAudioResponse `json:"data"`
		}
		if err := json.Unmarshal(result.Body, &envelope); err != nil {
			t.Fatalf("decode upload response: %v", err)
		}
		if bytes.Contains(result.Body, []byte("broadcast-audios/")) {
			t.Fatalf("upload response leaked object key: %s", result.Body)
		}
		return waitBroadcastAudioReady(t, repo, groupID, envelope.Data.ID)
	}

	audioA := upload(owner, groupA.ID, "同音频多时刻")
	audioB := upload(owner, groupA.ID, "不同时刻不同音频")

	forbidden := performBroadcastHandlerRequest(t, other, http.MethodGet, "/groups/:id/broadcast-audios", fmt.Sprintf("/groups/%d/broadcast-audios", groupA.ID), nil, "", ListBroadcastAudios)
	requireBroadcastStatus(t, forbidden, http.StatusForbidden)
	crossGroup := performBroadcastHandlerRequest(t, other, http.MethodGet, "/groups/:id/broadcast-audios/:audioId", fmt.Sprintf("/groups/%d/broadcast-audios/%d", groupB.ID, audioA.ID), nil, "", GetBroadcastAudio)
	requireBroadcastStatus(t, crossGroup, http.StatusNotFound)
	virtualResult := performBroadcastHandlerRequest(t, admin, http.MethodGet, "/groups/:id/broadcast-audios", fmt.Sprintf("/groups/%d/broadcast-audios", virtual.ID), nil, "", ListBroadcastAudios)
	requireBroadcastStatus(t, virtualResult, http.StatusBadRequest)

	detail := performBroadcastHandlerRequest(t, owner, http.MethodGet, "/groups/:id/broadcast-audios/:audioId", fmt.Sprintf("/groups/%d/broadcast-audios/%d", groupA.ID, audioA.ID), nil, "", GetBroadcastAudio)
	requireBroadcastStatus(t, detail, http.StatusOK)
	var detailEnvelope struct {
		Data broadcastAudioResponse `json:"data"`
	}
	if err := json.Unmarshal(detail.Body, &detailEnvelope); err != nil || detailEnvelope.Data.PreviewURL == "" {
		t.Fatalf("audio detail preview missing: err=%v body=%s", err, detail.Body)
	}

	createSchedule := func(audioID uint, name, localTime string) broadcastScheduleResponse {
		t.Helper()
		payload := []byte(fmt.Sprintf(`{"audio_id":%d,"name":%q,"schedule_type":"daily","timezone":"Asia/Shanghai","local_time":%q,"enabled":true}`, audioID, name, localTime))
		result := performBroadcastHandlerRequest(t, owner, http.MethodPost, "/groups/:id/broadcast-schedules", fmt.Sprintf("/groups/%d/broadcast-schedules", groupA.ID), payload, "application/json", CreateBroadcastSchedule)
		requireBroadcastStatus(t, result, http.StatusCreated)
		var envelope struct {
			Data broadcastScheduleResponse `json:"data"`
		}
		if err := json.Unmarshal(result.Body, &envelope); err != nil {
			t.Fatalf("decode schedule response: %v", err)
		}
		if !envelope.Data.EffectiveEnabled || envelope.Data.NextRunAt == nil {
			t.Fatalf("new schedule is not effective: %s", result.Body)
		}
		return envelope.Data
	}
	scheduleA1 := createSchedule(audioA.ID, "音频A上午", "08:00:00")
	scheduleA2 := createSchedule(audioA.ID, "音频A下午", "16:00:00")
	scheduleB := createSchedule(audioB.ID, "音频B夜间", "21:00:00")

	patchBody := []byte(`{"local_time":"09:30:00"}`)
	patched := performBroadcastHandlerRequest(t, owner, http.MethodPatch, "/groups/:id/broadcast-schedules/:scheduleId", fmt.Sprintf("/groups/%d/broadcast-schedules/%d", groupA.ID, scheduleA1.ID), patchBody, "application/json", UpdateBroadcastSchedule)
	requireBroadcastStatus(t, patched, http.StatusOK)
	persistedA1, _ := repo.GetSchedule(context.Background(), groupA.ID, scheduleA1.ID)
	persistedA2, _ := repo.GetSchedule(context.Background(), groupA.ID, scheduleA2.ID)
	persistedB, _ := repo.GetSchedule(context.Background(), groupA.ID, scheduleB.ID)
	if persistedA1 == nil || persistedA2 == nil || persistedB == nil || persistedA1.LocalTime != "09:30:00" || persistedA2.LocalTime != "16:00:00" || persistedB.LocalTime != "21:00:00" || persistedA2.AudioID != audioA.ID || persistedB.AudioID != audioB.ID {
		t.Fatalf("schedule independence lost: A1=%+v A2=%+v B=%+v", persistedA1, persistedA2, persistedB)
	}

	scheduleList := performBroadcastHandlerRequest(t, owner, http.MethodGet, "/groups/:id/broadcast-schedules", fmt.Sprintf("/groups/%d/broadcast-schedules", groupA.ID), nil, "", ListBroadcastSchedules)
	requireBroadcastStatus(t, scheduleList, http.StatusOK)
	var scheduleListEnvelope struct {
		Data struct {
			Items []broadcastScheduleResponse `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(scheduleList.Body, &scheduleListEnvelope); err != nil || len(scheduleListEnvelope.Data.Items) != 3 {
		t.Fatalf("schedule list mismatch: err=%v body=%s", err, scheduleList.Body)
	}

	audioList := performBroadcastHandlerRequest(t, owner, http.MethodGet, "/groups/:id/broadcast-audios", fmt.Sprintf("/groups/%d/broadcast-audios", groupA.ID), nil, "", ListBroadcastAudios)
	requireBroadcastStatus(t, audioList, http.StatusOK)
	var audioListEnvelope struct {
		Data struct {
			Items []broadcastAudioResponse `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(audioList.Body, &audioListEnvelope); err != nil {
		t.Fatal(err)
	}
	counts := make(map[uint]int64)
	for _, item := range audioListEnvelope.Data.Items {
		counts[item.ID] = item.ScheduleCount
	}
	if counts[audioA.ID] != 2 || counts[audioB.ID] != 1 {
		t.Fatalf("audio schedule counts=%v want A=2 B=1", counts)
	}

	udphub.RefreshGroupCache()
	udphub.RefreshGroupLinkCache()
	udphub.ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))
	if err := broadcastruntime.Init(cfg); err != nil {
		t.Fatalf("start broadcast scheduler: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := broadcastruntime.Stop(ctx); err != nil {
			t.Errorf("stop broadcast scheduler: %v", err)
		}
	}()

	trigger := func(scheduleID uint) model.BroadcastRun {
		t.Helper()
		result := performBroadcastHandlerRequest(t, owner, http.MethodPost, "/groups/:id/broadcast-schedules/:scheduleId/run", fmt.Sprintf("/groups/%d/broadcast-schedules/%d/run", groupA.ID, scheduleID), nil, "", RunBroadcastSchedule)
		requireBroadcastStatus(t, result, http.StatusAccepted)
		var envelope struct {
			Data model.BroadcastRun `json:"data"`
		}
		if err := json.Unmarshal(result.Body, &envelope); err != nil || envelope.Data.ID == 0 {
			t.Fatalf("decode manual run: err=%v body=%s", err, result.Body)
		}
		return envelope.Data
	}
	cancelledRun := trigger(scheduleA1.ID)
	cancelResult := performBroadcastHandlerRequest(t, owner, http.MethodPost, "/groups/:id/broadcast-runs/:runId/cancel", fmt.Sprintf("/groups/%d/broadcast-runs/%d/cancel", groupA.ID, cancelledRun.ID), nil, "", CancelBroadcastRun)
	requireBroadcastStatus(t, cancelResult, http.StatusAccepted)
	cancelledRun = waitBroadcastRunTerminal(t, repo, groupA.ID, cancelledRun.ID)
	if cancelledRun.Status != model.RunStatusCancelled || cancelledRun.ErrorCode != "manual_stop" || cancelledRun.SentPackets > 1 {
		t.Fatalf("cancelled manual run=%#v", cancelledRun)
	}
	crossCancel := performBroadcastHandlerRequest(t, other, http.MethodPost, "/groups/:id/broadcast-runs/:runId/cancel", fmt.Sprintf("/groups/%d/broadcast-runs/%d/cancel", groupB.ID, cancelledRun.ID), nil, "", CancelBroadcastRun)
	requireBroadcastStatus(t, crossCancel, http.StatusNotFound)

	udphub.ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))
	succeededRun := trigger(scheduleA2.ID)
	succeededRun = waitBroadcastRunTerminal(t, repo, groupA.ID, succeededRun.ID)
	if succeededRun.Status != model.RunStatusSucceeded || succeededRun.SentPackets != audioA.PacketCount || succeededRun.PlayedDurationMS != audioA.DurationMS {
		t.Fatalf("successful manual run=%#v audio=%#v", succeededRun, audioA)
	}
	var automaticRecords []gormdb.CommRecord
	if err := db.Where("group_id = ? AND is_auto_broadcast = ?", groupA.ID, true).Order("id ASC").Find(&automaticRecords).Error; err != nil {
		t.Fatal(err)
	}
	wantRecords := 1
	if cancelledRun.SentPackets > 0 {
		wantRecords++
	}
	if len(automaticRecords) != wantRecords {
		t.Fatalf("automatic communication records=%d want=%d", len(automaticRecords), wantRecords)
	}
	for _, record := range automaticRecords {
		if record.AudioPath != "" || record.AudioSize != 0 || !record.IsAutoBroadcast || record.SenderUsername != "system-broadcast" || record.SenderCallSign != "AUTO" {
			t.Fatalf("unexpected automatic communication record: %#v", record)
		}
		var deliveryCount int64
		if err := db.Model(&gormdb.CommRecordDeliveryGroup{}).Where("record_id = ? AND group_id = ?", record.ID, groupA.ID).Count(&deliveryCount).Error; err != nil || deliveryCount != 1 {
			t.Fatalf("automatic record delivery snapshot count=%d err=%v", deliveryCount, err)
		}
	}
	if _, err := os.Stat(filepath.Join(storageRoot, "comm-records")); !os.IsNotExist(err) {
		t.Fatalf("automatic broadcast created a communication recording directory: err=%v", err)
	}
	afterManual, err := repo.GetSchedule(context.Background(), groupA.ID, scheduleA2.ID)
	if err != nil || afterManual.NextRunAt == nil || persistedA2.NextRunAt == nil || !afterManual.NextRunAt.Equal(*persistedA2.NextRunAt) {
		t.Fatalf("manual run changed future schedule: before=%v after=%#v err=%v", persistedA2.NextRunAt, afterManual, err)
	}
	forbiddenRun := performBroadcastHandlerRequest(t, other, http.MethodPost, "/groups/:id/broadcast-schedules/:scheduleId/run", fmt.Sprintf("/groups/%d/broadcast-schedules/%d/run", groupA.ID, scheduleA1.ID), nil, "", RunBroadcastSchedule)
	requireBroadcastStatus(t, forbiddenRun, http.StatusForbidden)

	deleteInUse := performBroadcastHandlerRequest(t, owner, http.MethodDelete, "/groups/:id/broadcast-audios/:audioId", fmt.Sprintf("/groups/%d/broadcast-audios/%d", groupA.ID, audioA.ID), nil, "", DeleteBroadcastAudio)
	requireBroadcastStatus(t, deleteInUse, http.StatusConflict)

	unused := upload(owner, groupA.ID, "待删除音频")
	unusedOriginal, unusedPlayback := unused.OriginalObjectKey, unused.PlaybackObjectKey
	deletedUnused := performBroadcastHandlerRequest(t, owner, http.MethodDelete, "/groups/:id/broadcast-audios/:audioId", fmt.Sprintf("/groups/%d/broadcast-audios/%d", groupA.ID, unused.ID), nil, "", DeleteBroadcastAudio)
	requireBroadcastStatus(t, deletedUnused, http.StatusOK)
	if _, _, err := storage.Stat(context.Background(), unusedOriginal); err == nil {
		t.Fatal("deleted audio original object still exists")
	}
	if _, _, err := storage.Stat(context.Background(), unusedPlayback); err == nil {
		t.Fatal("deleted audio playback object still exists")
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	run := &model.BroadcastRun{ScheduleID: scheduleA1.ID, AudioID: audioA.ID, SourceGroupID: groupA.ID, ScheduledFor: now, Status: model.RunStatusSucceeded}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	runList := performBroadcastHandlerRequest(t, owner, http.MethodGet, "/groups/:id/broadcast-runs", fmt.Sprintf("/groups/%d/broadcast-runs?page=1&page_size=10", groupA.ID), nil, "", ListBroadcastRuns)
	requireBroadcastStatus(t, runList, http.StatusOK)
	var runEnvelope struct {
		Data struct {
			Total int64                `json:"total"`
			Items []model.BroadcastRun `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(runList.Body, &runEnvelope); err != nil || runEnvelope.Data.Total < 3 || len(runEnvelope.Data.Items) < 3 {
		t.Fatalf("run history mismatch: err=%v body=%s", err, runList.Body)
	}
	foundStatic := false
	for _, item := range runEnvelope.Data.Items {
		if item.ID == run.ID {
			foundStatic = true
			break
		}
	}
	if !foundStatic {
		t.Fatalf("run history omitted inserted run %d: %s", run.ID, runList.Body)
	}

	references, err := gormdb.ManagedStorageReferences()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{audioA.OriginalObjectKey, audioA.PlaybackObjectKey, audioB.OriginalObjectKey, audioB.PlaybackObjectKey} {
		if _, ok := references["broadcast-audios/"][key]; !ok {
			t.Fatalf("storage audit reference missing %q", key)
		}
	}
}

func performBroadcastHandlerRequest(t *testing.T, actor *gormdb.User, method, pattern, requestPath string, body []byte, contentType string, handler gin.HandlerFunc) broadcastHTTPResult {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, pattern, func(c *gin.Context) {
		c.Set("user", actor)
		handler(c)
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, requestPath, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	router.ServeHTTP(response, request)
	return broadcastHTTPResult{Status: response.Code, Body: response.Body.Bytes()}
}

func requireBroadcastStatus(t *testing.T, result broadcastHTTPResult, want int) {
	t.Helper()
	if result.Status != want {
		t.Fatalf("status=%d want=%d body=%s", result.Status, want, result.Body)
	}
}

func makeBroadcastMultipart(t *testing.T, filename, name string, data []byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	header.Set("Content-Type", "audio/wav")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("name", name); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func waitBroadcastAudioReady(t *testing.T, repo *repository.Repository, groupID int, audioID uint) *model.BroadcastAudio {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		audio, err := repo.GetAudio(context.Background(), groupID, audioID)
		if err != nil {
			t.Fatal(err)
		}
		switch audio.Status {
		case model.AudioStatusReady:
			return audio
		case model.AudioStatusFailed:
			t.Fatalf("audio processing failed: %s", audio.ErrorMessage)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("audio %d did not become ready", audioID)
	return nil
}

func waitBroadcastRunTerminal(t *testing.T, repo *repository.Repository, groupID int, runID uint) model.BroadcastRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err := repo.GetRun(context.Background(), groupID, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != model.RunStatusClaimed && run.Status != model.RunStatusPlaying {
			return *run
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("broadcast run %d did not finish", runID)
	return model.BroadcastRun{}
}

func makeBroadcastTestWAV(duration time.Duration) []byte {
	const sampleRate = 16000
	sampleCount := int(duration * sampleRate / time.Second)
	dataSize := sampleCount * 2
	result := make([]byte, 44+dataSize)
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(36+dataSize))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], 1)
	binary.LittleEndian.PutUint32(result[24:28], sampleRate)
	binary.LittleEndian.PutUint32(result[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(result[32:34], 2)
	binary.LittleEndian.PutUint16(result[34:36], 16)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], uint32(dataSize))
	for i := 0; i < sampleCount; i++ {
		value := int16(12000)
		if (i/80)%2 == 1 {
			value = -value
		}
		binary.LittleEndian.PutUint16(result[44+i*2:46+i*2], uint16(value))
	}
	return result
}
