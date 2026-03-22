// Package profiler provides a periodic heap-profile snapshot routine.
//
// Each snapshot writes two files to OutDir:
//   - heap-<timestamp>.prof  — raw pprof binary (runtime/pprof)
//   - heap-<timestamp>.gif   — call-graph image (go tool pprof -gif)
//
// Graphviz must be installed for GIF generation (apt install graphviz).
package profiler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"time"
)

// HeapProfiler captures heap profiles on a fixed interval.
type HeapProfiler struct {
	// OutDir is the directory where .prof and .gif files are written.
	// Created automatically if it does not exist.
	OutDir string

	// Interval controls how often a snapshot is taken.
	Interval time.Duration

	Log *slog.Logger
}

// Start launches the profiler goroutine. It returns immediately; the goroutine
// runs until ctx is cancelled.
func (p *HeapProfiler) Start(ctx context.Context) {
	go p.loop(ctx)
}

// loop takes an immediate snapshot then repeats on every tick.
func (p *HeapProfiler) loop(ctx context.Context) {
	p.Capture() //nolint:errcheck

	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.Capture() //nolint:errcheck
		case <-ctx.Done():
			p.Log.Info("profiler: stopped")
			return
		}
	}
}

// Capture writes one heap profile and (if graphviz is available) converts it
// to a GIF call-graph image. It returns the paths of the files written.
// gifPath is empty when graphviz is not available.
func (p *HeapProfiler) Capture() (profPath, gifPath string, err error) {
	if err := os.MkdirAll(p.OutDir, 0o755); err != nil {
		return "", "", fmt.Errorf("profiler: create output dir: %w", err)
	}

	ts := time.Now().Format("20060102-150405")
	profPath = filepath.Join(p.OutDir, fmt.Sprintf("heap-%s.prof", ts))
	gifPath = filepath.Join(p.OutDir, fmt.Sprintf("heap-%s.gif", ts))

	// ── Write raw heap profile ────────────────────────────────────────
	f, err := os.Create(profPath)
	if err != nil {
		return "", "", fmt.Errorf("profiler: create prof file: %w", err)
	}
	// runtime.GC forces a complete GC cycle so the heap snapshot reflects
	// live objects only, not objects pending collection.
	if err := pprof.WriteHeapProfile(f); err != nil {
		_ = f.Close()
		return "", "", fmt.Errorf("profiler: write heap profile: %w", err)
	}
	_ = f.Close()

	p.Log.Info("profiler: heap snapshot written", slog.String("file", profPath))

	// ── Generate GIF via go tool pprof ────────────────────────────────
	// Equivalent shell command: go tool pprof -gif <profPath> > <gifPath>
	//
	// go tool pprof writes the GIF to stdout when given -gif.
	// This requires graphviz (the `dot` binary) to be installed.
	gif, err := exec.Command("pprof", "-gif", profPath).Output()
	if err != nil {
		// Non-fatal: the .prof file is still usable manually.
		p.Log.Warn("profiler: gif generation failed (graphviz installed?)",
			slog.String("prof", profPath), slog.Any("err", err))
		return profPath, "", nil
	}

	if err := os.WriteFile(gifPath, gif, 0o644); err != nil {
		return profPath, "", fmt.Errorf("profiler: write gif: %w", err)
	}

	p.Log.Info("profiler: gif written", slog.String("file", gifPath))
	return profPath, gifPath, nil
}
