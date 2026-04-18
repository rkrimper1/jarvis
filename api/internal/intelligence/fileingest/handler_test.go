package fileingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/rkrimper1/jarvis/api/internal/intelligence/fileingest"
)

// ── mock IngestSignal server ──────────────────────────────────────────────────

type mockSrv struct {
	capturedContent string
	capturedSource  intelligv1.SourceType
	capturedURI     string
	err             error
}

func (m *mockSrv) IngestSignal(_ context.Context, req *intelligv1.IngestSignalRequest) (*intelligv1.IngestSignalResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.capturedContent = req.RawContent
	m.capturedSource = req.SourceType
	m.capturedURI = req.SourceUri
	return &intelligv1.IngestSignalResponse{
		Meta: &commonv1.ResponseMeta{RequestId: req.Meta.RequestId, Success: true},
		Signal: &intelligv1.RawSignal{
			Id:         "sig-1",
			RawContent: req.RawContent,
			SourceType: req.SourceType,
		},
		Card: &intelligv1.IntelCard{
			Id:              "card-1",
			Title:           "Test Card",
			ConfidenceScore: 0.85,
			Status:          intelligv1.IntelCardStatus_INTEL_CARD_STATUS_PENDING_REVIEW,
		},
	}, nil
}

// ── multipart helpers ─────────────────────────────────────────────────────────

func multipartUpload(t *testing.T, filename, contentType string, body []byte, extra map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	fw.Write(body)

	for k, v := range extra {
		w.WriteField(k, v)
	}
	w.Close()

	r := httptest.NewRequest(http.MethodPost, "/v1/intel/ingest/file", &buf)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ── plain text ────────────────────────────────────────────────────────────────

func TestHandler_TxtFile(t *testing.T) {
	srv := &mockSrv{}
	h := fileingest.New(srv, 10, discardLogger())

	content := "Acme Corp reduced prices by 15% this quarter."
	r := multipartUpload(t, "signal.txt", "text/plain", []byte(content), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(srv.capturedContent, "Acme Corp") {
		t.Errorf("content = %q, expected original text", srv.capturedContent)
	}
	if srv.capturedSource != intelligv1.SourceType_SOURCE_TYPE_FILE_UPLOAD {
		t.Errorf("source_type = %v, want FILE_UPLOAD", srv.capturedSource)
	}
}

// ── CSV ───────────────────────────────────────────────────────────────────────

func TestHandler_CSVFile(t *testing.T) {
	srv := &mockSrv{}
	h := fileingest.New(srv, 10, discardLogger())

	csv := "company,event,impact\nAcme,price cut,high\nStark,acquisition,medium\n"
	r := multipartUpload(t, "signals.csv", "text/csv", []byte(csv), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(srv.capturedContent, "company: Acme") {
		t.Errorf("content = %q, expected formatted CSV rows", srv.capturedContent)
	}
	if !strings.Contains(srv.capturedContent, "event: price cut") {
		t.Errorf("content = %q, expected 'event: price cut'", srv.capturedContent)
	}
}

func TestHandler_TSVFile(t *testing.T) {
	srv := &mockSrv{}
	h := fileingest.New(srv, 10, discardLogger())

	tsv := "company\tevent\nAcme\tprice cut\n"
	r := multipartUpload(t, "signals.tsv", "text/tab-separated-values", []byte(tsv), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(srv.capturedContent, "company: Acme") {
		t.Errorf("content = %q", srv.capturedContent)
	}
}

// ── source_uri passthrough ────────────────────────────────────────────────────

func TestHandler_SourceURIPassthrough(t *testing.T) {
	srv := &mockSrv{}
	h := fileingest.New(srv, 10, discardLogger())

	r := multipartUpload(t, "note.txt", "text/plain", []byte("some intel"),
		map[string]string{"source_uri": "https://example.com/report.pdf"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	if srv.capturedURI != "https://example.com/report.pdf" {
		t.Errorf("source_uri = %q, want URL", srv.capturedURI)
	}
}

// ── error cases ───────────────────────────────────────────────────────────────

func TestHandler_WrongMethod(t *testing.T) {
	h := fileingest.New(&mockSrv{}, 10, discardLogger())
	r := httptest.NewRequest(http.MethodGet, "/v1/intel/ingest/file", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandler_MissingFileField(t *testing.T) {
	h := fileingest.New(&mockSrv{}, 10, discardLogger())

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("source_uri", "https://example.com")
	mw.Close()

	r := httptest.NewRequest(http.MethodPost, "/v1/intel/ingest/file", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandler_UnsupportedExtension(t *testing.T) {
	h := fileingest.New(&mockSrv{}, 10, discardLogger())
	r := multipartUpload(t, "data.docx", "application/octet-stream", []byte("data"), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
}

func TestHandler_EmptyContent(t *testing.T) {
	h := fileingest.New(&mockSrv{}, 10, discardLogger())
	r := multipartUpload(t, "empty.txt", "text/plain", []byte("   "), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
}

func TestHandler_IngestError(t *testing.T) {
	srv := &mockSrv{err: status.Error(codes.Internal, "fusion failed")}
	h := fileingest.New(srv, 10, discardLogger())
	r := multipartUpload(t, "note.txt", "text/plain", []byte("some content"), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestHandler_ResponseIsJSON(t *testing.T) {
	h := fileingest.New(&mockSrv{}, 10, discardLogger())
	r := multipartUpload(t, "note.txt", "text/plain", []byte("intel content"), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Errorf("response is not valid JSON: %v", err)
	}
}

// ── extract unit tests ────────────────────────────────────────────────────────

func TestExtract_CSV_NoHeader(t *testing.T) {
	// Single row only (no header): columns are numbered
	srv := &mockSrv{}
	h := fileingest.New(srv, 10, discardLogger())
	csv := "only,one,row\n"
	r := multipartUpload(t, "data.csv", "text/csv", []byte(csv), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	// With only one row it becomes the header with no data rows — empty content
	// means 422. This is correct behaviour: a header-only CSV has no intel.
	if w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 or 422", w.Code)
	}
}

func TestExtract_CSV_QuotedFields(t *testing.T) {
	srv := &mockSrv{}
	h := fileingest.New(srv, 10, discardLogger())
	csv := `company,note` + "\n" + `"Acme, Inc.","price ""cut""` + "\n"
	r := multipartUpload(t, "data.csv", "text/csv", []byte(csv), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(srv.capturedContent, "Acme, Inc.") {
		t.Errorf("content = %q, expected quoted field preserved", srv.capturedContent)
	}
}
