package repoindex

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	maulerstore "mauler/internal/store"
)

func TestRichExtractorsIndexPDFOfficeAndZIPWithoutUnboundedChunks(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "report.pdf"), repoMinimalPDF("bounded PDF finding"))
	writeZIPFixture(t, filepath.Join(root, "notes.docx"), map[string]string{
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="urn:w"><w:body><w:p><w:r><w:t>Office evidence text</w:t></w:r></w:p></w:body></w:document>`,
		"[Content_Types].xml": `<Types/>`,
	})
	writeZIPFixture(t, filepath.Join(root, "slides.pptx"), map[string]string{
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="urn:p" xmlns:a="urn:a"><a:t>Presentation evidence text</a:t></p:sld>`,
	})
	writeZIPFixture(t, filepath.Join(root, "table.xlsx"), map[string]string{
		"xl/sharedStrings.xml":     `<sst><si><t>Spreadsheet evidence text</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c><v>42</v></c></row></sheetData></worksheet>`,
	})
	writeZIPFixture(t, filepath.Join(root, "sheet.ods"), map[string]string{
		"content.xml": `<office:document-content xmlns:office="urn:office" xmlns:text="urn:text"><text:p>ODS evidence text</text:p></office:document-content>`,
	})
	writeZIPFixture(t, filepath.Join(root, "bundle.zip"), map[string]string{
		"evidence/readme.txt": "not expanded by inventory extractor",
		"data/item.json":      `{"finding":true}`,
	})

	policy := DefaultPolicy(root)
	policy.ExtractorPolicy.ChunkBytes = 1024
	var chunks []Chunk
	manifest, err := Scan(context.Background(), policy, func(_ context.Context, chunk Chunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Complete || manifest.FilesIndexed != 6 {
		t.Fatalf("manifest = %#v", manifest)
	}
	for _, path := range []string{"report.pdf", "notes.docx", "slides.pptx", "table.xlsx", "sheet.ods", "bundle.zip"} {
		entry := assertEntryStatus(t, manifest, path, StatusIndexed)
		if entry.SHA256 == "" || entry.ChunkCount == 0 {
			t.Fatalf("rich entry missing evidence: %#v", entry)
		}
	}
	joined := ""
	for _, chunk := range chunks {
		if len(chunk.Text) > policy.ExtractorPolicy.ChunkBytes+utf8MaxRuneSlack || chunk.TrustLabel != TrustLabel {
			t.Fatalf("unbounded/untrusted chunk: %#v", chunk)
		}
		joined += chunk.Text
	}
	for _, want := range []string{"bounded PDF finding", "Office evidence text", "Presentation evidence text", "Spreadsheet evidence text", "ODS evidence text", "evidence/readme.txt", "compressed="} {
		if !strings.Contains(joined, want) {
			t.Fatalf("extracted text missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "not expanded by inventory extractor") {
		t.Fatalf("ZIP inventory unexpectedly expanded member content:\n%s", joined)
	}
	second, err := Scan(context.Background(), policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Digest != manifest.Digest || second.PolicyDigest != manifest.PolicyDigest {
		t.Fatalf("rich manifest is not deterministic: %#v then %#v", manifest, second)
	}
}

func TestRichExtractorReportsExpansionAndCorruptionExplicitly(t *testing.T) {
	root := t.TempDir()
	writeZIPFixture(t, filepath.Join(root, "large.docx"), map[string]string{
		"word/document.xml": `<w:document xmlns:w="urn:w"><w:t>` + strings.Repeat("x", 90_000) + `</w:t></w:document>`,
	})
	writeFixture(t, filepath.Join(root, "broken.pdf"), []byte("%PDF-1.4\nnot a valid PDF"))
	policy := DefaultPolicy(root)
	policy.ExtractorPolicy.MaxExpandedBytes = 64 * 1024
	manifest, err := Scan(context.Background(), policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEntryStatus(t, manifest, "large.docx", StatusExpansionLimit)
	assertEntryStatus(t, manifest, "broken.pdf", StatusExtractorError)
	if manifest.FilesIndexed != 0 || manifest.Chunks != 0 || !manifest.Complete {
		t.Fatalf("failed extractors reported coverage: %#v", manifest)
	}
}

func TestStoreSearchesRichExtractorChunksWithImmutableEvidence(t *testing.T) {
	root := t.TempDir()
	writeZIPFixture(t, filepath.Join(root, "assessment.docx"), map[string]string{
		"word/document.xml": `<w:document xmlns:w="urn:w"><w:t>RichFormatSentinel evidence</w:t></w:document>`,
	})
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	result, err := index.Index(context.Background(), DefaultPolicy(root))
	if err != nil {
		t.Fatal(err)
	}
	hits, err := index.Search(context.Background(), result.GenerationID, "RichFormatSentinel", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Path != "assessment.docx" || hits[0].FileSHA256 == "" || hits[0].ChunkID == "" || hits[0].TrustLabel != TrustLabel {
		t.Fatalf("rich search evidence = %#v", hits)
	}
}

func TestAdditionalMetadataExtractorsIndexWithoutArchiveOrDatabaseContent(t *testing.T) {
	root := t.TempDir()
	writeTARFixture(t, filepath.Join(root, "bundle.tar"), false, map[string]string{
		"docs/readme.txt": "TAR_MEMBER_BODY_MUST_NOT_BE_INDEXED",
	})
	writeTARFixture(t, filepath.Join(root, "bundle.tgz"), true, map[string]string{
		"nested/config.json": `{"secret":"TGZ_MEMBER_BODY_MUST_NOT_BE_INDEXED"}`,
	})
	writeSQLiteFixture(t, filepath.Join(root, "inventory.sqlite"))
	writeFixture(t, filepath.Join(root, "sample.exe"), minimalPEFixture())
	writeFixture(t, filepath.Join(root, "sample.elf"), minimalELFFixture())

	policy := DefaultPolicy(root)
	policy.ExtractorPolicy.ChunkBytes = 1024
	var chunks []Chunk
	manifest, err := Scan(context.Background(), policy, func(_ context.Context, chunk Chunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Complete || manifest.FilesIndexed != 5 {
		t.Fatalf("manifest = %#v", manifest)
	}
	for path, extractor := range map[string]string{
		"bundle.tar": tarExtractorVersion, "bundle.tgz": tarExtractorVersion,
		"inventory.sqlite": sqliteExtractorVersion, "sample.exe": peMetadataExtractorVersion, "sample.elf": elfMetadataExtractorVersion,
	} {
		entry := assertEntryStatus(t, manifest, path, StatusIndexed)
		if entry.Extractor != extractor || entry.SHA256 == "" || entry.ChunkCount == 0 {
			t.Fatalf("metadata entry = %#v", entry)
		}
	}
	var joined strings.Builder
	for _, chunk := range chunks {
		if len(chunk.Text) > policy.ExtractorPolicy.ChunkBytes+utf8MaxRuneSlack || chunk.TrustLabel != TrustLabel {
			t.Fatalf("unbounded/untrusted metadata chunk: %#v", chunk)
		}
		joined.WriteString(chunk.Text)
	}
	text := joined.String()
	for _, want := range []string{"docs/readme.txt", "nested/config.json", "SQLite schema", `name="accounts"`, `name="username"`, "PE metadata", "ELF metadata"} {
		if !strings.Contains(text, want) {
			t.Fatalf("metadata output missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"TAR_MEMBER_BODY_MUST_NOT_BE_INDEXED", "TGZ_MEMBER_BODY_MUST_NOT_BE_INDEXED", "SQLITE_ROW_CONTENT_MUST_NOT_BE_INDEXED"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("metadata extractor leaked content %q:\n%s", forbidden, text)
		}
	}
	second, err := Scan(context.Background(), policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Digest != manifest.Digest || second.PolicyDigest != manifest.PolicyDigest {
		t.Fatalf("metadata manifest is not deterministic: %#v then %#v", manifest, second)
	}
}

func TestAdditionalMetadataExtractorsReportLimitsAndCorruption(t *testing.T) {
	root := t.TempDir()
	writeTARFixture(t, filepath.Join(root, "oversized.tar"), false, map[string]string{
		"large.bin": strings.Repeat("x", 90_000),
	})
	writeFixture(t, filepath.Join(root, "broken.sqlite"), []byte("not a sqlite database"))
	writeFixture(t, filepath.Join(root, "broken.exe"), []byte("MZ but not PE"))
	writeFixture(t, filepath.Join(root, "broken.elf"), []byte("not ELF"))
	policy := DefaultPolicy(root)
	policy.ExtractorPolicy.MaxExpandedBytes = 64 * 1024
	manifest, err := Scan(context.Background(), policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEntryStatus(t, manifest, "oversized.tar", StatusExpansionLimit)
	assertEntryStatus(t, manifest, "broken.sqlite", StatusExtractorError)
	assertEntryStatus(t, manifest, "broken.exe", StatusExtractorError)
	assertEntryStatus(t, manifest, "broken.elf", StatusExtractorError)
	if manifest.FilesIndexed != 0 || manifest.Chunks != 0 || !manifest.Complete {
		t.Fatalf("failed metadata extractors reported coverage: %#v", manifest)
	}
}

func TestSevenZIPInventoryIsBoundedMetadataOnly(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "bundle.7z")
	write7ZIPFixture(t, archivePath)
	policy := DefaultPolicy(root)
	policy.ExtractorPolicy.ChunkBytes = 1024
	var chunks []Chunk
	manifest, err := Scan(context.Background(), policy, func(_ context.Context, chunk Chunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := assertEntryStatus(t, manifest, "bundle.7z", StatusIndexed)
	if entry.Extractor != sevenZIPExtractorVersion || entry.SHA256 == "" || entry.ChunkCount == 0 {
		t.Fatalf("7z entry missing immutable evidence: %#v", entry)
	}
	var joined strings.Builder
	for _, chunk := range chunks {
		joined.WriteString(chunk.Text)
	}
	text := joined.String()
	if !strings.Contains(text, "[7z inventory:") || !strings.Contains(text, "go.mod") {
		t.Fatalf("7z inventory missing metadata: %s", text)
	}
	if strings.Contains(text, "module mauler") {
		t.Fatalf("7z inventory leaked member content: %s", text)
	}

	limitedPolicy := policy.ExtractorPolicy
	limitedPolicy.MaxExpandedBytes = 1024
	limitedEntry := ManifestEntry{Root: filepath.ToSlash(root), Path: "bundle.7z", Extractor: sevenZIPExtractorVersion}
	limitedManifest := Manifest{}
	limitedOut := newExtractedChunkAccumulator(context.Background(), &limitedEntry, limitedPolicy, nil, &limitedManifest, nil)
	if err := extract7ZIPInventory(context.Background(), archivePath, limitedPolicy, limitedOut); !errors.Is(err, errExpandedLimit) {
		t.Fatalf("7z logical expansion limit error = %v, want %v", err, errExpandedLimit)
	}
}

func TestScannedPDFOCRIsPageAndOutputBounded(t *testing.T) {
	policy := DefaultPolicy(t.TempDir()).ExtractorPolicy
	policy.MaxOCRPages = 2
	policy.MaxExpandedBytes = 64 * 1024
	entry := ManifestEntry{Path: "scan.pdf", Root: "fixture", Extractor: pdfExtractorVersion}
	manifest := Manifest{}
	out := newExtractedChunkAccumulator(context.Background(), &entry, policy, nil, &manifest, nil)
	var rasterized []int
	tools := pdfOCRTools{
		rasterize: func(_ context.Context, _ string, page int, prefix string, maxBytes int64) (string, error) {
			rasterized = append(rasterized, page)
			if maxBytes <= 0 || !strings.Contains(prefix, fmt.Sprintf("page-%04d", page)) {
				t.Fatalf("unexpected OCR raster bounds: page=%d prefix=%q max=%d", page, prefix, maxBytes)
			}
			return prefix + ".png", nil
		},
		recognize: func(_ context.Context, imagePath string, maxBytes int64) (string, error) {
			if maxBytes <= 0 {
				t.Fatal("OCR output must have a positive remaining byte limit")
			}
			return "scanned evidence from " + filepath.Base(imagePath), nil
		},
	}
	if err := extractScannedPDFOCRWith(context.Background(), "scan.pdf", 2, policy, out, tools); err != nil {
		t.Fatal(err)
	}
	if err := out.finish(); err != nil {
		t.Fatal(err)
	}
	if entry.Extractor != pdfOCRExtractorVersion || entry.Language != "ocr-eng" || len(rasterized) != 2 || entry.ChunkCount == 0 {
		t.Fatalf("OCR metadata/bounds = entry %#v pages %#v", entry, rasterized)
	}

	blockedEntry := ManifestEntry{Path: "long-scan.pdf", Root: "fixture", Extractor: pdfExtractorVersion}
	blockedOut := newExtractedChunkAccumulator(context.Background(), &blockedEntry, policy, nil, &Manifest{}, nil)
	if err := extractScannedPDFOCRWith(context.Background(), "long-scan.pdf", 3, policy, blockedOut, tools); !errors.Is(err, errOCRPageLimit) {
		t.Fatalf("OCR page limit error = %v, want %v", err, errOCRPageLimit)
	}
}

func TestScannedPDFOCRLiveSmoke(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm is not installed")
	}
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skip("tesseract is not installed")
	}
	root := t.TempDir()
	path := filepath.Join(root, "scanned.pdf")
	writeFixture(t, path, repoScannedPDF("MAULER OCR"))
	policy := DefaultPolicy(root)
	policy.ExtractorPolicy.MaxOCRPages = 4
	policy.ExtractorPolicy.TimeoutMillis = 120_000
	var text strings.Builder
	manifest, err := Scan(context.Background(), policy, func(_ context.Context, chunk Chunk) error {
		text.WriteString(chunk.Text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := assertEntryStatus(t, manifest, filepath.Base(path), StatusIndexed)
	if entry.Extractor != pdfOCRExtractorVersion || entry.ChunkCount == 0 || !strings.Contains(strings.ToLower(text.String()), "mauler") {
		t.Fatalf("live OCR evidence = entry %#v text %q", entry, text.String())
	}
}

func repoScannedPDF(text string) []byte {
	const width, height, scale = 1000, 220, 14
	imageData := image.NewGray(image.Rect(0, 0, width, height))
	for index := range imageData.Pix {
		imageData.Pix[index] = 0xff
	}
	glyphs := map[rune][]string{
		'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
		'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
		'U': {"10001", "10001", "10001", "10001", "10001", "10001", "01110"},
		'L': {"10000", "10000", "10000", "10000", "10000", "10000", "11111"},
		'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
		'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
		'O': {"01110", "10001", "10001", "10001", "10001", "10001", "01110"},
		'C': {"01111", "10000", "10000", "10000", "10000", "10000", "01111"},
		' ': {"00000", "00000", "00000", "00000", "00000", "00000", "00000"},
	}
	x, startY := 45, 55
	for _, letter := range strings.ToUpper(text) {
		pattern := glyphs[letter]
		for row, bits := range pattern {
			for column, bit := range bits {
				if bit != '1' {
					continue
				}
				for py := 0; py < scale; py++ {
					for px := 0; px < scale; px++ {
						imageData.SetGray(x+column*scale+px, startY+row*scale+py, color.Gray{Y: 0})
					}
				}
			}
		}
		x += 6 * scale
	}
	var encoded bytes.Buffer
	_ = jpeg.Encode(&encoded, imageData, &jpeg.Options{Quality: 95})

	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n%scanned-image-only\n")
	offsets := make([]int, 6)
	writeObject := func(number int, header string, stream []byte) {
		offsets[number] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s", number, header)
		if stream != nil {
			output.WriteString("\nstream\n")
			output.Write(stream)
			output.WriteString("\nendstream")
		}
		output.WriteString("\nendobj\n")
	}
	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>", nil)
	writeObject(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", nil)
	writeObject(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 132] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>", nil)
	writeObject(4, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>", width, height, encoded.Len()), encoded.Bytes())
	content := []byte("q 600 0 0 132 0 0 cm /Im0 Do Q")
	writeObject(5, fmt.Sprintf("<< /Length %d >>", len(content)), content)
	xref := output.Len()
	output.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for number := 1; number <= 5; number++ {
		fmt.Fprintf(&output, "%010d 00000 n \n", offsets[number])
	}
	fmt.Fprintf(&output, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return output.Bytes()
}

func write7ZIPFixture(t *testing.T, path string) {
	t.Helper()
	const encoded = "N3q8ryccAAQS/sHE8AMAAAAAAABaAAAAAAAAADqCey/gCo0D6F0ANpvIsa4S7u3Yfks2MwIdtExatdK23XV++Qpmi8Ermbw7Vk93nJIDnIaCnVdejB5TY9eLzvdzEULEp7OHpgDjxj+jhWpTQDzNuZ02rdVgJT/MMVWpkJofayAKSaK0l1Jfs4yKOEEP6j2FTcZX++LW+BeR00m39A+O62uQrkglt1+gz3RalcUEyHDi6pOGW2+z54+8O1E++h8qufz5U2EouqMpLi5jL3uaR3yHA7Ibqj/Z0BDbZgLgf8xXFUZFwyoRUcJCJwmbwgmu7Rfj/Oq3K2GjygrdxALDUj0DBlYkxS6axXg5p+gA6H8HWmjYj5bZQzdP3cIq/7FLefUL9qKfY6p448KsXOGZiZWZ/iaVzL335Qgm4Pp0zAlU5dKFsc5dKS+QdVU2WZZKppDxxbtJjmH91/J+XB0Je3VcNhHpCn1kDrUzOTKLyp6vdkvS252GhddFgiK0FbB9LoL/pPq472hwbU/C23ym5f5rMr8Wx/Shqcb7OEhSZhRwBtgHts958Irp42ZYFid5XLdsM3XCvJfxGshH53Ejx70kG6ZE9eGhTw/Z1S4Q/gHH1r+/x8XVq+9nu4mYDCTA22W4bT6Cc3nynEJBNyaFCHJvQONdrwCtW+Y74NILfFv0CHVc7IaRn6K6vpxSFDTKQzq4fqIYQER5jXeU0Ij8mxO5NP9nMZS9Yq6U2Q5OHR0WuKTQJ7+l8avkCeFXUKri5X4N5Q+2VZVxLeDD54m4PjmN56nOLBiBgZmI6narvl8INBhNN7DNlutnNnlXdg3Sgp8dstNnJsj62xsLP5SdOuMsSx+1IbkqKEZyCZV2vQ4n+R5HUm6r7rkgqdd12QfYBdijKGiUXAaKdBWCSMP/za2xPv9oNv/0GG4QJVUG8H+g2W42XcqxgoYDIdBXky44xw9afiN+pBGVixvvUwJ0evnrkl41wIBMyV7HH8DyvoSCcX8L+CmJsFjv7AqcqyClDn/k0ivhT40XPIOwttZFru3z2TlMcZAWjpp/YinMy0AU6DlWobrWvQz14uUWd2SgbTHdNUx2Amy9pWUZ/EVQ4Gj5KeNRtp3wr6ilBj3i9NgcNVDChDoNJV9QbQpyGU4EuD0RvTkKBRL6ltC1tYmmMVpa2eVm1mpJE0LNN+mV+sIdum4L1i5kUTBzAy+9+KmcUiMl87bGvz1IxxlfNFsMymi9ZLgYfqGxY9HsmqpthSxiZJLJCNwMYyc5z7qd6uB1E8vCxYFcX/xmzvSEid60TJinIqrgytLFRgfPNzmzTbQ+iFHxFCLqoK3wPUsdTC2ATeNbEMF96xIBv4x6O4xLySPegcv0usRCjByr1LIgAAABBAYAAQmD8AAHCwEAASEhAQAMio4ACAoBlLBVTwAABQEZCgAAAAAAAAAAAAARDwBnAG8ALgBtAG8AZAAAABkEAAAAABQKAQD/DyuhimTdARUGAQAgAAAAAAA="
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeZIPFixture(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTARFixture(t *testing.T, path string, compressed bool, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	var target = interface{ Write([]byte) (int, error) }(f)
	var gz *gzip.Writer
	if compressed {
		gz = gzip.NewWriter(f)
		target = gz
	}
	tw := tar.NewWriter(target)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		content := files[name]
		header := &tar.Header{Name: name, Mode: 0o640, Size: int64(len(content)), ModTime: time.Unix(0, 0).UTC()}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeSQLiteFixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE accounts (id INTEGER PRIMARY KEY, username TEXT NOT NULL, token TEXT); CREATE INDEX accounts_username ON accounts(username); INSERT INTO accounts(username, token) VALUES ('alice', 'SQLITE_ROW_CONTENT_MUST_NOT_BE_INDEXED')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func minimalPEFixture() []byte {
	data := make([]byte, 0x80+4+20+40)
	copy(data[:2], []byte{'M', 'Z'})
	binary.LittleEndian.PutUint32(data[0x3c:0x40], 0x80)
	copy(data[0x80:0x84], []byte{'P', 'E', 0, 0})
	header := data[0x84 : 0x84+20]
	binary.LittleEndian.PutUint16(header[0:2], 0x8664)
	binary.LittleEndian.PutUint16(header[2:4], 1)
	binary.LittleEndian.PutUint32(header[4:8], 1)
	binary.LittleEndian.PutUint16(header[18:20], 0x2022)
	section := data[0x98 : 0x98+40]
	copy(section[:8], []byte(".text"))
	binary.LittleEndian.PutUint32(section[36:40], 0x60000020)
	return data
}

func minimalELFFixture() []byte {
	data := make([]byte, 128)
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4], data[5], data[6] = 2, 1, 1
	binary.LittleEndian.PutUint16(data[16:18], 2)
	binary.LittleEndian.PutUint16(data[18:20], 62)
	binary.LittleEndian.PutUint32(data[20:24], 1)
	binary.LittleEndian.PutUint64(data[24:32], 0x401000)
	binary.LittleEndian.PutUint64(data[40:48], 64)
	binary.LittleEndian.PutUint16(data[52:54], 64)
	binary.LittleEndian.PutUint16(data[58:60], 64)
	binary.LittleEndian.PutUint16(data[60:62], 1)
	return data
}

func repoMinimalPDF(text string) []byte {
	escaped := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(text)
	objects := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>\nendobj\n",
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n",
		fmt.Sprintf("5 0 obj\n<< /Length %d >>\nstream\nBT /F1 24 Tf 100 700 Td (%s) Tj ET\nendstream\nendobj\n", len("BT /F1 24 Tf 100 700 Td ("+escaped+") Tj ET\n"), escaped),
	}
	var sb bytes.Buffer
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
	return sb.Bytes()
}
