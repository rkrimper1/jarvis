package server_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	commandv1 "github.com/rkrimper1/jarvis/api/pb/command"
	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	"github.com/rkrimper1/jarvis/api/internal/command/server"
	"github.com/rkrimper1/jarvis/api/internal/profiler"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T) *server.CommandServer {
	t.Helper()
	dir := t.TempDir()
	hp := &profiler.HeapProfiler{
		OutDir:   dir,
		Interval: time.Hour,
		Log:      discardLogger(),
	}
	return server.New(hp, discardLogger())
}

// ── RequestMemoryProfile ──────────────────────────────────────────────────────

func TestRequestMemoryProfile_HappyPath(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.RequestMemoryProfile(context.Background(), &commandv1.RequestMemoryProfileRequest{
		Meta: &commonv1.RequestMeta{RequestId: "test-req-001"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ProfPath == "" {
		t.Error("expected non-empty ProfPath")
	}
	if _, statErr := os.Stat(resp.ProfPath); statErr != nil {
		t.Errorf("prof file not found at %q: %v", resp.ProfPath, statErr)
	}
	if resp.Meta == nil {
		t.Error("expected non-nil Meta in response")
	}
}

func TestRequestMemoryProfile_ProfFileInsideOutputDir(t *testing.T) {
	dir := t.TempDir()
	hp := &profiler.HeapProfiler{OutDir: dir, Interval: time.Hour, Log: discardLogger()}
	s := server.New(hp, discardLogger())

	resp, err := s.RequestMemoryProfile(context.Background(), &commandv1.RequestMemoryProfileRequest{
		Meta: &commonv1.RequestMeta{RequestId: "dir-check"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ProfPath) < len(dir) || resp.ProfPath[:len(dir)] != dir {
		t.Errorf("ProfPath %q is outside output dir %q", resp.ProfPath, dir)
	}
}

func TestRequestMemoryProfile_CaptureError(t *testing.T) {
	// Point OutDir to a file so MkdirAll fails.
	f, err := os.CreateTemp("", "not-a-dir-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	hp := &profiler.HeapProfiler{OutDir: f.Name(), Interval: time.Hour, Log: discardLogger()}
	s := server.New(hp, discardLogger())

	_, err = s.RequestMemoryProfile(context.Background(), &commandv1.RequestMemoryProfileRequest{
		Meta: &commonv1.RequestMeta{RequestId: "fail-req"},
	})
	if err == nil {
		t.Fatal("expected error when OutDir is an existing file, got nil")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.Internal {
		t.Errorf("expected codes.Internal, got %v", err)
	}
}
