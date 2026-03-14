package server_test

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"math"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	nlpv1    "github.com/rkrimper1/jarvis/api/pb/nlp"
	voicev1  "github.com/rkrimper1/jarvis/api/pb/voice"
	"github.com/rkrimper1/jarvis/api/internal/voice/config"
	"github.com/rkrimper1/jarvis/api/internal/voice/server"
)

// ── fake NLP server ───────────────────────────────────────────────────────────

type fakeNLPServer struct {
	nlpv1.UnimplementedNLPServiceServer
}

func (f *fakeNLPServer) ProcessDialogueTurn(
	_ context.Context,
	req *nlpv1.ProcessDialogueTurnRequest,
) (*nlpv1.ProcessDialogueTurnResponse, error) {
	return &nlpv1.ProcessDialogueTurnResponse{
		Meta: &commonv1.ResponseMeta{
			RequestId: req.GetMeta().GetRequestId(),
			Success:   true,
			Timestamp: timestamppb.Now(),
		},
		ReplyText:            "Understood: " + req.Utterance,
		ResolvedIntent:       nlpv1.Intent_INTENT_QUERY,
		RequiresConfirmation: false,
		SessionId:            req.SessionId,
	}, nil
}

func (f *fakeNLPServer) StreamVoiceInput(stream nlpv1.NLPService_StreamVoiceInputServer) error {
	for {
		if _, err := stream.Recv(); err != nil {
			return nil
		}
	}
}

// ── test harness ──────────────────────────────────────────────────────────────

const bufSize = 1 << 20 // 1 MB

type harness struct {
	voiceClient voicev1.VoiceServiceClient
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	// ── NLP stub via bufconn ─────────────────────────────────────────
	nlpLis := bufconn.Listen(bufSize)
	nlpSrv := grpc.NewServer()
	nlpv1.RegisterNLPServiceServer(nlpSrv, &fakeNLPServer{})
	go nlpSrv.Serve(nlpLis) //nolint:errcheck

	nlpConn, err := grpc.NewClient(
		"passthrough://bufnet/nlp",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return nlpLis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial nlp bufconn: %v", err)
	}

	// ── VoiceServer via NewWithClient ────────────────────────────────
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort:        50059,
			ShutdownTimeout: 5 * time.Second,
			MaxRecvMsgSize:  8 * 1024 * 1024,
			MaxSendMsgSize:  8 * 1024 * 1024,
		},
		Audio: config.AudioConfig{
			SampleRateHz:    16000,
			ChunkDurationMs: 20,
			VADSilenceMs:    150, // short for fast test turnaround
			MaxUtteranceSec: 30,
		},
		NLP: config.NLPUpstreamConfig{
			Addr:        "unused-in-test",
			DialTimeout: 5 * time.Second,
		},
		Session: config.SessionConfig{
			TTL:         30 * time.Minute,
			MaxSessions: 100,
		},
	}

	vs := server.NewWithClient(cfg, nlpv1.NewNLPServiceClient(nlpConn), log)

	voiceLis := bufconn.Listen(bufSize)
	voiceSrv := grpc.NewServer()
	voicev1.RegisterVoiceServiceServer(voiceSrv, vs)
	go voiceSrv.Serve(voiceLis) //nolint:errcheck

	voiceConn, err := grpc.NewClient(
		"passthrough://bufnet/voice",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return voiceLis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial voice bufconn: %v", err)
	}

	t.Cleanup(func() {
		voiceConn.Close()
		nlpConn.Close()
		voiceSrv.Stop()
		nlpSrv.Stop()
	})

	return &harness{voiceClient: voicev1.NewVoiceServiceClient(voiceConn)}
}

// ── proto helpers ─────────────────────────────────────────────────────────────

func makeStreamConfig(sessionID, userID string) *voicev1.ConverseRequest {
	return &voicev1.ConverseRequest{
		Payload: &voicev1.ConverseRequest_Config{
			Config: &voicev1.StreamConfig{
				Meta: &commonv1.RequestMeta{
					RequestId: "req-" + sessionID,
					UserId:    userID,
					SessionId: sessionID,
					Source:    "test",
					Timestamp: timestamppb.Now(),
				},
				AudioConfig: &voicev1.AudioConfig{
					Encoding:        voicev1.AudioEncoding_AUDIO_ENCODING_PCM_16BIT,
					SampleRateHz:    16000,
					ChannelCount:    1,
					FrameDurationMs: 20,
				},
				LanguageCode: "en-US",
			},
		},
	}
}

func makeToneChunk(seq int64, amplitude float64, isWakeWord bool) *voicev1.ConverseRequest {
	samples := 320
	buf := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		v := int16(amplitude * float64(math.MaxInt16) * math.Sin(2*math.Pi*float64(i)/float64(samples)))
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}
	return &voicev1.ConverseRequest{
		Payload: &voicev1.ConverseRequest_Audio{
			Audio: &voicev1.AudioChunk{
				Data:            buf,
				SequenceNum:     seq,
				CapturedAtMs:    time.Now().UnixMilli(),
				IsWakeWordFrame: isWakeWord,
			},
		},
	}
}

func makeSilentChunk(seq int64) *voicev1.ConverseRequest {
	return &voicev1.ConverseRequest{
		Payload: &voicev1.ConverseRequest_Audio{
			Audio: &voicev1.AudioChunk{
				Data:         make([]byte, 640),
				SequenceNum:  seq,
				CapturedAtMs: time.Now().UnixMilli(),
			},
		},
	}
}

func makeControl(t voicev1.ControlEvent_Type) *voicev1.ConverseRequest {
	return &voicev1.ConverseRequest{
		Payload: &voicev1.ConverseRequest_Event{
			Event: &voicev1.ControlEvent{Type: t},
		},
	}
}

// collectResponses drains a Converse stream until EOF or timeout.
func collectResponses(t *testing.T, stream voicev1.VoiceService_ConverseClient, timeout time.Duration) []*voicev1.ConverseResponse {
	t.Helper()
	ch := make(chan *voicev1.ConverseResponse, 64)
	go func() {
		defer close(ch)
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			ch <- msg
		}
	}()
	var out []*voicev1.ConverseResponse
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, msg)
		case <-deadline.C:
			return out
		}
	}
}

// hasState returns true if any response in the slice carries the given state.
func hasState(responses []*voicev1.ConverseResponse, want voicev1.StatusEvent_State) bool {
	for _, r := range responses {
		if s, ok := r.Payload.(*voicev1.ConverseResponse_Status); ok {
			if s.Status.State == want {
				return true
			}
		}
	}
	return false
}

// lastState returns the state from the last StatusEvent in the slice.
func lastState(responses []*voicev1.ConverseResponse) (voicev1.StatusEvent_State, bool) {
	for i := len(responses) - 1; i >= 0; i-- {
		if s, ok := responses[i].Payload.(*voicev1.ConverseResponse_Status); ok {
			return s.Status.State, true
		}
	}
	return 0, false
}

// ── Tests: protocol correctness ───────────────────────────────────────────────

func TestConverse_FirstMessage_MustBeConfig(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	// Send audio before config — protocol violation.
	_ = stream.Send(makeToneChunk(1, 0.5, false))
	_ = stream.CloseSend()

	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected error when first message is not StreamConfig, got nil")
	}
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestConverse_Config_MissingMeta_Rejected(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(&voicev1.ConverseRequest{
		Payload: &voicev1.ConverseRequest_Config{
			Config: &voicev1.StreamConfig{}, // nil meta
		},
	})
	_ = stream.CloseSend()

	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected error for missing meta, got nil")
	}
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestConverse_OpenSession_ReceivesIdleStatus(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig("sess-open", "tony"))

	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	s, ok := msg.Payload.(*voicev1.ConverseResponse_Status)
	if !ok {
		t.Fatalf("first response is %T, want *VoiceResponse_Status", msg.Payload)
	}
	if s.Status.State != voicev1.StatusEvent_STATE_IDLE {
		t.Errorf("initial state = %v, want STATE_IDLE", s.Status.State)
	}
	_ = stream.CloseSend()
}

func TestConverse_SessionID_PropagatedInResponses(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const wantID = "sess-propagation"
	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig(wantID, "tony"))

	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if msg.SessionId != wantID {
		t.Errorf("session_id = %q, want %q", msg.SessionId, wantID)
	}
	_ = stream.CloseSend()
}

// ── Tests: wake word + listening state ───────────────────────────────────────

func TestConverse_WakeWordFrame_TransitionsToListening(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig("sess-wake", "tony"))
	_ = stream.Send(makeToneChunk(1, 0.5, true /*isWakeWord*/))
	// Give server time to process without waiting for VAD.
	time.Sleep(100 * time.Millisecond)
	_ = stream.CloseSend()

	responses := collectResponses(t, stream, 3*time.Second)
	if !hasState(responses, voicev1.StatusEvent_STATE_LISTENING) {
		t.Error("expected STATE_LISTENING after wake word frame")
	}
}

func TestConverse_NonWakeWordAudio_NoListeningTransition(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig("sess-nowake", "tony"))
	// Regular audio without wake word flag.
	_ = stream.Send(makeToneChunk(1, 0.5, false /*isWakeWord*/))
	time.Sleep(50 * time.Millisecond)
	_ = stream.CloseSend()

	responses := collectResponses(t, stream, 2*time.Second)
	if hasState(responses, voicev1.StatusEvent_STATE_LISTENING) {
		t.Error("STATE_LISTENING should not appear without IsWakeWordFrame=true")
	}
}

// ── Tests: full utterance pipeline (EOS path) ─────────────────────────────────

func TestConverse_EndOfSpeech_RunsFullPipeline(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig("sess-eos", "tony"))
	_ = stream.Send(makeToneChunk(1, 0.5, true)) // wake word
	for i := int64(2); i <= 6; i++ {
		_ = stream.Send(makeToneChunk(i, 0.3, false))
	}
	_ = stream.Send(makeControl(voicev1.ControlEvent_TYPE_END_OF_SPEECH))
	_ = stream.CloseSend()

	responses := collectResponses(t, stream, 8*time.Second)

	checks := map[string]bool{
		"STATE_PROCESSING": false,
		"final Transcript": false,
		"Reply":            false,
		"final AudioReply": false,
	}

	for _, r := range responses {
		switch p := r.Payload.(type) {
		case *voicev1.ConverseResponse_Status:
			if p.Status.State == voicev1.StatusEvent_STATE_PROCESSING {
				checks["STATE_PROCESSING"] = true
			}
		case *voicev1.ConverseResponse_Transcript:
			if p.Transcript.IsFinal && p.Transcript.Text != "" {
				checks["final Transcript"] = true
			}
		case *voicev1.ConverseResponse_Reply:
			if p.Reply.ReplyText != "" {
				checks["Reply"] = true
			}
		case *voicev1.ConverseResponse_AudioReply:
			if p.AudioReply.IsFinalChunk {
				checks["final AudioReply"] = true
			}
		}
	}

	for name, ok := range checks {
		if !ok {
			t.Errorf("missing expected response: %s", name)
		}
	}
}

func TestConverse_VAD_TriggersUtteranceWithoutExplicitEOS(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig("sess-vad", "tony"))
	_ = stream.Send(makeToneChunk(1, 0.5, true)) // wake word — loud
	for i := int64(2); i <= 5; i++ {
		_ = stream.Send(makeToneChunk(i, 0.4, false)) // more voice
	}
	// Send silence frames for longer than VADSilenceMs (150ms in test harness).
	// 10 frames × 20 ms = 200 ms — exceeds the 150 ms window.
	now := time.Now()
	for i := int64(6); i <= 15; i++ {
		chunk := &voicev1.AudioChunk{
			Data:         make([]byte, 640),
			SequenceNum:  i,
			CapturedAtMs: now.Add(time.Duration(i-5) * 20 * time.Millisecond).UnixMilli(),
		}
		_ = stream.Send(&voicev1.ConverseRequest{
			Payload: &voicev1.ConverseRequest_Audio{Audio: chunk},
		})
	}
	_ = stream.CloseSend()

	responses := collectResponses(t, stream, 8*time.Second)
	if !hasState(responses, voicev1.StatusEvent_STATE_PROCESSING) {
		t.Error("VAD should have triggered processing without explicit END_OF_SPEECH")
	}
}

func TestConverse_NLPReply_ContainsUtteranceEcho(t *testing.T) {
	// The fake NLP server returns "Understood: " + utterance.
	// STT returns the stub text so we verify the echo reaches the Reply message.
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig("sess-reply", "tony"))
	_ = stream.Send(makeToneChunk(1, 0.5, true))
	_ = stream.Send(makeControl(voicev1.ControlEvent_TYPE_END_OF_SPEECH))
	_ = stream.CloseSend()

	responses := collectResponses(t, stream, 8*time.Second)

	for _, r := range responses {
		if p, ok := r.Payload.(*voicev1.ConverseResponse_Reply); ok {
			if p.Reply.ReplyText == "" {
				t.Error("Reply.reply_text is empty")
			}
			if p.Reply.Intent == "" {
				t.Error("Reply.intent is empty")
			}
			return
		}
	}
	t.Error("no Reply message received in responses")
}

// ── Tests: control events ─────────────────────────────────────────────────────

func TestConverse_Cancel_ReturnsToIdle(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig("sess-cancel", "tony"))
	_ = stream.Send(makeToneChunk(1, 0.5, true))
	_ = stream.Send(makeControl(voicev1.ControlEvent_TYPE_CANCEL))
	time.Sleep(100 * time.Millisecond)
	_ = stream.CloseSend()

	responses := collectResponses(t, stream, 3*time.Second)
	state, ok := lastState(responses)
	// After cancel the last observed state before ENDED should be IDLE.
	// Filter out the final ENDED to find the cancel-induced IDLE.
	for i := len(responses) - 1; i >= 0; i-- {
		if s, ok2 := responses[i].Payload.(*voicev1.ConverseResponse_Status); ok2 {
			if s.Status.State != voicev1.StatusEvent_STATE_ENDED {
				state = s.Status.State
				ok = true
				break
			}
		}
	}
	if !ok {
		t.Fatal("no status messages received")
	}
	if state != voicev1.StatusEvent_STATE_IDLE {
		t.Errorf("state after cancel = %v, want STATE_IDLE", state)
	}
}

func TestConverse_KeepAlive_NoStateTransition(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig("sess-ka", "tony"))
	// Read the IDLE status first.
	_, _ = stream.Recv()

	for i := 0; i < 5; i++ {
		_ = stream.Send(makeControl(voicev1.ControlEvent_TYPE_KEEP_ALIVE))
	}
	time.Sleep(50 * time.Millisecond)
	_ = stream.CloseSend()

	responses := collectResponses(t, stream, 2*time.Second)
	for _, r := range responses {
		if s, ok := r.Payload.(*voicev1.ConverseResponse_Status); ok {
			if s.Status.State == voicev1.StatusEvent_STATE_ERROR {
				t.Errorf("unexpected ERROR state after KEEP_ALIVE: %s", s.Status.Message)
			}
		}
	}
}

func TestConverse_NewTurn_ClearsBuffer(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	stream, _ := h.voiceClient.Converse(ctx)
	_ = stream.Send(makeStreamConfig("sess-newturn", "tony"))
	_ = stream.Send(makeToneChunk(1, 0.5, true))
	// NEW_TURN should discard the buffered audio without processing.
	_ = stream.Send(makeControl(voicev1.ControlEvent_TYPE_NEW_TURN))
	// Send END_OF_SPEECH immediately — buffer is empty, no utterance to process.
	_ = stream.Send(makeControl(voicev1.ControlEvent_TYPE_END_OF_SPEECH))
	time.Sleep(100 * time.Millisecond)
	_ = stream.CloseSend()

	responses := collectResponses(t, stream, 3*time.Second)
	// No Transcript or Reply should appear because NEW_TURN cleared the buffer.
	for _, r := range responses {
		if _, ok := r.Payload.(*voicev1.ConverseResponse_Transcript); ok {
			t.Error("Transcript should not appear after NEW_TURN cleared the buffer")
		}
	}
}

// ── Tests: session capacity ───────────────────────────────────────────────────

func TestConverse_SessionCapacity_Enforced(t *testing.T) {
	// Build a harness with max 1 session.
	nlpLis := bufconn.Listen(bufSize)
	nlpSrv := grpc.NewServer()
	nlpv1.RegisterNLPServiceServer(nlpSrv, &fakeNLPServer{})
	go nlpSrv.Serve(nlpLis) //nolint:errcheck

	nlpConn, _ := grpc.NewClient(
		"passthrough://bufnet/nlp-cap",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return nlpLis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Server:  config.ServerConfig{GRPCPort: 50059, MaxRecvMsgSize: 8 << 20, MaxSendMsgSize: 8 << 20},
		Audio:   config.AudioConfig{SampleRateHz: 16000, ChunkDurationMs: 20, VADSilenceMs: 150, MaxUtteranceSec: 30},
		NLP:     config.NLPUpstreamConfig{Addr: "unused", DialTimeout: 5 * time.Second},
		Session: config.SessionConfig{TTL: 30 * time.Minute, MaxSessions: 1}, // cap at 1
	}
	vs := server.NewWithClient(cfg, nlpv1.NewNLPServiceClient(nlpConn), log)

	voiceLis := bufconn.Listen(bufSize)
	voiceSrv := grpc.NewServer()
	voicev1.RegisterVoiceServiceServer(voiceSrv, vs)
	go voiceSrv.Serve(voiceLis) //nolint:errcheck
	t.Cleanup(func() { voiceSrv.Stop(); nlpSrv.Stop() })

	dial := func() voicev1.VoiceServiceClient {
		conn, _ := grpc.NewClient(
			"passthrough://bufnet/voice-cap",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return voiceLis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		t.Cleanup(func() { conn.Close() })
		return voicev1.NewVoiceServiceClient(conn)
	}

	// First session — must succeed.
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	s1, _ := dial().Converse(ctx1)
	_ = s1.Send(makeStreamConfig("sess-cap-1", "tony"))
	msg, err := s1.Recv()
	if err != nil {
		t.Fatalf("first session Recv: %v", err)
	}
	st, _ := msg.Payload.(*voicev1.ConverseResponse_Status)
	if st.Status.State != voicev1.StatusEvent_STATE_IDLE {
		t.Fatalf("first session did not get IDLE, got %v", st.Status.State)
	}

	// Second session — must be rejected with ResourceExhausted.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	s2, _ := dial().Converse(ctx2)
	_ = s2.Send(makeStreamConfig("sess-cap-2", "tony"))
	_, err = s2.Recv()
	if err == nil {
		t.Fatal("expected ResourceExhausted for second session, got nil")
	}
	if code := status.Code(err); code != codes.ResourceExhausted {
		t.Errorf("error code = %v, want ResourceExhausted", code)
	}

	cancel1()
}

// ── Tests: GetSession ─────────────────────────────────────────────────────────

func TestGetSession_UnknownID_NotFound(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.voiceClient.GetSession(ctx, &voicev1.GetSessionRequest{
		Meta:      &commonv1.RequestMeta{RequestId: "r1", UserId: "tony", SessionId: "ghost"},
		SessionId: "ghost",
	})
	if err == nil {
		t.Fatal("expected NotFound for unknown session, got nil")
	}
	if code := status.Code(err); code != codes.NotFound {
		t.Errorf("error code = %v, want NotFound", code)
	}
}

func TestGetSession_MissingMeta_InvalidArgument(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.voiceClient.GetSession(ctx, &voicev1.GetSessionRequest{SessionId: "x"})
	if err == nil {
		t.Fatal("expected InvalidArgument for missing meta, got nil")
	}
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}
