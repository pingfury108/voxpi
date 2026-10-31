//go:build linux && cgo

// Package tts 是 sherpa-onnx C API 的一层薄 cgo 封装，只暴露 VoxPi 需要的接口。
package tts

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party
#include <stdlib.h>
#include "c-api.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// Params 描述一个 TTS 引擎实例。
type Params struct {
	Engine      string  `json:"engine"` // vits（默认）| matcha
	Model       string  `json:"model"`
	Tokens      string  `json:"tokens"`
	Lexicon     string  `json:"lexicon"`
	Vocoder     string  `json:"vocoder"`  // matcha 专用
	DataDir     string  `json:"data_dir"` // espeak-ng-data，v1.x 上 Piper 需要
	NoiseScale  float32 `json:"noise_scale"`
	NoiseScaleW float32 `json:"noise_scale_w"`
	LengthScale float32 `json:"length_scale"`
	NumThreads  int32   `json:"num_threads"`
	Debug       bool    `json:"debug"`
}

// Engine 对应一个常驻内存的 sherpa-onnx offline TTS 实例，非并发安全。
type Engine struct {
	impl       *C.SherpaOnnxOfflineTts
	sampleRate int
	speakers   int
}

// New 创建并加载模型。字符串参数由 C 侧拷贝，创建完成后即可释放。
func New(p Params) (e *Engine, err error) {
	var cfg C.SherpaOnnxOfflineTtsConfig
	var allocs []unsafe.Pointer
	defer func() { freeAll(allocs) }()

	str := func(s string) *C.char {
		if s == "" {
			return nil
		}
		c := C.CString(s)
		allocs = append(allocs, unsafe.Pointer(c))
		return c
	}

	cfg.model.num_threads = C.int32_t(p.NumThreads)
	if p.Debug {
		cfg.model.debug = 1
	}

	switch p.Engine {
	case "", "vits":
		if p.Model == "" || p.Tokens == "" {
			return nil, errors.New("vits: model 和 tokens 不能为空")
		}
		m := &cfg.model.vits
		m.model = str(p.Model)
		m.tokens = str(p.Tokens)
		m.lexicon = str(p.Lexicon)
		m.data_dir = str(p.DataDir)
		m.noise_scale = C.float(orDefault(p.NoiseScale, 0.667))
		m.noise_scale_w = C.float(orDefault(p.NoiseScaleW, 0.8))
		m.length_scale = C.float(orDefault(p.LengthScale, 1.0))
	case "matcha":
		if p.Model == "" || p.Vocoder == "" || p.Tokens == "" {
			return nil, errors.New("matcha: model、vocoder、tokens 不能为空")
		}
		m := &cfg.model.matcha
		m.acoustic_model = str(p.Model)
		m.vocoder = str(p.Vocoder)
		m.tokens = str(p.Tokens)
		m.lexicon = str(p.Lexicon)
		m.data_dir = str(p.DataDir)
		m.noise_scale = C.float(orDefault(p.NoiseScale, 1.0))
		m.length_scale = C.float(orDefault(p.LengthScale, 1.0))
	default:
		return nil, fmt.Errorf("不支持的 engine: %q", p.Engine)
	}

	impl := C.SherpaOnnxCreateOfflineTts(&cfg)
	if impl == nil {
		return nil, errors.New("SherpaOnnxCreateOfflineTts 失败（检查模型路径与文件）")
	}

	e = &Engine{
		impl:       impl,
		sampleRate: int(C.SherpaOnnxOfflineTtsSampleRate(impl)),
		speakers:   int(C.SherpaOnnxOfflineTtsNumSpeakers(impl)),
	}
	return e, nil
}

// Generate 合成语音，返回 [-1,1] 的采样点与采样率。speed > 1 更快。
func (e *Engine) Generate(text string, speed float32) ([]float32, int, error) {
	if e == nil || e.impl == nil {
		return nil, 0, errors.New("engine 已关闭")
	}
	ctext := C.CString(text)
	defer C.free(unsafe.Pointer(ctext))

	var gcfg C.SherpaOnnxGenerationConfig
	gcfg.speed = C.float(speed)
	gcfg.silence_scale = 0.2

	audio := C.SherpaOnnxOfflineTtsGenerateWithConfig(e.impl, ctext, &gcfg, nil, nil)
	if audio == nil {
		return nil, 0, errors.New("合成失败")
	}
	defer C.SherpaOnnxDestroyOfflineTtsGeneratedAudio(audio)

	n := int(audio.n)
	out := make([]float32, n)
	if n > 0 {
		copy(out, unsafe.Slice((*float32)(unsafe.Pointer(audio.samples)), n))
	}
	return out, int(audio.sample_rate), nil
}

// SampleRate 返回模型输出采样率。
func (e *Engine) SampleRate() int { return e.sampleRate }

// NumSpeakers 返回模型支持的说话人数量。
func (e *Engine) NumSpeakers() int { return e.speakers }

// Close 释放底层实例。
func (e *Engine) Close() {
	if e != nil && e.impl != nil {
		C.SherpaOnnxDestroyOfflineTts(e.impl)
		e.impl = nil
	}
}

func freeAll(ps []unsafe.Pointer) {
	for _, p := range ps {
		C.free(p)
	}
}

func orDefault(v, def float32) float32 {
	if v == 0 {
		return def
	}
	return v
}
