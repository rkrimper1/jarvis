// Package server implements the gRPC VoiceService.
// It follows the same structural conventions as the nlp-service server:
//   - UnimplementedVoiceServiceServer embedded for forward compatibility
//   - All dependencies injected via New()
//   - slog.Logger used throughout
//   - NLP upstream called via gRPC to reuse existing intent + dialogue logic
package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/gen/common"
	nlpv1   "github.com/rkrimper1/jarvis/gen/nlp"
	voicev1 "github.com/rkrimper1/jarvis/gen/voice"

	"github.com/rkrimper1/jarvis/services/voice-service/internal/audio"
	"github.com/rkrimper1/jarvis/services/voice-service/internal/config"
	"github.com/rkrimper1/jarvis/services/voice-service/internal/session"
)

// VoiceServer implements voicev1.VoiceServiceServer.
type VoiceServer struct {
	voicev1.UnimplementedVoiceServiceServer

	cfg      *config.Config
	sessions *session.Store
	nlp      nlpv1.NLPServiceClient
	log      *slog.Logger
}

// New creates a VoiceServer, dials the NLP upstream, and initialises
// the session store. Mirrors the New() pattern across all Jarvis services.
func New(cfg *config.Config, log *slog.Logger) (*VoiceServer, error) {
	dialCtx, cancel := context.WithTimeout(context.Background(), cfg.NLP.DialTimeout)
	defer cancel()

	nlpConn, err := grpc.DialContext( //nolint:staticcheck
		dialCtx,
		cfg.NLP.Addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("dial nlp-service at %s: %w", cfg.NLP.Addr, err)
	}

	return &VoiceServer{
		cfg:      cfg,
		sessions: session.NewStore(cfg.Session.TTL, cfg.Session.MaxSessions),
		nlp:      nlpv1.NewNLPServiceClient(nlpConn),
		log:      log,
	}, nil
}

// NewWithClient creates a VoiceServer with a pre-wired NLP client.
// Intended for use in tests and integration harnesses where the caller
// manages the NLP connection (e.g. via bufconn) rather than dialling by address.
func NewWithClient(cfg *config.Config, nlpClient nlpv1.NLPServiceClient, log *slog.Logger) *VoiceServer {
	return &VoiceServer{
		cfg:      cfg,
		sessions: session.NewStore(cfg.Session.TTL, cfg.Session.MaxSessions),
		nlp:      nlpClient,
		log:      log,
	}
}


// ── Converse ───────────────────────────────────────────────────────────────

// Converse is the primary bidirectional stream.
// Protocol:
//  1. Client sends VoiceRequest{config} — exactly once — to open the session.
//  2. Client sends VoiceRequest{audio} continuously.
//  3. Client sends VoiceRequest{event=END_OF_SPEECH} or server VAD fires.
//  4. Server runs STT → NLPService.ProcessDialogueTurn → TTS stub → HUDAction.
//  5. Server streams Transcript, Reply, AudioReply, HUDAction messages back.
func (s *VoiceServer) Converse(stream voicev1.VoiceService_ConverseServer) error {
	ctx := stream.Context()

	// ── Step 1: receive StreamConfig ──────────────────────────────────
	firstMsg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Internal, "recv config: %v", err)
	}

	cfg, ok := firstMsg.Payload.(*voicev1.VoiceRequest_Config)
	if !ok {
		return status.Error(codes.InvalidArgument, "first message must be StreamConfig")
	}

	streamCfg := cfg.Config
	if streamCfg.GetMeta() == nil {
		return status.Error(codes.InvalidArgument, "StreamConfig.meta is required")
	}

	sess := s.sessions.Create(streamCfg)
	if sess == nil {
		return status.Error(codes.ResourceExhausted, "session capacity exceeded")
	}
	defer s.sessions.Delete(sess.ID)

	s.log.InfoContext(ctx, "Converse started",
		slog.String("session_id", sess.ID),
		slog.String("user_id", sess.UserID),
		slog.String("platform", streamCfg.GetClientInfo().GetPlatform()),
	)

	// Signal IDLE → client knows the stream is open and ready.
	if err := s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_IDLE, "", voicev1.VoiceErrorCode_VOICE_ERROR_UNSPECIFIED); err != nil {
		return err
	}

	// ── VAD + audio buffer ────────────────────────────────────────────
	vad := audio.NewVAD(
		0.02, // RMS threshold — tune per environment
		time.Duration(s.cfg.Audio.VADSilenceMs)*time.Millisecond,
	)
	var utteranceBuf []byte
	seqNum := int64(0)

	// ── Step 2-3: receive audio loop ──────────────────────────────────
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			s.log.InfoContext(ctx, "Converse: client closed stream", slog.String("session_id", sess.ID))
			break
		}
		if err != nil {
			s.log.ErrorContext(ctx, "Converse: recv error",
				slog.String("session_id", sess.ID),
				slog.Any("err", err),
			)
			return status.Errorf(codes.Internal, "recv: %v", err)
		}

		s.sessions.Touch(sess.ID)

		switch payload := msg.Payload.(type) {

		case *voicev1.VoiceRequest_Audio:
			chunk := payload.Audio

			// Wake-word frames skip VAD gating — begin listening immediately.
			if chunk.IsWakeWordFrame {
				s.sessions.SetState(sess.ID, session.StateListening)
				vad.Reset()
				if err := s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_LISTENING, "", voicev1.VoiceErrorCode_VOICE_ERROR_UNSPECIFIED); err != nil {
					return err
				}
			}

			utteranceBuf = append(utteranceBuf, chunk.Data...)

			capturedAt := time.UnixMilli(chunk.CapturedAtMs)
			_, eos := vad.Feed(chunk.Data, capturedAt)

			if eos {
				seqNum++
				if err := s.processUtterance(ctx, stream, sess, streamCfg, utteranceBuf, seqNum); err != nil {
					_ = s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_ERROR, err.Error(), voicev1.VoiceErrorCode_VOICE_ERROR_NLP_TIMEOUT)
				}
				utteranceBuf = nil
				vad.Reset()
				s.sessions.SetState(sess.ID, session.StateIdle)
				if err := s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_IDLE, "", voicev1.VoiceErrorCode_VOICE_ERROR_UNSPECIFIED); err != nil {
					return err
				}
			}

		case *voicev1.VoiceRequest_Event:
			switch payload.Event.Type {

			case voicev1.ControlEvent_TYPE_END_OF_SPEECH:
				if len(utteranceBuf) > 0 {
					seqNum++
					if err := s.processUtterance(ctx, stream, sess, streamCfg, utteranceBuf, seqNum); err != nil {
						_ = s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_ERROR, err.Error(), voicev1.VoiceErrorCode_VOICE_ERROR_NLP_TIMEOUT)
					}
					utteranceBuf = nil
					vad.Reset()
				}
				s.sessions.SetState(sess.ID, session.StateIdle)
				_ = s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_IDLE, "", voicev1.VoiceErrorCode_VOICE_ERROR_UNSPECIFIED)

			case voicev1.ControlEvent_TYPE_CANCEL:
				utteranceBuf = nil
				vad.Reset()
				s.sessions.SetState(sess.ID, session.StateIdle)
				_ = s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_IDLE, "cancelled", voicev1.VoiceErrorCode_VOICE_ERROR_UNSPECIFIED)

			case voicev1.ControlEvent_TYPE_NEW_TURN:
				utteranceBuf = nil
				vad.Reset()

			case voicev1.ControlEvent_TYPE_KEEP_ALIVE:
				// no-op — Touch() already called above

			default:
				s.log.WarnContext(ctx, "unknown ControlEvent type",
					slog.Int("type", int(payload.Event.Type)),
				)
			}
		}
	}

	// Signal clean session end.
	_ = s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_ENDED, "", voicev1.VoiceErrorCode_VOICE_ERROR_UNSPECIFIED)
	s.log.InfoContext(ctx, "Converse ended", slog.String("session_id", sess.ID))
	return nil
}

// ── processUtterance: STT → NLP → TTS stub → HUDAction ───────────────

func (s *VoiceServer) processUtterance(
	ctx context.Context,
	stream voicev1.VoiceService_ConverseServer,
	sess *session.Session,
	streamCfg *voicev1.StreamConfig,
	pcm []byte,
	seqNum int64,
) error {
	// 1. PROCESSING state
	s.sessions.SetState(sess.ID, session.StateProcessing)
	if err := s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_PROCESSING, "", voicev1.VoiceErrorCode_VOICE_ERROR_UNSPECIFIED); err != nil {
		return err
	}

	// 2. STT
	sttResult, err := audio.Transcribe(pcm, s.cfg.Audio.SampleRateHz, streamCfg.GetLanguageCode())
	if err != nil {
		return fmt.Errorf("STT failed: %w", err)
	}

	// 3. Stream final transcript to client (HUD subtitle)
	transcript := &voicev1.Transcript{
		Text:       sttResult.Text,
		IsFinal:    true,
		Confidence: sttResult.Confidence,
	}
	if err := stream.Send(&voicev1.VoiceResponse{
		SessionId:  sess.ID,
		SequenceNum: seqNum,
		Payload:    &voicev1.VoiceResponse_Transcript{Transcript: transcript},
	}); err != nil {
		return err
	}

	// 4. NLP — ProcessDialogueTurn (reuses nlp-service dialogue manager)
	nlpResp, err := s.nlp.ProcessDialogueTurn(ctx, &nlpv1.DialogueTurnRequest{
		Meta: &commonv1.RequestMeta{
			RequestId: fmt.Sprintf("voice-%s-%d", sess.ID, seqNum),
			UserId:    sess.UserID,
			SessionId: sess.NLPSession,
			Source:    "voice",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sess.NLPSession,
		Utterance: sttResult.Text,
		// Forward any context hints from the stream config.
		ContextHint: contextHint(streamCfg.GetContextTags()),
	})
	if err != nil {
		return fmt.Errorf("NLP dialogue turn: %w", err)
	}

	// 5. Stream Reply to client
	reply := &voicev1.Reply{
		ReplyText:            nlpResp.ReplyText,
		Intent:               nlpResp.ResolvedIntent.String(),
		RequiresConfirmation: nlpResp.RequiresConfirmation,
	}
	if err := stream.Send(&voicev1.VoiceResponse{
		SessionId:  sess.ID,
		SequenceNum: seqNum,
		Payload:    &voicev1.VoiceResponse_Reply{Reply: reply},
	}); err != nil {
		return err
	}

	// 6. SPEAKING state + TTS stub
	s.sessions.SetState(sess.ID, session.StateSpeaking)
	if err := s.sendStatus(stream, sess.ID, voicev1.StatusEvent_STATE_SPEAKING, "", voicev1.VoiceErrorCode_VOICE_ERROR_UNSPECIFIED); err != nil {
		return err
	}

	// TTS stub — replace with Cloud Text-to-Speech or ElevenLabs in prod.
	if err := stream.Send(&voicev1.VoiceResponse{
		SessionId:  sess.ID,
		SequenceNum: seqNum,
		Payload: &voicev1.VoiceResponse_AudioReply{
			AudioReply: &voicev1.AudioReply{
				Data:          []byte("[TTS stub — wire Cloud TTS here]"),
				Encoding:      voicev1.AudioEncoding_AUDIO_ENCODING_PCM_16BIT,
				Text:          nlpResp.ReplyText,
				IsFinalChunk:  true,
				ChunkIndex:    0,
			},
		},
	}); err != nil {
		return err
	}

	// 7. HUDAction — map NLP intent to a structured client action
	if action := intentToHUDAction(nlpResp); action != nil {
		if err := stream.Send(&voicev1.VoiceResponse{
			SessionId:  sess.ID,
			SequenceNum: seqNum,
			Payload:    &voicev1.VoiceResponse_Action{Action: action},
		}); err != nil {
			return err
		}
	}

	return nil
}

// ── GetSession ──────────────────────────────────────────────────────────────

func (s *VoiceServer) GetSession(
	ctx context.Context,
	req *voicev1.GetSessionRequest,
) (*voicev1.GetSessionResponse, error) {
	if req.GetMeta() == nil {
		return nil, status.Error(codes.InvalidArgument, "meta is required")
	}

	sess, ok := s.sessions.Get(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}

	return &voicev1.GetSessionResponse{
		Meta: &commonv1.ResponseMeta{
			RequestId: req.Meta.RequestId,
			Success:   true,
			Timestamp: timestamppb.Now(),
		},
		Session: &voicev1.VoiceSession{
			SessionId: sess.ID,
			UserId:    sess.UserID,
			StartedAt: timestamppb.New(sess.CreatedAt),
		},
	}, nil
}

// ── ListSessions ────────────────────────────────────────────────────────────

func (s *VoiceServer) ListSessions(
	ctx context.Context,
	req *voicev1.ListSessionsRequest,
) (*voicev1.ListSessionsResponse, error) {
	if req.GetMeta() == nil {
		return nil, status.Error(codes.InvalidArgument, "meta is required")
	}
	// Stub — production would query a persistent store (Firestore / Postgres).
	return &voicev1.ListSessionsResponse{
		Meta: &commonv1.ResponseMeta{
			RequestId: req.Meta.RequestId,
			Success:   true,
			Timestamp: timestamppb.Now(),
		},
	}, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (s *VoiceServer) sendStatus(
	stream voicev1.VoiceService_ConverseServer,
	sessionID string,
	state voicev1.StatusEvent_State,
	message string,
	errCode voicev1.VoiceErrorCode,
) error {
	return stream.Send(&voicev1.VoiceResponse{
		SessionId: sessionID,
		Payload: &voicev1.VoiceResponse_Status{
			Status: &voicev1.StatusEvent{
				State:     state,
				Message:   message,
				ErrorCode: errCode,
			},
		},
	})
}

// intentToHUDAction converts NLP resolved intent into a HUDAction.
// Extend this mapping as new intents are added to nlp.proto.
func intentToHUDAction(resp *nlpv1.DialogueTurnResponse) *voicev1.HUDAction {
	switch resp.ResolvedIntent {
	case nlpv1.Intent_INTENT_SYSTEM_CONTROL:
		return &voicev1.HUDAction{
			Type:        voicev1.HUDAction_TYPE_HARDWARE_CMD,
			PayloadJson: fmt.Sprintf(`{"intent":"%s"}`, resp.ResolvedIntent),
			Severity:    commonv1.Severity_SEVERITY_INFO,
		}
	case nlpv1.Intent_INTENT_THREAT_ALERT:
		return &voicev1.HUDAction{
			Type:        voicev1.HUDAction_TYPE_SECURITY_PROTOCOL,
			PayloadJson: fmt.Sprintf(`{"intent":"%s"}`, resp.ResolvedIntent),
			Severity:    commonv1.Severity_SEVERITY_CRITICAL,
		}
	case nlpv1.Intent_INTENT_EMERGENCY:
		return &voicev1.HUDAction{
			Type:        voicev1.HUDAction_TYPE_SECURITY_PROTOCOL,
			PayloadJson: fmt.Sprintf(`{"intent":"%s","level":"emergency"}`, resp.ResolvedIntent),
			Severity:    commonv1.Severity_SEVERITY_EMERGENCY,
		}
	default:
		return nil
	}
}

// contextHint flattens context tags to a single string for NLP.
func contextHint(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	hint := ""
	for i, t := range tags {
		if i > 0 {
			hint += ","
		}
		hint += t
	}
	return hint
}
