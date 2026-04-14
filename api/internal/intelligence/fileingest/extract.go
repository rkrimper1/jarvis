package fileingest

import (
	"bytes"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// ── plain text ────────────────────────────────────────────────────────────────

func extractText(data []byte) (string, error) {
	return strings.TrimSpace(string(data)), nil
}

// ── CSV / TSV ─────────────────────────────────────────────────────────────────

// extractDelimited parses CSV or TSV data and formats each row as
// "col1: val1 | col2: val2" using the header row as column names.
// If no header is detected (all values look like data), columns are numbered.
func extractDelimited(data []byte, comma rune) (string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = comma
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	records, err := r.ReadAll()
	if err != nil {
		return "", fmt.Errorf("parse delimited: %w", err)
	}
	if len(records) == 0 {
		return "", nil
	}

	headers := records[0]
	var sb strings.Builder
	for i, row := range records[1:] {
		if i > 0 {
			sb.WriteString("\n")
		}
		parts := make([]string, 0, len(row))
		for j, val := range row {
			val = strings.TrimSpace(val)
			if val == "" {
				continue
			}
			col := fmt.Sprintf("col%d", j+1)
			if j < len(headers) && strings.TrimSpace(headers[j]) != "" {
				col = strings.TrimSpace(headers[j])
			}
			parts = append(parts, col+": "+val)
		}
		sb.WriteString(strings.Join(parts, " | "))
	}
	return strings.TrimSpace(sb.String()), nil
}

// ── PDF ───────────────────────────────────────────────────────────────────────

// extractPDFText extracts human-readable text from a PDF using pdfcpu.
// It reads content streams for each page in-memory (no temp files) and
// parses PDF text-showing operators (Tj, TJ, ').
func extractPDFText(data []byte) (string, error) {
	conf := pdfmodel.NewDefaultConfiguration()
	conf.ValidationMode = pdfmodel.ValidationRelaxed

	rs := bytes.NewReader(data)
	ctx, err := api.ReadValidateAndOptimize(rs, conf)
	if err != nil {
		return "", fmt.Errorf("pdf: read: %w", err)
	}

	var sb strings.Builder
	for pageNr := 1; pageNr <= ctx.PageCount; pageNr++ {
		r, err := pdfcpu.ExtractPageContent(ctx, pageNr)
		if err != nil || r == nil {
			continue
		}
		raw, err := io.ReadAll(r)
		if err != nil {
			continue
		}
		if text := parseContentStream(raw); text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(text)
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// ── PDF content stream parser ─────────────────────────────────────────────────

// These regexes match the two forms of PDF text-showing operators:
//
//	(string) Tj   — show a single string
//	[(a)(b)] TJ   — show an array of strings (kerned text)
//	(string) '    — move to next line and show text
//
// PDF string literals may contain:
//   - Balanced nested parentheses
//   - Backslash escapes: \n \r \t \b \f \( \) \\
//   - Octal escapes: \ddd
//
// We handle the common cases conservatively; deeply nested parens in rare
// PDFs may produce partial results, which is acceptable for intel ingestion.
var (
	reTj    = regexp.MustCompile(`\(([^)\\]*(?:\\.[^)\\]*)*)\)\s*(?:Tj|')`)
	reTJ    = regexp.MustCompile(`\[([^\]]*)\]\s*TJ`)
	reParen = regexp.MustCompile(`\(([^)\\]*(?:\\.[^)\\]*)*)\)`)
	reHex   = regexp.MustCompile(`<([0-9a-fA-F]+)>\s*(?:Tj|')`)
)

func parseContentStream(data []byte) string {
	content := string(data)
	var parts []string

	// (string) Tj and (string) '
	for _, m := range reTj.FindAllStringSubmatch(content, -1) {
		if t := decodePDFString(m[1]); t != "" {
			parts = append(parts, t)
		}
	}

	// [(strings)] TJ
	for _, m := range reTJ.FindAllStringSubmatch(content, -1) {
		for _, pm := range reParen.FindAllStringSubmatch(m[1], -1) {
			if t := decodePDFString(pm[1]); t != "" {
				parts = append(parts, t)
			}
		}
	}

	// <hexstring> Tj — hex-encoded strings
	for _, m := range reHex.FindAllStringSubmatch(content, -1) {
		if t := decodeHexString(m[1]); t != "" {
			parts = append(parts, t)
		}
	}

	return strings.Join(parts, " ")
}

// decodePDFString decodes PDF string escape sequences into a plain string.
func decodePDFString(s string) string {
	if s == "" {
		return ""
	}
	var sb strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '\\' {
			sb.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			i++
			continue
		}
		switch s[i+1] {
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case '(':
			sb.WriteByte('(')
		case ')':
			sb.WriteByte(')')
		case '\\':
			sb.WriteByte('\\')
		default:
			// Octal escape \ddd
			if i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '7' {
				end := i + 2
				for end < i+4 && end < len(s) && s[end] >= '0' && s[end] <= '7' {
					end++
				}
				var val byte
				for _, c := range s[i+1 : end] {
					val = val*8 + (byte(c) - '0')
				}
				if val >= 32 && val < 128 {
					sb.WriteByte(val)
				}
				i = end
				continue
			}
			sb.WriteByte(s[i+1])
		}
		i += 2
	}
	return strings.TrimSpace(sb.String())
}

// decodeHexString decodes a PDF hex string <4865 6c6c 6f> into plain text.
// Non-printable bytes are silently dropped.
func decodeHexString(hex2 string) string {
	// Remove spaces
	hex2 = strings.ReplaceAll(hex2, " ", "")
	if len(hex2)%2 != 0 {
		hex2 += "0"
	}
	b, err := hex.DecodeString(hex2)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range b {
		if c >= 32 && c < 128 {
			sb.WriteByte(c)
		}
	}
	return strings.TrimSpace(sb.String())
}
