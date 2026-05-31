package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// ReadPDF extracts plain text from a local PDF, optionally restricted to pages.
type ReadPDF struct{}

func (t *ReadPDF) Name() string      { return "read_pdf" }
func (t *ReadPDF) Destructive() bool { return false }

func (t *ReadPDF) Description() string {
	return "Extract readable text from a local PDF file. Use this for documents, reports, papers, manuals, and PDFs the user wants analysed. Supports optional 1-indexed page ranges and output limiting."
}

func (t *ReadPDF) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Local PDF file path to read"},
    "start_page": {"type": "integer", "description": "First page to extract (1-indexed, inclusive). Omit to start at page 1."},
    "end_page": {"type": "integer", "description": "Last page to extract (1-indexed, inclusive). Omit to read to the final page."},
    "max_chars": {"type": "integer", "description": "Maximum characters to return. Defaults to 12000 and is capped at 60000."}
  },
  "required": ["path"],
  "additionalProperties": false
}`)
}

type readPDFParams struct {
	Path      string `json:"path"`
	StartPage int    `json:"start_page"`
	EndPage   int    `json:"end_page"`
	MaxChars  int    `json:"max_chars"`
}

func (t *ReadPDF) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p readPDFParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("read_pdf: bad params: %w", err)
	}
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("read_pdf: path is required")
	}

	path := NormalizeHostPath(p.Path)
	f, reader, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("read_pdf: %w", err)
	}
	defer f.Close()

	total := reader.NumPage()
	if total <= 0 {
		return fmt.Sprintf("# %s  (0 pages)\nNo pages found.", path), nil
	}

	start := 1
	end := total
	if p.StartPage > 0 {
		start = p.StartPage
	}
	if p.EndPage > 0 {
		end = p.EndPage
	}
	if start < 1 {
		start = 1
	}
	if end > total {
		end = total
	}
	if start > end {
		return "", fmt.Errorf("read_pdf: start_page %d > end_page %d", start, end)
	}

	limit := p.MaxChars
	if limit <= 0 {
		limit = 12000
	}
	if limit > 60000 {
		limit = 60000
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s  (pages %d-%d of %d)\n", path, start, end, total)
	truncated := false

	for pageNum := start; pageNum <= end; pageNum++ {
		page := reader.Page(pageNum)
		if page.V.IsNull() || page.V.Key("Contents").Kind() == pdf.Null {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			fmt.Fprintf(&sb, "\n## Page %d\n[extraction error: %v]\n", pageNum, err)
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		addLimited(&sb, fmt.Sprintf("\n## Page %d\n%s\n", pageNum, text), limit)
		if sb.Len() >= limit {
			truncated = true
			break
		}
	}

	if truncated {
		sb.WriteString("\n[truncated: narrow the page range or raise max_chars to read more]\n")
	}
	out := strings.TrimRight(sb.String(), "\n")
	if strings.TrimSpace(out) == strings.TrimSpace(fmt.Sprintf("# %s  (pages %d-%d of %d)", path, start, end, total)) {
		return out + "\nNo extractable text found. This PDF may be scanned/image-only; use OCR outside TheMauler for now.", nil
	}
	return out, nil
}

func addLimited(sb *strings.Builder, text string, limit int) {
	remaining := limit - sb.Len()
	if remaining <= 0 {
		return
	}
	if len(text) <= remaining {
		sb.WriteString(text)
		return
	}
	cut := remaining
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	sb.WriteString(text[:cut])
}
