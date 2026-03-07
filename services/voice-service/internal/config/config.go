// Package config holds configuration for the voice service.
// Values are loaded from environment variables with sensible defaults
// so the service works out-of-the-box with docker compose up.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the root configuration for the voice service.
type Config struct {
	Server   ServerConfig
	Audio    AudioConfig
	NLP      NLPUpstreamConfig
	Session  SessionConfig
	Log      LogConfig
}

// ServerConfig controls the gRPC listener.
type ServerConfig struct {
	GRPCPort        int
	ShutdownTimeout time.Duration
	// 8 MB default — large enough for 500ms of PCM audio per message.
	MaxRecvMsgSize int
	MaxSendMsgSize int
}

// AudioConfig holds STT pipeline tuning parameters.
type AudioConfig struct {
	// SampleRateHz expected from iOS clients (16000 recommended).
	SampleRateHz int
	// ChunkDurationMs is how long each audio chunk should cover.
	ChunkDurationMs int
	// VADSilenceMs is silence duration (ms) before the server emits END_OF_SPEECH.
	VADSilenceMs int
	// MaxUtteranceSec caps how long a single utterance can be before force-commit.
	MaxUtteranceSec int
}

// NLPUpstreamConfig dials the nlp-service for intent + dialogue processing.
// Mirrors the upstream pattern used in gateway/internal/config/config.go.
type NLPUpstreamConfig struct {
	Addr        string
	DialTimeout time.Duration
}

// SessionConfig controls in-memory session store behaviour.
type SessionConfig struct {
	// TTL after which an idle session is evicted.
	TTL time.Duration
	// MaxSessions caps total concurrent sessions (memory guard).
	MaxSessions int
}

// LogConfig controls structured logging — identical to other services.
type LogConfig struct {
	Level  string // debug | info | warn | error
	Format string // json | text
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			GRPCPort:        envInt("GRPC_PORT", 50059),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
			MaxRecvMsgSize:  envInt("MAX_RECV_MSG_SIZE", 8*1024*1024), // 8 MB
			MaxSendMsgSize:  envInt("MAX_SEND_MSG_SIZE", 8*1024*1024), // 8 MB
		},
		Audio: AudioConfig{
			SampleRateHz:    envInt("AUDIO_SAMPLE_RATE_HZ", 16000),
			ChunkDurationMs: envInt("AUDIO_CHUNK_DURATION_MS", 20),
			VADSilenceMs:    envInt("AUDIO_VAD_SILENCE_MS", 800),
			MaxUtteranceSec: envInt("AUDIO_MAX_UTTERANCE_SEC", 30),
		},
		NLP: NLPUpstreamConfig{
			Addr:        envString("NLP_ADDR", "nlp-service:50051"),
			DialTimeout: envDuration("NLP_DIAL_TIMEOUT", 5*time.Second),
		},
		Session: SessionConfig{
			TTL:         envDuration("SESSION_TTL", 30*time.Minute),
			MaxSessions: envInt("SESSION_MAX", 1000),
		},
		Log: LogConfig{
			Level:  envString("LOG_LEVEL", "info"),
			Format: envString("LOG_FORMAT", "json"),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid voice config: %w", err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Server.GRPCPort < 1 || c.Server.GRPCPort > 65535 {
		return fmt.Errorf("grpc_port %d out of range", c.Server.GRPCPort)
	}
	if c.Audio.SampleRateHz <= 0 {
		return fmt.Errorf("audio_sample_rate_hz must be positive")
	}
	if c.NLP.Addr == "" {
		return fmt.Errorf("NLP_ADDR must not be empty")
	}
	return nil
}

// ── helpers — identical to other services ────────────────────────────

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
