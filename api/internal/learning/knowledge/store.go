// Package knowledge provides the SQLite-backed knowledge store for the
// learning service. Entries are searchable via FTS5 full-text search with
// a fuzzy fallback, and stale entries (older than StaleDays) are excluded
// from results. When nothing fresh is found, the caller may trigger an
// external search (web search or Claude API) which saves and returns the result.
package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	learningv1 "github.com/rkrimper1/jarvis/api/pb/learning"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const schema = `
CREATE TABLE IF NOT EXISTS knowledge (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    query        TEXT NOT NULL,
    summary      TEXT NOT NULL,
    source       TEXT NOT NULL CHECK(source IN ('web_search','claude_api','manual')),
    confidence   REAL DEFAULT 1.0,
    tags         TEXT DEFAULT '',
    created_at   DATETIME DEFAULT (datetime('now')),
    updated_at   DATETIME DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_knowledge_query ON knowledge(query);

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts
    USING fts5(query, summary, tags, content=knowledge, content_rowid=id);

-- Keep FTS index in sync with the knowledge table
CREATE TRIGGER IF NOT EXISTS knowledge_ai AFTER INSERT ON knowledge BEGIN
    INSERT INTO knowledge_fts(rowid, query, summary, tags)
    VALUES (new.id, new.query, new.summary, new.tags);
END;

CREATE TRIGGER IF NOT EXISTS knowledge_au AFTER UPDATE ON knowledge BEGIN
    INSERT INTO knowledge_fts(knowledge_fts, rowid, query, summary, tags)
    VALUES ('delete', old.id, old.query, old.summary, old.tags);
    INSERT INTO knowledge_fts(rowid, query, summary, tags)
    VALUES (new.id, new.query, new.summary, new.tags);
END;

CREATE TRIGGER IF NOT EXISTS knowledge_ad AFTER DELETE ON knowledge BEGIN
    INSERT INTO knowledge_fts(knowledge_fts, rowid, query, summary, tags)
    VALUES ('delete', old.id, old.query, old.summary, old.tags);
END;
`

// Store is the SQLite-backed knowledge store.
type Store struct {
	db        *sql.DB
	staleDays int
	apiKey    string // Anthropic API key for Claude search
	model     string
}

// New opens (or creates) the SQLite database at dbPath and applies the schema.
func New(dbPath string, staleDays int, apiKey, model string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("knowledge: open db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("knowledge: apply schema: %w", err)
	}
	return &Store{db: db, staleDays: staleDays, apiKey: apiKey, model: model}, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Search looks up fresh knowledge entries matching query.
// Returns (results, found) where found is false when the DB has no fresh match.
func (s *Store) Search(ctx context.Context, query string) ([]*learningv1.KnowledgeEntry, bool, error) {
	staleThreshold := time.Now().AddDate(0, 0, -s.staleDays).Format("2006-01-02 15:04:05")

	// ── 1. FTS5 full-text match ───────────────────────────────────────
	rows, err := s.db.QueryContext(ctx, `
		SELECT k.id, k.query, k.summary, k.source, k.confidence, k.tags,
		       k.created_at, k.updated_at
		FROM knowledge k
		JOIN knowledge_fts f ON k.id = f.rowid
		WHERE knowledge_fts MATCH ?
		  AND k.updated_at >= ?
		ORDER BY rank
		LIMIT 10
	`, sanitizeFTS(query), staleThreshold)
	if err != nil {
		return nil, false, fmt.Errorf("knowledge: fts search: %w", err)
	}
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, false, err
	}
	if len(entries) > 0 {
		return entries, true, nil
	}

	// ── 2. Fuzzy fallback: LIKE on query + summary ────────────────────
	like := "%" + query + "%"
	rows, err = s.db.QueryContext(ctx, `
		SELECT id, query, summary, source, confidence, tags, created_at, updated_at
		FROM knowledge
		WHERE (query LIKE ? OR summary LIKE ?)
		  AND updated_at >= ?
		ORDER BY updated_at DESC
		LIMIT 10
	`, like, like, staleThreshold)
	if err != nil {
		return nil, false, fmt.Errorf("knowledge: fuzzy search: %w", err)
	}
	entries, err = scanEntries(rows)
	if err != nil {
		return nil, false, err
	}
	return entries, len(entries) > 0, nil
}

// SearchWithClaude calls the Claude API with web_search tool, saves the
// result, and returns the new entry.
func (s *Store) SearchWithClaude(ctx context.Context, query string, src learningv1.KnowledgeSource) (*learningv1.KnowledgeEntry, error) {
	if s.apiKey == "" {
		return nil, fmt.Errorf("knowledge: ANTHROPIC_API_KEY not configured")
	}

	client := anthropic.NewClient(option.WithAPIKey(s.apiKey))

	var summary string
	var sourceStr string

	switch src {
	case learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_WEB_SEARCH:
		sourceStr = "web_search"
		// Use web_search_20250305 tool
		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 1024,
			Tools: []anthropic.ToolUnionParam{
				anthropic.ToolUnionParam{
					OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{
						Name: "web_search",
					},
				},
			},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(
					fmt.Sprintf("Search the web and provide a concise factual summary for: %s", query),
				)),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("knowledge: claude web search: %w", err)
		}
		summary = extractText(resp)

	default: // KNOWLEDGE_SOURCE_CLAUDE_API
		sourceStr = "claude_api"
		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 1024,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(
					fmt.Sprintf("Provide a concise factual summary for: %s", query),
				)),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("knowledge: claude api search: %w", err)
		}
		summary = extractText(resp)
	}

	if summary == "" {
		return nil, fmt.Errorf("knowledge: empty response from Claude")
	}

	return s.Save(ctx, query, summary, sourceStr, 0.85, "")
}

// Save inserts or updates a knowledge entry and returns the stored record.
func (s *Store) Save(ctx context.Context, query, summary, source string, confidence float64, tags string) (*learningv1.KnowledgeEntry, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO knowledge (query, summary, source, confidence, tags)
		VALUES (?, ?, ?, ?, ?)
	`, query, summary, source, confidence, tags)
	if err != nil {
		return nil, fmt.Errorf("knowledge: save: %w", err)
	}
	id, _ := res.LastInsertId()

	row := s.db.QueryRowContext(ctx, `
		SELECT id, query, summary, source, confidence, tags, created_at, updated_at
		FROM knowledge WHERE id = ?
	`, id)
	entries, err := scanEntries(&rowsWrapper{row: row})
	if err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("knowledge: reload after save: %w", err)
	}
	return entries[0], nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func sanitizeFTS(q string) string {
	// Wrap in quotes to treat the whole query as a phrase in FTS5
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(q, `"`, `""`))
}

func extractText(msg *anthropic.Message) string {
	var sb strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

// scanner is implemented by both *sql.Rows and our rowsWrapper.
type scanner interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

// rowsWrapper adapts a single *sql.Row to the scanner interface.
type rowsWrapper struct {
	row  *sql.Row
	done bool
}

func (r *rowsWrapper) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}
func (r *rowsWrapper) Scan(dest ...any) error { return r.row.Scan(dest...) }
func (r *rowsWrapper) Close() error           { return nil }
func (r *rowsWrapper) Err() error             { return nil }

func scanEntries(rows scanner) ([]*learningv1.KnowledgeEntry, error) {
	defer rows.Close()
	var out []*learningv1.KnowledgeEntry
	for rows.Next() {
		var e learningv1.KnowledgeEntry
		var src string
		var createdAt, updatedAt string
		if err := rows.Scan(&e.Id, &e.Query, &e.Summary, &src,
			&e.Confidence, &e.Tags, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("knowledge: scan row: %w", err)
		}
		e.Source = sourceFromString(src)
		e.CreatedAt = parseTS(createdAt)
		e.UpdatedAt = parseTS(updatedAt)
		out = append(out, &e)
	}
	return out, rows.Err()
}

func sourceFromString(s string) learningv1.KnowledgeSource {
	switch s {
	case "web_search":
		return learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_WEB_SEARCH
	case "claude_api":
		return learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_CLAUDE_API
	case "manual":
		return learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_MANUAL
	default:
		return learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_UNSPECIFIED
	}
}

func parseTS(s string) *timestamppb.Timestamp {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return timestamppb.New(t)
		}
	}
	return timestamppb.Now()
}
