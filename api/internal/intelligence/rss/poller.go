// Package rss provides the RSS/Atom poller that fetches feeds on a fixed
// interval and ingests new items as intel signals via the fusion engine.
package rss

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/intelligence/fusion"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
)

// store is the subset of *intelstore.Store that the Poller requires.
type store interface {
	SignalExistsBySourceURI(ctx context.Context, uri string) (bool, error)
	SaveSignal(ctx context.Context, sourceType intelligv1.SourceType, rawContent, sourceURI string) (*intelligv1.RawSignal, error)
	SaveCard(ctx context.Context, title, summary string, oppType intelligv1.OpportunityType, confidence float32, suggestedAction string, signalIDs []string) (*intelligv1.IntelCard, error)
}

// Config holds tunable parameters for the RSS poller.
type Config struct {
	FeedURLs []string      // RSS_FEED_URLS — one or more feed URLs to poll
	Interval time.Duration // RSS_POLL_INTERVAL — default: 15m
}

// Poller fetches RSS/Atom feeds on a fixed interval and ingests new items
// as intel signals via the fusion engine.
type Poller struct {
	cfg    Config
	store  store
	engine fusion.Engine
	client *http.Client
	log    *slog.Logger
}

// New creates a Poller. Returns nil if cfg.FeedURLs is empty — the caller
// can safely skip a nil Poller without branching.
func New(cfg Config, s store, eng fusion.Engine, log *slog.Logger) *Poller {
	if len(cfg.FeedURLs) == 0 {
		return nil
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 15 * time.Minute
	}
	return &Poller{
		cfg:    cfg,
		store:  s,
		engine: eng,
		client: &http.Client{Timeout: 30 * time.Second},
		log:    log,
	}
}

// Start launches the polling loop in a new goroutine. It returns immediately.
// The loop runs until ctx is cancelled.
func (p *Poller) Start(ctx context.Context) {
	go p.run(ctx)
}

func (p *Poller) run(ctx context.Context) {
	p.log.Info("rss poller starting",
		slog.Int("feeds", len(p.cfg.FeedURLs)),
		slog.Duration("interval", p.cfg.Interval),
	)
	// Poll immediately on startup, then on the ticker cadence.
	p.pollAll(ctx)
	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			p.log.Info("rss poller stopped")
			return
		case <-ticker.C:
			p.pollAll(ctx)
		}
	}
}

func (p *Poller) pollAll(ctx context.Context) {
	for _, url := range p.cfg.FeedURLs {
		if ctx.Err() != nil {
			return
		}
		if err := p.pollFeed(ctx, url); err != nil {
			p.log.Error("rss: poll feed failed", slog.String("url", url), slog.Any("err", err))
		}
	}
}

func (p *Poller) pollFeed(ctx context.Context, feedURL string) error {
	items, err := p.fetchAndParse(ctx, feedURL)
	if err != nil {
		return err
	}
	newCount, skipped := 0, 0
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		exists, err := p.store.SignalExistsBySourceURI(ctx, item.dedupeKey)
		if err != nil {
			p.log.Warn("rss: dedup check failed", slog.String("key", item.dedupeKey), slog.Any("err", err))
			continue
		}
		if exists {
			skipped++
			continue
		}
		if err := p.ingestItem(ctx, item); err != nil {
			p.log.Error("rss: ingest failed", slog.String("title", item.title), slog.Any("err", err))
			continue
		}
		newCount++
	}
	p.log.Info("rss: feed polled",
		slog.String("url", feedURL),
		slog.Int("new", newCount),
		slog.Int("skipped", skipped),
	)
	return nil
}

func (p *Poller) ingestItem(ctx context.Context, item feedItem) error {
	rawContent := buildRawContent(item)
	signal, err := p.store.SaveSignal(ctx, intelligv1.SourceType_SOURCE_TYPE_RSS, rawContent, item.dedupeKey)
	if err != nil {
		return fmt.Errorf("save signal: %w", err)
	}
	result, err := p.engine.Fuse(ctx, rawContent)
	if err != nil {
		return fmt.Errorf("fusion: %w", err)
	}
	if _, err := p.store.SaveCard(ctx,
		result.Title,
		result.Summary,
		result.OpportunityType,
		result.ConfidenceScore,
		result.SuggestedAction,
		[]string{signal.Id},
	); err != nil {
		return fmt.Errorf("save card: %w", err)
	}
	p.log.Info("rss: item ingested",
		slog.String("title", item.title),
		slog.Float64("confidence", float64(result.ConfidenceScore)),
	)
	return nil
}

func buildRawContent(item feedItem) string {
	var sb strings.Builder
	sb.WriteString(item.title)
	if item.description != "" {
		sb.WriteString("\n\n")
		sb.WriteString(item.description)
	}
	if item.link != "" {
		sb.WriteString("\n\nSource: ")
		sb.WriteString(item.link)
	}
	return sb.String()
}

// ── feed fetching ─────────────────────────────────────────────────────────────

func (p *Poller) fetchAndParse(ctx context.Context, feedURL string) ([]feedItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "JARVIS-IntelPoller/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return parseFeed(body)
}

// ── feed parsing ──────────────────────────────────────────────────────────────

type feedItem struct {
	title       string
	description string
	link        string
	dedupeKey   string // guid/id or link — stored as source_uri for dedup
}

// parseFeed probes the XML root element and dispatches to the RSS or Atom parser.
func parseFeed(data []byte) ([]feedItem, error) {
	type probe struct {
		XMLName xml.Name
	}
	var p probe
	if err := xml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}
	if p.XMLName.Local == "feed" {
		return parseAtom(data)
	}
	return parseRSS(data)
}

// ── RSS 2.0 ───────────────────────────────────────────────────────────────────

type rssRoot struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
}

func parseRSS(data []byte) ([]feedItem, error) {
	var root rssRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}
	items := make([]feedItem, 0, len(root.Channel.Items))
	for _, r := range root.Channel.Items {
		key := strings.TrimSpace(r.GUID)
		if key == "" {
			key = strings.TrimSpace(r.Link)
		}
		if key == "" {
			continue // no dedup key — cannot safely ingest
		}
		items = append(items, feedItem{
			title:       strings.TrimSpace(r.Title),
			description: strings.TrimSpace(r.Description),
			link:        strings.TrimSpace(r.Link),
			dedupeKey:   key,
		})
	}
	return items, nil
}

// ── Atom ──────────────────────────────────────────────────────────────────────

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	Summary string     `xml:"summary"`
	ID      string     `xml:"id"`
	Links   []atomLink `xml:"link"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

func parseAtom(data []byte) ([]feedItem, error) {
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("parse atom: %w", err)
	}
	items := make([]feedItem, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		key := strings.TrimSpace(e.ID)
		if key == "" {
			continue
		}
		items = append(items, feedItem{
			title:       strings.TrimSpace(e.Title),
			description: strings.TrimSpace(e.Summary),
			link:        atomAlternateLink(e.Links),
			dedupeKey:   key,
		})
	}
	return items, nil
}

func atomAlternateLink(links []atomLink) string {
	for _, l := range links {
		if l.Rel == "alternate" || l.Rel == "" {
			return l.Href
		}
	}
	return ""
}
