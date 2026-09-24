package repoindex

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"debug/elf"
	"debug/pe"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	_ "modernc.org/sqlite"
)

const (
	pdfExtractorVersion         = "pdf-text-v1"
	officeExtractorVersion      = "openxml-text-v1"
	zipExtractorVersion         = "zip-inventory-v1"
	tarExtractorVersion         = "tar-inventory-v1"
	sqliteExtractorVersion      = "sqlite-schema-v1"
	peMetadataExtractorVersion  = "pe-metadata-v1"
	elfMetadataExtractorVersion = "elf-metadata-v1"
)

var (
	errExpandedLimit = errors.New("expanded output limit reached")
	errEncrypted     = errors.New("encrypted archive member is unsupported")
)

func richExtractorKind(path string) string {
	lower := strings.ToLower(filepath.ToSlash(path))
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return "tar"
	}
	if strings.HasSuffix(lower, ".so") || strings.Contains(filepath.Base(lower), ".so.") {
		return "elf"
	}
	switch strings.ToLower(filepath.Ext(lower)) {
	case ".pdf":
		return "pdf"
	case ".docx", ".pptx", ".xlsx", ".ods":
		return "office"
	case ".zip":
		return "zip"
	case ".tar":
		return "tar"
	case ".db", ".sqlite", ".sqlite3":
		return "sqlite"
	case ".exe", ".dll", ".sys", ".efi", ".ocx":
		return "pe"
	case ".elf", ".o":
		return "elf"
	default:
		return ""
	}
}

func indexRichFile(ctx context.Context, path string, entry *ManifestEntry, policy ExtractorPolicy, sink ChunkSink, manifest *Manifest, progressSink ProgressSink) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.TimeoutMillis)*time.Millisecond)
	defer cancel()

	if err := hashSourceFile(ctx, path, entry, manifest); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			entry.Status, entry.Detail = StatusExtractorLimit, fmt.Sprintf("extractor exceeded %d ms", policy.TimeoutMillis)
			return nil
		}
		return err
	}
	if entry.Status != "" {
		return nil
	}

	kind := richExtractorKind(path)
	acc := newExtractedChunkAccumulator(ctx, entry, policy, sink, manifest, progressSink)
	var err error
	switch kind {
	case "pdf":
		entry.Extractor, entry.Language, entry.Encoding = pdfExtractorVersion, "pdf", "utf-8"
		err = extractPDF(ctx, path, acc)
	case "office":
		entry.Extractor, entry.Language, entry.Encoding = officeExtractorVersion, "office-document", "utf-8"
		err = extractOffice(ctx, path, policy, acc)
	case "zip":
		entry.Extractor, entry.Language, entry.Encoding = zipExtractorVersion, "archive-metadata", "utf-8"
		err = extractZIPInventory(ctx, path, policy, acc)
	case "tar":
		entry.Extractor, entry.Language, entry.Encoding = tarExtractorVersion, "archive-metadata", "utf-8"
		err = extractTARInventory(ctx, path, policy, acc)
	case "sqlite":
		entry.Extractor, entry.Language, entry.Encoding = sqliteExtractorVersion, "database-metadata", "utf-8"
		err = extractSQLiteSchema(ctx, path, policy, acc)
	case "pe":
		entry.Extractor, entry.Language, entry.Encoding = peMetadataExtractorVersion, "binary-metadata", "utf-8"
		err = extractPEMetadata(ctx, path, policy, acc)
	case "elf":
		entry.Extractor, entry.Language, entry.Encoding = elfMetadataExtractorVersion, "binary-metadata", "utf-8"
		err = extractELFMetadata(ctx, path, policy, acc)
	default:
		entry.Status, entry.Detail = StatusUnsupported, "no rich extractor for this format"
		return nil
	}

	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			entry.Status, entry.Detail = StatusExtractorLimit, fmt.Sprintf("extractor exceeded %d ms", policy.TimeoutMillis)
			return nil
		case errors.Is(err, context.Canceled):
			entry.Status, entry.Detail = StatusReadError, err.Error()
			return err
		case errors.Is(err, errExpandedLimit):
			entry.Status, entry.Detail = StatusExpansionLimit, fmt.Sprintf("expanded output exceeds max_expanded_bytes %d", policy.MaxExpandedBytes)
			return nil
		case errors.Is(err, errEncrypted):
			entry.Status, entry.Detail = StatusEncrypted, err.Error()
			return nil
		default:
			entry.Status, entry.Detail = StatusExtractorError, err.Error()
			return nil
		}
	}
	if acc.extractedBytes == 0 {
		entry.Status, entry.Detail = StatusUnsupported, "no extractable text or metadata"
		if kind == "pdf" {
			entry.Detail = "no extractable text; scanned PDFs require OCR"
		}
		return nil
	}
	if err := acc.finish(); err != nil {
		entry.Status, entry.Detail = StatusReadError, "chunk sink: "+err.Error()
		return err
	}
	entry.Status = StatusIndexed
	manifest.FilesIndexed++
	return nil
}

func hashSourceFile(ctx context.Context, path string, entry *ManifestEntry, manifest *Manifest) error {
	f, err := os.Open(path)
	if err != nil {
		entry.Status, entry.Detail = StatusReadError, err.Error()
		return nil
	}
	defer f.Close()
	hasher := sha256.New()
	buf := make([]byte, 64*1024)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			entry.Status, entry.Detail = StatusReadError, err.Error()
			return err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			_, _ = hasher.Write(buf[:n])
			read += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			entry.Status, entry.Detail = StatusReadError, readErr.Error()
			return nil
		}
	}
	entry.SHA256 = hex.EncodeToString(hasher.Sum(nil))
	manifest.BytesRead += read
	if read != entry.Size {
		entry.Status, entry.Detail = StatusReadError, fmt.Sprintf("file size changed during scan: stat=%d read=%d", entry.Size, read)
		return nil
	}
	return nil
}

type extractedChunkAccumulator struct {
	ctx            context.Context
	entry          *ManifestEntry
	policy         ExtractorPolicy
	sink           ChunkSink
	manifest       *Manifest
	progressSink   ProgressSink
	staged         []Chunk
	chunk          strings.Builder
	line           int
	chunkStart     int
	ordinal        int
	extractedBytes int64
}

func newExtractedChunkAccumulator(ctx context.Context, entry *ManifestEntry, policy ExtractorPolicy, sink ChunkSink, manifest *Manifest, progressSink ProgressSink) *extractedChunkAccumulator {
	return &extractedChunkAccumulator{ctx: ctx, entry: entry, policy: policy, sink: sink, manifest: manifest, progressSink: progressSink, line: 1, chunkStart: 1}
}

func (a *extractedChunkAccumulator) append(text string) error {
	if text == "" {
		return nil
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if a.extractedBytes+int64(len(text)) > a.policy.MaxExpandedBytes {
		return errExpandedLimit
	}
	a.extractedBytes += int64(len(text))
	for _, r := range text {
		encodedLen := utf8.RuneLen(r)
		if encodedLen < 0 {
			encodedLen = 1
		}
		if a.chunk.Len() > 0 && a.chunk.Len()+encodedLen > a.policy.ChunkBytes {
			if err := a.flush(a.line); err != nil {
				return err
			}
		}
		a.chunk.WriteRune(r)
		if r == '\n' {
			if a.line-a.chunkStart+1 >= a.policy.ChunkLines {
				if err := a.flush(a.line); err != nil {
					return err
				}
			}
			a.line++
			if a.chunk.Len() == 0 {
				a.chunkStart = a.line
			}
		}
	}
	return nil
}

func (a *extractedChunkAccumulator) finish() error {
	if err := a.flush(a.line); err != nil {
		return err
	}
	for _, item := range a.staged {
		if a.sink != nil {
			if err := a.sink(a.ctx, item); err != nil {
				return err
			}
		}
		a.entry.ChunkCount++
		a.manifest.Chunks++
		if a.progressSink != nil {
			a.progressSink(Progress{FilesSeen: a.manifest.FilesSeen, FilesIndexed: a.manifest.FilesIndexed, BytesRead: a.manifest.BytesRead, Chunks: a.manifest.Chunks, CurrentPath: a.entry.Path, Status: "extracting"})
		}
	}
	a.staged = nil
	return nil
}

func (a *extractedChunkAccumulator) flush(endLine int) error {
	if a.chunk.Len() == 0 {
		return nil
	}
	a.ordinal++
	text := a.chunk.String()
	textHash := sha256.Sum256([]byte(text))
	textDigest := hex.EncodeToString(textHash[:])
	idHash := sha256.Sum256([]byte(a.entry.Extractor + "\x00" + a.entry.Root + "\x00" + a.entry.Path + "\x00" + strconv.Itoa(a.chunkStart) + "\x00" + strconv.Itoa(endLine) + "\x00" + textDigest))
	item := Chunk{
		ID: hex.EncodeToString(idHash[:]), Root: a.entry.Root, Path: a.entry.Path, Ordinal: a.ordinal,
		StartLine: a.chunkStart, EndLine: endLine, Text: text, TextSHA256: textDigest,
		TrustLabel: TrustLabel, ExtractorVersion: a.entry.Extractor,
	}
	a.staged = append(a.staged, item)
	a.chunk.Reset()
	a.chunkStart = a.line
	return nil
}

func extractPDF(ctx context.Context, path string, out *extractedChunkAccumulator) error {
	f, reader, err := pdf.Open(path)
	if err != nil {
		return fmt.Errorf("open PDF: %w", err)
	}
	defer f.Close()
	pages := reader.NumPage()
	found := false
	for pageNum := 1; pageNum <= pages; pageNum++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		page := reader.Page(pageNum)
		if page.V.IsNull() || page.V.Key("Contents").Kind() == pdf.Null {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			return fmt.Errorf("extract PDF page %d: %w", pageNum, err)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		found = true
		if err := out.append(fmt.Sprintf("[PDF page %d of %d]\n%s\n", pageNum, pages, text)); err != nil {
			return err
		}
	}
	if !found {
		return nil
	}
	return nil
}

func extractZIPInventory(ctx context.Context, path string, policy ExtractorPolicy, out *extractedChunkAccumulator) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open ZIP: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > policy.MaxArchiveEntries {
		return errExpandedLimit
	}
	files := append([]*zip.File(nil), zr.File...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	if err := out.append(fmt.Sprintf("[ZIP inventory: %d entries]\n", len(files))); err != nil {
		return err
	}
	for _, member := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if member.Flags&0x1 != 0 {
			return fmt.Errorf("%w: %s", errEncrypted, member.Name)
		}
		if err := out.append(fmt.Sprintf("%s\tcompressed=%d\texpanded=%d\tmethod=%d\tdir=%t\n", filepath.ToSlash(member.Name), member.CompressedSize64, member.UncompressedSize64, member.Method, member.FileInfo().IsDir())); err != nil {
			return err
		}
	}
	return nil
}

func extractTARInventory(ctx context.Context, path string, policy ExtractorPolicy, out *extractedChunkAccumulator) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open TAR: %w", err)
	}
	defer f.Close()
	var source io.Reader = f
	lower := strings.ToLower(filepath.ToSlash(path))
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("open gzip TAR: %w", err)
		}
		defer gz.Close()
		source = gz
	}
	reader := tar.NewReader(source)
	if err := out.append("[TAR inventory]\n"); err != nil {
		return err
	}
	entries := 0
	var logicalBytes int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read TAR header: %w", err)
		}
		entries++
		if entries > policy.MaxArchiveEntries || header.Size < 0 || header.Size > policy.MaxExpandedBytes-logicalBytes {
			return errExpandedLimit
		}
		logicalBytes += header.Size
		line := fmt.Sprintf("name=%q\ttype=%s\tsize=%d\tmode=%#o", filepath.ToSlash(header.Name), tarTypeName(header.Typeflag), header.Size, header.Mode)
		if header.Linkname != "" {
			line += fmt.Sprintf("\tlink=%q", filepath.ToSlash(header.Linkname))
		}
		if err := out.append(line + "\n"); err != nil {
			return err
		}
	}
	if entries == 0 {
		return fmt.Errorf("TAR contains no entries")
	}
	return nil
}

func tarTypeName(flag byte) string {
	switch flag {
	case tar.TypeReg, tar.TypeRegA:
		return "file"
	case tar.TypeDir:
		return "directory"
	case tar.TypeSymlink:
		return "symlink"
	case tar.TypeLink:
		return "hardlink"
	case tar.TypeChar:
		return "character-device"
	case tar.TypeBlock:
		return "block-device"
	case tar.TypeFifo:
		return "fifo"
	default:
		return fmt.Sprintf("type-%d", flag)
	}
}

func extractSQLiteSchema(ctx context.Context, path string, policy ExtractorPolicy, out *extractedChunkAccumulator) error {
	slashPath := filepath.ToSlash(path)
	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	dsn := (&url.URL{Scheme: "file", Path: slashPath, RawQuery: "mode=ro&immutable=1"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open SQLite: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("open SQLite: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return fmt.Errorf("enable SQLite query-only mode: %w", err)
	}
	var pageCount, pageSize int64
	if err := db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return fmt.Errorf("read SQLite page count: %w", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return fmt.Errorf("read SQLite page size: %w", err)
	}
	type sqliteObject struct {
		kind, name, table string
	}
	rows, err := db.QueryContext(ctx, `
SELECT type, name, tbl_name
FROM sqlite_schema
WHERE type IN ('table','view','index','trigger') AND name NOT LIKE 'sqlite_%'
ORDER BY type, name
LIMIT ?`, policy.MaxArchiveEntries+1)
	if err != nil {
		return fmt.Errorf("read SQLite schema: %w", err)
	}
	var objects []sqliteObject
	for rows.Next() {
		var object sqliteObject
		if err := rows.Scan(&object.kind, &object.name, &object.table); err != nil {
			rows.Close()
			return fmt.Errorf("read SQLite schema row: %w", err)
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate SQLite schema rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close SQLite schema rows: %w", err)
	}
	if len(objects) > policy.MaxArchiveEntries {
		return errExpandedLimit
	}
	if err := out.append(fmt.Sprintf("[SQLite schema: objects=%d pages=%d page_size=%d]\n", len(objects), pageCount, pageSize)); err != nil {
		return err
	}
	metadataEntries := 0
	for _, object := range objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		metadataEntries++
		if metadataEntries > policy.MaxArchiveEntries {
			return errExpandedLimit
		}
		if err := out.append(fmt.Sprintf("object\ttype=%s\tname=%q\ttable=%q\n", object.kind, object.name, object.table)); err != nil {
			return err
		}
		if object.kind != "table" && object.kind != "view" {
			continue
		}
		columnRows, err := db.QueryContext(ctx, `SELECT cid, name, type, [notnull], pk FROM pragma_table_info(?) ORDER BY cid`, object.name)
		if err != nil {
			return fmt.Errorf("read SQLite columns for %q: %w", object.name, err)
		}
		for columnRows.Next() {
			var cid, notNull, primaryKey int
			var name, columnType string
			if err := columnRows.Scan(&cid, &name, &columnType, &notNull, &primaryKey); err != nil {
				columnRows.Close()
				return fmt.Errorf("read SQLite column for %q: %w", object.name, err)
			}
			metadataEntries++
			if metadataEntries > policy.MaxArchiveEntries {
				columnRows.Close()
				return errExpandedLimit
			}
			if err := out.append(fmt.Sprintf("column\ttable=%q\tcid=%d\tname=%q\ttype=%q\tnot_null=%t\tprimary_key=%t\n", object.name, cid, name, columnType, notNull != 0, primaryKey != 0)); err != nil {
				columnRows.Close()
				return err
			}
		}
		if err := columnRows.Err(); err != nil {
			columnRows.Close()
			return fmt.Errorf("iterate SQLite columns for %q: %w", object.name, err)
		}
		if err := columnRows.Close(); err != nil {
			return fmt.Errorf("close SQLite columns for %q: %w", object.name, err)
		}
	}
	return nil
}

func extractPEMetadata(ctx context.Context, path string, policy ExtractorPolicy, out *extractedChunkAccumulator) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("open PE: %w", err)
	}
	defer file.Close()
	if len(file.Sections) > policy.MaxArchiveEntries {
		return errExpandedLimit
	}
	if err := out.append(fmt.Sprintf("[PE metadata]\nmachine=%#x\tsections=%d\ttimestamp=%d\tcharacteristics=%#x\n", file.Machine, len(file.Sections), file.TimeDateStamp, file.Characteristics)); err != nil {
		return err
	}
	switch optional := file.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		if err := out.append(fmt.Sprintf("format=PE32\tentry=%#x\timage_base=%#x\tsubsystem=%d\tdll_characteristics=%#x\n", optional.AddressOfEntryPoint, optional.ImageBase, optional.Subsystem, optional.DllCharacteristics)); err != nil {
			return err
		}
	case *pe.OptionalHeader64:
		if err := out.append(fmt.Sprintf("format=PE32+\tentry=%#x\timage_base=%#x\tsubsystem=%d\tdll_characteristics=%#x\n", optional.AddressOfEntryPoint, optional.ImageBase, optional.Subsystem, optional.DllCharacteristics)); err != nil {
			return err
		}
	default:
		if err := out.append("format=PE-object\n"); err != nil {
			return err
		}
	}
	for _, section := range file.Sections {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := out.append(fmt.Sprintf("section\tname=%q\tvirtual_size=%d\traw_size=%d\tcharacteristics=%#x\n", section.Name, section.VirtualSize, section.Size, section.Characteristics)); err != nil {
			return err
		}
	}
	libraries, err := file.ImportedLibraries()
	if err != nil {
		return fmt.Errorf("read PE imports: %w", err)
	}
	if len(libraries)+len(file.Sections) > policy.MaxArchiveEntries {
		return errExpandedLimit
	}
	sort.Strings(libraries)
	for _, library := range libraries {
		if err := out.append(fmt.Sprintf("import\tlibrary=%q\n", library)); err != nil {
			return err
		}
	}
	return nil
}

func extractELFMetadata(ctx context.Context, path string, policy ExtractorPolicy, out *extractedChunkAccumulator) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("open ELF: %w", err)
	}
	defer file.Close()
	if len(file.Sections) > policy.MaxArchiveEntries {
		return errExpandedLimit
	}
	if err := out.append(fmt.Sprintf("[ELF metadata]\nclass=%s\tdata=%s\ttype=%s\tmachine=%s\tosabi=%s\tentry=%#x\n", file.Class, file.Data, file.Type, file.Machine, file.OSABI, file.Entry)); err != nil {
		return err
	}
	for _, section := range file.Sections {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := out.append(fmt.Sprintf("section\tname=%q\ttype=%s\tflags=%#x\taddress=%#x\tsize=%d\n", section.Name, section.Type, uint64(section.Flags), section.Addr, section.Size)); err != nil {
			return err
		}
	}
	libraries, err := file.ImportedLibraries()
	if err != nil {
		return fmt.Errorf("read ELF imports: %w", err)
	}
	if len(libraries)+len(file.Sections) > policy.MaxArchiveEntries {
		return errExpandedLimit
	}
	sort.Strings(libraries)
	for _, library := range libraries {
		if err := out.append(fmt.Sprintf("needed\tlibrary=%q\n", library)); err != nil {
			return err
		}
	}
	return nil
}

func extractOffice(ctx context.Context, path string, policy ExtractorPolicy, out *extractedChunkAccumulator) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open Office container: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > policy.MaxArchiveEntries {
		return errExpandedLimit
	}
	ext := strings.ToLower(filepath.Ext(path))
	files := append([]*zip.File(nil), zr.File...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	matched := 0
	for _, member := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !officeTextMember(ext, member.Name) {
			continue
		}
		if member.Flags&0x1 != 0 {
			return fmt.Errorf("%w: %s", errEncrypted, member.Name)
		}
		matched++
		if member.UncompressedSize64 > uint64(policy.MaxExpandedBytes) {
			return errExpandedLimit
		}
		if err := out.append(fmt.Sprintf("[Office part %s]\n", filepath.ToSlash(member.Name))); err != nil {
			return err
		}
		if err := extractXMLText(ctx, member, out); err != nil {
			return fmt.Errorf("extract %s: %w", member.Name, err)
		}
	}
	if matched == 0 {
		return fmt.Errorf("container has no recognised document text parts")
	}
	return nil
}

func officeTextMember(ext, name string) bool {
	name = filepath.ToSlash(strings.ToLower(name))
	switch ext {
	case ".docx":
		return name == "word/document.xml" || strings.HasPrefix(name, "word/header") && strings.HasSuffix(name, ".xml") || strings.HasPrefix(name, "word/footer") && strings.HasSuffix(name, ".xml") || name == "word/comments.xml"
	case ".pptx":
		return strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") || name == "ppt/presentation.xml"
	case ".xlsx":
		return name == "xl/sharedstrings.xml" || name == "xl/workbook.xml" || strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml")
	case ".ods":
		return name == "content.xml" || name == "meta.xml"
	default:
		return false
	}
}

func extractXMLText(ctx context.Context, member *zip.File, out *extractedChunkAccumulator) error {
	r, err := member.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	decoder := xml.NewDecoder(io.LimitReader(r, out.policy.MaxExpandedBytes+1))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		chars, ok := token.(xml.CharData)
		if !ok {
			continue
		}
		text := strings.Join(strings.Fields(string(chars)), " ")
		if text == "" {
			continue
		}
		if err := out.append(text + "\n"); err != nil {
			return err
		}
	}
	return nil
}
