package server_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/rkrimper1/jarvis/api/internal/intelligence/fusion"
	"github.com/rkrimper1/jarvis/api/internal/intelligence/server"
	intelstore "github.com/rkrimper1/jarvis/api/internal/intelligence/store"
	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestStore(t *testing.T) *intelstore.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := intelstore.New(db, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return s
}

func meta(id string) *commonv1.RequestMeta {
	return &commonv1.RequestMeta{RequestId: id, UserId: "u1"}
}

// mockEngine is a fusion.Engine that returns a fixed Result or error.
type mockEngine struct {
	result *fusion.Result
	err    error
}

func (m *mockEngine) Fuse(_ context.Context, _ string) (*fusion.Result, error) {
	return m.result, m.err
}

func okEngine() *mockEngine {
	return &mockEngine{result: &fusion.Result{
		Title:           "Test Title",
		Summary:         "Test summary for the intel card.",
		OpportunityType: intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL,
		ConfidenceScore: 0.88,
		SuggestedAction: "Brief the team immediately.",
	}}
}

// ── meta validation ───────────────────────────────────────────────────────────

func TestIngestSignal_MissingMeta(t *testing.T) {
	srv := server.NewWithIntelHunt(slog.Default(), newTestStore(t), okEngine())
	_, err := srv.IngestSignal(context.Background(), &intelligv1.IngestSignalRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestIngestSignal_MissingRequestID(t *testing.T) {
	srv := server.NewWithIntelHunt(slog.Default(), newTestStore(t), okEngine())
	_, err := srv.IngestSignal(context.Background(), &intelligv1.IngestSignalRequest{
		Meta: &commonv1.RequestMeta{UserId: "u1"},
	})
	assertCode(t, err, codes.InvalidArgument)
}

// ── FailedPrecondition when store/engine not wired ────────────────────────────

func TestIngestSignal_NoStore(t *testing.T) {
	srv := server.New(slog.Default())
	_, err := srv.IngestSignal(context.Background(), &intelligv1.IngestSignalRequest{
		Meta: meta("req-1"), RawContent: "some signal",
	})
	assertCode(t, err, codes.FailedPrecondition)
}

func TestListIntelCards_NoStore(t *testing.T) {
	srv := server.New(slog.Default())
	_, err := srv.ListIntelCards(context.Background(), &intelligv1.ListIntelCardsRequest{Meta: meta("req-1")})
	assertCode(t, err, codes.FailedPrecondition)
}

func TestConfirmAction_NoStore(t *testing.T) {
	srv := server.New(slog.Default())
	_, err := srv.ConfirmAction(context.Background(), &intelligv1.ConfirmActionRequest{
		Meta:      meta("req-1"),
		CardId:    "card-1",
		NewStatus: intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED,
	})
	assertCode(t, err, codes.FailedPrecondition)
}

// ── IngestSignal happy path ───────────────────────────────────────────────────

func TestIngestSignal_HappyPath(t *testing.T) {
	srv := server.NewWithIntelHunt(slog.Default(), newTestStore(t), okEngine())
	resp, err := srv.IngestSignal(context.Background(), &intelligv1.IngestSignalRequest{
		Meta:       meta("req-1"),
		SourceType: intelligv1.SourceType_SOURCE_TYPE_MANUAL,
		RawContent: "Acme cuts enterprise pricing by 15%.",
	})
	if err != nil {
		t.Fatalf("IngestSignal: %v", err)
	}
	if resp.Signal == nil || resp.Signal.Id == "" {
		t.Error("expected non-empty signal")
	}
	if resp.Card == nil || resp.Card.Id == "" {
		t.Error("expected non-empty card")
	}
	if resp.Card.Title != "Test Title" {
		t.Errorf("card title = %q, want %q", resp.Card.Title, "Test Title")
	}
	if resp.Card.Status != intelligv1.IntelCardStatus_INTEL_CARD_STATUS_PENDING_REVIEW {
		t.Errorf("card status = %v, want PENDING_REVIEW", resp.Card.Status)
	}
	if resp.Meta == nil || !resp.Meta.Success {
		t.Error("expected meta.success = true")
	}
}

func TestIngestSignal_EmptyRawContent(t *testing.T) {
	srv := server.NewWithIntelHunt(slog.Default(), newTestStore(t), okEngine())
	_, err := srv.IngestSignal(context.Background(), &intelligv1.IngestSignalRequest{
		Meta: meta("req-1"), RawContent: "",
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestIngestSignal_FusionError(t *testing.T) {
	eng := &mockEngine{err: errors.New("claude unavailable")}
	srv := server.NewWithIntelHunt(slog.Default(), newTestStore(t), eng)
	_, err := srv.IngestSignal(context.Background(), &intelligv1.IngestSignalRequest{
		Meta: meta("req-1"), RawContent: "some signal",
	})
	assertCode(t, err, codes.Internal)
}

// ── ListIntelCards ────────────────────────────────────────────────────────────

func TestListIntelCards_Empty(t *testing.T) {
	srv := server.NewWithIntelHunt(slog.Default(), newTestStore(t), okEngine())
	resp, err := srv.ListIntelCards(context.Background(), &intelligv1.ListIntelCardsRequest{
		Meta: meta("req-1"),
	})
	if err != nil {
		t.Fatalf("ListIntelCards: %v", err)
	}
	if resp.TotalCount != 0 {
		t.Errorf("total_count = %d, want 0", resp.TotalCount)
	}
	if len(resp.Cards) != 0 {
		t.Errorf("len(cards) = %d, want 0", len(resp.Cards))
	}
}

func TestListIntelCards_AfterIngest(t *testing.T) {
	store := newTestStore(t)
	srv := server.NewWithIntelHunt(slog.Default(), store, okEngine())
	ctx := context.Background()

	// Ingest 3 signals
	for i := range 3 {
		_, err := srv.IngestSignal(ctx, &intelligv1.IngestSignalRequest{
			Meta:       meta("ingest-" + string(rune('1'+i))),
			RawContent: "signal content",
		})
		if err != nil {
			t.Fatalf("IngestSignal %d: %v", i, err)
		}
	}

	resp, err := srv.ListIntelCards(ctx, &intelligv1.ListIntelCardsRequest{Meta: meta("list-1")})
	if err != nil {
		t.Fatalf("ListIntelCards: %v", err)
	}
	if resp.TotalCount != 3 {
		t.Errorf("total_count = %d, want 3", resp.TotalCount)
	}
}

func TestListIntelCards_StatusFilter(t *testing.T) {
	store := newTestStore(t)
	srv := server.NewWithIntelHunt(slog.Default(), store, okEngine())
	ctx := context.Background()

	// Ingest 2 signals
	resp1, err := srv.IngestSignal(ctx, &intelligv1.IngestSignalRequest{Meta: meta("i-1"), RawContent: "signal 1"})
	if err != nil {
		t.Fatalf("IngestSignal 1: %v", err)
	}
	_, err = srv.IngestSignal(ctx, &intelligv1.IngestSignalRequest{Meta: meta("i-2"), RawContent: "signal 2"})
	if err != nil {
		t.Fatalf("IngestSignal 2: %v", err)
	}

	// Confirm first card
	_, err = srv.ConfirmAction(ctx, &intelligv1.ConfirmActionRequest{
		Meta:      meta("c-1"),
		CardId:    resp1.Card.Id,
		NewStatus: intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED,
	})
	if err != nil {
		t.Fatalf("ConfirmAction: %v", err)
	}

	// Filter by CONFIRMED
	resp, err := srv.ListIntelCards(ctx, &intelligv1.ListIntelCardsRequest{
		Meta:         meta("l-1"),
		StatusFilter: intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED,
	})
	if err != nil {
		t.Fatalf("ListIntelCards: %v", err)
	}
	if resp.TotalCount != 1 {
		t.Errorf("total_count = %d, want 1", resp.TotalCount)
	}
}

// ── ConfirmAction ─────────────────────────────────────────────────────────────

func TestConfirmAction_MissingCardID(t *testing.T) {
	srv := server.NewWithIntelHunt(slog.Default(), newTestStore(t), okEngine())
	_, err := srv.ConfirmAction(context.Background(), &intelligv1.ConfirmActionRequest{
		Meta:      meta("req-1"),
		NewStatus: intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED,
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestConfirmAction_InvalidStatus(t *testing.T) {
	srv := server.NewWithIntelHunt(slog.Default(), newTestStore(t), okEngine())
	_, err := srv.ConfirmAction(context.Background(), &intelligv1.ConfirmActionRequest{
		Meta:      meta("req-1"),
		CardId:    "some-card",
		NewStatus: intelligv1.IntelCardStatus_INTEL_CARD_STATUS_PENDING_REVIEW,
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestConfirmAction_NotFound(t *testing.T) {
	srv := server.NewWithIntelHunt(slog.Default(), newTestStore(t), okEngine())
	_, err := srv.ConfirmAction(context.Background(), &intelligv1.ConfirmActionRequest{
		Meta:      meta("req-1"),
		CardId:    "no-such-card",
		NewStatus: intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED,
	})
	assertCode(t, err, codes.NotFound)
}

func TestConfirmAction_Confirm(t *testing.T) {
	store := newTestStore(t)
	srv := server.NewWithIntelHunt(slog.Default(), store, okEngine())
	ctx := context.Background()

	ingestResp, err := srv.IngestSignal(ctx, &intelligv1.IngestSignalRequest{
		Meta: meta("i-1"), RawContent: "signal content",
	})
	if err != nil {
		t.Fatalf("IngestSignal: %v", err)
	}

	confirmResp, err := srv.ConfirmAction(ctx, &intelligv1.ConfirmActionRequest{
		Meta:      meta("c-1"),
		CardId:    ingestResp.Card.Id,
		NewStatus: intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED,
	})
	if err != nil {
		t.Fatalf("ConfirmAction: %v", err)
	}
	if confirmResp.Card.Status != intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED {
		t.Errorf("status = %v, want CONFIRMED", confirmResp.Card.Status)
	}
}

func TestConfirmAction_Dismiss(t *testing.T) {
	store := newTestStore(t)
	srv := server.NewWithIntelHunt(slog.Default(), store, okEngine())
	ctx := context.Background()

	ingestResp, err := srv.IngestSignal(ctx, &intelligv1.IngestSignalRequest{
		Meta: meta("i-1"), RawContent: "signal content",
	})
	if err != nil {
		t.Fatalf("IngestSignal: %v", err)
	}

	confirmResp, err := srv.ConfirmAction(ctx, &intelligv1.ConfirmActionRequest{
		Meta:      meta("c-1"),
		CardId:    ingestResp.Card.Id,
		NewStatus: intelligv1.IntelCardStatus_INTEL_CARD_STATUS_DISMISSED,
	})
	if err != nil {
		t.Fatalf("ConfirmAction: %v", err)
	}
	if confirmResp.Card.Status != intelligv1.IntelCardStatus_INTEL_CARD_STATUS_DISMISSED {
		t.Errorf("status = %v, want DISMISSED", confirmResp.Card.Status)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Errorf("expected gRPC error %v, got nil", want)
		return
	}
	if got := status.Code(err); got != want {
		t.Errorf("gRPC code = %v, want %v (err: %v)", got, want, err)
	}
}
