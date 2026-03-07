package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the Agent Coordinator service.
type Config struct {
	Server     ServerConfig
	Registry   RegistryConfig
	Scheduler  SchedulerConfig
	EventBus   EventBusConfig
	Log        LogConfig
}

type ServerConfig struct {
	GRPCPort        int
	ShutdownTimeout time.Duration
	MaxRecvMsgSize  int
	MaxSendMsgSize  int
}

type RegistryConfig struct {
	// How long without a heartbeat before an agent is marked OFFLINE
	HeartbeatTimeout time.Duration
	// How often the registry runs its stale-agent GC sweep
	GCSweepInterval time.Duration
}

type SchedulerConfig struct {
	// How many tasks can be queued before back-pressure kicks in
	MaxQueueDepth int
	// How long a dispatched task can run before it is marked timed-out
	TaskTimeout time.Duration
}

type EventBusConfig struct {
	// Per-subscriber channel buffer depth
	SubscriberBuffer int
	// How often the bus emits synthetic patrol events (dev/demo mode)
	SimulationInterval time.Duration
}

type LogConfig struct {
	Level  string
	Format string
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			GRPCPort:        envInt("GRPC_PORT", 50053),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
			MaxRecvMsgSize:  envInt("MAX_RECV_MSG_SIZE", 4*1024*1024),
			MaxSendMsgSize:  envInt("MAX_SEND_MSG_SIZE", 4*1024*1024),
		},
		Registry: RegistryConfig{
			HeartbeatTimeout: envDuration("REGISTRY_HEARTBEAT_TIMEOUT", 30*time.Second),
			GCSweepInterval:  envDuration("REGISTRY_GC_INTERVAL", 15*time.Second),
		},
		Scheduler: SchedulerConfig{
			MaxQueueDepth: envInt("SCHEDULER_MAX_QUEUE", 256),
			TaskTimeout:   envDuration("SCHEDULER_TASK_TIMEOUT", 5*time.Minute),
		},
		EventBus: EventBusConfig{
			SubscriberBuffer:   envInt("EVENTBUS_BUFFER", 32),
			SimulationInterval: envDuration("EVENTBUS_SIM_INTERVAL", 8*time.Second),
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
	if c.Scheduler.MaxQueueDepth < 1 {
		return fmt.Errorf("scheduler max_queue_depth must be >= 1")
	}
	if c.EventBus.SubscriberBuffer < 1 {
		return fmt.Errorf("eventbus subscriber_buffer must be >= 1")
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
