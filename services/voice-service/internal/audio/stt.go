// Package audio — stt.go
//
// Defines the Transcriber interface that decouples the VoiceServer from any
// specific STT backend.
//
// Two implementations are provided in this repo:
//
//   StubTranscriber          Zero-dep placeholder. Default for local dev/tests.
//   CloudSpeechTranscriber   Google Cloud Speech-to-Text v1. Lives in stt_cloud.go.
//                            Selected at runtime when STT_PROVIDER=cloud_speech.
package audio

import "context"

// ── Interface ─────────────────────────────────────────────────────────────────

// Transcriber converts a raw PCM-16LE utterance buffer into text.
// Implementations must be safe for concurrent use.
type Transcriber interface {
	// Transcribe processes one complete utterance and returns the STT result.
	//   utterancePCM — concatenated PCM-16LE frames for a single turn.
	//   sampleRateHz — Hz from stream config (typically 16000).
	//   languageCode — BCP-47 tag, e.g. "en-US".
	// ctx carries the parent request deadline; implementations must honour it.
	Transcribe(ctx context.Context, utterancePCM []byte, sampleRateHz int, languageCode string) (*STTResult, error)

	// Close releases any underlying connections or resources.
	Close() error
}

// ── StubTranscriber ───────────────────────────────────────────────────────────

// StubTranscriber satisfies Transcriber without any external calls.
// It is the default backend (STT_PROVIDER=stub) and is always used in tests.
type StubTranscriber struct{}

// NewStubTranscriber returns a StubTranscriber.
func NewStubTranscriber() *StubTranscriber { return &StubTranscriber{} }

// Transcribe returns a deterministic placeholder so the full pipeline
// runs end-to-end without a GCP project or network access.
func (s *StubTranscriber) Transcribe(_ context.Context, pcm []byte, _ int, _ string) (*STTResult, error) {
	if len(pcm) == 0 {
		return &STTResult{Text: "", Confidence: 0, IsFinal: true}, nil
	}
	return &STTResult{
		Text:       "[STT stub — wire Cloud Speech or Whisper here]",
		Confidence: 0.99,
		IsFinal:    true,
	}, nil
}

// Close is a no-op for the stub.
func (s *StubTranscriber) Close() error { return nil }
