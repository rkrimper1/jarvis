package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/rkrimper1/jarvis/api/middleware"
	"github.com/rkrimper1/jarvis/api/rest"

	commandv1  "github.com/rkrimper1/jarvis/api/pb/command"
	businessv1 "github.com/rkrimper1/jarvis/api/pb/business"
	facilityv1 "github.com/rkrimper1/jarvis/api/pb/facility"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
	learningv1 "github.com/rkrimper1/jarvis/api/pb/learning"
	nlpv1      "github.com/rkrimper1/jarvis/api/pb/nlp"
	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
	taskv1     "github.com/rkrimper1/jarvis/api/pb/task"
	userv1     "github.com/rkrimper1/jarvis/api/pb/user"
	voicev1    "github.com/rkrimper1/jarvis/api/pb/voice"

	commandserver  "github.com/rkrimper1/jarvis/api/internal/command/server"
	businessserver "github.com/rkrimper1/jarvis/api/internal/business-ops/server"
	alexaclient    "github.com/rkrimper1/jarvis/api/internal/facility/alexa"
	facilityserver "github.com/rkrimper1/jarvis/api/internal/facility/server"
	intelligserver "github.com/rkrimper1/jarvis/api/internal/intelligence/server"
	intelstore     "github.com/rkrimper1/jarvis/api/internal/intelligence/store"
	"github.com/rkrimper1/jarvis/api/internal/intelligence/fusion"
	learningserver "github.com/rkrimper1/jarvis/api/internal/learning/server"
	nlpserver      "github.com/rkrimper1/jarvis/api/internal/nlp/server"
	securityserver "github.com/rkrimper1/jarvis/api/internal/security/server"
	taskserver     "github.com/rkrimper1/jarvis/api/internal/task/server"
	taskstore      "github.com/rkrimper1/jarvis/api/internal/task/store"
	userserver     "github.com/rkrimper1/jarvis/api/internal/user/server"
	userstore      "github.com/rkrimper1/jarvis/api/internal/user/store"
	voiceserver    "github.com/rkrimper1/jarvis/api/internal/voice/server"

	"github.com/rkrimper1/jarvis/api/internal/profiler"
	nlpconfig      "github.com/rkrimper1/jarvis/api/internal/nlp/config"
	securityconfig "github.com/rkrimper1/jarvis/api/internal/security/config"
	voiceconfig    "github.com/rkrimper1/jarvis/api/internal/voice/config"
)

// serviceNames is the list of gRPC fully-qualified service names registered
// with the health server. Used to mark all services SERVING / NOT_SERVING
// on startup and shutdown.
var serviceNames = []string{
	"jarvis.command.CommandService",
	"jarvis.business.BusinessOpsService",
	"jarvis.facility.FacilityService",
	"jarvis.intelligence.IntelligenceService",
	"jarvis.learning.LearningService",
	"jarvis.nlp.NLPService",
	"jarvis.security.SecurityService",
	"jarvis.task.TaskService",
	"jarvis.user.UserService",
	"jarvis.voice.VoiceService",
}

// newServer creates the unified gRPC server and grpc-gateway HTTP mux,
// instantiates all service implementations, and registers them on both.
// db is the shared jarvis.db connection (nil disables all SQLite stores).
func newServer(log *slog.Logger, maxRecv, maxSend int, hp *profiler.HeapProfiler, learningCfg learningserver.Config, db *sql.DB, faceCfg securityserver.FaceConfig, alexaClient *alexaclient.Client, alexaDebug bool, cookiesPath string, httpMux *http.ServeMux, tokenSecret string, fusionCfg fusion.Config) (*grpc.Server, *health.Server, *runtime.ServeMux, error) {
	ctx := context.Background()

	// ── gRPC server ───────────────────────────────────────────────────
	grpcSrv := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxRecv),
		grpc.MaxSendMsgSize(maxSend),
		grpc.ChainUnaryInterceptor(
			middleware.UnaryTracing(),
			middleware.UnaryRecovery(log),
			middleware.UnaryLogger(log),
		),
		grpc.ChainStreamInterceptor(
			middleware.StreamTracing(),
			middleware.StreamRecovery(log),
			middleware.StreamLogger(log),
		),
	)

	// ── User store (shared by user service + security auth) ───────────
	var uStore *userstore.Store
	if db != nil {
		var err error
		uStore, err = userstore.New(db, log)
		if err != nil {
			log.Error("user store init failed", slog.Any("err", err))
			// non-fatal — fall back to hardcoded auth engine
		}
	}

	// ── Service: command ──────────────────────────────────────────────
	cmdSrv := commandserver.New(hp, log)
	commandv1.RegisterCommandServiceServer(grpcSrv, cmdSrv)

	// ── Service: business-ops ─────────────────────────────────────────
	businessv1.RegisterBusinessOpsServiceServer(grpcSrv, businessserver.New(log))

	// ── Service: facility ─────────────────────────────────────────────
	facilitySrv := facilityserver.NewWithAlexa(log, alexaClient, alexaDebug, cookiesPath)
	if cookiesPath != "" {
		httpMux.HandleFunc("/alexa/cookie-status", facilitySrv.CookieStatusHandler())
		httpMux.HandleFunc("/alexa/cookies", facilitySrv.CookieUploadHandler())
		httpMux.HandleFunc("/alexa/text-command", facilitySrv.TextCommandHandler())
	}
	facilityv1.RegisterFacilityServiceServer(grpcSrv, facilitySrv)

	// ── Service: intelligence ─────────────────────────────────────────
	var intelligSrv *intelligserver.IntelligenceServer
	if db != nil && fusionCfg.APIKey != "" {
		iStore, err := intelstore.New(db, log)
		if err != nil {
			log.Error("intel store init failed", slog.Any("err", err))
		} else {
			eng, err := fusion.New(fusionCfg)
			if err != nil {
				log.Error("fusion engine init failed", slog.Any("err", err))
			} else {
				intelligSrv = intelligserver.NewWithIntelHunt(log, iStore, eng)
				log.Info("intel hunt enabled")
			}
		}
	}
	if intelligSrv == nil {
		intelligSrv = intelligserver.New(log)
	}
	intelligv1.RegisterIntelligenceServiceServer(grpcSrv, intelligSrv)

	// ── Service: learning ─────────────────────────────────────────────
	learnSrv := learningserver.New(log, learningCfg)
	learningv1.RegisterLearningServiceServer(grpcSrv, learnSrv)

	// ── Service: nlp ──────────────────────────────────────────────────
	nlpCfg, err := nlpconfig.Load()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("nlp config: %w", err)
	}
	nlpSrv := nlpserver.New(nlpCfg, log)
	nlpv1.RegisterNLPServiceServer(grpcSrv, nlpSrv)

	// ── Service: security ─────────────────────────────────────────────
	secCfg, err := securityconfig.Load()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("security config: %w", err)
	}
	faceCfg.AnthropicKey = learningCfg.AnthropicAPIKey
	faceCfg.ClaudeModel = learningCfg.ClaudeModel
	secSrv := securityserver.NewWithUserStoreFace(secCfg, log, uStore, db, faceCfg)
	securityv1.RegisterSecurityServiceServer(grpcSrv, secSrv)

	// ── Service: task ─────────────────────────────────────────────────
	var taskSrv *taskserver.TaskServer
	if db != nil {
		tStore, err := taskstore.New(db, log)
		if err != nil {
			log.Error("task store init failed", slog.Any("err", err))
		} else {
			taskSrv = taskserver.New(tStore, tokenSecret, log)
		}
	}
	if taskSrv == nil {
		taskSrv = taskserver.New(nil, tokenSecret, log)
	}
	taskv1.RegisterTaskServiceServer(grpcSrv, taskSrv)

	// ── Service: user ─────────────────────────────────────────────────
	userSrv := userserver.New(uStore, log)
	userv1.RegisterUserServiceServer(grpcSrv, userSrv)

	// ── Service: voice ────────────────────────────────────────────────
	// NLP is wired in-process via the adapter — no network hop.
	voiceCfg, err := voiceconfig.Load()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("voice config: %w", err)
	}
	voiceSrv, err := voiceserver.New(voiceCfg, &nlpServerAdapter{srv: nlpSrv}, log)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("voice server init: %w", err)
	}
	voicev1.RegisterVoiceServiceServer(grpcSrv, voiceSrv)

	// ── Health + reflection ───────────────────────────────────────────
	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcSrv, healthSrv)
	for _, name := range serviceNames {
		healthSrv.SetServingStatus(name, grpc_health_v1.HealthCheckResponse_SERVING)
	}
	reflection.Register(grpcSrv)

	// ── grpc-gateway HTTP mux ─────────────────────────────────────────
	// HandlerServer variants call service implementations in-process —
	// no dial, no serialization overhead.
	gwMux := runtime.NewServeMux(
		runtime.WithErrorHandler(rest.CustomErrorHandler),
	)
	if err := commandv1.RegisterCommandServiceHandlerServer(ctx, gwMux, cmdSrv); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway command: %w", err)
	}
	if err := businessv1.RegisterBusinessOpsServiceHandlerServer(ctx, gwMux, businessserver.New(log)); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway business-ops: %w", err)
	}
	if err := facilityv1.RegisterFacilityServiceHandlerServer(ctx, gwMux, facilitySrv); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway facility: %w", err)
	}
	if err := intelligv1.RegisterIntelligenceServiceHandlerServer(ctx, gwMux, intelligSrv); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway intelligence: %w", err)
	}
	if err := learningv1.RegisterLearningServiceHandlerServer(ctx, gwMux, learnSrv); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway learning: %w", err)
	}
	if err := nlpv1.RegisterNLPServiceHandlerServer(ctx, gwMux, nlpSrv); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway nlp: %w", err)
	}
	if err := securityv1.RegisterSecurityServiceHandlerServer(ctx, gwMux, secSrv); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway security: %w", err)
	}
	if err := taskv1.RegisterTaskServiceHandlerServer(ctx, gwMux, taskSrv); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway task: %w", err)
	}
	if err := userv1.RegisterUserServiceHandlerServer(ctx, gwMux, userSrv); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway user: %w", err)
	}
	if err := voicev1.RegisterVoiceServiceHandlerServer(ctx, gwMux, voiceSrv); err != nil {
		return nil, nil, nil, fmt.Errorf("gateway voice: %w", err)
	}

	return grpcSrv, healthSrv, gwMux, nil
}

// markNotServing transitions all registered services to NOT_SERVING during
// graceful shutdown so load balancers drain connections before we stop.
func markNotServing(healthSrv *health.Server) {
	for _, name := range serviceNames {
		healthSrv.SetServingStatus(name, grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	}
}
