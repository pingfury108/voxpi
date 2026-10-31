package audio

import (
	"encoding/binary"
	"io"
	"math"
)

// WAVHeaderSize 是 16-bit 单声道 PCM WAV 的文件头长度。
const WAVHeaderSize = 44

// PCM16 把 [-1,1] 的浮点采样转换为 16-bit 小端 PCM。
func PCM16(samples []float32) []byte {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		v := math.Round(float64(s) * 32767)
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(int16(v)))
	}
	return buf
}

// WriteWAV16 写出标准的 16-bit 单声道 WAV。
func WriteWAV16(w io.Writer, samples []float32, sampleRate int) error {
	pcm := PCM16(samples)
	dataLen := len(pcm)
	byteRate := sampleRate * 2

	hdr := make([]byte, 0, WAVHeaderSize)
	hdr = append(hdr, "RIFF"...)
	hdr = binary.LittleEndian.AppendUint32(hdr, uint32(36+dataLen))
	hdr = append(hdr, "WAVEfmt "...)
	hdr = binary.LittleEndian.AppendUint32(hdr, 16) // fmt chunk size
	hdr = binary.LittleEndian.AppendUint16(hdr, 1)  // PCM
	hdr = binary.LittleEndian.AppendUint16(hdr, 1)  // mono
	hdr = binary.LittleEndian.AppendUint32(hdr, uint32(sampleRate))
	hdr = binary.LittleEndian.AppendUint32(hdr, uint32(byteRate))
	hdr = binary.LittleEndian.AppendUint16(hdr, 2)  // block align
	hdr = binary.LittleEndian.AppendUint16(hdr, 16) // bits per sample
	hdr = append(hdr, "data"...)
	hdr = binary.LittleEndian.AppendUint32(hdr, uint32(dataLen))

	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}

// AudioDuration 返回采样点在给定采样率下的时长秒数。
func AudioDuration(n, sampleRate int) float64 {
	if sampleRate <= 0 {
		return 0
	}
	return float64(n) / float64(sampleRate)
}
