package audio_test

import (
	"context"
	"testing"

	"github.com/rkrimper1/jarvis/api/internal/voice/audio"
)

// ── StubSynthesizer ───────────────────────────────────────────────────────────

func TestStubSynthesizer_ReturnsChunk(t *testing.T) {
	s := audio.NewStubSynthesizer()
	ch, err := s.Synthesize(context.Background(), audio.SynthesisRequest{
		Text:         "Good evening, sir.",
		LanguageCode: "en-US",
	})
	if err != nil {
		t.Fatalf("Synthesize returned unexpected error: %v", err)
	}

	var chunks []audio.SynthesisChunk
	for c := range ch {
		chunks = append(chunks, c)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one SynthesisChunk, got none")
	}
}

func TestStubSynthesizer_LastChunkIsFinal(t *testing.T) {
	s := audio.NewStubSynthesizer()
	ch, _ := s.Synthesize(context.Background(), audio.SynthesisRequest{Text: "hello"})

	var last audio.SynthesisChunk
	for c := range ch {
		last = c
	}
	if !last.IsFinal {
		t.Error("last chunk must have IsFinal=true")
	}
}

func TestStubSynthesizer_NoErrorAfterDrain(t *testing.T) {
	s := audio.NewStubSynthesizer()
	ch, _ := s.Synthesize(context.Background(), audio.SynthesisRequest{Text: "hello"})
	for range ch {
	}
	if err := s.Err(); err != nil {
		t.Errorf("Err() after successful synthesis = %v, want nil", err)
	}
}

func TestStubSynthesizer_EmptyText_ReturnsChunk(t *testing.T) {
	// Empty text is a valid (if unusual) request — stub must not panic or error.
	s := audio.NewStubSynthesizer()
	ch, err := s.Synthesize(context.Background(), audio.SynthesisRequest{Text: ""})
	if err != nil {
		t.Fatalf("empty text: unexpected error: %v", err)
	}
	var count int
	for range ch {
		count++
	}
	if count == 0 {
		t.Error("empty text should still produce at least one chunk")
	}
}

func TestStubSynthesizer_ChunkEncoding_IsPCM(t *testing.T) {
	s := audio.NewStubSynthesizer()
	ch, _ := s.Synthesize(context.Background(), audio.SynthesisRequest{Text: "test"})
	for c := range ch {
		if c.Encoding != 1 {
			t.Errorf("stub encoding = %d, want 1 (PCM_16BIT)", c.Encoding)
		}
	}
}

func TestStubSynthesizer_ChunkIndex_StartsAtZero(t *testing.T) {
	s := audio.NewStubSynthesizer()
	ch, _ := s.Synthesize(context.Background(), audio.SynthesisRequest{Text: "test"})
	first := true
	for c := range ch {
		if first {
			if c.Index != 0 {
				t.Errorf("first chunk index = %d, want 0", c.Index)
			}
			first = false
		}
	}
}

func TestStubSynthesizer_Close_NoError(t *testing.T) {
	s := audio.NewStubSynthesizer()
	if err := s.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

func TestStubSynthesizer_ContextCancel_ChannelDrained(t *testing.T) {
	// Even with a cancelled context the stub should complete normally —
	// it is synchronous and never blocks on I/O.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Synthesize

	s := audio.NewStubSynthesizer()
	ch, err := s.Synthesize(ctx, audio.SynthesisRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("unexpected error with cancelled ctx: %v", err)
	}
	// Drain fully — must not block.
	for range ch {
	}
}

func TestStubSynthesizer_ConcurrentSafe(t *testing.T) {
	// 20 goroutines calling Synthesize concurrently — race detector will catch issues.
	s := audio.NewStubSynthesizer()
	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func() {
			ch, _ := s.Synthesize(context.Background(), audio.SynthesisRequest{Text: "concurrent"})
			for range ch {
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

// ── SynthesisRequest field coverage ──────────────────────────────────────────

func TestSynthesisRequest_FieldsPreserved(t *testing.T) {
	req := audio.SynthesisRequest{
		Text:         "All systems operational.",
		LanguageCode: "en-GB",
		VoiceID:      "en-GB-Neural2-B",
		SpeakingRate: 1.2,
		Pitch:        -2.0,
	}
	if req.Text == "" {
		t.Error("Text should be set")
	}
	if req.LanguageCode != "en-GB" {
		t.Errorf("LanguageCode = %q, want en-GB", req.LanguageCode)
	}
	if req.SpeakingRate != 1.2 {
		t.Errorf("SpeakingRate = %f, want 1.2", req.SpeakingRate)
	}
}

// ── Interface compliance ──────────────────────────────────────────────────────

func TestStubSynthesizer_ImplementsInterface(t *testing.T) {
	// Compile-time assertion via interface assignment.
	var _ audio.Synthesizer = audio.NewStubSynthesizer()
}
