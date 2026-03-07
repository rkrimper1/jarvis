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

	securityv1 "github.com/rkrimper1/jarvis/gen/security"
	"github.com/rkrimper1/jarvis/services/security-service/internal/config"
	"github.com/rkrimper1/jarvis/services/security-service/internal/server"
	"github.com/rkrimper1/jarvis/services/security-service/pkg/middleware"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("config load failed", slog.Any("err", err))
		os.Exit(1)
	}

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

	securityServer := server.New(cfg, log)
	securityv1.RegisterSecurityServiceServer(grpcServer, securityServer)

	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthSrv)
	healthSrv.SetServingStatus("jarvis.security.SecurityService", grpc_health_v1.HealthCheckResponse_SERVING)

	reflection.Register(grpcServer)

	addr := fmt.Sprintf(":%d", cfg.Server.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("listen failed", slog.String("addr", addr), slog.Any("err", err))
		os.Exit(1)
	}

	log.Info("JARVIS Security Service starting",
		slog.String("addr", addr),
		slog.String("service", "security-service"),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
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

	healthSrv.SetServingStatus("jarvis.security.SecurityService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

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
		log.Warn("shutdown timeout — forcing stop")
		grpcServer.Stop()
	}
}
