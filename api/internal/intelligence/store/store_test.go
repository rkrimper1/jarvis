package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/rkrimper1/jarvis/api/internal/intelligence/store"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s, err := store.New(db, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return s
}

// ── RawSignal ─────────────────────────────────────────────────────────────────

func TestSaveSignal_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sig, err := s.SaveSignal(ctx, intelligv1.SourceType_SOURCE_TYPE_MANUAL, "test content", "")
	if err != nil {
		t.Fatalf("SaveSignal: %v", err)
	}
	if sig.Id == "" {
		t.Error("expected non-empty id")
	}
	if sig.RawContent != "test content" {
		t.Errorf("raw_content = %q, want %q", sig.RawContent, "test content")
	}
	if sig.SourceType != intelligv1.SourceType_SOURCE_TYPE_MANUAL {
		t.Errorf("source_type = %v, want MANUAL", sig.SourceType)
	}
	if sig.IngestedAt == nil {
		t.Error("expected ingested_at to be set")
	}
}

func TestSaveSignal_RSSSourceType(t *testing.T) {
	s := newTestStore(t)
	sig, err := s.SaveSignal(context.Background(), intelligv1.SourceType_SOURCE_TYPE_RSS, "rss item", "https://example.com/feed")
	if err != nil {
		t.Fatalf("SaveSignal: %v", err)
	}
	if sig.SourceType != intelligv1.SourceType_SOURCE_TYPE_RSS {
		t.Errorf("source_type = %v, want RSS", sig.SourceType)
	}
	if sig.SourceUri != "https://example.com/feed" {
		t.Errorf("source_uri = %q, want %q", sig.SourceUri, "https://example.com/feed")
	}
}

func TestGetSignal_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetSignal(context.Background(), "no-such-id")
	if err == nil {
		t.Error("expected error for missing signal")
	}
}

// ── IntelCard ─────────────────────────────────────────────────────────────────

func TestSaveCard_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	card, err := s.SaveCard(ctx,
		"Arc Reactor Vulnerability",
		"Leaked schematics indicate a critical flaw in Mk IV power coupling.",
		intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL,
		0.87,
		"Patch the power coupling firmware immediately.",
		[]string{"signal-1", "signal-2"},
	)
	if err != nil {
		t.Fatalf("SaveCard: %v", err)
	}
	if card.Id == "" {
		t.Error("expected non-empty id")
	}
	if card.Title != "Arc Reactor Vulnerability" {
		t.Errorf("title = %q", card.Title)
	}
	if card.Status != intelligv1.IntelCardStatus_INTEL_CARD_STATUS_PENDING_REVIEW {
		t.Errorf("status = %v, want PENDING_REVIEW", card.Status)
	}
	if card.OpportunityType != intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL {
		t.Errorf("opportunity_type = %v, want TACTICAL", card.OpportunityType)
	}
	if len(card.RawSignalIds) != 2 {
		t.Errorf("raw_signal_ids len = %d, want 2", len(card.RawSignalIds))
	}
	if card.CreatedAt == nil || card.UpdatedAt == nil {
		t.Error("expected timestamps to be set")
	}
}

func TestSaveCard_NilSignalIDs(t *testing.T) {
	s := newTestStore(t)
	card, err := s.SaveCard(context.Background(), "Title", "Summary",
		intelligv1.OpportunityType_OPPORTUNITY_TYPE_STRATEGIC, 0.5, "Do something", nil)
	if err != nil {
		t.Fatalf("SaveCard: %v", err)
	}
	// nil should come back as nil/empty, not a panic
	if len(card.RawSignalIds) != 0 {
		t.Errorf("expected empty raw_signal_ids, got %v", card.RawSignalIds)
	}
}

func TestGetCard_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetCard(context.Background(), "no-such-id")
	if err == nil {
		t.Error("expected error for missing card")
	}
}

// ── UpdateCardStatus ──────────────────────────────────────────────────────────

func TestUpdateCardStatus_Confirm(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	card, err := s.SaveCard(ctx, "T", "S", intelligv1.OpportunityType_OPPORTUNITY_TYPE_RESOURCE, 0.6, "Act", nil)
	if err != nil {
		t.Fatalf("SaveCard: %v", err)
	}

	updated, err := s.UpdateCardStatus(ctx, card.Id, intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED)
	if err != nil {
		t.Fatalf("UpdateCardStatus: %v", err)
	}
	if updated.Status != intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED {
		t.Errorf("status = %v, want CONFIRMED", updated.Status)
	}
}

func TestUpdateCardStatus_Dismiss(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	card, err := s.SaveCard(ctx, "T", "S", intelligv1.OpportunityType_OPPORTUNITY_TYPE_RESOURCE, 0.6, "Act", nil)
	if err != nil {
		t.Fatalf("SaveCard: %v", err)
	}

	updated, err := s.UpdateCardStatus(ctx, card.Id, intelligv1.IntelCardStatus_INTEL_CARD_STATUS_DISMISSED)
	if err != nil {
		t.Fatalf("UpdateCardStatus: %v", err)
	}
	if updated.Status != intelligv1.IntelCardStatus_INTEL_CARD_STATUS_DISMISSED {
		t.Errorf("status = %v, want DISMISSED", updated.Status)
	}
}

func TestUpdateCardStatus_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UpdateCardStatus(context.Background(), "no-such-id", intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED)
	if err == nil {
		t.Error("expected error for missing card")
	}
}

// ── ListCards ─────────────────────────────────────────────────────────────────

func TestListCards_AllStatuses(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := range 3 {
		if _, err := s.SaveCard(ctx, "Card", "Summary", intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL, float32(i)*0.3, "Act", nil); err != nil {
			t.Fatalf("SaveCard %d: %v", i, err)
		}
	}

	cards, total, _, err := s.ListCards(ctx, intelligv1.IntelCardStatus_INTEL_CARD_STATUS_UNSPECIFIED, 0, "")
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(cards) != 3 {
		t.Errorf("len(cards) = %d, want 3", len(cards))
	}
}

func TestListCards_StatusFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c1, _ := s.SaveCard(ctx, "C1", "S", intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL, 0.9, "Act", nil)
	s.SaveCard(ctx, "C2", "S", intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL, 0.5, "Act", nil)
	s.UpdateCardStatus(ctx, c1.Id, intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED)

	cards, total, _, err := s.ListCards(ctx, intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED, 0, "")
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(cards) != 1 || cards[0].Id != c1.Id {
		t.Errorf("expected confirmed card; got %v", cards)
	}
}

func TestListCards_Pagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := range 5 {
		if _, err := s.SaveCard(ctx, "Card", "Summary", intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL, float32(i)*0.1, "Act", nil); err != nil {
			t.Fatalf("SaveCard %d: %v", i, err)
		}
	}

	// Page 1: size 2
	page1, total, nextToken, err := s.ListCards(ctx, intelligv1.IntelCardStatus_INTEL_CARD_STATUS_UNSPECIFIED, 2, "")
	if err != nil {
		t.Fatalf("ListCards page1: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(page1) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1))
	}
	if nextToken == "" {
		t.Error("expected non-empty next_token after page 1")
	}

	// Page 2: use next token
	page2, _, nextToken2, err := s.ListCards(ctx, intelligv1.IntelCardStatus_INTEL_CARD_STATUS_UNSPECIFIED, 2, nextToken)
	if err != nil {
		t.Fatalf("ListCards page2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2))
	}
	if nextToken2 == "" {
		t.Error("expected non-empty next_token after page 2")
	}

	// Page 3: last page
	page3, _, nextToken3, err := s.ListCards(ctx, intelligv1.IntelCardStatus_INTEL_CARD_STATUS_UNSPECIFIED, 2, nextToken2)
	if err != nil {
		t.Fatalf("ListCards page3: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("page3 len = %d, want 1", len(page3))
	}
	if nextToken3 != "" {
		t.Errorf("expected empty next_token on last page, got %q", nextToken3)
	}
}

func TestListCards_DefaultPageSize(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert exactly 1 card — passing pageSize=0 should still work (uses default)
	if _, err := s.SaveCard(ctx, "C", "S", intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL, 0.5, "Act", nil); err != nil {
		t.Fatalf("SaveCard: %v", err)
	}

	cards, total, _, err := s.ListCards(ctx, intelligv1.IntelCardStatus_INTEL_CARD_STATUS_UNSPECIFIED, 0, "")
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if total != 1 || len(cards) != 1 {
		t.Errorf("expected 1 card; total=%d len=%d", total, len(cards))
	}
}
