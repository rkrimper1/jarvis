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
	Server  ServerConfig
	Audio   AudioConfig
	STT     STTConfig
	TTS     TTSConfig
	NLP     NLPUpstreamConfig
	Session SessionConfig
	Log     LogConfig
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


// STTConfig selects and configures the speech-to-text backend.
type STTConfig struct {
	// Provider selects the backend: "stub" (default) or "cloud_speech".
	Provider string
	// GCPProject is required when Provider == "cloud_speech".
	GCPProject string
	// CredentialsFile is the path to a GCP service-account JSON key.
	// Leave empty to use Application Default Credentials.
	CredentialsFile string
	// Model is the Cloud Speech model name. Default: "latest_long".
	Model string
	// MaxSyncDurationSec is the threshold for switching to LongRunningRecognize.
	MaxSyncDurationSec int
	// EnableWordTimeOffsets requests per-word timing in the STT response.
	EnableWordTimeOffsets bool
	// EnableAutomaticPunctuation adds punctuation to transcripts.
	EnableAutomaticPunctuation bool
	// SpeechContextPhrases biases recognition toward Jarvis vocabulary.
	SpeechContextPhrases []string
}

// TTSConfig selects and configures the text-to-speech backend.
type TTSConfig struct {
	// Provider selects the backend: "stub" (default) or "cloud_tts".
	Provider string
	// GCPProject is required when Provider == "cloud_tts".
	GCPProject string
	// CredentialsFile is the path to a GCP service-account JSON key.
	// Leave empty to use Application Default Credentials.
	CredentialsFile string
	// VoiceID selects the Cloud TTS voice, e.g. "en-US-Journey-D".
	// See: https://cloud.google.com/text-to-speech/docs/voices
	VoiceID string
	// LanguageCode is the BCP-47 fallback when the stream config omits it.
	LanguageCode string
	// SpeakingRate adjusts speed [0.25, 4.0]. 0 uses the API default (1.0).
	SpeakingRate float64
	// Pitch adjusts pitch in semitones [-20, 20]. 0 uses the API default.
	Pitch float64
	// AudioEncoding controls the wire format sent to the iOS client.
	// Supported values: "pcm" (LINEAR16), "opus", "aac". Default: "pcm".
	AudioEncoding string
	// ChunkSizeBytes is the target PCM chunk size per AudioReply message.
	// Smaller = lower first-byte latency; larger = fewer messages.
	// Default: 8192 (≈256ms at 16kHz mono PCM-16).
	ChunkSizeBytes int
}

// NLPUpstreamConfig dials the nlp-service for intent + dialogue processing.
// Mirrors the upstream pattern used in gateway/internal/config/config.go.
type NLPUpstreamConfig struct {
	Addr        string
	DialTimeout time.Duration
}

// SessionConfig controls session store behaviour.
// When Provider == "memory" (default) an in-process map is used.
// When Provider == "redis" sessions are stored in Redis so they survive
// restarts and are shared across horizontal replicas.
type SessionConfig struct {
	// Provider selects the backend: "memory" (default) or "redis".
	Provider string
	// TTL after which an idle session is evicted.
	TTL time.Duration
	// MaxSessions caps total concurrent sessions in the MemoryStore.
	MaxSessions int
	// RedisAddr is the "host:port" of the Redis instance.
	// Required when Provider == "redis".
	RedisAddr string
	// RedisPassword is the Redis AUTH password. Leave empty for no auth.
	RedisPassword string
	// RedisDB selects the Redis logical database (0–15). Default 0.
	RedisDB int
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
		STT: STTConfig{
			Provider:                   envString("STT_PROVIDER", "stub"),
			GCPProject:                 envString("GCP_PROJECT", ""),
			CredentialsFile:            envString("GOOGLE_APPLICATION_CREDENTIALS", ""),
			Model:                      envString("STT_MODEL", "latest_long"),
			MaxSyncDurationSec:         envInt("STT_MAX_SYNC_DURATION_SEC", 55),
			EnableWordTimeOffsets:      envBool("STT_WORD_TIME_OFFSETS", false),
			EnableAutomaticPunctuation: envBool("STT_AUTO_PUNCTUATION", true),
		},
		TTS: TTSConfig{
			Provider:        envString("TTS_PROVIDER", "stub"),
			GCPProject:      envString("GCP_PROJECT", ""),
			CredentialsFile: envString("GOOGLE_APPLICATION_CREDENTIALS", ""),
			VoiceID:         envString("TTS_VOICE_ID", "en-US-Journey-D"),
			LanguageCode:    envString("TTS_LANGUAGE_CODE", "en-US"),
			SpeakingRate:    envFloat("TTS_SPEAKING_RATE", 1.0),
			Pitch:           envFloat("TTS_PITCH", 0.0),
			AudioEncoding:   envString("TTS_AUDIO_ENCODING", "pcm"),
			ChunkSizeBytes:  envInt("TTS_CHUNK_SIZE_BYTES", 8192),
		},
		NLP: NLPUpstreamConfig{
			Addr:        envString("NLP_ADDR", "nlp-service:50051"),
			DialTimeout: envDuration("NLP_DIAL_TIMEOUT", 5*time.Second),
		},
		Session: SessionConfig{
			Provider:      envString("SESSION_PROVIDER", "memory"),
			TTL:           envDuration("SESSION_TTL", 30*time.Minute),
			MaxSessions:   envInt("SESSION_MAX", 1000),
			RedisAddr:     envString("REDIS_ADDR", "redis:6379"),
			RedisPassword: envString("REDIS_PASSWORD", ""),
			RedisDB:       envInt("REDIS_DB", 0),
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

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
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
