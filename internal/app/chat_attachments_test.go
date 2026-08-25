package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareChatAttachmentPathKeepsLargeFileOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swagger.json")
	data := make([]byte, 250_000)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	attachment, err := prepareChatAttachmentPath(`"` + path + `"`)
	if err != nil {
		t.Fatal(err)
	}
	if attachment.Name != "swagger.json" || attachment.Kind != "document" || attachment.MIME != "application/json" {
		t.Fatalf("unexpected attachment metadata: %#v", attachment)
	}
	if attachment.Size != int64(len(data)) || attachment.Content != "" || attachment.Truncated {
		t.Fatalf("large attachment must remain metadata-only: %#v", attachment)
	}
	want, _ := filepath.Abs(path)
	if attachment.Path != filepath.Clean(want) {
		t.Fatalf("path = %q, want %q", attachment.Path, filepath.Clean(want))
	}
}

func TestPrepareChatAttachmentPathRejectsMissingFilesAndFolders(t *testing.T) {
	dir := t.TempDir()
	if _, err := prepareChatAttachmentPath(dir); err == nil {
		t.Fatal("expected folder attachment to be rejected")
	}
	if _, err := prepareChatAttachmentPath(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("expected missing attachment to be rejected")
	}
}

func TestPrepareChatAttachmentPathAcceptsFileURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := "file:///" + filepath.ToSlash(path)
	if filepath.VolumeName(path) == "" {
		raw = "file://" + filepath.ToSlash(path)
	}
	attachment, err := prepareChatAttachmentPath(raw)
	if err != nil {
		t.Fatal(err)
	}
	if attachment.Name != "notes.txt" || attachment.Content != "" {
		t.Fatalf("unexpected file URL attachment: %#v", attachment)
	}
}
