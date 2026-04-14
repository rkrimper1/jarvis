package rss

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/intelligence/fusion"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
)

// ── mock store ────────────────────────────────────────────────────────────────

type mockStore struct {
	seen    map[string]bool
	signals []*intelligv1.RawSignal
	cards   []*intelligv1.IntelCard
	saveErr error
}

func newMockStore() *mockStore {
	return &mockStore{seen: make(map[string]bool)}
}

func (m *mockStore) SignalExistsBySourceURI(_ context.Context, uri string) (bool, error) {
	return m.seen[uri], nil
}

func (m *mockStore) SaveSignal(_ context.Context, sourceType intelligv1.SourceType, rawContent, sourceURI string) (*intelligv1.RawSignal, error) {
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	sig := &intelligv1.RawSignal{
		Id:         "sig-" + sourceURI,
		RawContent: rawContent,
		SourceType: sourceType,
		SourceUri:  sourceURI,
	}
	m.signals = append(m.signals, sig)
	m.seen[sourceURI] = true
	return sig, nil
}

func (m *mockStore) SaveCard(_ context.Context, title, summary string, oppType intelligv1.OpportunityType, confidence float32, suggestedAction string, signalIDs []string) (*intelligv1.IntelCard, error) {
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	card := &intelligv1.IntelCard{
		Id:              "card-" + title,
		Title:           title,
		Summary:         summary,
		OpportunityType: oppType,
		ConfidenceScore: confidence,
		SuggestedAction: suggestedAction,
		RawSignalIds:    signalIDs,
		Status:          intelligv1.IntelCardStatus_INTEL_CARD_STATUS_PENDING_REVIEW,
	}
	m.cards = append(m.cards, card)
	return card, nil
}

// ── mock engine ───────────────────────────────────────────────────────────────

type mockEngine struct {
	result *fusion.Result
	err    error
}

func (m *mockEngine) Fuse(_ context.Context, _ string) (*fusion.Result, error) {
	return m.result, m.err
}

func okEngine() *mockEngine {
	return &mockEngine{result: &fusion.Result{
		Title:           "Test Card",
		Summary:         "A test intelligence card.",
		OpportunityType: intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL,
		ConfidenceScore: 0.85,
		SuggestedAction: "Review immediately.",
	}}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ── RSS/Atom feed fixtures ────────────────────────────────────────────────────

const rssFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Acme cuts prices</title>
      <description>Acme Corp reduced enterprise pricing by 15% effective immediately.</description>
      <link>https://example.com/acme-prices</link>
      <guid>https://example.com/acme-prices</guid>
    </item>
    <item>
      <title>New market entrant</title>
      <description>Stark Industries enters the cloud storage market.</description>
      <link>https://example.com/stark-cloud</link>
      <guid>guid-stark-cloud-001</guid>
    </item>
  </channel>
</rss>`

const atomFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Atom Test Feed</title>
  <entry>
    <title>Supply chain disruption</title>
    <summary>Key supplier announces 30-day production pause.</summary>
    <id>urn:uuid:supply-chain-001</id>
    <link href="https://example.com/supply-chain" rel="alternate"/>
  </entry>
  <entry>
    <title>Regulatory change</title>
    <summary>New data residency rules take effect Q3.</summary>
    <id>urn:uuid:regulatory-001</id>
    <link href="https://example.com/regulatory"/>
  </entry>
</feed>`

const rssNoGUID = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item>
      <title>Item with link only</title>
      <description>Uses link as dedup key.</description>
      <link>https://example.com/link-only</link>
    </item>
    <item>
      <title>Item with no key</title>
      <description>No guid, no link — should be skipped.</description>
    </item>
  </channel>
</rss>`

// ── parse tests ───────────────────────────────────────────────────────────────

func TestParseRSS(t *testing.T) {
	items, err := parseFeed([]byte(rssFeed))
	if err != nil {
		t.Fatalf("parseFeed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	if items[0].title != "Acme cuts prices" {
		t.Errorf("title = %q", items[0].title)
	}
	if items[0].dedupeKey != "https://example.com/acme-prices" {
		t.Errorf("dedupeKey = %q", items[0].dedupeKey)
	}
	// Second item uses explicit GUID (not link) as dedup key
	if items[1].dedupeKey != "guid-stark-cloud-001" {
		t.Errorf("dedupeKey = %q, want guid", items[1].dedupeKey)
	}
	if items[1].link != "https://example.com/stark-cloud" {
		t.Errorf("link = %q", items[1].link)
	}
}

func TestParseAtom(t *testing.T) {
	items, err := parseFeed([]byte(atomFeedXML))
	if err != nil {
		t.Fatalf("parseFeed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	if items[0].dedupeKey != "urn:uuid:supply-chain-001" {
		t.Errorf("dedupeKey = %q", items[0].dedupeKey)
	}
	if items[0].link != "https://example.com/supply-chain" {
		t.Errorf("link = %q", items[0].link)
	}
	if items[0].description != "Key supplier announces 30-day production pause." {
		t.Errorf("description = %q", items[0].description)
	}
	// Entry with no rel attribute — link should still be captured
	if items[1].link != "https://example.com/regulatory" {
		t.Errorf("link = %q", items[1].link)
	}
}

func TestParseRSS_FallbackToLink(t *testing.T) {
	items, err := parseFeed([]byte(rssNoGUID))
	if err != nil {
		t.Fatalf("parseFeed: %v", err)
	}
	// Item with no guid/link should be dropped; item with link should be kept.
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1 (item with no key should be dropped)", len(items))
	}
	if items[0].dedupeKey != "https://example.com/link-only" {
		t.Errorf("dedupeKey = %q, want link", items[0].dedupeKey)
	}
}

func TestParseFeed_InvalidXML(t *testing.T) {
	_, err := parseFeed([]byte("not xml at all"))
	if err == nil {
		t.Error("expected error for invalid XML")
	}
}

// ── poller integration tests ──────────────────────────────────────────────────

func serveRSS(t *testing.T, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestPollFeed_NewItems(t *testing.T) {
	ts := serveRSS(t, rssFeed)
	s := newMockStore()
	p := New(Config{FeedURLs: []string{ts.URL}, Interval: time.Minute}, s, okEngine(), discardLogger())

	if err := p.pollFeed(context.Background(), ts.URL); err != nil {
		t.Fatalf("pollFeed: %v", err)
	}
	if len(s.signals) != 2 {
		t.Errorf("signals = %d, want 2", len(s.signals))
	}
	if len(s.cards) != 2 {
		t.Errorf("cards = %d, want 2", len(s.cards))
	}
}

func TestPollFeed_DeduplicatesSeenItems(t *testing.T) {
	ts := serveRSS(t, rssFeed)
	s := newMockStore()
	s.seen["https://example.com/acme-prices"] = true // pre-seen

	p := New(Config{FeedURLs: []string{ts.URL}, Interval: time.Minute}, s, okEngine(), discardLogger())
	if err := p.pollFeed(context.Background(), ts.URL); err != nil {
		t.Fatalf("pollFeed: %v", err)
	}
	if len(s.signals) != 1 {
		t.Errorf("signals = %d, want 1 (one should be skipped)", len(s.signals))
	}
}

func TestPollFeed_AllItemsSeen(t *testing.T) {
	ts := serveRSS(t, rssFeed)
	s := newMockStore()
	s.seen["https://example.com/acme-prices"] = true
	s.seen["guid-stark-cloud-001"] = true

	p := New(Config{FeedURLs: []string{ts.URL}, Interval: time.Minute}, s, okEngine(), discardLogger())
	if err := p.pollFeed(context.Background(), ts.URL); err != nil {
		t.Fatalf("pollFeed: %v", err)
	}
	if len(s.signals) != 0 {
		t.Errorf("signals = %d, want 0", len(s.signals))
	}
}

func TestPollFeed_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)

	p := New(Config{FeedURLs: []string{ts.URL}, Interval: time.Minute}, newMockStore(), okEngine(), discardLogger())
	if err := p.pollFeed(context.Background(), ts.URL); err == nil {
		t.Error("expected error for HTTP 500")
	}
}

func TestPollFeed_FusionError_CardNotSaved(t *testing.T) {
	ts := serveRSS(t, rssFeed)
	s := newMockStore()
	eng := &mockEngine{err: errors.New("claude unavailable")}

	p := New(Config{FeedURLs: []string{ts.URL}, Interval: time.Minute}, s, eng, discardLogger())
	// pollFeed logs errors per-item but never returns them
	if err := p.pollFeed(context.Background(), ts.URL); err != nil {
		t.Fatalf("pollFeed should not surface per-item errors: %v", err)
	}
	// No cards when fusion fails
	if len(s.cards) != 0 {
		t.Errorf("cards = %d, want 0 (fusion failed)", len(s.cards))
	}
}

func TestNew_EmptyFeedsReturnsNil(t *testing.T) {
	p := New(Config{}, newMockStore(), okEngine(), discardLogger())
	if p != nil {
		t.Error("expected nil poller for empty feed list")
	}
}

func TestNew_DefaultInterval(t *testing.T) {
	p := New(Config{FeedURLs: []string{"http://example.com/feed"}}, newMockStore(), okEngine(), discardLogger())
	if p == nil {
		t.Fatal("expected non-nil poller")
	}
	if p.cfg.Interval != 15*time.Minute {
		t.Errorf("interval = %v, want 15m", p.cfg.Interval)
	}
}

func TestStart_ContextCancellation(t *testing.T) {
	ts := serveRSS(t, rssFeed)
	p := New(Config{FeedURLs: []string{ts.URL}, Interval: time.Hour}, newMockStore(), okEngine(), discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	cancel()
	// Give the goroutine a moment to observe cancellation — no hang, no panic.
	time.Sleep(50 * time.Millisecond)
}

func TestBuildRawContent(t *testing.T) {
	item := feedItem{
		title:       "Acme cuts prices",
		description: "15% reduction in enterprise tier.",
		link:        "https://example.com/acme",
		dedupeKey:   "https://example.com/acme",
	}
	content := buildRawContent(item)
	if !strings.Contains(content, "Acme cuts prices") {
		t.Error("content should contain title")
	}
	if !strings.Contains(content, "15% reduction") {
		t.Error("content should contain description")
	}
	if !strings.Contains(content, "https://example.com/acme") {
		t.Error("content should contain source link")
	}
}

func TestBuildRawContent_NoDescription(t *testing.T) {
	item := feedItem{title: "Headline only", link: "https://example.com/h"}
	content := buildRawContent(item)
	if !strings.Contains(content, "Headline only") {
		t.Error("content should contain title")
	}
}
