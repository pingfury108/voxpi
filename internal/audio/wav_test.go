package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestWriteWAV16Header(t *testing.T) {
	samples := []float32{0, 0.5, -0.5, 1.5, -1.5}
	var buf bytes.Buffer
	if err := WriteWAV16(&buf, samples, 22050); err != nil {
		t.Fatalf("WriteWAV16: %v", err)
	}
	b := buf.Bytes()
	if len(b) != WAVHeaderSize+len(samples)*2 {
		t.Fatalf("长度 = %d, 期望 %d", len(b), WAVHeaderSize+len(samples)*2)
	}
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatalf("RIFF/WAVE 标识不正确: %q %q", b[0:4], b[8:12])
	}
	if got := binary.LittleEndian.Uint32(b[24:]); got != 22050 {
		t.Fatalf("采样率 = %d, 期望 22050", got)
	}
	if got := binary.LittleEndian.Uint32(b[40:]); got != uint32(len(samples)*2) {
		t.Fatalf("data 长度 = %d, 期望 %d", got, len(samples)*2)
	}
	// 越界采样应被削顶
	if got := int16(binary.LittleEndian.Uint16(b[WAVHeaderSize+6:])); got != 32767 {
		t.Fatalf("削顶值 = %d, 期望 32767", got)
	}
	if got := int16(binary.LittleEndian.Uint16(b[WAVHeaderSize+8:])); got != -32768 {
		t.Fatalf("削顶值 = %d, 期望 -32768", got)
	}
}

func TestPCM16RoundTrip(t *testing.T) {
	samples := []float32{0, 1, -1, 0.5}
	pcm := PCM16(samples)
	if len(pcm) != len(samples)*2 {
		t.Fatalf("PCM 长度 = %d, 期望 %d", len(pcm), len(samples)*2)
	}
	for i, s := range samples {
		want := int16(math.Round(float64(s) * 32767))
		got := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		if got != want {
			t.Fatalf("样本 %d = %d, 期望 %d", i, got, want)
		}
	}
}

func TestAudioDuration(t *testing.T) {
	if got := AudioDuration(22050, 22050); got != 1 {
		t.Fatalf("时长 = %v, 期望 1", got)
	}
	if got := AudioDuration(100, 0); got != 0 {
		t.Fatalf("采样率为 0 时应返回 0，得到 %v", got)
	}
}
