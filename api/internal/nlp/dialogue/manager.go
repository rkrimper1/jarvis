// Package dialogue manages multi-turn conversation sessions.
package dialogue

import (
	"fmt"
	"sync"
	"time"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	nlpv1 "github.com/rkrimper1/jarvis/api/pb/nlp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Session holds the state of a single conversation session.
type Session struct {
	ID        string
	UserID    string
	History   []*nlpv1.DialogueHistory
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Manager manages all active dialogue sessions.
type Manager struct {
	mu              sync.RWMutex
	sessions        map[string]*Session
	maxHistoryTurns int
	sessionTTL      time.Duration
}

// NewManager creates a new dialogue Manager.
func NewManager(maxHistory int, ttl time.Duration) *Manager {
	m := &Manager{
		sessions:        make(map[string]*Session),
		maxHistoryTurns: maxHistory,
		sessionTTL:      ttl,
	}
	go m.runGC()
	return m
}

// GetOrCreate returns an existing session or creates a new one.
func (m *Manager) GetOrCreate(sessionID, userID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[sessionID]; ok {
		s.UpdatedAt = time.Now()
		return s
	}

	s := &Session{
		ID:        sessionID,
		UserID:    userID,
		History:   make([]*nlpv1.DialogueHistory, 0),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.sessions[sessionID] = s
	return s
}

// AppendTurn adds a user and assistant turn to the session history.
func (m *Manager) AppendTurn(sessionID, userText, jarvisReply string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionID]
	if !ok {
		return
	}

	now := timestamppb.Now()
	s.History = append(s.History,
		&nlpv1.DialogueHistory{Role: "user", Text: userText, Timestamp: now},
		&nlpv1.DialogueHistory{Role: "jarvis", Text: jarvisReply, Timestamp: now},
	)

	// Trim to max history window
	if len(s.History) > m.maxHistoryTurns*2 {
		s.History = s.History[len(s.History)-m.maxHistoryTurns*2:]
	}
	s.UpdatedAt = time.Now()
}

// BuildReply generates a contextual reply.
// In production this calls an LLM; here we use a pattern-based responder
// that demonstrates the dialogue contract cleanly.
func (m *Manager) BuildReply(
	session *Session,
	utterance string,
	intent nlpv1.Intent,
	confidenceThresh float32,
	confidence float32,
) (reply string, requiresConfirmation bool) {

	requiresConfirmation = confidence < confidenceThresh

	switch intent {
	case nlpv1.Intent_INTENT_EMERGENCY:
		return "Understood. Initiating emergency protocol immediately. All systems on alert.", false

	case nlpv1.Intent_INTENT_COMMAND:
		if requiresConfirmation {
			return fmt.Sprintf(
				"I want to make sure I understand — you'd like me to: \"%s\". Shall I proceed?",
				utterance,
			), true
		}
		return fmt.Sprintf("Executing: %s. Standing by for confirmation of completion.", utterance), false

	case nlpv1.Intent_INTENT_ANALYSIS_REQUEST:
		return "Running analysis now. I'll have results for you momentarily, sir.", false

	case nlpv1.Intent_INTENT_QUERY:
		return fmt.Sprintf("Searching my databases for: \"%s\". One moment.", utterance), false

	case nlpv1.Intent_INTENT_SMALL_TALK:
		return m.smallTalkReply(session, utterance), false

	default:
		return "I didn't quite catch that. Could you rephrase, sir?", false
	}
}

func (m *Manager) smallTalkReply(s *Session, utterance string) string {
	greetings := map[string]string{
		"hello":        "Hello, sir. All systems nominal. How may I assist?",
		"hi":           "Good to hear from you, sir. What do you need?",
		"good morning": "Good morning, sir. Shall I run your morning briefing?",
		"good evening": "Good evening, sir. A quiet night, I hope.",
		"how are you":  "All systems running at optimal capacity, sir. Thank you for asking.",
		"thanks":       "Of course, sir. That's what I'm here for.",
		"thank you":    "Always a pleasure, sir.",
	}

	lower := utterance
	for trigger, reply := range greetings {
		if len(lower) >= len(trigger) && lower[:len(trigger)] == trigger {
			return reply
		}
	}
	return "I'm afraid I didn't follow that, sir. Could you be more specific?"
}

// Delete removes a session.
func (m *Manager) Delete(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// runGC periodically removes expired sessions.
func (m *Manager) runGC() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		cutoff := time.Now().Add(-m.sessionTTL)
		for id, s := range m.sessions {
			if s.UpdatedAt.Before(cutoff) {
				delete(m.sessions, id)
			}
		}
		m.mu.Unlock()
	}
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
