// Package config 负责读取 voxpi.json。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"voxpi/internal/tts"
)

// Config 是 VoxPi 的运行时配置。
type Config struct {
	Listen        string                `json:"listen"`
	APIKey        string                `json:"api_key"`
	NumThreads    int32                 `json:"num_threads"`
	QueueSize     int                   `json:"queue_size"`
	MaxInputChars int                   `json:"max_input_chars"`
	Models        map[string]tts.Params `json:"models"`
}

// Load 读取配置文件并补齐默认值。模型内的相对路径按配置文件所在目录解析。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}

	if c.Listen == "" {
		c.Listen = "127.0.0.1:8000"
	}
	if c.NumThreads <= 0 {
		c.NumThreads = int32(runtime.NumCPU())
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 8
	}
	if c.MaxInputChars <= 0 {
		c.MaxInputChars = 2000
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv("VOXPI_API_KEY")
	}
	if len(c.Models) == 0 {
		return nil, fmt.Errorf("%s 没有配置任何 models", path)
	}

	base := filepath.Dir(path)
	for name, p := range c.Models {
		p.Model = abs(base, p.Model)
		p.Tokens = abs(base, p.Tokens)
		p.Lexicon = abs(base, p.Lexicon)
		p.Vocoder = abs(base, p.Vocoder)
		p.DataDir = abs(base, p.DataDir)
		p.NumThreads = c.NumThreads
		if err := checkExists(name, p); err != nil {
			return nil, err
		}
		c.Models[name] = p
	}
	return &c, nil
}

func abs(base, p string) string {
	p = os.ExpandEnv(p)
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

func checkExists(name string, p tts.Params) error {
	required := map[string]string{"model": p.Model, "tokens": p.Tokens}
	if p.DataDir != "" {
		required["data_dir"] = p.DataDir
	}
	if p.Lexicon != "" {
		required["lexicon"] = p.Lexicon
	}
	if p.Vocoder != "" {
		required["vocoder"] = p.Vocoder
	}
	for field, path := range required {
		if path == "" {
			return fmt.Errorf("model %q: %s 不能为空", name, field)
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("model %q: %s 不可用: %w", name, field, err)
		}
	}
	return nil
}
