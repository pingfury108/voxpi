// Package server 实现 OpenAI 兼容的 /v1/audio/speech 服务。
package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"voxpi/internal/audio"
	"voxpi/internal/config"
	"voxpi/internal/tts"
)

const maxBodyBytes = 1 << 20

// Server 持有已加载的模型、单 worker 队列和 HTTP 路由。
type Server struct {
	cfg     *config.Config
	engines map[string]*tts.Engine
	jobs    chan *job
	ffmpeg  string
	started time.Time

	wg     sync.WaitGroup
	closed chan struct{}
}

type job struct {
	model string
	text  string
	speed float32
	resp  chan result
}

type result struct {
	samples    []float32
	sampleRate int
	genTime    time.Duration
	err        error
}

type speechRequest struct {
	Model          string   `json:"model"`
	Input          string   `json:"input"`
	Voice          string   `json:"voice"`
	ResponseFormat string   `json:"response_format"`
	Speed          *float64 `json:"speed"`
}

// New 加载全部模型并启动推理 worker。模型加载耗时可能数十秒。
func New(cfg *config.Config) (*Server, error) {
	s := &Server{
		cfg:     cfg,
		engines: make(map[string]*tts.Engine, len(cfg.Models)),
		jobs:    make(chan *job, cfg.QueueSize),
		started: time.Now(),
		closed:  make(chan struct{}),
	}

	for name, p := range cfg.Models {
		begin := time.Now()
		eng, err := tts.New(p)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("加载模型 %q 失败: %w", name, err)
		}
		s.engines[name] = eng
		slog.Info("模型已加载", "model", name, "sample_rate", eng.SampleRate(),
			"speakers", eng.NumSpeakers(), "cost", time.Since(begin).Round(time.Millisecond))
	}

	if path, err := exec.LookPath("ffmpeg"); err == nil {
		s.ffmpeg = path
		slog.Info("检测到 ffmpeg，可返回 mp3", "path", path)
	} else {
		slog.Warn("未找到 ffmpeg，mp3 请求将返回 400")
	}

	s.wg.Add(1)
	go s.worker()
	return s, nil
}

// Handler 返回带鉴权的路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/audio/speech", s.handleSpeech)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return s.auth(mux)
}

// Close 停止 worker 并释放模型。
func (s *Server) Close() {
	select {
	case <-s.closed:
		return
	default:
	}
	close(s.closed)
	close(s.jobs)
	s.wg.Wait()
	for _, eng := range s.engines {
		eng.Close()
	}
}

func (s *Server) worker() {
	defer s.wg.Done()
	for j := range s.jobs {
		eng := s.engines[j.model]
		begin := time.Now()
		samples, rate, err := eng.Generate(j.text, j.speed)
		res := result{samples: samples, sampleRate: rate, genTime: time.Since(begin), err: err}
		select {
		case j.resp <- res:
		case <-s.closed:
			return
		}
	}
}

func (s *Server) auth(next http.Handler) http.Handler {
	if s.cfg.APIKey == "" {
		return next
	}
	want := "Bearer " + s.cfg.APIKey
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != want {
			writeError(w, http.StatusUnauthorized, "invalid api key", "invalid_request_error")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleSpeech(w http.ResponseWriter, r *http.Request) {
	var req speechRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error(), "invalid_request_error")
		return
	}

	model, err := s.resolveModel(req.Model, req.Voice)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	text := strings.TrimSpace(req.Input)
	if text == "" {
		writeError(w, http.StatusBadRequest, "input 不能为空", "invalid_request_error")
		return
	}
	if n := len([]rune(text)); n > s.cfg.MaxInputChars {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("input 过长: %d 字符，上限 %d", n, s.cfg.MaxInputChars), "invalid_request_error")
		return
	}

	speed := 1.0
	if req.Speed != nil {
		speed = *req.Speed
	}
	if speed < 0.25 || speed > 4.0 {
		writeError(w, http.StatusBadRequest, "speed 需在 0.25–4.0 之间", "invalid_request_error")
		return
	}

	format := strings.ToLower(req.ResponseFormat)
	if format == "" {
		format = "wav"
	}
	switch format {
	case "wav", "pcm":
	case "mp3":
		if s.ffmpeg == "" {
			writeError(w, http.StatusBadRequest, "服务端未安装 ffmpeg，请使用 wav 或 pcm", "invalid_request_error")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "response_format 仅支持 wav、pcm、mp3", "invalid_request_error")
		return
	}

	j := &job{model: model, text: text, speed: float32(speed), resp: make(chan result, 1)}
	select {
	case s.jobs <- j:
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "队列已满，请稍后重试", "rate_limit_error")
		return
	}

	var res result
	select {
	case res = <-j.resp:
	case <-r.Context().Done():
		slog.Warn("客户端已断开", "model", model)
		return
	}
	if res.err != nil {
		slog.Error("合成失败", "model", model, "err", res.err)
		writeError(w, http.StatusInternalServerError, "合成失败: "+res.err.Error(), "server_error")
		return
	}

	audioMS := int64(audio.AudioDuration(len(res.samples), res.sampleRate) * 1000)
	rtf := 0.0
	if audioMS > 0 {
		rtf = float64(res.genTime.Milliseconds()) / float64(audioMS)
	}
	w.Header().Set("X-VoxPi-Model", model)
	w.Header().Set("X-VoxPi-Gen-Ms", strconv.FormatInt(res.genTime.Milliseconds(), 10))
	w.Header().Set("X-VoxPi-Audio-Ms", strconv.FormatInt(audioMS, 10))
	w.Header().Set("X-VoxPi-Rtf", fmt.Sprintf("%.3f", rtf))

	body, contentType, err := encode(format, res.samples, res.sampleRate, s.ffmpeg)
	if err != nil {
		slog.Error("编码失败", "format", format, "err", err)
		writeError(w, http.StatusInternalServerError, "编码失败: "+err.Error(), "server_error")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if _, err := w.Write(body); err != nil {
		slog.Warn("写响应失败", "err", err)
	}

	slog.Info("speech",
		"model", model,
		"chars", len([]rune(text)),
		"speed", speed,
		"format", format,
		"gen_ms", res.genTime.Milliseconds(),
		"audio_ms", audioMS,
		"rtf", fmt.Sprintf("%.2f", rtf))
}

func (s *Server) resolveModel(model, voice string) (string, error) {
	if model != "" {
		if _, ok := s.engines[model]; !ok {
			return "", fmt.Errorf("未知 model %q，可选: %s", model, strings.Join(s.modelNames(), ", "))
		}
		return model, nil
	}
	if voice != "" {
		if _, ok := s.engines[voice]; ok {
			return voice, nil
		}
	}
	if len(s.engines) == 1 {
		for name := range s.engines {
			return name, nil
		}
	}
	return "", fmt.Errorf("请指定 model，可选: %s", strings.Join(s.modelNames(), ", "))
}

func (s *Server) modelNames() []string {
	names := make([]string, 0, len(s.engines))
	for name := range s.engines {
		names = append(names, name)
	}
	return names
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	type modelInfo struct {
		ID         string `json:"id"`
		Object     string `json:"object"`
		Created    int64  `json:"created"`
		OwnedBy    string `json:"owned_by"`
		SampleRate int    `json:"sample_rate"`
		Speakers   int    `json:"speakers"`
	}
	data := make([]modelInfo, 0, len(s.engines))
	for name, eng := range s.engines {
		data = append(data, modelInfo{
			ID: name, Object: "model", Created: s.started.Unix(), OwnedBy: "voxpi",
			SampleRate: eng.SampleRate(), Speakers: eng.NumSpeakers(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	models := make([]map[string]any, 0, len(s.engines))
	for name, eng := range s.engines {
		models = append(models, map[string]any{
			"id": name, "sample_rate": eng.SampleRate(), "speakers": eng.NumSpeakers(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"uptime_s":       int64(time.Since(s.started).Seconds()),
		"queue_depth":    len(s.jobs),
		"queue_capacity": cap(s.jobs),
		"models":         models,
		"temp_c":         cpuTempC(),
		"rss_mb":         rssMB(),
		"goroutines":     runtime.NumGoroutine(),
	})
}

func encode(format string, samples []float32, sampleRate int, ffmpeg string) ([]byte, string, error) {
	switch format {
	case "wav":
		var buf bytes.Buffer
		buf.Grow(audio.WAVHeaderSize + len(samples)*2)
		if err := audio.WriteWAV16(&buf, samples, sampleRate); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "audio/wav", nil
	case "pcm":
		return audio.PCM16(samples), "audio/pcm", nil
	case "mp3":
		return encodeMP3(samples, sampleRate, ffmpeg)
	default:
		return nil, "", errors.New("不支持的格式: " + format)
	}
}

func encodeMP3(samples []float32, sampleRate int, ffmpeg string) ([]byte, string, error) {
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "s16le", "-ar", strconv.Itoa(sampleRate), "-ac", "1",
		"-i", "pipe:0", "-f", "mp3", "-b:a", "64k", "pipe:1",
	}
	cmd := exec.Command(ffmpeg, args...)
	cmd.Stdin = bytes.NewReader(audio.PCM16(samples))
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), "audio/mpeg", nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, typ string) {
	if typ == "" {
		typ = "invalid_request_error"
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{"message": msg, "type": typ, "code": status},
	})
}

func cpuTempC() float64 {
	raw, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	milli, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0
	}
	return float64(milli) / 1000
}

func rssMB() int {
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return pages * os.Getpagesize() / (1 << 20)
}
