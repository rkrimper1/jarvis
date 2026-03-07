package audio_test

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/services/voice-service/internal/audio"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// silentFrame returns n PCM-16LE samples all zero.
func silentFrame(samples int) []byte {
	return make([]byte, samples*2)
}

// toneFrame returns n PCM-16LE samples of a sine wave at the given amplitude [0,1].
func toneFrame(samples int, amplitude float64) []byte {
	buf := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		v := int16(amplitude * float64(math.MaxInt16) * math.Sin(2*math.Pi*float64(i)/float64(samples)))
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}
	return buf
}

// ── VAD ───────────────────────────────────────────────────────────────────────

func TestVAD_SilenceBeforeSpeech_NoEOS(t *testing.T) {
	vad := audio.NewVAD(0.01, 500*time.Millisecond)
	now := time.Now()

	for i := 0; i < 10; i++ {
		voiced, eos := vad.Feed(silentFrame(320), now.Add(time.Duration(i)*20*time.Millisecond))
		if voiced {
			t.Errorf("frame %d: silent frame reported voiced", i)
		}
		if eos {
			t.Errorf("frame %d: EOS before speech ever started", i)
		}
	}
}

func TestVAD_VoiceDetected(t *testing.T) {
	vad := audio.NewVAD(0.01, 500*time.Millisecond)
	now := time.Now()

	voiced, eos := vad.Feed(toneFrame(320, 0.5), now)
	if !voiced {
		t.Error("expected voiced=true for loud tone frame")
	}
	if eos {
		t.Error("EOS should not fire immediately after voice")
	}
}

func TestVAD_EOSAfterSilenceWindow(t *testing.T) {
	silenceWindow := 200 * time.Millisecond
	vad := audio.NewVAD(0.01, silenceWindow)
	now := time.Now()

	// Feed a voiced frame to start the utterance.
	vad.Feed(toneFrame(320, 0.5), now)

	// Feed silence frames spaced 20 ms apart — total 220 ms > silence window.
	var gotEOS bool
	for i := 1; i <= 12; i++ {
		ts := now.Add(time.Duration(i) * 20 * time.Millisecond)
		_, eos := vad.Feed(silentFrame(320), ts)
		if eos {
			gotEOS = true
			break
		}
	}
	if !gotEOS {
		t.Error("expected EOS to fire after silence window elapsed")
	}
}

func TestVAD_EOSDoesNotFireBeforeSilenceWindow(t *testing.T) {
	silenceWindow := 500 * time.Millisecond
	vad := audio.NewVAD(0.01, silenceWindow)
	now := time.Now()

	// Voiced frame.
	vad.Feed(toneFrame(320, 0.5), now)

	// 3 silent frames = 60 ms — well under 500 ms window.
	for i := 1; i <= 3; i++ {
		_, eos := vad.Feed(silentFrame(320), now.Add(time.Duration(i)*20*time.Millisecond))
		if eos {
			t.Errorf("EOS fired too early at frame %d (60ms < 500ms window)", i)
		}
	}
}

func TestVAD_VoiceResetsEOSTimer(t *testing.T) {
	silenceWindow := 100 * time.Millisecond
	vad := audio.NewVAD(0.01, silenceWindow)
	base := time.Now()

	// Voiced → 80 ms silence → voiced again → 80 ms silence → expect no EOS yet.
	vad.Feed(toneFrame(320, 0.5), base)

	// 4 silent frames = 80 ms (under window).
	for i := 1; i <= 4; i++ {
		vad.Feed(silentFrame(320), base.Add(time.Duration(i)*20*time.Millisecond))
	}

	// Another voiced frame resets the clock.
	vad.Feed(toneFrame(320, 0.5), base.Add(80*time.Millisecond))

	// 4 more silent frames from new voice timestamp — still under window.
	for i := 1; i <= 4; i++ {
		_, eos := vad.Feed(silentFrame(320), base.Add(80*time.Millisecond+time.Duration(i)*20*time.Millisecond))
		if eos {
			t.Errorf("EOS fired before silence window reset expired at frame %d", i)
		}
	}
}

func TestVAD_Reset_ClearsState(t *testing.T) {
	silenceWindow := 50 * time.Millisecond
	vad := audio.NewVAD(0.01, silenceWindow)
	now := time.Now()

	// Start speech.
	vad.Feed(toneFrame(320, 0.5), now)
	// Reset.
	vad.Reset()

	// Silent frames after reset should not trigger EOS (never started).
	for i := 1; i <= 5; i++ {
		_, eos := vad.Feed(silentFrame(320), now.Add(time.Duration(i)*20*time.Millisecond+silenceWindow))
		if eos {
			t.Errorf("EOS fired after Reset at frame %d", i)
		}
	}
}

func TestVAD_BelowThreshold_TreatedAsSilence(t *testing.T) {
	vad := audio.NewVAD(0.5, 100*time.Millisecond) // high threshold
	now := time.Now()

	// Low amplitude — should not register as voiced.
	voiced, _ := vad.Feed(toneFrame(320, 0.1), now)
	if voiced {
		t.Error("low-amplitude frame should not be voiced with threshold=0.5")
	}
}

// ── Transcribe stub ───────────────────────────────────────────────────────────

func TestTranscribe_ReturnsStubResult(t *testing.T) {
	result, err := audio.Transcribe(toneFrame(16000, 0.3), 16000, "en-US")
	if err != nil {
		t.Fatalf("Transcribe returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Transcribe returned nil result")
	}
	if !result.IsFinal {
		t.Error("stub result should have IsFinal=true")
	}
	if result.Text == "" {
		t.Error("stub result should have non-empty Text")
	}
	if result.Confidence <= 0 || result.Confidence > 1 {
		t.Errorf("confidence out of range [0,1]: %f", result.Confidence)
	}
}

func TestTranscribe_NilInput_NoError(t *testing.T) {
	// Stub must tolerate empty input gracefully.
	result, err := audio.Transcribe(nil, 16000, "en-US")
	if err != nil {
		t.Fatalf("Transcribe with nil PCM returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result even for nil PCM")
	}
}
