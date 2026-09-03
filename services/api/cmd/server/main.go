package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"video-generator/services/api/internal/agents/core"
	"video-generator/services/api/internal/agents/novel"
	"video-generator/services/api/internal/httpapi"
	"video-generator/services/api/internal/store"
	"video-generator/services/api/internal/worker"
)

func main() {
	addr := env("API_ADDR", "127.0.0.1:8080")
	dataDir := env("DATA_DIR", "./data")
	workerURL := env("WORKER_URL", "http://127.0.0.1:8090")
	webDist := env("WEB_DIST", "./apps/web/dist")

	s, err := store.New(dataDir)
	if err != nil {
		slog.Error("initialize store", "error", err)
		os.Exit(1)
	}
	// Agent 与视频复用服务端口，但使用独立的业务包、长请求客户端和存储目录。
	novelAgent, err := novel.New(dataDir, core.NewWorker(workerURL))
	if err != nil {
		slog.Error("initialize novel agent", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(s, worker.New(workerURL), webDist, novelAgent).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	slog.Info("api listening", "addr", addr, "worker", workerURL)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
