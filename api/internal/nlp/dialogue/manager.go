// Package dialogue manages multi-turn conversation sessions.
package dialogue

import (
	"context"
	"log/slog"

	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	nlpv1 "github.com/rkrimper1/jarvis/api/pb/nlp"
	"github.com/rkrimper1/jarvis/api/internal/integrations/claude"
)

// Manager manages multi-turn dialogue sessions backed by Redis.
type Manager struct {
	store        *RedisStore
	claude       *claude.Client
	maxHistory   int
	confidThresh float32
	log          *slog.Logger
}

// NewManager creates a Manager wired to Redis and Claude.
func NewManager(store *RedisStore, claudeClient *claude.Client, maxHistory int, confidThresh float32, log *slog.Logger) *Manager {
	return &Manager{
		store:        store,
		claude:       claudeClient,
		maxHistory:   maxHistory,
		confidThresh: confidThresh,
		log:          log,
	}
}

// BuildReply generates a reply via Claude for AI-handled intents.
// INTENT_EMERGENCY and INTENT_COMMAND remain deterministic.
func (m *Manager) BuildReply(
	ctx context.Context,
	sessionID string,
	utterance string,
	intent nlpv1.Intent,
	confidence float32,
) (reply string, requiresConfirmation bool, err error) {

	requiresConfirmation = confidence < m.confidThresh

	// Deterministic intents — never delegate to Claude.
	switch intent {
	case nlpv1.Intent_INTENT_EMERGENCY:
		return "Understood. Initiating emergency protocol immediately. All systems on alert.", false, nil
	case nlpv1.Intent_INTENT_COMMAND:
		if requiresConfirmation {
			return "I want to make sure I understand — you'd like me to: \"" + utterance + "\". Shall I proceed?", true, nil
		}
		return "Executing: " + utterance + ". Standing by for confirmation of completion.", false, nil
	}

	// Claude-handled intents: load history, call API, persist turn.
	if m.claude == nil {
		return "I seem to be having trouble connecting my thoughts at the moment. Please try again, sir.", false, nil
	}
	history, loadErr := m.store.Load(ctx, sessionID)
	if loadErr != nil {
		m.log.WarnContext(ctx, "dialogue: failed to load session history, proceeding without it",
			slog.String("session_id", sessionID),
			slog.Any("err", loadErr),
		)
		history = nil
	}

	turns := make([]claude.Turn, len(history))
	for i, h := range history {
		turns[i] = claude.Turn{Role: h.Role, Content: h.Content}
	}

	reply, err = m.claude.Complete(ctx, SystemPrompt(intent), turns, utterance)
	if err != nil {
		m.log.ErrorContext(ctx, "dialogue: claude call failed", slog.Any("err", err))
		return "I seem to be having trouble connecting my thoughts at the moment. Please try again, sir.", false, nil
	}

	if storeErr := m.store.Append(ctx, sessionID, utterance, reply, m.maxHistory); storeErr != nil {
		m.log.WarnContext(ctx, "dialogue: failed to persist turn",
			slog.String("session_id", sessionID),
			slog.Any("err", storeErr),
		)
	}

	return reply, requiresConfirmation, nil
}

// BuildHistory returns the stored history as proto DialogueHistory entries.
func (m *Manager) BuildHistory(ctx context.Context, sessionID string) ([]*nlpv1.DialogueHistory, error) {
	turns, err := m.store.Load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]*nlpv1.DialogueHistory, len(turns))
	for i, t := range turns {
		out[i] = &nlpv1.DialogueHistory{
			Role:      t.Role,
			Text:      t.Content,
			Timestamp: timestamppb.Now(),
		}
	}
	return out, nil
}

// DeleteSession removes a session from Redis.
func (m *Manager) DeleteSession(ctx context.Context, sessionID string) error {
	return m.store.Delete(ctx, sessionID)
}

// MetaSuccess builds a successful ResponseMeta.
func MetaSuccess(requestID string) *commonv1.ResponseMeta {
	return &commonv1.ResponseMeta{
		RequestId: requestID,
		Success:   true,
		Timestamp: timestamppb.Now(),
	}
}

// MetaError builds an error ResponseMeta.
func MetaError(requestID, code, msg string) *commonv1.ResponseMeta {
	return &commonv1.ResponseMeta{
		RequestId:    requestID,
		Success:      false,
		ErrorCode:    code,
		ErrorMessage: msg,
		Timestamp:    timestamppb.Now(),
	}
}
