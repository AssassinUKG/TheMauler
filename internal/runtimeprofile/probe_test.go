package runtimeprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGGUF(t *testing.T, name string, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	contents := append([]byte("GGUF"), body...)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestProbeGGUFForMTP_DetectsNextNHeads(t *testing.T) {
	path := writeGGUF(t, "model.gguf", []byte("...blk.0.nextn.weight...padding..."))
	info := ProbeGGUFForMTP(path)
	if !info.Confident || !info.HasMTPHeads {
		t.Fatalf("expected confident MTP detection, got %+v", info)
	}
}

func TestProbeGGUFForMTP_NoMarkersIsConfidentNegative(t *testing.T) {
	path := writeGGUF(t, "plain.gguf", []byte("...blk.0.attn.weight...output.weight..."))
	info := ProbeGGUFForMTP(path)
	if !info.Confident {
		t.Fatalf("expected confident result for readable GGUF, got %+v", info)
	}
	if info.HasMTPHeads {
		t.Fatalf("expected no MTP heads, got %+v", info)
	}
}

func TestProbeGGUFForMTP_NonGGUFIsNotConfident(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notgguf.bin")
	if err := os.WriteFile(path, []byte("this is not a gguf file with nextn text"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	info := ProbeGGUFForMTP(path)
	if info.Confident {
		t.Fatalf("non-GGUF must not be confident, got %+v", info)
	}
}

func TestProbeGGUFForMTP_MissingPathIsNotConfident(t *testing.T) {
	info := ProbeGGUFForMTP(filepath.Join(t.TempDir(), "does-not-exist.gguf"))
	if info.Confident || info.HasMTPHeads {
		t.Fatalf("missing file must be non-confident, got %+v", info)
	}
	if ProbeGGUFForMTP("").Confident {
		t.Fatalf("empty path must be non-confident")
	}
}
