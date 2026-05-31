package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPDFExtractsText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(path, minimalPDF("Hello from PDF"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := (&ReadPDF{}).Run(context.Background(), json.RawMessage(fmt.Sprintf(`{"path":%q}`, path)))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "Hello from PDF") || !strings.Contains(out, "pages 1-1 of 1") {
		t.Fatalf("unexpected PDF output:\n%s", out)
	}
}

func TestReadPDFValidatesPageRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(path, minimalPDF("Hello"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := (&ReadPDF{}).Run(context.Background(), json.RawMessage(fmt.Sprintf(`{"path":%q,"start_page":2,"end_page":1}`, path)))
	if err == nil || !strings.Contains(err.Error(), "start_page 2 > end_page 1") {
		t.Fatalf("expected page range error, got %v", err)
	}
}

func minimalPDF(text string) []byte {
	escaped := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(text)
	objects := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>\nendobj\n",
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n",
		fmt.Sprintf("5 0 obj\n<< /Length %d >>\nstream\nBT /F1 24 Tf 100 700 Td (%s) Tj ET\nendstream\nendobj\n", len("BT /F1 24 Tf 100 700 Td ("+escaped+") Tj ET\n"), escaped),
	}

	var sb strings.Builder
	sb.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = sb.Len()
		sb.WriteString(obj)
	}
	xref := sb.Len()
	fmt.Fprintf(&sb, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&sb, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&sb, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(sb.String())
}
