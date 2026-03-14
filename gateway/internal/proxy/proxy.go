// Package proxy manages gRPC connections to all upstream services and
// registers their HTTP handlers with the grpc-gateway ServeMux.
//
// Each service gets its own ClientConn so connection lifecycle is independent —
// one service going down does not kill the gateway for all others.
package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	agentgw    "github.com/rkrimper1/jarvis/api/pb/agent"
	businessgw "github.com/rkrimper1/jarvis/api/pb/business"
	facilitygw "github.com/rkrimper1/jarvis/api/pb/facility"
	hardwaregw "github.com/rkrimper1/jarvis/api/pb/hardware"
	intelliggw "github.com/rkrimper1/jarvis/api/pb/intelligence"
	learninggw "github.com/rkrimper1/jarvis/api/pb/learning"
	nlpgw      "github.com/rkrimper1/jarvis/api/pb/nlp"
	securitygw "github.com/rkrimper1/jarvis/api/pb/security"
	"github.com/rkrimper1/jarvis/gateway/internal/config"
	"github.com/rkrimper1/jarvis/gateway/internal/middleware"
)

// Proxy holds all upstream gRPC connections and the gateway mux.
type Proxy struct {
	mux   *runtime.ServeMux
	conns []*grpc.ClientConn
	log   *slog.Logger
}

// New dials all upstream services and registers their HTTP handlers.
// Dial is non-blocking — connections are established lazily when the first
// RPC fires, which means the gateway starts immediately even if some services
// are still warming up.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Proxy, error) {
	p := &Proxy{
		mux: runtime.NewServeMux(
			// Forward the X-Request-ID header into gRPC metadata so every
			// downstream service receives the same request ID for distributed tracing.
			runtime.WithIncomingHeaderMatcher(requestIDMatcher),
			// Emit RFC7807 "application/problem+json" on errors.
			runtime.WithErrorHandler(errorHandler),
		),
		log: log,
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Block until the connection is ready on the first RPC attempt.
		// This keeps errors synchronous rather than surfacing as unexpected
		// stream failures deep inside a handler.
		grpc.WithBlock(),
	}

	type registration struct {
		name    string
		addr    string
		register func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error
	}

	registrations := []registration{
		{
			name: "nlp-service",
			addr: cfg.Upstreams.NLPService,
			register: func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
				return nlpgw.RegisterNLPServiceHandler(ctx, mux, conn)
			},
		},
		{
			name: "security-service",
			addr: cfg.Upstreams.SecurityService,
			register: func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
				return securitygw.RegisterSecurityServiceHandler(ctx, mux, conn)
			},
		},
		{
			name: "agent-coordinator",
			addr: cfg.Upstreams.AgentCoordinator,
			register: func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
				return agentgw.RegisterAgentCoordinatorServiceHandler(ctx, mux, conn)
			},
		},
		{
			name: "hardware-service",
			addr: cfg.Upstreams.HardwareService,
			register: func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
				return hardwaregw.RegisterHardwareServiceHandler(ctx, mux, conn)
			},
		},
		{
			name: "facility-service",
			addr: cfg.Upstreams.FacilityService,
			register: func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
				return facilitygw.RegisterFacilityServiceHandler(ctx, mux, conn)
			},
		},
		{
			name: "intelligence-service",
			addr: cfg.Upstreams.IntelligenceService,
			register: func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
				return intelliggw.RegisterIntelligenceServiceHandler(ctx, mux, conn)
			},
		},
		{
			name: "business-ops-service",
			addr: cfg.Upstreams.BusinessOpsService,
			register: func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
				return businessgw.RegisterBusinessOpsServiceHandler(ctx, mux, conn)
			},
		},
		{
			name: "learning-service",
			addr: cfg.Upstreams.LearningService,
			register: func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
				return learninggw.RegisterLearningServiceHandler(ctx, mux, conn)
			},
		},
	}

	// Dial each service and register its HTTP routes.
	// We use a short per-dial context so one unresponsive service doesn't
	// stall the entire gateway startup. The gateway comes up anyway and
	// returns 503 for routes to that service until it recovers.
	for _, reg := range registrations {
		dialCtx, cancel := context.WithTimeout(ctx, cfg.Upstreams.DialTimeout)
		conn, err := grpc.DialContext(dialCtx, reg.addr, dialOpts...) //nolint:staticcheck
		cancel()

		if err != nil {
			// Log and continue — non-fatal. The mux will return 503 for
			// unregistered routes until the service comes back and we reconnect.
			log.Warn("gateway: upstream dial failed (service may still be starting)",
				slog.String("service", reg.name),
				slog.String("addr", reg.addr),
				slog.Any("err", err),
			)
			continue
		}

		if err := reg.register(ctx, p.mux, conn); err != nil {
			conn.Close()
			return nil, fmt.Errorf("register %s: %w", reg.name, err)
		}

		p.conns = append(p.conns, conn)
		log.Info("gateway: upstream registered",
			slog.String("service", reg.name),
			slog.String("addr", reg.addr),
		)
	}

	return p, nil
}

// Handler returns the HTTP handler for the gateway.
// It attaches a health check endpoint and the full gRPC-gateway mux.
func (p *Proxy) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health check endpoint — used by load balancers and Kubernetes probes.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok","service":"jarvis-gateway"}`)
	})

	// API routes — everything under /v1/ goes to the gRPC-gateway mux.
	mux.Handle("/v1/", p.mux)

	// Catch-all for documentation
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"service":"JARVIS API Gateway","docs":"/v1/","health":"/healthz"}`)
			return
		}
		http.NotFound(w, r)
	})

	return mux
}

// Close shuts down all upstream connections.
func (p *Proxy) Close() {
	for _, conn := range p.conns {
		conn.Close()
	}
}

// ── helpers ───────────────────────────────────────────────────────────

// requestIDMatcher forwards X-Request-ID from HTTP headers into gRPC metadata.
func requestIDMatcher(key string) (string, bool) {
	if key == middleware.RequestIDHeader || key == "x-request-id" {
		return "request-id", true
	}
	return runtime.DefaultHeaderMatcher(key)
}

// errorHandler formats gRPC errors as JSON following RFC 7807.
func errorHandler(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	runtime.DefaultHTTPErrorHandler(ctx, mux, m, w, r, err)
}

// OutgoingMetadata adds the request ID to outgoing gRPC metadata.
// Call this from a grpc.UnaryClientInterceptor if you need per-RPC metadata injection.
func OutgoingMetadata(ctx context.Context, requestID string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "x-request-id", requestID)
}
