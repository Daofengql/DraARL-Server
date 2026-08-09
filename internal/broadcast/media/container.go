package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	ContainerVersion        = 1
	OpusSampleRate          = 16000
	OpusFrameDurationMS     = 60
	MaxFramesPerPacket      = 2
	MaxDraARLPacketBytes    = 800
	DraARLHeaderBytes       = 90
	MaxVoicePayloadBytes    = MaxDraARLPacketBytes - DraARLHeaderBytes
	containerHeaderBytes    = 52
	maxContainerPacketCount = 1000
)

var (
	containerMagic      = [4]byte{'D', 'A', 'B', 'R'}
	ErrInvalidContainer = errors.New("invalid DABR container")
)

type ContainerMetadata struct {
	PacketCount int
	FrameCount  int
	DurationMS  int
	Size        int64
	SHA256      [32]byte
}

type Container struct {
	Metadata ContainerMetadata
	Packets  [][]byte
}

func BuildContainer(frames [][]byte) ([]byte, ContainerMetadata, error) {
	if len(frames) == 0 {
		return nil, ContainerMetadata{}, fmt.Errorf("%w: no Opus frames", ErrInvalidContainer)
	}
	packets := make([][]byte, 0, (len(frames)+1)/2)
	for index := 0; index < len(frames); {
		packet := make([]byte, 0, MaxVoicePayloadBytes)
		for count := 0; count < MaxFramesPerPacket && index < len(frames); count++ {
			frame := frames[index]
			if len(frame) == 0 || len(frame) > 1000 || len(frame) > int(^uint16(0)) {
				return nil, ContainerMetadata{}, fmt.Errorf("%w: invalid Opus frame length", ErrInvalidContainer)
			}
			if len(packet)+2+len(frame) > MaxVoicePayloadBytes {
				if count == 0 {
					return nil, ContainerMetadata{}, fmt.Errorf("%w: Opus frame exceeds protocol payload", ErrInvalidContainer)
				}
				break
			}
			packet = binary.BigEndian.AppendUint16(packet, uint16(len(frame)))
			packet = append(packet, frame...)
			index++
		}
		packets = append(packets, packet)
	}
	if len(packets) > maxContainerPacketCount {
		return nil, ContainerMetadata{}, fmt.Errorf("%w: too many packets", ErrInvalidContainer)
	}

	payload := bytes.NewBuffer(nil)
	for _, packet := range packets {
		_ = binary.Write(payload, binary.BigEndian, uint16(len(packet)))
		_, _ = payload.Write(packet)
	}
	digest := sha256.Sum256(payload.Bytes())
	durationMS := len(frames) * OpusFrameDurationMS
	output := bytes.NewBuffer(make([]byte, 0, containerHeaderBytes+payload.Len()))
	_, _ = output.Write(containerMagic[:])
	_ = output.WriteByte(ContainerVersion)
	_ = output.WriteByte(0)
	_ = binary.Write(output, binary.BigEndian, uint16(OpusFrameDurationMS))
	_ = binary.Write(output, binary.BigEndian, uint32(OpusSampleRate))
	_ = binary.Write(output, binary.BigEndian, uint32(len(packets)))
	_ = binary.Write(output, binary.BigEndian, uint32(durationMS))
	_, _ = output.Write(digest[:])
	_, _ = output.Write(payload.Bytes())
	metadata := ContainerMetadata{PacketCount: len(packets), FrameCount: len(frames), DurationMS: durationMS, Size: int64(output.Len()), SHA256: digest}
	return output.Bytes(), metadata, nil
}

func ReadContainer(reader io.Reader, maxBytes int64) (*Container, error) {
	if maxBytes < containerHeaderBytes {
		return nil, fmt.Errorf("%w: invalid size limit", ErrInvalidContainer)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidContainer, err)
	}
	if int64(len(data)) > maxBytes || len(data) < containerHeaderBytes {
		return nil, fmt.Errorf("%w: invalid file size", ErrInvalidContainer)
	}
	if !bytes.Equal(data[:4], containerMagic[:]) || data[4] != ContainerVersion || data[5] != 0 {
		return nil, fmt.Errorf("%w: unsupported header", ErrInvalidContainer)
	}
	if binary.BigEndian.Uint16(data[6:8]) != OpusFrameDurationMS || binary.BigEndian.Uint32(data[8:12]) != OpusSampleRate {
		return nil, fmt.Errorf("%w: unsupported audio parameters", ErrInvalidContainer)
	}
	packetCount := int(binary.BigEndian.Uint32(data[12:16]))
	durationMS := int(binary.BigEndian.Uint32(data[16:20]))
	if packetCount < 1 || packetCount > maxContainerPacketCount || durationMS < OpusFrameDurationMS || durationMS > 60*1000 {
		return nil, fmt.Errorf("%w: invalid metadata", ErrInvalidContainer)
	}
	payload := data[containerHeaderBytes:]
	wantDigest := data[20:52]
	gotDigest := sha256.Sum256(payload)
	if !bytes.Equal(wantDigest, gotDigest[:]) {
		return nil, fmt.Errorf("%w: checksum mismatch", ErrInvalidContainer)
	}

	packets := make([][]byte, 0, packetCount)
	offset := 0
	frameCount := 0
	for offset < len(payload) && len(packets) < packetCount {
		if len(payload)-offset < 2 {
			return nil, fmt.Errorf("%w: truncated packet length", ErrInvalidContainer)
		}
		packetLength := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
		offset += 2
		if packetLength < 3 || packetLength > MaxVoicePayloadBytes || packetLength > len(payload)-offset {
			return nil, fmt.Errorf("%w: invalid packet length", ErrInvalidContainer)
		}
		packet := append([]byte(nil), payload[offset:offset+packetLength]...)
		offset += packetLength
		frames, ok := validateMergedFrames(packet)
		if !ok || frames < 1 || frames > MaxFramesPerPacket {
			return nil, fmt.Errorf("%w: invalid merged Opus payload", ErrInvalidContainer)
		}
		frameCount += frames
		packets = append(packets, packet)
	}
	if offset != len(payload) || len(packets) != packetCount || durationMS != frameCount*OpusFrameDurationMS {
		return nil, fmt.Errorf("%w: inconsistent metadata", ErrInvalidContainer)
	}
	return &Container{Metadata: ContainerMetadata{PacketCount: packetCount, FrameCount: frameCount, DurationMS: durationMS, Size: int64(len(data)), SHA256: gotDigest}, Packets: packets}, nil
}

func validateMergedFrames(packet []byte) (int, bool) {
	offset, frames := 0, 0
	for offset < len(packet) {
		if len(packet)-offset < 2 {
			return 0, false
		}
		length := int(binary.BigEndian.Uint16(packet[offset : offset+2]))
		offset += 2
		if length < 1 || length > 1000 || length > len(packet)-offset {
			return 0, false
		}
		offset += length
		frames++
	}
	return frames, offset == len(packet)
}
