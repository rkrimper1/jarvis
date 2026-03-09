// Package audio — tts.go
//
// Defines the Synthesizer interface that decouples the VoiceServer from any
// specific TTS backend.
//
// Two implementations ship in this repo:
//
//   StubSynthesizer          Zero-dep placeholder. Default for local dev/tests.
//                            Returns a single empty PCM chunk so the stream
//                            protocol is exercised end-to-end.
//
//   CloudTTSSynthesizer      Google Cloud Text-to-Speech v1. Lives in
//                            tts_cloud.go. Selected at runtime when
//                            TTS_PROVIDER=cloud_tts.
//
// The VoiceServer calls Synthesize() and streams each SynthesisChunk it
// receives back to the iOS client as an AudioReply message. This lets
// playback start on-device before the full utterance is synthesised.
package audio

import "context"

// ── Types ─────────────────────────────────────────────────────────────────────

// SynthesisChunk is one segment of a synthesised audio reply.
// The server streams these directly into ConverseResponse.AudioReply messages.
type SynthesisChunk struct {
	// PCM or encoded audio bytes in the format declared by Encoding.
	Data []byte
	// Encoding mirrors voicev1.AudioEncoding values; kept as int32 to avoid
	// importing the proto package from the audio layer.
	//   1 = PCM_16BIT  2 = OPUS  3 = AAC
	Encoding int32
	// IsFinal is true on the last chunk of a complete utterance.
	IsFinal bool
	// Index is the zero-based chunk sequence number within this utterance.
	Index int32
}

// SynthesisRequest carries everything the TTS backend needs per utterance.
type SynthesisRequest struct {
	// Text is the plaintext or SSML string to synthesise.
	Text string
	// LanguageCode is a BCP-47 tag, e.g. "en-US". Falls back to "en-US" if empty.
	LanguageCode string
	// VoiceID selects the voice. Interpretation is provider-specific:
	//   cloud_tts  → voice name, e.g. "en-US-Journey-D"
	//   stub       → ignored
	VoiceID string
	// SpeakingRate adjusts speed [0.25, 4.0]. 1.0 = normal. 0 means use default.
	SpeakingRate float64
	// Pitch adjusts pitch in semitones [-20, 20]. 0 means use default.
	Pitch float64
}

// ── Interface ─────────────────────────────────────────────────────────────────

// Synthesizer converts text into audio chunks streamed to the caller via a
// channel. The channel is closed by the implementation when synthesis is
// complete or when an error occurs.
//
// Callers must drain the returned channel fully; failing to do so leaks the
// goroutine started by the implementation.
//
// All implementations must be safe for concurrent use — one goroutine per
// active voice session will call Synthesize simultaneously.
type Synthesizer interface {
	// Synthesize starts synthesis and returns a read channel of audio chunks.
	// The first chunk is delivered as soon as the first audio segment is ready,
	// enabling streaming playback on the client before synthesis completes.
	// Any error is signalled by closing the channel early; callers should check
	// Err() after the channel is drained.
	//
	// ctx cancellation aborts synthesis in-flight. The channel is always closed,
	// even on cancellation or error.
	Synthesize(ctx context.Context, req SynthesisRequest) (<-chan SynthesisChunk, error)

	// Err returns the first error encountered during the most recent Synthesize
	// call. Safe to call only after the chunk channel has been fully drained.
	// Returns nil if synthesis completed successfully.
	Err() error

	// Close releases any underlying connections or resources.
	Close() error
}

// ── StubSynthesizer ───────────────────────────────────────────────────────────

// StubSynthesizer satisfies Synthesizer without any external calls.
// It returns a single empty PCM chunk so the full gRPC stream protocol
// (SPEAKING state → AudioReply → back to IDLE) is exercised in tests.
type StubSynthesizer struct {
	lastErr error
}

// NewStubSynthesizer returns a StubSynthesizer.
func NewStubSynthesizer() *StubSynthesizer { return &StubSynthesizer{} }

// Synthesize returns a channel that immediately emits one stub chunk and closes.
func (s *StubSynthesizer) Synthesize(_ context.Context, req SynthesisRequest) (<-chan SynthesisChunk, error) {
	ch := make(chan SynthesisChunk, 1)
	ch <- SynthesisChunk{
		// Empty PCM so tests don't play noise, but IsFinal=true closes the turn.
		Data:     []byte("[TTS stub — wire Cloud TTS here]"),
		Encoding: 1, // PCM_16BIT
		IsFinal:  true,
		Index:    0,
	}
	close(ch)
	s.lastErr = nil
	return ch, nil
}

// Err always returns nil for the stub.
func (s *StubSynthesizer) Err() error { return s.lastErr }

// Close is a no-op for the stub.
func (s *StubSynthesizer) Close() error { return nil }
