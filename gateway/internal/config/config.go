// Package config holds gateway configuration loaded from environment variables.
// The gateway is the single HTTP/1.1 entry point into the JARVIS gRPC mesh.
// All upstream addresses default to Docker Compose service names so the
// gateway works out-of-the-box with `docker compose up`.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the root configuration object.
type Config struct {
	HTTP      HTTPConfig
	Upstreams UpstreamConfig
	Auth      AuthConfig
	Log       LogConfig
}

// HTTPConfig controls the REST listener.
type HTTPConfig struct {
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	CORSOrigins     []string // "*" for open-access in dev
}

// UpstreamConfig holds gRPC dial addresses for every backend service.
type UpstreamConfig struct {
	NLPService          string
	SecurityService     string
	AgentCoordinator    string
	HardwareService     string
	FacilityService     string
	IntelligenceService string
	BusinessOpsService  string
	LearningService     string
	DialTimeout         time.Duration
}

// AuthConfig configures the token validation middleware.
type AuthConfig struct {
	// Shared secret used to validate HMAC-signed tokens issued by SecurityService.
	// In production this would be fetched from Secret Manager.
	TokenSecret string
	// Routes exempt from authentication (e.g. health checks, /v1/security/authenticate)
	PublicPaths []string
}

// LogConfig controls structured logging.
type LogConfig struct {
	Level  string
	Format string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		HTTP: HTTPConfig{
			Port:            envInt("HTTP_PORT", 8080),
			ReadTimeout:     envDuration("HTTP_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    envDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
			CORSOrigins:     []string{envString("CORS_ORIGINS", "*")},
		},
		Upstreams: UpstreamConfig{
			NLPService:          envString("NLP_ADDR", "nlp-service:50051"),
			SecurityService:     envString("SECURITY_ADDR", "security-service:50052"),
			AgentCoordinator:    envString("AGENT_ADDR", "agent-coordinator:50053"),
			HardwareService:     envString("HARDWARE_ADDR", "hardware-service:50054"),
			FacilityService:     envString("FACILITY_ADDR", "facility-service:50055"),
			IntelligenceService: envString("INTELLIGENCE_ADDR", "intelligence-service:50056"),
			BusinessOpsService:  envString("BUSINESS_ADDR", "business-ops-service:50057"),
			LearningService:     envString("LEARNING_ADDR", "learning-service:50058"),
			DialTimeout:         envDuration("DIAL_TIMEOUT", 5*time.Second),
		},
		Auth: AuthConfig{
			TokenSecret: envString("TOKEN_SECRET", "jarvis-dev-secret"),
			PublicPaths: []string{
				"/healthz",
				"/v1/security/authenticate",
				"/v1/nlp/dialogue", // allow unauthenticated demo calls
			},
		},
		Log: LogConfig{
			Level:  envString("LOG_LEVEL", "info"),
			Format: envString("LOG_FORMAT", "json"),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid gateway config: %w", err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
		return fmt.Errorf("http_port %d out of range", c.HTTP.Port)
	}
	if c.Auth.TokenSecret == "" {
		return fmt.Errorf("TOKEN_SECRET must not be empty")
	}
	return nil
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
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
