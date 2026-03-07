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

	hardwarev1 "github.com/rkrimper1/jarvis/gen/hardware"
	"github.com/rkrimper1/jarvis/services/hardware-service/internal/config"
	"github.com/rkrimper1/jarvis/services/hardware-service/internal/server"
	"github.com/rkrimper1/jarvis/services/hardware-service/pkg/middleware"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("config load failed", slog.Any("err", err))
		os.Exit(1)
	}

	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(cfg.Server.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.Server.MaxSendMsgSize),
		grpc.ChainUnaryInterceptor(middleware.UnaryRecovery(log), middleware.UnaryLogger(log)),
		grpc.ChainStreamInterceptor(middleware.StreamRecovery(log), middleware.StreamLogger(log)),
	)

	hardwarev1.RegisterHardwareServiceServer(grpcServer, server.New(cfg, log))

	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthSrv)
	healthSrv.SetServingStatus("jarvis.hardware.HardwareService", grpc_health_v1.HealthCheckResponse_SERVING)
	reflection.Register(grpcServer)

	addr := fmt.Sprintf(":%d", cfg.Server.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("listen failed", slog.String("addr", addr), slog.Any("err", err))
		os.Exit(1)
	}
	log.Info("JARVIS Hardware Service starting", slog.String("addr", addr))

	go grpcServer.Serve(lis)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	healthSrv.SetServingStatus("jarvis.hardware.HardwareService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	stopped := make(chan struct{})
	go func() { grpcServer.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
		log.Info("hardware service stopped cleanly")
	case <-ctx.Done():
		grpcServer.Stop()
	}
}
