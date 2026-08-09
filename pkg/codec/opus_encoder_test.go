package codec

import (
	"testing"
)

func TestEncodePCMToOpus(t *testing.T) {
	encoder, err := GetOpusEncoder()
	if err != nil {
		t.Skipf("Opus 编码器不可用（缺少 libopus？）: %v", err)
	}

	// 生成 3840 字节的静音 PCM 数据（双帧）
	pcmData := make([]byte, 3840) // 全零 = 静音

	opusData, err := encoder.EncodePCMToOpus(pcmData)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}

	if len(opusData) == 0 {
		t.Error("编码输出为空")
	}

	// 验证输出格式：至少 4 字节（两个长度前缀）
	if len(opusData) < 4 {
		t.Errorf("输出太短: %d 字节", len(opusData))
	}
}
