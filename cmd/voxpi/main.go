// Command voxpi 是 OpenAI 兼容的离线 TTS 服务，基于 sherpa-onnx（Piper/Matcha）。
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"voxpi/internal/config"
	"voxpi/internal/server"
)

const version = "0.1.0"

func main() {
	configPath := flag.String("config", "voxpi.json", "配置文件路径")
	listen := flag.String("listen", "", "覆盖配置中的监听地址，例如 0.0.0.0:8000")
	showVersion := flag.Bool("version", false, "打印版本后退出")
	flag.Parse()

	if *showVersion {
		println("voxpi", version)
		return
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("读取配置失败", "err", err)
		os.Exit(1)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}

	srv, err := server.New(cfg)
	if err != nil {
		slog.Error("启动失败", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("VoxPi 启动", "version", version, "listen", cfg.Listen, "models", len(cfg.Models))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		slog.Error("HTTP 服务异常", "err", err)
		srv.Close()
		os.Exit(1)
	case <-ctx.Done():
	}

	slog.Info("收到退出信号，开始关闭")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP 关闭超时", "err", err)
	}
	srv.Close()
	slog.Info("已退出")
}
