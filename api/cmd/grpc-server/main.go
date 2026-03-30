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

	"contrib.go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/trace"

	"github.com/rkrimper1/jarvis/api/internal/profiler"
	"github.com/rkrimper1/jarvis/api/internal/security/faceanalysis"
	learningserver  "github.com/rkrimper1/jarvis/api/internal/learning/server"
	securityserver  "github.com/rkrimper1/jarvis/api/internal/security/server"
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

	tracingEnabled := envString("TRACING_ENABLED", "false") == "true"
	flushTraces := initTracing(tracingEnabled, log)
	defer flushTraces()

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

	faceOutputDir      := envString("FACE_OUTPUT_DIR", "")
	faceCascadePath    := envString("FACE_CASCADE_PATH", "")
	faceMinSize        := envInt("FACE_MIN_SIZE", 65)
	faceQuality        := envFloat32("FACE_QUALITY_THRESHOLD", 6.0)
	faceCluster        := envFloat64("FACE_CLUSTER_OVERLAP", 0.25)
	faceTriangleSize   := envFloat64("FACE_OUTPUT_TRIANGLE_SIZE", 0)
	faceOpacity        := envFloat64("FACE_OUTPUT_OPACITY", 0)
	faceFontSize       := envFloat64("FACE_OUTPUT_FONT_SIZE", 0)
	faceMaxImageBytes  := envInt("FACE_MAX_IMAGE_BYTES", 0) // 0 → server default (5 MiB)
	analyticsDBPath    := envString("SECURITY_ANALYTICS_DB_PATH", "")

	grpcSrv, healthSrv, gwMux, err := newServer(log, maxRecv, maxSend, hp, learningCfg, usersDBPath, securityserver.FaceConfig{
		CascadePath:     faceCascadePath,
		OutputDir:       faceOutputDir,
		AnalyticsDBPath: analyticsDBPath,
		DetectParams: faceanalysis.DetectParams{
			MinSize:          faceMinSize,
			QualityThreshold: faceQuality,
			ClusterOverlap:   faceCluster,
		},
		AnnotateParams: faceanalysis.AnnotateParams{
			TriangleSize: faceTriangleSize,
			Opacity:      faceOpacity,
			FontSize:     faceFontSize,
		},
		MaxImageBytes: faceMaxImageBytes,
	})
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

	// ── HTTP/REST listener (grpc-gateway + static face images) ───────
	httpAddr := fmt.Sprintf(":%d", httpPort)
	httpMux := http.NewServeMux()
	httpMux.Handle("/v1/", gwMux)
	if faceOutputDir != "" {
		httpMux.Handle("/faces/", http.StripPrefix("/faces/", http.FileServer(http.Dir(faceOutputDir))))
	}
	// Fallback: anything else goes to the gateway (health, etc.)
	httpMux.Handle("/", gwMux)
	httpSrv := &http.Server{
		Addr:    httpAddr,
		Handler: httpMux,
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

func initTracing(enabled bool, log *slog.Logger) func() {
	if !enabled {
		trace.ApplyConfig(trace.Config{DefaultSampler: trace.NeverSample()})
		log.Info("tracing disabled — set TRACING_ENABLED=true to enable")
		return func() {}
	}
	exporter, err := stackdriver.NewExporter(stackdriver.Options{
		OnError: func(e error) {
			log.Error("stackdriver trace export error", slog.Any("err", e))
		},
	})
	if err != nil {
		log.Error("stackdriver exporter init failed — tracing disabled", slog.Any("err", err))
		trace.ApplyConfig(trace.Config{DefaultSampler: trace.NeverSample()})
		return func() {}
	}
	trace.RegisterExporter(exporter)
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	log.Info("tracing enabled", slog.String("exporter", "stackdriver"))
	return func() { exporter.Flush() }
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

func envFloat32(key string, def float32) float32 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			return float32(f)
		}
	}
	return def
}

func envFloat64(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
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
