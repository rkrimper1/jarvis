package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	GRPCPort        int
	ShutdownTimeout time.Duration
	MaxRecvMsgSize  int
	MaxSendMsgSize  int
	LogLevel        string
}

func Load() (*Config, error) {
	cfg := &Config{
		GRPCPort:        envInt("GRPC_PORT", 50058),
		ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxRecvMsgSize:  envInt("MAX_RECV_MSG_SIZE", 4*1024*1024),
		MaxSendMsgSize:  envInt("MAX_SEND_MSG_SIZE", 4*1024*1024),
		LogLevel:        envString("LOG_LEVEL", "info"),
	}
	if cfg.GRPCPort < 1 || cfg.GRPCPort > 65535 {
		return nil, fmt.Errorf("grpc_port out of range")
	}
	return cfg, nil
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" { return v }
	return def
}
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil { return i }
	}
	return def
}
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil { return d }
	}
	return def
}
