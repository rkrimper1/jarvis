// Package fileingest provides an HTTP handler for uploading files
// (plain text, CSV, TSV, PDF) and ingesting them as intel signals.
package fileingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"

	"github.com/google/uuid"
)

const defaultMaxMB = 10

// ingestCaller is the subset of the IntelligenceServer used by the handler.
type ingestCaller interface {
	IngestSignal(ctx context.Context, req *intelligv1.IngestSignalRequest) (*intelligv1.IngestSignalResponse, error)
}

// Handler is an http.Handler that accepts multipart file uploads and ingests
// them through the IntelligenceService IngestSignal RPC.
type Handler struct {
	srv   ingestCaller
	maxMB int64
	log   *slog.Logger
}

// New creates a Handler. maxMB is the file size cap in mebibytes; 0 uses
// the default of 10 MiB.
func New(srv ingestCaller, maxMB int64, log *slog.Logger) *Handler {
	if maxMB <= 0 {
		maxMB = defaultMaxMB
	}
	return &Handler{srv: srv, maxMB: maxMB, log: log}
}

// ServeHTTP handles POST /v1/intel/ingest/file.
//
// Expected multipart/form-data fields:
//
//	file       (required) — the file to ingest (.txt, .csv, .tsv, .pdf)
//	source_uri (optional) — URL or description of the original source
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if err := r.ParseMultipartForm(h.maxMB << 20); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("parse multipart: %v", err))
		return
	}

	f, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "field 'file' is required")
		return
	}
	defer f.Close()

	if header.Size > h.maxMB<<20 {
		jsonError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("file exceeds %d MiB limit", h.maxMB))
		return
	}

	data, err := io.ReadAll(f)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read file")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	rawContent, err := extractContent(ext, data)
	if err != nil {
		h.log.ErrorContext(r.Context(), "fileingest: extract failed",
			slog.String("filename", header.Filename),
			slog.String("ext", ext),
			slog.Any("err", err),
		)
		jsonError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("extract %s content: %v", ext, err))
		return
	}
	if strings.TrimSpace(rawContent) == "" {
		jsonError(w, http.StatusUnprocessableEntity, "no extractable text content found in file")
		return
	}

	sourceURI := r.FormValue("source_uri")

	req := &intelligv1.IngestSignalRequest{
		Meta:        &commonv1.RequestMeta{RequestId: uuid.New().String()},
		SourceType:  intelligv1.SourceType_SOURCE_TYPE_FILE_UPLOAD,
		RawContent:  rawContent,
		SourceUri:   sourceURI,
	}
	resp, err := h.srv.IngestSignal(r.Context(), req)
	if err != nil {
		h.log.ErrorContext(r.Context(), "fileingest: ingest signal failed",
			slog.String("filename", header.Filename),
			slog.Any("err", err),
		)
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("ingest signal: %v", err))
		return
	}

	h.log.InfoContext(r.Context(), "fileingest: file ingested",
		slog.String("filename", header.Filename),
		slog.String("card_id", resp.Card.GetId()),
		slog.Float64("confidence", float64(resp.Card.GetConfidenceScore())),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.log.ErrorContext(r.Context(), "fileingest: encode response failed", slog.Any("err", err))
	}
}

// extractContent dispatches to the correct extractor based on file extension.
func extractContent(ext string, data []byte) (string, error) {
	switch ext {
	case ".txt":
		return extractText(data)
	case ".csv":
		return extractDelimited(data, ',')
	case ".tsv":
		return extractDelimited(data, '\t')
	case ".pdf":
		return extractPDFText(data)
	default:
		return "", fmt.Errorf("unsupported file type %q — supported: .txt .csv .tsv .pdf", ext)
	}
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
