// Package server implements the gRPC NLPService.
package server

import (
	"context"
	"io"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	nlpv1 "github.com/rkrimper1/jarvis/api/pb/nlp"
	claudeclient "github.com/rkrimper1/jarvis/api/internal/integrations/claude"
	"github.com/rkrimper1/jarvis/api/internal/nlp/config"
	"github.com/rkrimper1/jarvis/api/internal/nlp/dialogue"
	"github.com/rkrimper1/jarvis/api/internal/nlp/entity"
	"github.com/rkrimper1/jarvis/api/internal/nlp/intent"
	"github.com/rkrimper1/jarvis/api/middleware"
)

// NLPServer is the gRPC server that implements nlpv1.NLPServiceServer.
type NLPServer struct {
	nlpv1.UnimplementedNLPServiceServer

	cfg        *config.Config
	classifier *intent.Classifier
	extractor  *entity.Extractor
	dialogue   *dialogue.Manager
	log        *slog.Logger
}

// New creates a new NLPServer with all dependencies wired.
func New(cfg *config.Config, log *slog.Logger) *NLPServer {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	redisStore := dialogue.NewRedisStore(rdb, cfg.Dialogue.SessionTTL)
	claudeClient := claudeclient.New(cfg.Claude.APIKey, cfg.Claude.Model, cfg.Claude.MaxTokens)

	return &NLPServer{
		cfg:        cfg,
		classifier: intent.New(),
		extractor:  entity.New(),
		dialogue:   dialogue.NewManager(redisStore, claudeClient, cfg.Dialogue.MaxHistoryTurns, cfg.Dialogue.ConfidenceThresh, log),
		log:        log,
	}
}

// ── ParseIntent ───────────────────────────────────────────────────────

// ParseIntent parses a raw utterance and returns an intent + entities.
func (s *NLPServer) ParseIntent(
	ctx context.Context,
	req *nlpv1.ParseIntentRequest,
) (*nlpv1.ParseIntentResponse, error) {

	if err := validateParseMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "nlp/ParseIntent")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	s.log.InfoContext(ctx, "ParseIntent",
		slog.String("request_id", req.Meta.RequestId),
		slog.String("text", req.RawText),
	)

	result := s.classifier.Classify(req.RawText, req.ContextTags)
	entities := s.extractor.Extract(req.RawText)
	suggestions := intent.SuggestActions(result.Intent, entityValues(entities))

	return &nlpv1.ParseIntentResponse{
		Meta: &commonv1.ResponseMeta{
			RequestId: req.Meta.RequestId,
			Success:   true,
			Timestamp: timestamppb.Now(),
		},
		Intent:           result.Intent,
		Confidence:       result.Confidence,
		CanonicalCommand: result.Canonical,
		Entities:         entities,
		SuggestedActions: suggestions,
	}, nil
}

// ── ProcessDialogueTurn ───────────────────────────────────────────────

// ProcessDialogueTurn handles one turn of a multi-turn conversation.
func (s *NLPServer) ProcessDialogueTurn(
	ctx context.Context,
	req *nlpv1.ProcessDialogueTurnRequest,
) (*nlpv1.ProcessDialogueTurnResponse, error) {

	if req.GetMeta() == nil {
		return nil, status.Error(codes.InvalidArgument, "meta is required")
	}
	ctx, span := trace.StartSpan(ctx, "nlp/ProcessDialogueTurn")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if req.Utterance == "" {
		return nil, status.Error(codes.InvalidArgument, "utterance is required")
	}

	sessionID := req.SessionId
	if sessionID == "" {
		sessionID = req.Meta.SessionId
	}

	s.log.InfoContext(ctx, "ProcessDialogueTurn",
		slog.String("session_id", sessionID),
		slog.String("utterance", req.Utterance),
	)

	result := s.classifier.Classify(req.Utterance, nil)

	reply, requiresConfirmation, err := s.dialogue.BuildReply(
		ctx,
		sessionID,
		req.Utterance,
		result.Intent,
		result.Confidence,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "dialogue: %v", err)
	}

	return &nlpv1.ProcessDialogueTurnResponse{
		Meta:                 dialogue.MetaSuccess(req.Meta.RequestId),
		ReplyText:            reply,
		ResolvedIntent:       result.Intent,
		RequiresConfirmation: requiresConfirmation,
		SessionId:            sessionID,
	}, nil
}

// ── StreamVoiceInput ──────────────────────────────────────────────────

// StreamVoiceInput handles a bidirectional voice transcription stream.
// Each incoming ParseRequest chunk is classified and a ParseResponse is
// streamed back — simulating real-time HUD feedback.
func (s *NLPServer) StreamVoiceInput(
	stream nlpv1.NLPService_StreamVoiceInputServer,
) error {

	ctx := stream.Context()
	s.log.InfoContext(ctx, "StreamVoiceInput started")

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			s.log.InfoContext(ctx, "StreamVoiceInput: client closed stream")
			return nil
		}
		if err != nil {
			s.log.ErrorContext(ctx, "StreamVoiceInput: recv error", slog.Any("err", err))
			return status.Errorf(codes.Internal, "stream recv: %v", err)
		}

		if req.RawText == "" {
			// skip empty frames (silence / padding)
			continue
		}

		ctx, uttSpan := trace.StartSpan(ctx, "nlp/StreamVoiceInput/classify")
		result := s.classifier.Classify(req.RawText, req.ContextTags)
		entities := s.extractor.Extract(req.RawText)
		uttSpan.AddAttributes(
			trace.StringAttribute("intent", result.Intent.String()),
			trace.Float64Attribute("confidence", float64(result.Confidence)),
		)
		uttSpan.End()

		resp := &nlpv1.StreamVoiceInputResponse{
			Meta: &commonv1.ResponseMeta{
				RequestId: req.GetMeta().GetRequestId(),
				Success:   true,
				Timestamp: timestamppb.Now(),
			},
			Intent:           result.Intent,
			Confidence:       result.Confidence,
			CanonicalCommand: result.Canonical,
			Entities:         entities,
			SuggestedActions: intent.SuggestActions(result.Intent, entityValues(entities)),
		}

		if err := stream.Send(resp); err != nil {
			s.log.ErrorContext(ctx, "StreamVoiceInput: send error", slog.Any("err", err))
			return status.Errorf(codes.Internal, "stream send: %v", err)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────

func validateParseMeta(meta *commonv1.RequestMeta) error {
	if meta == nil {
		return status.Error(codes.InvalidArgument, "meta is required")
	}
	if meta.RequestId == "" {
		return status.Error(codes.InvalidArgument, "meta.request_id is required")
	}
	return nil
}

func entityValues(entities []*nlpv1.Entity) []string {
	vals := make([]string, 0, len(entities))
	for _, e := range entities {
		vals = append(vals, e.Value)
	}
	return vals
}
