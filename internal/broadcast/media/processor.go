package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/repository"
	"draarl/internal/config"
	"draarl/pkg/storage"
)

var (
	ErrMediaDuration = errors.New("broadcast audio duration is invalid")
	ErrMediaStreams  = errors.New("broadcast audio streams are invalid")
	ErrMediaRatio    = errors.New("broadcast audio compression ratio is invalid")
	ErrMediaProcess  = errors.New("broadcast audio processing failed")
)

type Processor struct {
	config  config.BroadcastConfig
	repo    *repository.Repository
	jobs    chan uint
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	metrics processorMetrics
}

func NewProcessor(cfg config.BroadcastConfig, repo *repository.Repository) *Processor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Processor{config: cfg, repo: repo, jobs: make(chan uint, 128), ctx: ctx, cancel: cancel}
}

func (p *Processor) Start() error {
	if p == nil || p.repo == nil {
		return fmt.Errorf("broadcast media processor is not configured")
	}
	if _, err := exec.LookPath(p.config.FFmpegPath); err != nil {
		return fmt.Errorf("find ffmpeg: %w", err)
	}
	if _, err := exec.LookPath(p.config.FFprobePath); err != nil {
		return fmt.Errorf("find ffprobe: %w", err)
	}
	p.wg.Add(1)
	go p.worker()
	audios, err := p.repo.ListProcessingAudios(p.ctx, 1000)
	if err != nil {
		p.cancel()
		p.wg.Wait()
		return fmt.Errorf("recover processing broadcast audios: %w", err)
	}
	for _, audio := range audios {
		if err := p.Enqueue(audio.ID); err != nil {
			p.cancel()
			p.wg.Wait()
			return err
		}
	}
	p.metrics.running.Store(true)
	return nil
}

func (p *Processor) Stop(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.metrics.running.Store(false)
	p.cancel()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Processor) Enqueue(audioID uint) error {
	if p == nil || audioID == 0 {
		return fmt.Errorf("invalid broadcast audio job")
	}
	select {
	case p.jobs <- audioID:
		p.metrics.enqueued.Add(1)
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	default:
		return fmt.Errorf("broadcast audio queue is full")
	}
}

func (p *Processor) worker() {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case audioID := <-p.jobs:
			if err := p.ProcessAudio(p.ctx, audioID); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("[BROADCAST] process audio id=%d failed: %v", audioID, err)
			}
		}
	}
}

func (p *Processor) ProcessAudio(parent context.Context, audioID uint) error {
	audio, err := p.repo.GetAudioByID(parent, audioID)
	if err != nil {
		return err
	}
	if audio.Status != model.AudioStatusProcessing {
		return nil
	}
	startedAt := p.metrics.begin()
	succeeded := false
	defer func() { p.metrics.finish(startedAt, succeeded) }()
	ctx, cancel := context.WithTimeout(parent, time.Duration(p.config.TranscodeTimeoutSeconds)*time.Second)
	defer cancel()
	metadata, playbackKey, recordKey, recordSize, err := p.process(ctx, audio)
	if err != nil {
		if parent.Err() == nil && !errors.Is(err, context.Canceled) {
			_ = p.repo.MarkAudioFailed(context.WithoutCancel(parent), audio.ID, safeProcessingMessage(err))
		}
		return err
	}
	if err := p.repo.MarkAudioReady(parent, audio.ID, playbackKey, metadata.Size, recordKey, recordSize, metadata.DurationMS, metadata.PacketCount); err != nil {
		_ = storage.Delete(context.WithoutCancel(parent), playbackKey)
		_ = storage.Delete(context.WithoutCancel(parent), recordKey)
		return fmt.Errorf("commit broadcast playback metadata: %w", err)
	}
	succeeded = true
	return nil
}

func (p *Processor) process(ctx context.Context, audio *model.BroadcastAudio) (ContainerMetadata, string, string, int64, error) {
	tempDir, err := os.MkdirTemp("", "draarl-broadcast-")
	if err != nil {
		return ContainerMetadata{}, "", "", 0, fmt.Errorf("%w: create temp directory", ErrMediaProcess)
	}
	defer os.RemoveAll(tempDir)
	inputPath := filepath.Join(tempDir, "input"+path.Ext(audio.OriginalObjectKey))
	if err := p.copyOriginal(ctx, audio, inputPath); err != nil {
		return ContainerMetadata{}, "", "", 0, err
	}
	probe, err := p.probe(ctx, inputPath)
	if err != nil {
		return ContainerMetadata{}, "", "", 0, err
	}
	if err := p.validateProbe(audio, probe); err != nil {
		return ContainerMetadata{}, "", "", 0, err
	}
	oggPath := filepath.Join(tempDir, "playback.opus")
	if err := p.transcode(ctx, inputPath, oggPath); err != nil {
		return ContainerMetadata{}, "", "", 0, err
	}
	ogg, err := os.Open(oggPath)
	if err != nil {
		return ContainerMetadata{}, "", "", 0, fmt.Errorf("%w: open transcoded output", ErrMediaProcess)
	}
	frames, parseErr := ParseOggOpus(ogg, p.config.MaxUploadBytes*2)
	_ = ogg.Close()
	if parseErr != nil {
		return ContainerMetadata{}, "", "", 0, fmt.Errorf("%w: %v", ErrMediaProcess, parseErr)
	}
	containerData, metadata, err := BuildContainer(frames)
	if err != nil {
		return ContainerMetadata{}, "", "", 0, fmt.Errorf("%w: %v", ErrMediaProcess, err)
	}
	if metadata.DurationMS > p.config.MaxAudioDurationSeconds*1000+OpusFrameDurationMS {
		return ContainerMetadata{}, "", "", 0, ErrMediaDuration
	}
	container, err := ReadContainer(bytes.NewReader(containerData), int64(len(containerData)))
	if err != nil {
		return ContainerMetadata{}, "", "", 0, fmt.Errorf("%w: verify container: %v", ErrMediaProcess, err)
	}
	recordData, _, err := BuildRawOpusFromPackets(container.Packets, container.Metadata.PacketCount)
	if err != nil {
		return ContainerMetadata{}, "", "", 0, fmt.Errorf("%w: build communication recording: %v", ErrMediaProcess, err)
	}
	playbackKey := path.Join(path.Dir(audio.OriginalObjectKey), "playback.dabr")
	if err := storage.Put(ctx, playbackKey, bytes.NewReader(containerData), int64(len(containerData)), "application/vnd.draarl.broadcast"); err != nil {
		return ContainerMetadata{}, "", "", 0, fmt.Errorf("write broadcast playback object: %w", err)
	}
	recordKey := path.Join(path.Dir(audio.OriginalObjectKey), "record.raw")
	if err := storage.Put(ctx, recordKey, bytes.NewReader(recordData), int64(len(recordData)), "application/octet-stream"); err != nil {
		_ = storage.Delete(context.WithoutCancel(ctx), playbackKey)
		return ContainerMetadata{}, "", "", 0, fmt.Errorf("write broadcast recording object: %w", err)
	}
	return metadata, playbackKey, recordKey, int64(len(recordData)), nil
}

func (p *Processor) copyOriginal(ctx context.Context, audio *model.BroadcastAudio, target string) error {
	reader, err := storage.Open(ctx, audio.OriginalObjectKey)
	if err != nil {
		return fmt.Errorf("open broadcast original: %w", err)
	}
	defer reader.Close()
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: create temp input", ErrMediaProcess)
	}
	written, copyErr := io.Copy(file, io.LimitReader(&contextReader{ctx: ctx, reader: reader}, p.config.MaxUploadBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("%w: copy temp input", ErrMediaProcess)
	}
	if written <= 0 || written > p.config.MaxUploadBytes || written != audio.OriginalSize {
		return fmt.Errorf("%w: original size mismatch", ErrMediaProcess)
	}
	return nil
}

type probeResult struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Channels  int    `json:"channels"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		Size     string `json:"size"`
	} `json:"format"`
}

func (p *Processor) probe(ctx context.Context, inputPath string) (probeResult, error) {
	command := exec.CommandContext(ctx, p.config.FFprobePath,
		"-v", "error", "-protocol_whitelist", "file,pipe",
		"-show_entries", "format=duration,size:stream=codec_type,channels", "-of", "json", inputPath,
	)
	var stdout bytes.Buffer
	stderr := &limitedBuffer{limit: 8192}
	command.Stdout, command.Stderr = &stdout, stderr
	if err := p.runCommand(command); err != nil {
		return probeResult{}, fmt.Errorf("%w: ffprobe: %v: %s", ErrMediaProcess, err, stderr.String())
	}
	if stdout.Len() > 64*1024 {
		return probeResult{}, fmt.Errorf("%w: oversized ffprobe output", ErrMediaProcess)
	}
	var result probeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return probeResult{}, fmt.Errorf("%w: invalid ffprobe output", ErrMediaProcess)
	}
	return result, nil
}

func (p *Processor) validateProbe(audio *model.BroadcastAudio, probe probeResult) error {
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || duration <= 0 || duration > float64(p.config.MaxAudioDurationSeconds) {
		return ErrMediaDuration
	}
	audioStreams, channels := 0, 0
	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "audio":
			audioStreams++
			channels = stream.Channels
		case "video":
			return ErrMediaStreams
		}
	}
	if audioStreams != 1 || channels < 1 || channels > 2 {
		return ErrMediaStreams
	}
	decodedBytes := duration * OpusSampleRate * 2 * float64(channels)
	if audio.OriginalSize <= 0 || decodedBytes/float64(audio.OriginalSize) > 1000 {
		return ErrMediaRatio
	}
	return nil
}

func (p *Processor) transcode(ctx context.Context, inputPath, outputPath string) error {
	// Keep the source timeline intact. In particular, scheduled broadcasts must
	// retain leading and trailing silence instead of shortening the uploaded media.
	filter := "loudnorm=I=-20:TP=-3:LRA=7,alimiter=limit=0.85"
	command := exec.CommandContext(ctx, p.config.FFmpegPath,
		"-nostdin", "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "file,pipe",
		"-i", inputPath, "-map", "0:a:0", "-vn", "-ac", "1", "-ar", strconv.Itoa(OpusSampleRate),
		"-af", filter, "-c:a", "libopus", "-application", "voip", "-frame_duration", strconv.Itoa(OpusFrameDurationMS),
		"-b:a", "20k", "-vbr", "on", "-compression_level", "10", "-threads", "1", "-filter_threads", "1",
		"-f", "opus", outputPath,
	)
	stderr := &limitedBuffer{limit: 8192}
	command.Stdout, command.Stderr = io.Discard, stderr
	if err := p.runCommand(command); err != nil {
		return fmt.Errorf("%w: ffmpeg: %v: %s", ErrMediaProcess, err, stderr.String())
	}
	return nil
}

func (p *Processor) runCommand(command *exec.Cmd) error {
	if err := command.Start(); err != nil {
		return err
	}
	if err := applyMediaProcessLimits(command.Process.Pid, p.config.TranscodeMemoryLimitMB, p.config.TranscodeCPULimitSeconds); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	return command.Wait()
}

func safeProcessingMessage(err error) string {
	switch {
	case errors.Is(err, ErrMediaDuration):
		return "音频时长无效或超过站点限制"
	case errors.Is(err, ErrMediaStreams):
		return "音频包含不支持的视频、多音轨或声道配置"
	case errors.Is(err, ErrMediaRatio):
		return "音频压缩比异常"
	default:
		return "音频处理失败"
	}
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (w *limitedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = w.buffer.Write(data)
	}
	return originalLength, nil
}

func (w *limitedBuffer) String() string { return strings.TrimSpace(w.buffer.String()) }

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}
