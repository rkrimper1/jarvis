package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server    ServerConfig
	Telemetry TelemetryConfig
	Log       LogConfig
}

type ServerConfig struct {
	GRPCPort        int
	ShutdownTimeout time.Duration
	MaxRecvMsgSize  int
	MaxSendMsgSize  int
}

type TelemetryConfig struct {
	StreamInterval time.Duration // how often the telemetry simulator emits readings
	BufferSize     int           // per-subscriber channel buffer
}

type LogConfig struct {
	Level  string
	Format string
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			GRPCPort:        envInt("GRPC_PORT", 50054),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
			MaxRecvMsgSize:  envInt("MAX_RECV_MSG_SIZE", 4*1024*1024),
			MaxSendMsgSize:  envInt("MAX_SEND_MSG_SIZE", 4*1024*1024),
		},
		Telemetry: TelemetryConfig{
			StreamInterval: envDuration("TELEMETRY_INTERVAL", 2*time.Second),
			BufferSize:     envInt("TELEMETRY_BUFFER", 32),
		},
		Log: LogConfig{
			Level:  envString("LOG_LEVEL", "info"),
			Format: envString("LOG_FORMAT", "json"),
		},
	}
	if cfg.Server.GRPCPort < 1 || cfg.Server.GRPCPort > 65535 {
		return nil, fmt.Errorf("grpc_port out of range")
	}
	return cfg, nil
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
