package media

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/config"
)

func TestContainerRoundTrip(t *testing.T) {
	frames := [][]byte{{0xf8, 1, 2}, {0xf8, 3, 4, 5}, {0xf8, 6}}
	data, metadata, err := BuildContainer(frames)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.PacketCount != 2 || metadata.FrameCount != 3 || metadata.DurationMS != 180 || len(data) > containerHeaderBytes+2*MaxVoicePayloadBytes {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	parsed, err := ReadContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Metadata != metadata || len(parsed.Packets) != 2 {
		t.Fatalf("round trip mismatch: %#v", parsed)
	}
	if framesInFirst, ok := validateMergedFrames(parsed.Packets[0]); !ok || framesInFirst != 2 {
		t.Fatalf("first packet frames=%d valid=%v", framesInFirst, ok)
	}
}

func TestPacketDurationUsesMergedFrameLengths(t *testing.T) {
	one := []byte{0, 3, 1, 2, 3}
	two := append(append([]byte{}, one...), 0, 2, 4, 5)
	if got, err := PacketDuration(one); err != nil || got != 60*time.Millisecond {
		t.Fatalf("one frame duration=%s err=%v", got, err)
	}
	if got, err := PacketDuration(two); err != nil || got != 120*time.Millisecond {
		t.Fatalf("two frame duration=%s err=%v", got, err)
	}
	for _, invalid := range [][]byte{nil, {0, 0}, {0, 3, 1}, append(append(append([]byte{}, one...), one...), one...)} {
		if _, err := PacketDuration(invalid); err == nil {
			t.Fatalf("invalid packet accepted: %x", invalid)
		}
	}
}

func TestContainerRejectsCorruptionAndOversizedFrame(t *testing.T) {
	if _, _, err := BuildContainer([][]byte{make([]byte, MaxVoicePayloadBytes)}); err == nil {
		t.Fatal("oversized frame was accepted")
	}
	data, _, err := BuildContainer([][]byte{{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if _, err := ReadContainer(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("corrupted container was accepted")
	}
}

func TestParseOggOpus(t *testing.T) {
	stream := appendOggPage(nil, 99, 0, [][]byte{append([]byte("OpusHead"), make([]byte, 11)...), []byte("OpusTags")})
	stream = appendOggPage(stream, 99, 1, [][]byte{{0xf8, 1, 2}, {0xf8, 3, 4}})
	frames, err := ParseOggOpus(bytes.NewReader(stream), int64(len(stream)))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || !bytes.Equal(frames[1], []byte{0xf8, 3, 4}) {
		t.Fatalf("unexpected frames: %x", frames)
	}
	if _, err := ParseOggOpus(bytes.NewReader(stream[:len(stream)-1]), int64(len(stream))); err == nil {
		t.Fatal("truncated Ogg stream was accepted")
	}
}

func TestValidateUploadHeader(t *testing.T) {
	wav := append([]byte("RIFF"), make([]byte, 4)...)
	wav = append(wav, []byte("WAVEfmt ")...)
	if _, _, err := ValidateUploadHeader("notice.wav", "audio/wav", wav); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateUploadHeader("notice.mp3", "audio/mpeg", wav); err == nil {
		t.Fatal("mismatched signature was accepted")
	}
	if _, _, err := ValidateUploadHeader("notice.wav", "application/octet-stream", wav); err == nil {
		t.Fatal("generic MIME was accepted")
	}
}

func TestRealFFmpegPipeline(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_BROADCAST_FFMPEG_E2E")), "true") {
		t.Skip("set DRAARL_BROADCAST_FFMPEG_E2E=true to run the real ffmpeg pipeline")
	}
	cfg := config.BroadcastConfig{}
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath(cfg.FFmpegPath); err != nil {
		t.Fatalf("ffmpeg is required: %v", err)
	}
	if _, err := exec.LookPath(cfg.FFprobePath); err != nil {
		t.Fatalf("ffprobe is required: %v", err)
	}
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "input.wav")
	generate := exec.Command(cfg.FFmpegPath,
		"-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=880:duration=2",
		"-ac", "2", "-ar", "44100", inputPath,
	)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate WAV: %v: %s", err, output)
	}
	stat, err := os.Stat(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	processor := &Processor{config: cfg}
	probe, err := processor.probe(t.Context(), inputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.validateProbe(&model.BroadcastAudio{OriginalSize: stat.Size()}, probe); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(tempDir, "playback.opus")
	if err := processor.transcode(t.Context(), inputPath, outputPath); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	frames, err := ParseOggOpus(file, cfg.MaxUploadBytes)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	data, metadata, err := BuildContainer(frames)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.DurationMS < 1900 || metadata.DurationMS > 2100 || metadata.PacketCount < 15 {
		t.Fatalf("unexpected real media metadata: %#v", metadata)
	}
	container, err := ReadContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for index, packet := range container.Packets {
		if len(packet)+DraARLHeaderBytes > MaxDraARLPacketBytes {
			t.Fatalf("packet %d exceeds DraARL limit: %d", index, len(packet)+DraARLHeaderBytes)
		}
	}
}

func appendOggPage(output []byte, serial, sequence uint32, packets [][]byte) []byte {
	header := make([]byte, 27)
	copy(header[:4], "OggS")
	header[4] = 0
	if sequence == 0 {
		header[5] |= 0x02
	} else {
		header[5] |= 0x04
	}
	binary.LittleEndian.PutUint32(header[14:18], serial)
	binary.LittleEndian.PutUint32(header[18:22], sequence)
	lacing := make([]byte, 0, len(packets))
	body := make([]byte, 0)
	for _, packet := range packets {
		if len(packet) >= 255 {
			panic("test packet too large")
		}
		lacing = append(lacing, byte(len(packet)))
		body = append(body, packet...)
	}
	header[26] = byte(len(lacing))
	output = append(output, header...)
	output = append(output, lacing...)
	return append(output, body...)
}
