package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/trace"
	_ "modernc.org/sqlite"

	alexaclient    "github.com/rkrimper1/jarvis/api/internal/facility/alexa"
	"github.com/rkrimper1/jarvis/api/internal/intelligence/fusion"
	rsspoller      "github.com/rkrimper1/jarvis/api/internal/intelligence/rss"
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

	alexaDebug    := envString("ALEXA_DEBUG", "false") == "true"
	alexaCookies  := envString("ALEXA_COOKIES_PATH", "")

	var alexaClient *alexaclient.Client
	if alexaCookies != "" {
		var err error
		alexaClient, err = alexaclient.New(rootCtx, alexaCookies, alexaDebug)
		if err != nil {
			log.Warn("alexa client init failed — Alexa features disabled", slog.Any("err", err))
		} else {
			log.Info("alexa client ready", slog.String("cookies", alexaCookies))
			alexaClient.StartKeepAlive(rootCtx, envDuration("ALEXA_KEEPALIVE_INTERVAL", 12*time.Hour))
		}
	}

	hp := &profiler.HeapProfiler{
		OutDir:   envString("PPROF_DIR", "/tmp/profiles"),
		Interval: envDuration("PPROF_INTERVAL", 5*time.Minute),
		Log:      log,
	}
	hp.Start(rootCtx)

	// ── Shared SQLite database ────────────────────────────────────────
	// All stores (users, tasks, analytics, audit, knowledge) share one file.
	// WAL mode and foreign keys are enabled once here.
	jarvisDBPath := envString("JARVIS_DB_PATH", "")
	var sharedDB *sql.DB
	if jarvisDBPath != "" {
		var err error
		sharedDB, err = sql.Open("sqlite", jarvisDBPath)
		if err != nil {
			log.Error("jarvis db: open failed", slog.String("path", jarvisDBPath), slog.Any("err", err))
			os.Exit(1)
		}
		if _, err = sharedDB.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON`); err != nil {
			log.Error("jarvis db: pragma failed", slog.Any("err", err))
			os.Exit(1)
		}
		log.Info("jarvis db: opened", slog.String("path", jarvisDBPath))
		defer sharedDB.Close()
	}

	learningCfg := learningserver.Config{
		KnowledgeDB:        sharedDB,
		KnowledgeStaleDays: envInt("KNOWLEDGE_STALE_DAYS", 30),
		WebSearchMaxUses:   envInt("KNOWLEDGE_WEB_SEARCH_MAX_USES", 10),
		AnthropicAPIKey:    envString("ANTHROPIC_API_KEY", ""),
		ClaudeModel:        envString("CLAUDE_MODEL", "claude-sonnet-4-6"),
	}

	tokenSecret  := envString("TOKEN_SECRET", "stark-industries-dev-secret-change-in-prod")

	fusionCfg := fusion.Config{
		APIKey:        envString("ANTHROPIC_API_KEY", ""),
		Model:         envString("FUSION_MODEL", ""),
		MaxTokens:     envInt("FUSION_MAX_TOKENS", 0),
		ConfImmediate: envFloat32("FUSION_CONF_IMMEDIATE", 0),
		ConfVerify:    envFloat32("FUSION_CONF_VERIFY", 0),
		ConfReview:    envFloat32("FUSION_CONF_REVIEW", 0),
	}

	rssCfg := rsspoller.Config{
		FeedURLs: envStringSlice("RSS_FEED_URLS"),
		Interval: envDuration("RSS_POLL_INTERVAL", 15*time.Minute),
	}

	faceOutputDir      := envString("FACE_OUTPUT_DIR", "")
	faceCascadePath    := envString("FACE_CASCADE_PATH", "")
	faceMinSize        := envInt("FACE_MIN_SIZE", 65)
	faceQuality        := envFloat32("FACE_QUALITY_THRESHOLD", 6.0)
	faceCluster        := envFloat64("FACE_CLUSTER_OVERLAP", 0.25)
	faceTriangleSize   := envFloat64("FACE_OUTPUT_TRIANGLE_SIZE", 0)
	faceOpacity        := envFloat64("FACE_OUTPUT_OPACITY", 0)
	faceFontSize       := envFloat64("FACE_OUTPUT_FONT_SIZE", 0)
	faceMaxImageBytes  := envInt("FACE_MAX_IMAGE_BYTES", 0) // 0 → server default (5 MiB)

	// Create the HTTP mux first so newServer can register /alexa/* handlers on it.
	httpMux := http.NewServeMux()

	grpcSrv, healthSrv, gwMux, err := newServer(rootCtx, log, maxRecv, maxSend, hp, learningCfg, sharedDB, securityserver.FaceConfig{
		CascadePath: faceCascadePath,
		OutputDir:   faceOutputDir,
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
	}, alexaClient, alexaDebug, alexaCookies, httpMux, tokenSecret, fusionCfg, rssCfg)
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

// envStringSlice reads a comma-separated env var and returns the non-empty parts.
// Returns nil if the variable is unset or empty.
func envStringSlice(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
