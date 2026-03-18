package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the NLP service.
// Values are loaded from environment variables with sensible defaults.
type Config struct {
	Server   ServerConfig
	Dialogue DialogueConfig
	Claude   ClaudeConfig
	Redis    RedisConfig
	Log      LogConfig
}

type ServerConfig struct {
	GRPCPort        int
	ShutdownTimeout time.Duration
	MaxRecvMsgSize  int // bytes
	MaxSendMsgSize  int // bytes
}

type DialogueConfig struct {
	MaxHistoryTurns  int
	SessionTTL       time.Duration
	ConfidenceThresh float32 // below this → requires_confirmation = true
}

type ClaudeConfig struct {
	APIKey    string
	Model     string
	MaxTokens int
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type LogConfig struct {
	Level  string // debug | info | warn | error
	Format string // json | text
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			GRPCPort:        envInt("GRPC_PORT", 50051),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
			MaxRecvMsgSize:  envInt("MAX_RECV_MSG_SIZE", 4*1024*1024),  // 4 MB
			MaxSendMsgSize:  envInt("MAX_SEND_MSG_SIZE", 4*1024*1024),  // 4 MB
		},
		Dialogue: DialogueConfig{
			MaxHistoryTurns:  envInt("DIALOGUE_MAX_HISTORY", 20),
			SessionTTL:       envDuration("DIALOGUE_SESSION_TTL", 30*time.Minute),
			ConfidenceThresh: float32(envFloat("DIALOGUE_CONFIDENCE_THRESH", 0.6)),
		},
		Claude: ClaudeConfig{
			APIKey:    envString("ANTHROPIC_API_KEY", ""),
			Model:     envString("CLAUDE_MODEL", "claude-sonnet-4-6"),
			MaxTokens: envInt("CLAUDE_MAX_TOKENS", 1024),
		},
		Redis: RedisConfig{
			Addr:     envString("REDIS_ADDR", "localhost:6379"),
			Password: envString("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
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
	if c.Dialogue.ConfidenceThresh < 0 || c.Dialogue.ConfidenceThresh > 1 {
		return fmt.Errorf("confidence_thresh must be 0.0–1.0")
	}
	if c.Claude.APIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is required")
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────

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
