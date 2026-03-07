// Package audio handles server-side audio frame processing.
// It provides a simple energy-based Voice Activity Detector (VAD) that
// signals end-of-speech after a configurable silence window, and a
// Chunker that accumulates incoming AudioChunk bytes into utterance-sized
// buffers ready for an STT backend.
//
// In production the STT step would call a Cloud Speech-to-Text API or an
// on-prem Whisper instance. This package stubs that call so the service
// compiles and runs end-to-end while the integration is wired up.
package audio

import (
	"encoding/binary"
	"math"
	"time"
)

// VAD is a simple RMS energy-based voice activity detector.
// It is intentionally stateless between calls so the VoiceServer can
// create one per stream without any shared state.
type VAD struct {
	silenceThreshold float64
	silenceDuration  time.Duration

	lastVoiceAt time.Time
	started     bool
}

// NewVAD creates a VAD with the given energy threshold (0–1 normalised RMS)
// and the silence window after which EndOfSpeech returns true.
func NewVAD(silenceThreshold float64, silenceDuration time.Duration) *VAD {
	return &VAD{
		silenceThreshold: silenceThreshold,
		silenceDuration:  silenceDuration,
	}
}

// Feed processes a single PCM-16 audio frame and returns whether the
// server should consider speech as having ended.
// Returns (voiceDetected, endOfSpeech).
func (v *VAD) Feed(pcmData []byte, capturedAt time.Time) (voiced bool, eos bool) {
	rms := rmsEnergy(pcmData)
	voiced = rms > v.silenceThreshold

	if voiced {
		v.lastVoiceAt = capturedAt
		v.started = true
		return voiced, false
	}

	if !v.started {
		return false, false
	}

	if time.Since(v.lastVoiceAt) >= v.silenceDuration {
		return false, true
	}

	return false, false
}

// Reset clears VAD state between utterances.
func (v *VAD) Reset() {
	v.lastVoiceAt = time.Time{}
	v.started = false
}

// rmsEnergy computes normalised RMS for a PCM-16LE byte slice.
func rmsEnergy(data []byte) float64 {
	if len(data) < 2 {
		return 0
	}
	samples := len(data) / 2
	var sumSq float64
	for i := 0; i < len(data)-1; i += 2 {
		s := int16(binary.LittleEndian.Uint16(data[i:]))
		f := float64(s) / math.MaxInt16
		sumSq += f * f
	}
	return math.Sqrt(sumSq / float64(samples))
}

// STTResult is the output of a speech-to-text pass over an utterance buffer.
type STTResult struct {
	Text       string
	Confidence float32
	IsFinal    bool
	Words      []WordTiming
}

// WordTiming holds per-word timing from the STT engine.
type WordTiming struct {
	Word       string
	StartMs    int64
	EndMs      int64
	Confidence float32
}

// Transcribe stubs the STT call. Replace with Cloud Speech / Whisper in prod.
// The utterancePCM slice contains concatenated PCM-16LE frames for one turn.
func Transcribe(utterancePCM []byte, sampleRateHz int, languageCode string) (*STTResult, error) {
	// Stub: return a placeholder so the pipeline runs end-to-end.
	// Production implementation:
	//   client, _ := speech.NewClient(ctx)
	//   resp, _ := client.Recognize(ctx, &speechpb.RecognizeRequest{...})
	_ = utterancePCM
	_ = sampleRateHz
	_ = languageCode

	return &STTResult{
		Text:       "[STT stub — wire Cloud Speech or Whisper here]",
		Confidence: 0.99,
		IsFinal:    true,
	}, nil
}
