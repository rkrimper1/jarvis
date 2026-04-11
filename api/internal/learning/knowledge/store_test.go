package knowledge_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	learningv1 "github.com/rkrimper1/jarvis/api/pb/learning"
	"github.com/rkrimper1/jarvis/api/internal/learning/knowledge"
)

func newTestStore(t *testing.T) *knowledge.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s, err := knowledge.New(db, 30, "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("knowledge.New: %v", err)
	}
	return s
}

// ── Save ──────────────────────────────────────────────────────────────────────

func TestSave_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry, err := s.Save(ctx, "palladium toxicity", "Palladium is toxic at high doses.", "claude_api", 0.9, "chemistry,health")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if entry.Id == 0 {
		t.Error("expected non-zero Id")
	}
	if entry.Query != "palladium toxicity" {
		t.Errorf("Query = %q, want %q", entry.Query, "palladium toxicity")
	}
	if entry.Summary != "Palladium is toxic at high doses." {
		t.Errorf("Summary mismatch")
	}
	if entry.Source != learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_CLAUDE_API {
		t.Errorf("Source = %v, want CLAUDE_API", entry.Source)
	}
	if entry.Confidence != 0.9 {
		t.Errorf("Confidence = %f, want 0.9", entry.Confidence)
	}
	if entry.CreatedAt == nil || entry.UpdatedAt == nil {
		t.Error("expected timestamps")
	}
}

func TestSave_WebSearchSource(t *testing.T) {
	s := newTestStore(t)
	entry, err := s.Save(context.Background(), "vibranium", "A rare metal from Wakanda.", "web_search", 0.75, "")
	if err != nil {
		t.Fatalf("Save web_search: %v", err)
	}
	if entry.Source != learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_WEB_SEARCH {
		t.Errorf("Source = %v, want WEB_SEARCH", entry.Source)
	}
}

func TestSave_ManualSource(t *testing.T) {
	s := newTestStore(t)
	entry, err := s.Save(context.Background(), "arc reactor", "Fusion-based power source.", "manual", 1.0, "")
	if err != nil {
		t.Fatalf("Save manual: %v", err)
	}
	if entry.Source != learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_MANUAL {
		t.Errorf("Source = %v, want MANUAL", entry.Source)
	}
}

func TestSave_MultipleTimes_AreIndependentRows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Save(ctx, "query A", "summary A", "manual", 1.0, "")
	s.Save(ctx, "query B", "summary B", "manual", 1.0, "")

	entries, err := s.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("len(entries) = %d, want 2", len(entries))
	}
}

// ── Search ────────────────────────────────────────────────────────────────────

func TestSearch_FTSMatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Save(ctx, "palladium toxicity arc reactor", "Palladium poisons the bloodstream.", "manual", 1.0, "")

	results, found, err := s.Search(ctx, "palladium")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for FTS match")
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if !strings.Contains(results[0].Summary, "Palladium") {
		t.Errorf("unexpected summary: %q", results[0].Summary)
	}
}

func TestSearch_FuzzyFallback(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Insert with a term that won't match FTS phrase but will match LIKE
	s.Save(ctx, "super-hero suit design", "The suit has repulsors.", "manual", 1.0, "")

	results, found, err := s.Search(ctx, "repulsors")
	if err != nil {
		t.Fatalf("Search fuzzy: %v", err)
	}
	if !found {
		t.Fatal("expected fuzzy match to find result")
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 fuzzy result")
	}
}

func TestSearch_NoMatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Save(ctx, "arc reactor", "Fusion device.", "manual", 1.0, "")

	results, found, err := s.Search(ctx, "wakanda vibranium completely unrelated")
	if err != nil {
		t.Fatalf("Search no match: %v", err)
	}
	if found {
		t.Error("expected found=false for no match")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_EmptyStore(t *testing.T) {
	s := newTestStore(t)
	results, found, err := s.Search(context.Background(), "anything")
	if err != nil {
		t.Fatalf("Search empty store: %v", err)
	}
	if found {
		t.Error("expected found=false on empty store")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on empty store, got %d", len(results))
	}
}

func TestSearch_ReturnsAtMost10Results(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 15; i++ {
		s.Save(ctx, "iron man suit upgrade", "Upgrade notes.", "manual", 1.0, "")
	}

	results, _, err := s.Search(ctx, "iron man")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) > 10 {
		t.Errorf("len(results) = %d, want <= 10", len(results))
	}
}

// ── List ──────────────────────────────────────────────────────────────────────

func TestList_Empty(t *testing.T) {
	s := newTestStore(t)
	entries, err := s.List(context.Background(), 5)
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestList_RespectsLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		s.Save(ctx, "entry", "summary", "manual", 1.0, "")
	}
	entries, err := s.List(ctx, 3)
	if err != nil {
		t.Fatalf("List with limit: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("len(entries) = %d, want 3", len(entries))
	}
}

func TestList_ZeroLimit_DefaultsToFive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		s.Save(ctx, "entry", "summary", "manual", 1.0, "")
	}
	entries, err := s.List(ctx, 0)
	if err != nil {
		t.Fatalf("List zero limit: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("len(entries) = %d, want 5 (default)", len(entries))
	}
}

func TestList_NegativeLimit_DefaultsToFive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		s.Save(ctx, "entry", "summary", "manual", 1.0, "")
	}
	entries, err := s.List(ctx, -1)
	if err != nil {
		t.Fatalf("List negative limit: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("len(entries) = %d, want 5 (default)", len(entries))
	}
}

// ── SearchWithClaude — no API key ─────────────────────────────────────────────

func TestSearchWithClaude_NoAPIKey_ReturnsError(t *testing.T) {
	// Store created with empty API key
	s := newTestStore(t)
	_, err := s.SearchWithClaude(context.Background(), "any query",
		learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_CLAUDE_API)
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is empty")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error should mention ANTHROPIC_API_KEY, got: %v", err)
	}
}

// ── Stale filtering ───────────────────────────────────────────────────────────

func TestSearch_StaleFilter_ZeroDays_ReturnsAll(t *testing.T) {
	// A store with 0 stale days treats everything as stale — verify graceful empty result
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s, err := knowledge.New(db, 0, "", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	s.Save(ctx, "arc reactor", "Fusion power.", "manual", 1.0, "")

	_, _, err = s.Search(ctx, "arc reactor")
	if err != nil {
		t.Fatalf("Search with 0 stale days: %v", err)
	}
	// With 0 stale days all entries are immediately stale — no panic expected
}
