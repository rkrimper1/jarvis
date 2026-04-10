package reports_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/business-ops/reports"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func mustParsePayload(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("data is not valid JSON: %v", err)
	}
	return m
}

// ── Generate – basic properties ───────────────────────────────────────────────

func TestGenerate_ReturnsNonEmptyID(t *testing.T) {
	r := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), nil)
	if r.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestGenerate_IDHasRptPrefix(t *testing.T) {
	r := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), nil)
	if !strings.HasPrefix(r.ID, "rpt-") {
		t.Errorf("expected ID to start with 'rpt-', got %q", r.ID)
	}
}

func TestGenerate_FormatIsJSON(t *testing.T) {
	r := reports.Generate("OPERATIONS", time.Now().Add(-24*time.Hour), time.Now(), nil)
	if r.Format != "JSON" {
		t.Errorf("Format: got %q, want %q", r.Format, "JSON")
	}
}

func TestGenerate_DataIsValidJSON(t *testing.T) {
	r := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), nil)
	if len(r.Data) == 0 {
		t.Fatal("expected non-empty Data")
	}
	mustParsePayload(t, r.Data)
}

func TestGenerate_SummaryIsNonEmpty(t *testing.T) {
	r := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), nil)
	if r.Summary == "" {
		t.Error("expected non-empty Summary")
	}
}

// ── Generate – unique IDs ─────────────────────────────────────────────────────

func TestGenerate_UniqueIDs(t *testing.T) {
	r1 := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), nil)
	r2 := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), nil)
	if r1.ID == r2.ID {
		t.Errorf("expected unique IDs, both got %q", r1.ID)
	}
}

// ── Generate – known report types ─────────────────────────────────────────────

func TestGenerate_FinancialReport_SummaryContainsRevenue(t *testing.T) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	r := reports.Generate("FINANCIAL", from, to, nil)
	if !strings.Contains(r.Summary, "financial") {
		t.Errorf("FINANCIAL summary missing 'financial': %q", r.Summary)
	}
	// Period should appear in summary.
	if !strings.Contains(r.Summary, "2025-01-01") {
		t.Errorf("FINANCIAL summary missing period start: %q", r.Summary)
	}
}

func TestGenerate_OperationsReport_SummaryContainsUptime(t *testing.T) {
	r := reports.Generate("OPERATIONS", time.Now().Add(-48*time.Hour), time.Now(), nil)
	if !strings.Contains(r.Summary, "Operations") {
		t.Errorf("OPERATIONS summary missing 'Operations': %q", r.Summary)
	}
}

func TestGenerate_ThreatSummary_SummaryContainsThreat(t *testing.T) {
	r := reports.Generate("THREAT_SUMMARY", time.Now().Add(-24*time.Hour), time.Now(), nil)
	if !strings.Contains(r.Summary, "Threat") {
		t.Errorf("THREAT_SUMMARY summary missing 'Threat': %q", r.Summary)
	}
}

func TestGenerate_UnknownType_SummaryContainsType(t *testing.T) {
	r := reports.Generate("CUSTOM_REPORT", time.Now().Add(-24*time.Hour), time.Now(), nil)
	if !strings.Contains(r.Summary, "CUSTOM_REPORT") {
		t.Errorf("custom report summary should contain type: %q", r.Summary)
	}
}

// ── Generate – zero time period ───────────────────────────────────────────────

func TestGenerate_ZeroTimePeriod_SummaryContainsNA(t *testing.T) {
	var zero time.Time
	r := reports.Generate("FINANCIAL", zero, zero, nil)
	if r.Summary == "" {
		t.Error("expected non-empty summary for zero-time period")
	}
	// When both times are zero, period should show "N/A".
	if !strings.Contains(r.Summary, "N/A") {
		t.Errorf("expected 'N/A' in summary for zero-time period, got %q", r.Summary)
	}
}

func TestGenerate_ZeroFromOnly_SummaryContainsNA(t *testing.T) {
	var zero time.Time
	r := reports.Generate("OPERATIONS", zero, time.Now(), nil)
	// One of the times is zero, condition is !from.IsZero() && !to.IsZero() -> period = "N/A"
	if !strings.Contains(r.Summary, "N/A") {
		t.Errorf("expected 'N/A' when from is zero, got %q", r.Summary)
	}
}

// ── Generate – payload content ────────────────────────────────────────────────

func TestGenerate_PayloadContainsReportType(t *testing.T) {
	r := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), nil)
	m := mustParsePayload(t, r.Data)
	if m["report_type"] != "FINANCIAL" {
		t.Errorf("report_type: got %v, want %q", m["report_type"], "FINANCIAL")
	}
}

func TestGenerate_PayloadContainsPeriod(t *testing.T) {
	r := reports.Generate("OPERATIONS", time.Now().Add(-24*time.Hour), time.Now(), nil)
	m := mustParsePayload(t, r.Data)
	period, ok := m["period"]
	if !ok {
		t.Error("expected 'period' key in payload")
	}
	if period == nil {
		t.Error("expected non-nil 'period' in payload")
	}
}

func TestGenerate_PayloadContainsSections(t *testing.T) {
	r := reports.Generate("OPERATIONS", time.Now().Add(-24*time.Hour), time.Now(), nil)
	m := mustParsePayload(t, r.Data)
	sections, ok := m["sections"]
	if !ok {
		t.Error("expected 'sections' key in payload")
	}
	sl, ok := sections.([]any)
	if !ok {
		t.Fatalf("sections should be an array, got %T", sections)
	}
	if len(sl) == 0 {
		t.Error("expected at least one section in payload")
	}
}

func TestGenerate_PayloadContainsMetrics(t *testing.T) {
	r := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), nil)
	m := mustParsePayload(t, r.Data)
	if _, ok := m["metrics"]; !ok {
		t.Error("expected 'metrics' key in payload")
	}
}

func TestGenerate_PayloadContainsFilters(t *testing.T) {
	filters := []string{"department:RD", "quarter:Q1"}
	r := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), filters)
	m := mustParsePayload(t, r.Data)
	filtersVal, ok := m["filters"]
	if !ok {
		t.Error("expected 'filters' key in payload")
	}
	fl, ok := filtersVal.([]any)
	if !ok {
		t.Fatalf("filters should be an array, got %T", filtersVal)
	}
	if len(fl) != len(filters) {
		t.Errorf("filters length: got %d, want %d", len(fl), len(filters))
	}
}

func TestGenerate_NilFilters_PayloadFiltersEmpty(t *testing.T) {
	r := reports.Generate("FINANCIAL", time.Now().Add(-24*time.Hour), time.Now(), nil)
	m := mustParsePayload(t, r.Data)
	filtersVal := m["filters"]
	// Nil slice marshals to null in JSON.
	if filtersVal != nil {
		// Some implementations marshal nil slice as empty array — either is fine.
		fl, ok := filtersVal.([]any)
		if ok && len(fl) != 0 {
			t.Errorf("expected empty filters for nil input, got %v", fl)
		}
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestGenerate_ConcurrentSafe(t *testing.T) {
	var wg sync.WaitGroup
	const goroutines = 20
	ids := make([]string, goroutines)
	var mu sync.Mutex

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			r := reports.Generate("OPERATIONS", time.Now().Add(-time.Hour), time.Now(), nil)
			mu.Lock()
			ids[n] = r.ID
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool)
	for _, id := range ids {
		if id == "" {
			t.Error("got empty ID in concurrent test")
		}
		if seen[id] {
			t.Errorf("duplicate ID in concurrent test: %q", id)
		}
		seen[id] = true
	}
}

// ── All known report types ────────────────────────────────────────────────────

func TestGenerate_AllKnownTypes_SummaryNonEmpty(t *testing.T) {
	types := []string{"FINANCIAL", "OPERATIONS", "THREAT_SUMMARY", "UNKNOWN_TYPE"}
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)
	for _, rt := range types {
		r := reports.Generate(rt, from, to, nil)
		if r.Summary == "" {
			t.Errorf("report type %q: expected non-empty Summary", rt)
		}
		if len(r.Data) == 0 {
			t.Errorf("report type %q: expected non-empty Data", rt)
		}
	}
}
