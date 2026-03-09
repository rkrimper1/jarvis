package server

// tts_cloud.go — Cloud TTS provider selection.
//
// newCloudTTSSynthesizer is called by newSynthesizer() in server.go when
// TTS_PROVIDER=cloud_tts. The real implementation lives here once
// cloud.google.com/go/texttospeech is added to go.mod.
//
// Until then this file returns a clear startup error so the service fails
// fast rather than silently falling back to the stub in production.

import (
	"fmt"
	"log/slog"

	"github.com/rkrimper1/jarvis/services/voice-service/internal/audio"
	"github.com/rkrimper1/jarvis/services/voice-service/internal/config"
)

// newCloudTTSSynthesizer is a placeholder until the Cloud TTS dep is added.
// Replace this entire file with the real implementation after running:
//
//	go get cloud.google.com/go/texttospeech
func newCloudTTSSynthesizer(cfg *config.Config, _ *slog.Logger) (audio.Synthesizer, error) {
	return nil, fmt.Errorf(
		"TTS_PROVIDER=cloud_tts requested but Cloud TTS dep is not yet wired "+
			"(GCP_PROJECT=%q, TTS_VOICE=%q) — add cloud.google.com/go/texttospeech to go.mod first",
		cfg.TTS.GCPProject,
		cfg.TTS.VoiceID,
	)
}
