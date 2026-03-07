package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the Security service.
type Config struct {
	Server   ServerConfig
	Token    TokenConfig
	Audit    AuditConfig
	Threat   ThreatConfig
	Log      LogConfig
}

type ServerConfig struct {
	GRPCPort        int
	ShutdownTimeout time.Duration
	MaxRecvMsgSize  int
	MaxSendMsgSize  int
}

type TokenConfig struct {
	Secret          string        // HMAC signing secret — must be set in prod
	AccessTokenTTL  time.Duration // default 1 hour
	Issuer          string
}

type AuditConfig struct {
	MaxPageSize    int
	RetentionDays  int
}

type ThreatConfig struct {
	AlertBroadcastInterval time.Duration // how often to push alerts to subscribers
	ConfidenceThresh       float32
}

type LogConfig struct {
	Level  string
	Format string
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			GRPCPort:        envInt("GRPC_PORT", 50052),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
			MaxRecvMsgSize:  envInt("MAX_RECV_MSG_SIZE", 4*1024*1024),
			MaxSendMsgSize:  envInt("MAX_SEND_MSG_SIZE", 4*1024*1024),
		},
		Token: TokenConfig{
			Secret:         envString("TOKEN_SECRET", "stark-industries-dev-secret-change-in-prod"),
			AccessTokenTTL: envDuration("TOKEN_TTL", time.Hour),
			Issuer:         envString("TOKEN_ISSUER", "jarvis.security"),
		},
		Audit: AuditConfig{
			MaxPageSize:   envInt("AUDIT_MAX_PAGE_SIZE", 100),
			RetentionDays: envInt("AUDIT_RETENTION_DAYS", 365),
		},
		Threat: ThreatConfig{
			AlertBroadcastInterval: envDuration("THREAT_BROADCAST_INTERVAL", 5*time.Second),
			ConfidenceThresh:       float32(envFloat("THREAT_CONFIDENCE_THRESH", 0.65)),
		},
		Log: LogConfig{
			Level:  envString("LOG_LEVEL", "info"),
			Format: envString("LOG_FORMAT", "json"),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Server.GRPCPort < 1 || c.Server.GRPCPort > 65535 {
		return fmt.Errorf("grpc_port %d out of range", c.Server.GRPCPort)
	}
	if c.Token.Secret == "" {
		return fmt.Errorf("TOKEN_SECRET must not be empty")
	}
	if c.Audit.MaxPageSize < 1 {
		return fmt.Errorf("audit max_page_size must be >= 1")
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

func envFloat(key string, def float64) float64 {
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
