package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"draarl/internal/broadcast/media"
	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/repository"
	broadcastruntime "draarl/internal/broadcast/runtime"
	schedruntime "draarl/internal/broadcast/scheduler/runtime"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	broadcastMultipartOverhead = int64(1 << 20)
	broadcastPreviewExpiry     = 10 * time.Minute
)

type broadcastAudioResponse struct {
	ID               uint      `json:"id"`
	GroupID          int       `json:"group_id"`
	Name             string    `json:"name"`
	OriginalMIMEType string    `json:"original_mime_type"`
	OriginalSize     int64     `json:"original_size"`
	PlaybackSize     int64     `json:"playback_size"`
	DurationMS       int       `json:"duration_ms"`
	PacketCount      int       `json:"packet_count"`
	SHA256           string    `json:"sha256"`
	Status           string    `json:"status"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	ScheduleCount    int64     `json:"schedule_count"`
	PreviewURL       string    `json:"preview_url,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type broadcastScheduleResponse struct {
	model.BroadcastSchedule
	EffectiveEnabled bool `json:"effective_enabled"`
}

type broadcastScheduleRequest struct {
	AudioID      *uint      `json:"audio_id"`
	Name         *string    `json:"name"`
	ScheduleType *string    `json:"schedule_type"`
	Timezone     *string    `json:"timezone"`
	ScheduledAt  *time.Time `json:"scheduled_at"`
	LocalTime    *string    `json:"local_time"`
	WeekdayMask  *uint8     `json:"weekday_mask"`
	Enabled      *bool      `json:"enabled"`
}

func ListBroadcastAudios(c *gin.Context) {
	_, _, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	repo := repository.Default()
	audios, err := repo.ListAudios(c.Request.Context(), groupID)
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_audio_list_failed", "读取播报音频失败")
		return
	}
	counts, err := repo.AudioScheduleCounts(c.Request.Context(), groupID)
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_audio_list_failed", "读取音频引用失败")
		return
	}
	items := make([]broadcastAudioResponse, 0, len(audios))
	for i := range audios {
		items = append(items, makeBroadcastAudioResponse(&audios[i], counts[audios[i].ID], ""))
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "成功", "data": gin.H{"items": items}})
}

func UploadBroadcastAudio(c *gin.Context) {
	user, group, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	cfg := config.TryGet()
	if cfg == nil || !cfg.Broadcast.Enabled {
		writeBroadcastError(c, http.StatusServiceUnavailable, "broadcast_disabled", "站点未启用自动播报")
		return
	}
	if !storage.IsEnabled() {
		writeBroadcastError(c, http.StatusServiceUnavailable, "storage_unavailable", "存储服务不可用")
		return
	}
	maxBytes := cfg.Broadcast.MaxUploadBytes
	if maxBytes <= 0 {
		maxBytes = config.DefaultBroadcastMaxUploadBytes
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+broadcastMultipartOverhead)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_audio_file_required", "请选择音频文件")
		return
	}
	if fileHeader.Size <= 0 || fileHeader.Size > maxBytes {
		writeBroadcastError(c, http.StatusRequestEntityTooLarge, "broadcast_audio_too_large", "音频文件大小超出限制")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_audio_invalid", "无法读取音频文件")
		return
	}
	defer file.Close()
	prefix := make([]byte, 512)
	n, readErr := io.ReadFull(file, prefix)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_audio_invalid", "无法读取音频文件")
		return
	}
	extension, mediaType, err := media.ValidateUploadHeader(fileHeader.Filename, fileHeader.Header.Get("Content-Type"), prefix[:n])
	if err != nil {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_audio_invalid", "音频扩展名、MIME 或文件内容不匹配")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_audio_invalid", "无法读取音频文件")
		return
	}
	displayName := strings.TrimSpace(c.PostForm("name"))
	if displayName == "" {
		displayName = path.Base(strings.ReplaceAll(fileHeader.Filename, "\\", "/"))
	}
	if displayName == "" || utf8.RuneCountInString(displayName) > 255 {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_audio_name_invalid", "音频名称不能为空且不能超过 255 个字符")
		return
	}
	objectKey := path.Join("broadcast-audios", strconv.Itoa(groupID), uuid.NewString(), "original"+extension)
	hasher := sha256.New()
	counter := &broadcastCountingReader{reader: io.TeeReader(io.LimitReader(file, maxBytes+1), hasher)}
	if err := storage.Put(c.Request.Context(), objectKey, counter, fileHeader.Size, mediaType); err != nil {
		writeBroadcastError(c, http.StatusServiceUnavailable, "broadcast_audio_store_failed", "保存音频文件失败")
		return
	}
	if counter.count != fileHeader.Size || counter.count > maxBytes {
		_ = storage.Delete(context.WithoutCancel(c.Request.Context()), objectKey)
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_audio_size_mismatch", "音频文件大小校验失败")
		return
	}
	audio := &model.BroadcastAudio{
		GroupID: groupID, Name: displayName, OriginalObjectKey: objectKey,
		OriginalMIMEType: mediaType, OriginalSize: counter.count,
		SHA256: hex.EncodeToString(hasher.Sum(nil)), Status: model.AudioStatusProcessing, CreatedBy: user.ID,
	}
	repo := repository.Default()
	if err := repo.CreateAudio(c.Request.Context(), audio); err != nil {
		_ = storage.Delete(context.WithoutCancel(c.Request.Context()), objectKey)
		writeBroadcastRepositoryError(c, err)
		return
	}
	if err := media.Enqueue(audio.ID); err != nil {
		_, _, _ = repo.DeleteAudio(context.WithoutCancel(c.Request.Context()), groupID, audio.ID)
		_ = storage.Delete(context.WithoutCancel(c.Request.Context()), objectKey)
		writeBroadcastError(c, http.StatusServiceUnavailable, "broadcast_media_queue_unavailable", "音频处理服务暂不可用")
		return
	}
	oplog.AddLog(fmt.Sprintf("上传自动播报音频: %s (群组: %s, ID: %d)", audio.Name, group.Name, audio.ID), "broadcast_audio_upload", user.ID, user.Name, user.CallSign, c.ClientIP())
	c.JSON(http.StatusAccepted, gin.H{"code": http.StatusAccepted, "message": "音频已进入处理队列", "data": makeBroadcastAudioResponse(audio, 0, "")})
}

func GetBroadcastAudio(c *gin.Context) {
	_, _, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	audioID, ok := parseBroadcastUintParam(c, "audioId")
	if !ok {
		return
	}
	repo := repository.Default()
	audio, err := repo.GetAudio(c.Request.Context(), groupID, audioID)
	if err != nil {
		writeBroadcastRepositoryError(c, err)
		return
	}
	counts, err := repo.AudioScheduleCounts(c.Request.Context(), groupID)
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_audio_read_failed", "读取音频详情失败")
		return
	}
	previewURL, err := storage.ReadURL(c.Request.Context(), audio.OriginalObjectKey, broadcastPreviewExpiry)
	if err != nil {
		writeBroadcastError(c, http.StatusServiceUnavailable, "broadcast_audio_preview_unavailable", "生成试听地址失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "成功", "data": makeBroadcastAudioResponse(audio, counts[audio.ID], previewURL)})
}

func DeleteBroadcastAudio(c *gin.Context) {
	user, group, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	audioID, ok := parseBroadcastUintParam(c, "audioId")
	if !ok {
		return
	}
	originalKey, playbackKey, err := repository.Default().DeleteAudio(c.Request.Context(), groupID, audioID)
	if err != nil {
		writeBroadcastRepositoryError(c, err)
		return
	}
	cleanupPending := false
	for _, key := range []string{originalKey, playbackKey} {
		if key == "" {
			continue
		}
		if err := storage.Delete(context.WithoutCancel(c.Request.Context()), key); err != nil {
			cleanupPending = true
			log.Printf("[BROADCAST] cleanup deleted audio object key=%s failed: %v", key, err)
		}
	}
	oplog.AddLog(fmt.Sprintf("删除自动播报音频: ID=%d (群组: %s)", audioID, group.Name), "broadcast_audio_delete", user.ID, user.Name, user.CallSign, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "播报音频已删除", "data": gin.H{"cleanup_pending": cleanupPending}})
}

func ListBroadcastSchedules(c *gin.Context) {
	_, _, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	schedules, err := repository.Default().ListSchedules(c.Request.Context(), groupID)
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_schedule_list_failed", "读取播报计划失败")
		return
	}
	items := make([]broadcastScheduleResponse, 0, len(schedules))
	for i := range schedules {
		items = append(items, makeBroadcastScheduleResponse(&schedules[i]))
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "成功", "data": gin.H{"items": items}})
}

func CreateBroadcastSchedule(c *gin.Context) {
	user, group, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	var req broadcastScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_schedule_invalid", "播报计划参数无效")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	schedule := &model.BroadcastSchedule{GroupID: groupID, Enabled: enabled, CreatedBy: user.ID, UpdatedBy: user.ID}
	applyBroadcastScheduleRequest(schedule, &req)
	if err := repository.Default().SaveSchedule(c.Request.Context(), schedule, time.Now().UTC()); err != nil {
		writeBroadcastRepositoryError(c, err)
		return
	}
	oplog.AddLog(fmt.Sprintf("创建自动播报计划: %s (群组: %s, ID: %d)", schedule.Name, group.Name, schedule.ID), "broadcast_schedule_create", user.ID, user.Name, user.CallSign, c.ClientIP())
	c.JSON(http.StatusCreated, gin.H{"code": http.StatusCreated, "message": "播报计划已创建", "data": makeBroadcastScheduleResponse(schedule)})
}

func UpdateBroadcastSchedule(c *gin.Context) {
	user, group, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	scheduleID, ok := parseBroadcastUintParam(c, "scheduleId")
	if !ok {
		return
	}
	repo := repository.Default()
	schedule, err := repo.GetSchedule(c.Request.Context(), groupID, scheduleID)
	if err != nil {
		writeBroadcastRepositoryError(c, err)
		return
	}
	var req broadcastScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_schedule_invalid", "播报计划参数无效")
		return
	}
	applyBroadcastScheduleRequest(schedule, &req)
	schedule.UpdatedBy = user.ID
	if err := repo.SaveSchedule(c.Request.Context(), schedule, time.Now().UTC()); err != nil {
		writeBroadcastRepositoryError(c, err)
		return
	}
	action := "broadcast_schedule_update"
	if req.Enabled != nil {
		if *req.Enabled {
			action = "broadcast_schedule_enable"
		} else {
			action = "broadcast_schedule_disable"
		}
	}
	oplog.AddLog(fmt.Sprintf("更新自动播报计划: %s (群组: %s, ID: %d)", schedule.Name, group.Name, schedule.ID), action, user.ID, user.Name, user.CallSign, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "播报计划已更新", "data": makeBroadcastScheduleResponse(schedule)})
}

func DeleteBroadcastSchedule(c *gin.Context) {
	user, group, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	scheduleID, ok := parseBroadcastUintParam(c, "scheduleId")
	if !ok {
		return
	}
	if err := repository.Default().DeleteSchedule(c.Request.Context(), groupID, scheduleID); err != nil {
		writeBroadcastRepositoryError(c, err)
		return
	}
	oplog.AddLog(fmt.Sprintf("删除自动播报计划: ID=%d (群组: %s)", scheduleID, group.Name), "broadcast_schedule_delete", user.ID, user.Name, user.CallSign, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "播报计划已删除"})
}

func ListBroadcastRuns(c *gin.Context) {
	_, _, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	runs, total, err := repository.Default().ListRuns(c.Request.Context(), groupID, page, pageSize)
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_run_list_failed", "读取播报执行历史失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "成功", "data": gin.H{"items": runs, "total": total, "page": max(page, 1), "page_size": normalizeBroadcastPageSize(pageSize)}})
}

func RunBroadcastSchedule(c *gin.Context) {
	user, group, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	scheduleID, ok := parseBroadcastUintParam(c, "scheduleId")
	if !ok {
		return
	}
	if cfg := config.TryGet(); cfg == nil || !cfg.Broadcast.Enabled {
		writeBroadcastError(c, http.StatusServiceUnavailable, "broadcast_disabled", "站点未启用自动播报")
		return
	}
	run, code, err := broadcastruntime.TriggerManual(c.Request.Context(), groupID, scheduleID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			writeBroadcastError(c, http.StatusNotFound, "broadcast_resource_not_found", "播报计划不存在")
		case errors.Is(err, broadcastruntime.ErrUnavailable), errors.Is(err, schedruntime.ErrSchedulerStopped):
			writeBroadcastError(c, http.StatusServiceUnavailable, "broadcast_scheduler_unavailable", "自动播报调度器暂不可用")
		case errors.Is(err, schedruntime.ErrOperationalDisabled):
			writeBroadcastError(c, http.StatusConflict, "site_broadcast_disabled", "站点自动播报运行开关当前已关闭")
		case errors.Is(err, schedruntime.ErrSchedulerBusy):
			writeBroadcastError(c, http.StatusServiceUnavailable, "broadcast_scheduler_busy", "自动播报任务已达到并发上限")
		default:
			log.Printf("[BROADCAST] manual trigger failed: group_id=%d schedule_id=%d", groupID, scheduleID)
			writeBroadcastError(c, http.StatusInternalServerError, "broadcast_manual_run_failed", "手动触发自动播报失败")
		}
		return
	}
	if code != "" {
		switch code {
		case "virtual_group_broadcast_suspended":
			writeBroadcastError(c, http.StatusConflict, "virtual_group_broadcast_suspended", "当前互联策略已挂起该实体组的自动播报")
		case "schedule_disabled":
			writeBroadcastError(c, http.StatusConflict, "broadcast_schedule_disabled", "播报计划当前已停用")
		case "group_unavailable":
			writeBroadcastError(c, http.StatusConflict, "broadcast_group_unavailable", "播报实体组当前不可用")
		case "audio_unavailable":
			writeBroadcastError(c, http.StatusConflict, "broadcast_audio_not_ready", "播报音频当前不可用")
		case "site_broadcast_disabled":
			writeBroadcastError(c, http.StatusConflict, "site_broadcast_disabled", "站点自动播报运行开关当前已关闭")
		default:
			writeBroadcastError(c, http.StatusConflict, "broadcast_run_not_eligible", "播报计划当前不可执行")
		}
		return
	}
	oplog.AddLog(fmt.Sprintf("手动触发自动播报: 计划ID=%d (群组: %s, 执行ID: %d)", scheduleID, group.Name, run.ID), "broadcast_schedule_run", user.ID, user.Name, user.CallSign, c.ClientIP())
	c.JSON(http.StatusAccepted, gin.H{"code": http.StatusAccepted, "message": "自动播报已触发", "data": run})
}

func CancelBroadcastRun(c *gin.Context) {
	user, group, groupID, ok := requireManagedBroadcastGroup(c)
	if !ok {
		return
	}
	runID, ok := parseBroadcastUintParam(c, "runId")
	if !ok {
		return
	}
	run, err := repository.Default().GetRun(c.Request.Context(), groupID, runID)
	if err != nil {
		writeBroadcastRepositoryError(c, err)
		return
	}
	if run.Status != model.RunStatusClaimed && run.Status != model.RunStatusPlaying {
		writeBroadcastError(c, http.StatusConflict, "broadcast_run_not_playing", "该自动播报当前未在执行")
		return
	}
	if !broadcastruntime.CancelRun(run.ID, nil) {
		writeBroadcastError(c, http.StatusConflict, "broadcast_run_not_playing", "该自动播报当前未在本节点执行")
		return
	}
	oplog.AddLog(fmt.Sprintf("停止自动播报: 执行ID=%d (群组: %s)", run.ID, group.Name), "broadcast_run_cancel", user.ID, user.Name, user.CallSign, c.ClientIP())
	c.JSON(http.StatusAccepted, gin.H{"code": http.StatusAccepted, "message": "停止请求已提交", "data": gin.H{"run_id": run.ID}})
}

func requireManagedBroadcastGroup(c *gin.Context) (*gormdb.User, *gormdb.Group, int, bool) {
	id64, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || id64 <= 0 {
		writeBroadcastError(c, http.StatusBadRequest, "invalid_group_id", "群组 ID 无效")
		return nil, nil, 0, false
	}
	group, err := gormdb.NewGroupRepository().GetGroupByID(int(id64))
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "group_read_failed", "读取群组失败")
		return nil, nil, 0, false
	}
	if group == nil {
		writeBroadcastError(c, http.StatusNotFound, "group_not_found", "群组不存在")
		return nil, nil, 0, false
	}
	user, ok := requireCurrentUser(c)
	if !ok {
		return nil, nil, 0, false
	}
	if !canManageGroup(user, group) {
		writeBroadcastError(c, http.StatusForbidden, "broadcast_group_forbidden", "无权管理该群组的自动播报")
		return nil, nil, 0, false
	}
	if group.IsVirtual || !isSupportedGroupType(group.Type) {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_entity_group_required", "自动播报只能绑定实体群组")
		return nil, nil, 0, false
	}
	return user, group, group.ID, true
}

func parseBroadcastUintParam(c *gin.Context, name string) (uint, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(c.Param(name)), 10, 32)
	if err != nil || value == 0 {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_resource_id_invalid", "资源 ID 无效")
		return 0, false
	}
	return uint(value), true
}

func applyBroadcastScheduleRequest(schedule *model.BroadcastSchedule, req *broadcastScheduleRequest) {
	if req.AudioID != nil {
		schedule.AudioID = *req.AudioID
	}
	if req.Name != nil {
		schedule.Name = *req.Name
	}
	if req.ScheduleType != nil {
		schedule.ScheduleType = *req.ScheduleType
	}
	if req.Timezone != nil {
		schedule.Timezone = *req.Timezone
	}
	if req.ScheduledAt != nil {
		schedule.ScheduledAt = req.ScheduledAt
	}
	if req.LocalTime != nil {
		schedule.LocalTime = *req.LocalTime
	}
	if req.WeekdayMask != nil {
		schedule.WeekdayMask = *req.WeekdayMask
	}
	if req.Enabled != nil {
		schedule.Enabled = *req.Enabled
	}
}

func makeBroadcastAudioResponse(audio *model.BroadcastAudio, scheduleCount int64, previewURL string) broadcastAudioResponse {
	return broadcastAudioResponse{
		ID: audio.ID, GroupID: audio.GroupID, Name: audio.Name, OriginalMIMEType: audio.OriginalMIMEType,
		OriginalSize: audio.OriginalSize, PlaybackSize: audio.PlaybackSize, DurationMS: audio.DurationMS,
		PacketCount: audio.PacketCount, SHA256: audio.SHA256, Status: audio.Status, ErrorMessage: audio.ErrorMessage,
		ScheduleCount: scheduleCount, PreviewURL: previewURL, CreatedAt: audio.CreatedAt, UpdatedAt: audio.UpdatedAt,
	}
}

func makeBroadcastScheduleResponse(schedule *model.BroadcastSchedule) broadcastScheduleResponse {
	return broadcastScheduleResponse{BroadcastSchedule: *schedule, EffectiveEnabled: schedule.Enabled && schedule.SuspendedReason == "" && schedule.NextRunAt != nil}
}

func normalizeBroadcastPageSize(pageSize int) int {
	if pageSize < 1 {
		return 20
	}
	if pageSize > 100 {
		return 100
	}
	return pageSize
}

func writeBroadcastRepositoryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeBroadcastError(c, http.StatusNotFound, "broadcast_resource_not_found", "播报资源不存在")
	case errors.Is(err, repository.ErrAudioInUse):
		writeBroadcastError(c, http.StatusConflict, "broadcast_audio_in_use", "音频仍被播报计划引用")
	case errors.Is(err, repository.ErrAudioNotReady):
		writeBroadcastError(c, http.StatusConflict, "broadcast_audio_not_ready", "音频尚未处理完成")
	case errors.Is(err, repository.ErrAudioGroupMismatch):
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_audio_group_mismatch", "音频不属于当前群组")
	case errors.Is(err, repository.ErrInvalidEntityGroup):
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_entity_group_required", "自动播报只能绑定实体群组")
	case errors.Is(err, repository.ErrInvalidAudio):
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_audio_invalid", "播报音频参数无效")
	case errors.Is(err, repository.ErrInvalidSchedule):
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_schedule_invalid", "播报计划参数或时间无效")
	default:
		log.Printf("[BROADCAST] repository request failed: %v", err)
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_request_failed", "自动播报操作失败")
	}
}

func writeBroadcastError(c *gin.Context, status int, errorCode, message string) {
	c.JSON(status, gin.H{"code": status, "error_code": errorCode, "message": message})
}

type broadcastCountingReader struct {
	reader io.Reader
	count  int64
}

func (r *broadcastCountingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.count += int64(n)
	return n, err
}
