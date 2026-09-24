package repoindex

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"os"
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
