package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/profiler"
	learningserver "github.com/rkrimper1/jarvis/api/internal/learning/server"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(log)

	grpcPort        := envInt("GRPC_PORT", 50051)
	httpPort        := envInt("HTTP_PORT", 8080)
	shutdownTimeout := envDuration("SHUTDOWN_TIMEOUT", 10*time.Second)
	maxRecv         := envInt("MAX_RECV_MSG_SIZE", 8*1024*1024)
	maxSend         := envInt("MAX_SEND_MSG_SIZE", 8*1024*1024)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	hp := &profiler.HeapProfiler{
		OutDir:   envString("PPROF_DIR", "/tmp/profiles"),
		Interval: envDuration("PPROF_INTERVAL", 5*time.Minute),
		Log:      log,
	}
	hp.Start(rootCtx)

	learningCfg := learningserver.Config{
		KnowledgeDBPath:    envString("KNOWLEDGE_DB_PATH", ""),
		KnowledgeStaleDays: envInt("KNOWLEDGE_STALE_DAYS", 30),
		WebSearchMaxUses:   envInt("KNOWLEDGE_WEB_SEARCH_MAX_USES", 10),
		AnthropicAPIKey:    envString("ANTHROPIC_API_KEY", ""),
		ClaudeModel:        envString("CLAUDE_MODEL", "claude-sonnet-4-6"),
	}

	usersDBPath := envString("USERS_DB_PATH", "")

	grpcSrv, healthSrv, gwMux, err := newServer(log, maxRecv, maxSend, hp, learningCfg, usersDBPath)
	if err != nil {
		log.Error("server init failed", slog.Any("err", err))
		os.Exit(1)
	}

	// ── gRPC listener ─────────────────────────────────────────────────
	grpcAddr := fmt.Sprintf(":%d", grpcPort)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("grpc listen failed", slog.String("addr", grpcAddr), slog.Any("err", err))
		os.Exit(1)
	}

	// ── HTTP/REST listener (grpc-gateway) ─────────────────────────────
	httpAddr := fmt.Sprintf(":%d", httpPort)
	httpSrv := &http.Server{
		Addr:    httpAddr,
		Handler: gwMux,
	}

	log.Info("JARVIS starting",
		slog.String("grpc", grpcAddr),
		slog.String("http", httpAddr),
		slog.Int("services", len(serviceNames)),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			errCh <- err
		}
	}()
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
	case err := <-errCh:
		log.Error("server error", slog.Any("err", err))
	}

	rootCancel()
	markNotServing(healthSrv)

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	_ = httpSrv.Shutdown(ctx)

	stopped := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		log.Info("server stopped cleanly")
	case <-ctx.Done():
		log.Warn("shutdown timeout — forcing stop")
		grpcSrv.Stop()
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
