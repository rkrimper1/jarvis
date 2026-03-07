package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	voicev1 "github.com/rkrimper1/jarvis/gen/voice"
	"github.com/rkrimper1/jarvis/services/voice-service/internal/config"
	"github.com/rkrimper1/jarvis/services/voice-service/internal/server"
	"github.com/rkrimper1/jarvis/services/voice-service/pkg/middleware"
)

func main() {
	// ── Logger ────────────────────────────────────────────────────────
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(log)

	// ── Config ────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Error("failed to load config", slog.Any("err", err))
		os.Exit(1)
	}

	// ── Voice server (dials NLP upstream) ────────────────────────────
	voiceServer, err := server.New(cfg, log)
	if err != nil {
		log.Error("failed to initialise voice server", slog.Any("err", err))
		os.Exit(1)
	}

	// ── gRPC Server ───────────────────────────────────────────────────
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(cfg.Server.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.Server.MaxSendMsgSize),
		grpc.ChainUnaryInterceptor(
			middleware.UnaryRecovery(log),
			middleware.UnaryLogger(log),
		),
		grpc.ChainStreamInterceptor(
			middleware.StreamRecovery(log),
			middleware.StreamLogger(log),
		),
	)

	// Register voice service
	voicev1.RegisterVoiceServiceServer(grpcServer, voiceServer)

	// Health check — consistent with all other Jarvis services
	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthSrv)
	healthSrv.SetServingStatus("jarvis.voice.VoiceService", grpc_health_v1.HealthCheckResponse_SERVING)

	// Reflection — grpcurl / Postman / Evans support
	reflection.Register(grpcServer)

	// ── Listener ──────────────────────────────────────────────────────
	addr := fmt.Sprintf(":%d", cfg.Server.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to listen", slog.String("addr", addr), slog.Any("err", err))
		os.Exit(1)
	}

	// ── Start ─────────────────────────────────────────────────────────
	log.Info("JARVIS Voice Service starting",
		slog.String("addr", addr),
		slog.String("service", "voice-service"),
		slog.String("nlp_upstream", cfg.NLP.Addr),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			errCh <- err
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
	case err := <-errCh:
		log.Error("server error", slog.Any("err", err))
	}

	healthSrv.SetServingStatus("jarvis.voice.VoiceService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	log.Info("gracefully stopping server...")
	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	select {
	case <-stopped:
		log.Info("server stopped cleanly")
	case <-ctx.Done():
		log.Warn("shutdown timeout exceeded — forcing stop")
		grpcServer.Stop()
	}
}
