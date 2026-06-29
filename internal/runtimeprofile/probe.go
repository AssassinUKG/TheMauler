package runtimeprofile

import (
	"bytes"
	"io"
	"os"
	"strings"
)

// ModelMTPInfo is what we learn about a concrete model artifact (as opposed to
// its family). It is the robust signal that drives auto-enabling MTP: a family
// can be MTP-capable while a specific GGUF is not (and vice versa).
type ModelMTPInfo struct {
	// HasMTPHeads is true when the artifact carries multi-token-prediction /
	// next-N prediction heads (so llama.cpp --spec-type draft-mtp can self-draft).
	HasMTPHeads bool
	// Confident is true only when we actually inspected the artifact. When false,
	// callers should fall back to weaker signals (name/registry).
	Confident bool
	// Detail is a short human-readable explanation for logs and the UI tooltip.
	Detail string
}

// ggufMagic is the 4-byte little-endian magic at the start of every GGUF file.
var ggufMagic = []byte{'G', 'G', 'U', 'F'}

// mtpNeedles are lowercase ASCII markers that appear in the GGUF metadata-KV /
// tensor-info region of an MTP artifact. These are distinctive enough that a
// substring hit is a reliable positive without a full schema parse:
//   - "nextn"        — Qwen/DeepSeek next-N prediction layers (tensor + KV keys)
//   - "multi_token"  — multi-token-prediction metadata keys
//   - "mtp"          — head/tensor names and arch suffixes
var mtpNeedles = []string{"nextn", "multi_token", "mtp"}

// ggufProbeBytes caps how much of the file we read. The metadata-KV and
// tensor-info sections (which carry every tensor name) live at the start of the
// file, well before the tensor data blob — 8 MiB comfortably covers even large
// models' headers without reading gigabytes.
const ggufProbeBytes = 8 << 20

// ProbeGGUFForMTP inspects a local GGUF file's header region for MTP heads.
//
// It never panics and is safe to call on any path: unreadable or non-GGUF files
// return Confident=false so the caller falls back to name/registry signals.
func ProbeGGUFForMTP(path string) ModelMTPInfo {
	path = strings.TrimSpace(path)
	if path == "" {
		return ModelMTPInfo{Detail: "no model path available to probe"}
	}
	f, err := os.Open(path)
	if err != nil {
		return ModelMTPInfo{Detail: "could not open model file: " + err.Error()}
	}
	defer f.Close()

	buf := make([]byte, ggufProbeBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return ModelMTPInfo{Detail: "could not read model header: " + err.Error()}
	}
	header := buf[:n]

	if n < len(ggufMagic) || !bytes.Equal(header[:len(ggufMagic)], ggufMagic) {
		// Not a GGUF (could be a remote/HF id, a directory, or a different
		// format). We genuinely don't know — let the caller fall back.
		return ModelMTPInfo{Detail: "not a GGUF artifact; cannot probe heads"}
	}

	lower := bytes.ToLower(header)
	for _, needle := range mtpNeedles {
		if bytes.Contains(lower, []byte(needle)) {
			return ModelMTPInfo{
				HasMTPHeads: true,
				Confident:   true,
				Detail:      "GGUF header advertises MTP heads (matched \"" + needle + "\")",
			}
		}
	}
	return ModelMTPInfo{
		HasMTPHeads: false,
		Confident:   true,
		Detail:      "GGUF header has no MTP/next-N markers",
	}
}
