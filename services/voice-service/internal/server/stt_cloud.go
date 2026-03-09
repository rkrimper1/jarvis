package server

// stt_cloud.go — Cloud Speech provider selection.
//
// newCloudSpeechTranscriber is called by newTranscriber() when
// STT_PROVIDER=cloud_speech.  The real implementation (stt_cloud_impl.go)
// is added once cloud.google.com/go/speech is in go.mod.
//
// Until then this file returns a clear error so the service fails fast
// at startup rather than silently falling back to the stub.

import (
	"fmt"
	"log/slog"

	"github.com/rkrimper1/jarvis/services/voice-service/internal/audio"
	"github.com/rkrimper1/jarvis/services/voice-service/internal/config"
)

// newCloudSpeechTranscriber is a placeholder until the Cloud Speech dep is
// added to go.mod. Replace this file entirely with the real implementation
// once `go get cloud.google.com/go/speech` has been run.
func newCloudSpeechTranscriber(cfg *config.Config, _ *slog.Logger) (audio.Transcriber, error) {
	return nil, fmt.Errorf(
		"STT_PROVIDER=cloud_speech requested but Cloud Speech dep is not yet wired "+
			"(GCP_PROJECT=%q) — add cloud.google.com/go/speech to go.mod first",
		cfg.STT.GCPProject,
	)
}
