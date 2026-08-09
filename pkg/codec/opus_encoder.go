package codec

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/hraban/opus"
)

const (
	opusSampleRate   = 16000                // 16kHz
	opusChannels     = 1                    // 单声道
	opusFrameSamples = 960                  // 60ms @ 16kHz
	opusFrameBytes   = opusFrameSamples * 2 // 1920 bytes (int16)
	opusMaxDataBytes = 400                  // 单帧 Opus 最大输出
	opusBitrate      = 24000                // 24kbps VOIP
)

// OpusEncoder 线程安全的 Opus 编码器
type OpusEncoder struct {
	mu      sync.Mutex
	encoder *opus.Encoder
}

var (
	globalEncoder *OpusEncoder
	encoderOnce   sync.Once
	encoderErr    error
)

// GetOpusEncoder 获取全局 Opus 编码器单例
func GetOpusEncoder() (*OpusEncoder, error) {
	encoderOnce.Do(func() {
		enc, err := opus.NewEncoder(opusSampleRate, opusChannels, opus.AppVoIP)
		if err != nil {
			encoderErr = fmt.Errorf("创建 Opus 编码器失败: %w", err)
			return
		}
		if err := enc.SetBitrate(opusBitrate); err != nil {
			encoderErr = fmt.Errorf("设置 Opus 比特率失败: %w", err)
			return
		}
		globalEncoder = &OpusEncoder{encoder: enc}
	})
	return globalEncoder, encoderErr
}

// EncodePCMToOpus 将 PCM 16kHz 单声道 16-bit 数据编码为 Opus 双帧合并格式
// 输入 pcmData 应为 opusFrameBytes * 2 = 3840 字节（双帧）
// 输出格式：[len1(2B BE)][opusFrame1][len2(2B BE)][opusFrame2]
func (e *OpusEncoder) EncodePCMToOpus(pcmData []byte) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(pcmData) < opusFrameBytes*2 {
		return nil, fmt.Errorf("PCM 数据不足：需要 %d 字节，实际 %d 字节", opusFrameBytes*2, len(pcmData))
	}

	// 编码第一帧
	frame1 := make([]byte, opusMaxDataBytes)
	n1, err := e.encoder.Encode(int16FromBytes(pcmData[:opusFrameBytes]), frame1)
	if err != nil {
		return nil, fmt.Errorf("编码第一帧失败: %w", err)
	}

	// 编码第二帧
	frame2 := make([]byte, opusMaxDataBytes)
	n2, err := e.encoder.Encode(int16FromBytes(pcmData[opusFrameBytes:opusFrameBytes*2]), frame2)
	if err != nil {
		return nil, fmt.Errorf("编码第二帧失败: %w", err)
	}

	// 合并输出：[len1(2B BE)][data1][len2(2B BE)][data2]
	output := make([]byte, 0, 4+n1+n2)
	lenBuf := make([]byte, 2)

	binary.BigEndian.PutUint16(lenBuf, uint16(n1))
	output = append(output, lenBuf...)
	output = append(output, frame1[:n1]...)

	binary.BigEndian.PutUint16(lenBuf, uint16(n2))
	output = append(output, lenBuf...)
	output = append(output, frame2[:n2]...)

	return output, nil
}

// int16FromBytes 将 byte slice 转为 int16 slice
func int16FromBytes(b []byte) []int16 {
	// 确保长度为偶数
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	result := make([]int16, len(b)/2)
	for i := range result {
		result[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return result
}
