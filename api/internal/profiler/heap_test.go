package profiler_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/profiler"
)

func newTestProfiler(t *testing.T) (*profiler.HeapProfiler, string) {
	t.Helper()
	dir := t.TempDir()
	p := &profiler.HeapProfiler{
		OutDir:   dir,
		Interval: time.Hour, // long interval — we call Capture() directly
		Log:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	return p, dir
}

// ── Capture ───────────────────────────────────────────────────────────────────

func TestCapture_WritesProfFile(t *testing.T) {
	p, dir := newTestProfiler(t)

	profPath, _, err := p.Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if profPath == "" {
		t.Fatal("expected non-empty profPath")
	}
	if _, err := os.Stat(profPath); err != nil {
		t.Errorf("prof file not found: %v", err)
	}
	if !strings.HasSuffix(profPath, ".prof") {
		t.Errorf("profPath = %q, want *.prof suffix", profPath)
	}
	if filepath.Dir(profPath) != dir {
		t.Errorf("profPath dir = %q, want %q", filepath.Dir(profPath), dir)
	}
}

func TestCapture_ProfFileIsNonEmpty(t *testing.T) {
	p, _ := newTestProfiler(t)
	profPath, _, err := p.Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	info, err := os.Stat(profPath)
	if err != nil {
		t.Fatalf("stat prof file: %v", err)
	}
	if info.Size() == 0 {
		t.Error("expected non-empty .prof file")
	}
}

func TestCapture_ProfPathContainsTimestamp(t *testing.T) {
	p, _ := newTestProfiler(t)
	profPath, _, err := p.Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	base := filepath.Base(profPath)
	if !strings.HasPrefix(base, "heap-") {
		t.Errorf("prof filename = %q, want heap-<timestamp>.prof", base)
	}
}

func TestCapture_CreatesOutDirIfAbsent(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "sub", "profiles")
	p := &profiler.HeapProfiler{
		OutDir:   dir,
		Interval: time.Hour,
		Log:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	if _, _, err := p.Capture(); err != nil {
		t.Fatalf("Capture with missing dir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected OutDir to be created: %v", err)
	}
}

func TestCapture_MultipleCapturesProduceDistinctFiles(t *testing.T) {
	p, dir := newTestProfiler(t)

	// Sleep 1 second so the timestamp differs
	p.Capture() //nolint
	time.Sleep(1100 * time.Millisecond)
	p.Capture() //nolint

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	profs := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".prof") {
			profs++
		}
	}
	if profs < 2 {
		t.Errorf("expected >= 2 .prof files, got %d", profs)
	}
}

// ── Start (goroutine lifecycle) ───────────────────────────────────────────────

func TestStart_CapturesImmediately(t *testing.T) {
	p, dir := newTestProfiler(t)
	// Use a very short interval to ensure the goroutine ticks at least once
	p.Interval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)

	// Allow time for the immediate capture to happen
	time.Sleep(200 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond) // let goroutine exit

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var profs []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".prof") {
			profs = append(profs, e.Name())
		}
	}
	if len(profs) == 0 {
		t.Error("expected at least one .prof file after Start")
	}
}

func TestStart_StopsOnContextCancel(t *testing.T) {
	p, dir := newTestProfiler(t)
	p.Interval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	time.Sleep(80 * time.Millisecond)

	// Count files before and after waiting a full interval
	before, _ := countProfs(t, dir)
	time.Sleep(150 * time.Millisecond)
	after, _ := countProfs(t, dir)

	if after > before {
		t.Error("expected no new captures after context cancel")
	}
}

func countProfs(t *testing.T, dir string) (int, []string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".prof") {
			names = append(names, e.Name())
		}
	}
	return len(names), names
}
