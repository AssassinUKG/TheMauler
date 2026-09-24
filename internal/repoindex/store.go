package repoindex

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Store struct{ db *sql.DB }

type IndexResult struct {
	GenerationID string   `json:"generation_id"`
	Manifest     Manifest `json:"manifest"`
}

type GenerationStatus struct {
	ID             string `json:"id"`
	PolicyDigest   string `json:"policy_digest"`
	ManifestDigest string `json:"manifest_digest"`
	Status         string `json:"status"`
	Complete       bool   `json:"complete"`
	FilesSeen      int    `json:"files_seen"`
	FilesIndexed   int    `json:"files_indexed"`
	BytesRead      int64  `json:"bytes_read"`
	ChunkCount     int    `json:"chunk_count"`
	Error          string `json:"error,omitempty"`
	StartedAt      string `json:"started_at"`
	CompletedAt    string `json:"completed_at,omitempty"`
}

type SearchHit struct {
	GenerationID string  `json:"generation_id"`
	ChunkID      string  `json:"chunk_id"`
	Root         string  `json:"root"`
	Path         string  `json:"path"`
	StartLine    int     `json:"start_line"`
	EndLine      int     `json:"end_line"`
	Text         string  `json:"text"`
	TextSHA256   string  `json:"text_sha256"`
	FileSHA256   string  `json:"file_sha256"`
	TrustLabel   string  `json:"trust_label"`
	Rank         float64 `json:"rank"`
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("repoindex: database is required")
	}
	return &Store{db: db}, nil
}

// Index writes a replacement generation in one transaction. The active
// generation pointer changes only with the final commit, so a failed or
// cancelled replacement cannot displace the last complete index.
func (s *Store) Index(ctx context.Context, policy IndexPolicy) (IndexResult, error) {
	return s.IndexWithProgress(ctx, policy, nil)
}

// IndexWithProgress writes a replacement generation while exposing scan
// counters outside the SQLite transaction. The active pointer still changes
// only after the complete generation commits.
func (s *Store) IndexWithProgress(ctx context.Context, policy IndexPolicy, progressSink ProgressSink) (IndexResult, error) {
	return s.indexWithProgress(ctx, policy, progressSink, "", nil)
}

// RefreshWithProgress creates a new immutable generation while reusing chunks
// only for files whose current source SHA-256 matches the prior active manifest.
func (s *Store) RefreshWithProgress(ctx context.Context, policy IndexPolicy, progressSink ProgressSink) (IndexResult, error) {
	active, err := s.ActiveGeneration(ctx, policy)
	if err != nil {
		if err == sql.ErrNoRows {
			return s.IndexWithProgress(ctx, policy, progressSink)
		}
		return IndexResult{}, err
	}
	previous, err := s.Manifest(ctx, active.ID)
	if err != nil {
		return IndexResult{}, err
	}
	return s.indexWithProgress(ctx, policy, progressSink, active.ID, &previous)
}

func (s *Store) Refresh(ctx context.Context, policy IndexPolicy) (IndexResult, error) {
	return s.RefreshWithProgress(ctx, policy, nil)
}

func (s *Store) indexWithProgress(ctx context.Context, policy IndexPolicy, progressSink ProgressSink, previousGeneration string, previous *Manifest) (IndexResult, error) {
	normalized, _, err := normalizePolicy(policy)
	if err != nil {
		return IndexResult{}, err
	}
	policyJSON, _ := json.Marshal(normalized)
	generationID, err := newGenerationID()
	if err != nil {
		return IndexResult{}, err
	}
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		manifest := Manifest{Version: 1, PolicyDigest: policyDigest(normalized), Cancelled: errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)}
		finalizeManifest(&manifest)
		s.recordFailedGeneration(generationID, normalized, manifest, startedAt, err)
		return IndexResult{GenerationID: generationID, Manifest: manifest}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO repo_index_generations(id, policy_digest, status, policy_json, started_at)
VALUES (?, ?, 'scanning', ?, ?)`, generationID, policyDigest(normalized), string(policyJSON), startedAt); err != nil {
		_ = tx.Rollback()
		return IndexResult{}, err
	}

	chunkSink := func(ctx context.Context, chunk Chunk) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO repo_index_chunks(
  generation_id, chunk_id, root, path, ordinal, start_line, end_line,
  text, text_sha256, trust_label, extractor_version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			generationID, chunk.ID, chunk.Root, chunk.Path, chunk.Ordinal, chunk.StartLine, chunk.EndLine,
			chunk.Text, chunk.TextSHA256, chunk.TrustLabel, chunk.ExtractorVersion)
		return err
	}
	var manifest Manifest
	var incremental IncrementalStats
	var scanErr error
	if previous != nil && previousGeneration != "" {
		manifest, incremental, scanErr = ScanIncrementalWithProgress(ctx, normalized, *previous, chunkSink, progressSink)
	} else {
		manifest, scanErr = ScanWithProgress(ctx, normalized, chunkSink, progressSink)
	}
	if scanErr != nil {
		_ = tx.Rollback()
		s.recordFailedGeneration(generationID, normalized, manifest, startedAt, scanErr)
		return IndexResult{GenerationID: generationID, Manifest: manifest}, scanErr
	}
	for _, reused := range incremental.Reused {
		result, err := tx.ExecContext(ctx, `
INSERT INTO repo_index_chunks(
  generation_id, chunk_id, root, path, ordinal, start_line, end_line,
  text, text_sha256, trust_label, extractor_version
)
SELECT ?, chunk_id, root, path, ordinal, start_line, end_line,
       text, text_sha256, trust_label, extractor_version
FROM repo_index_chunks
WHERE generation_id=? AND root=? AND path=?
ORDER BY ordinal`, generationID, previousGeneration, reused.Root, reused.Path)
		if err != nil {
			_ = tx.Rollback()
			return IndexResult{GenerationID: generationID, Manifest: manifest}, err
		}
		copied, _ := result.RowsAffected()
		for _, entry := range manifest.Entries {
			if entry.Root == reused.Root && entry.Path == reused.Path && copied != int64(entry.ChunkCount) {
				_ = tx.Rollback()
				return IndexResult{GenerationID: generationID, Manifest: manifest}, fmt.Errorf("repoindex: reused chunk count mismatch for %s: copied %d, expected %d", reused.Path, copied, entry.ChunkCount)
			}
		}
	}
	for ordinal, root := range normalized.Roots {
		if _, err := tx.ExecContext(ctx, `INSERT INTO repo_index_roots(generation_id, ordinal, root) VALUES (?, ?, ?)`, generationID, ordinal, root); err != nil {
			_ = tx.Rollback()
			return IndexResult{GenerationID: generationID, Manifest: manifest}, err
		}
	}
	for _, entry := range manifest.Entries {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO repo_index_files(
  generation_id, root, path, language, encoding, extractor, size, mod_time,
  sha256, status, detail, chunk_count
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			generationID, entry.Root, entry.Path, entry.Language, entry.Encoding, entry.Extractor,
			entry.Size, entry.ModTime.Format(time.RFC3339Nano), entry.SHA256, entry.Status, entry.Detail, entry.ChunkCount); err != nil {
			_ = tx.Rollback()
			return IndexResult{GenerationID: generationID, Manifest: manifest}, err
		}
	}
	manifestJSON, _ := json.Marshal(manifest)
	completedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
UPDATE repo_index_generations SET
  manifest_digest=?, status='complete', complete=1, manifest_json=?, files_seen=?,
  files_indexed=?, bytes_read=?, chunk_count=?, completed_at=?
WHERE id=?`, manifest.Digest, string(manifestJSON), manifest.FilesSeen, manifest.FilesIndexed,
		manifest.BytesRead, manifest.Chunks, completedAt, generationID); err != nil {
		_ = tx.Rollback()
		return IndexResult{GenerationID: generationID, Manifest: manifest}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO repo_index_active(policy_digest, generation_id, updated_at) VALUES (?, ?, ?)
ON CONFLICT(policy_digest) DO UPDATE SET generation_id=excluded.generation_id, updated_at=excluded.updated_at`,
		manifest.PolicyDigest, generationID, completedAt); err != nil {
		_ = tx.Rollback()
		return IndexResult{GenerationID: generationID, Manifest: manifest}, err
	}
	if err := tx.Commit(); err != nil {
		return IndexResult{GenerationID: generationID, Manifest: manifest}, err
	}
	return IndexResult{GenerationID: generationID, Manifest: manifest}, nil
}

// HasChanges performs a metadata-only comparison suitable for watch polling.
// A positive result must still pass Refresh's SHA verification before reuse.
func (s *Store) HasChanges(ctx context.Context, policy IndexPolicy) (bool, error) {
	active, err := s.ActiveGeneration(ctx, policy)
	if err != nil {
		return false, err
	}
	manifest, err := s.Manifest(ctx, active.ID)
	if err != nil {
		return false, err
	}
	stamps, err := Snapshot(ctx, policy)
	if err != nil {
		return false, err
	}
	if len(stamps) != len(manifest.Entries) {
		return true, nil
	}
	previous := make(map[string]ManifestEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		previous[manifestEntryKey(entry.Root, entry.Path)] = entry
	}
	for _, stamp := range stamps {
		entry, ok := previous[manifestEntryKey(stamp.Root, stamp.Path)]
		if !ok || entry.Size != stamp.Size || !entry.ModTime.Equal(stamp.ModTime) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) recordFailedGeneration(id string, policy IndexPolicy, manifest Manifest, startedAt string, cause error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	policyJSON, _ := json.Marshal(policy)
	manifestJSON, _ := json.Marshal(manifest)
	status := "failed"
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		status = "cancelled"
	}
	_, _ = s.db.ExecContext(ctx, `
INSERT INTO repo_index_generations(
  id, policy_digest, manifest_digest, status, complete, policy_json, manifest_json,
  files_seen, files_indexed, bytes_read, chunk_count, error, started_at, completed_at
) VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, policyDigest(policy), manifest.Digest, status, string(policyJSON), string(manifestJSON),
		manifest.FilesSeen, manifest.FilesIndexed, manifest.BytesRead, manifest.Chunks, cause.Error(),
		startedAt, time.Now().UTC().Format(time.RFC3339Nano))
}

func (s *Store) ActiveGeneration(ctx context.Context, policy IndexPolicy) (GenerationStatus, error) {
	normalized, _, err := normalizePolicy(policy)
	if err != nil {
		return GenerationStatus{}, err
	}
	var id string
	if err := s.db.QueryRowContext(ctx, `SELECT generation_id FROM repo_index_active WHERE policy_digest=?`, policyDigest(normalized)).Scan(&id); err != nil {
		return GenerationStatus{}, err
	}
	return s.Generation(ctx, id)
}

func (s *Store) Generation(ctx context.Context, id string) (GenerationStatus, error) {
	var status GenerationStatus
	var complete int
	err := s.db.QueryRowContext(ctx, `
SELECT id, policy_digest, manifest_digest, status, complete, files_seen, files_indexed,
       bytes_read, chunk_count, error, started_at, completed_at
FROM repo_index_generations WHERE id=?`, id).Scan(
		&status.ID, &status.PolicyDigest, &status.ManifestDigest, &status.Status, &complete,
		&status.FilesSeen, &status.FilesIndexed, &status.BytesRead, &status.ChunkCount,
		&status.Error, &status.StartedAt, &status.CompletedAt,
	)
	status.Complete = complete != 0
	return status, err
}

// Manifest returns the immutable manifest stored with a completed or failed
// generation. Source bodies remain in the chunk table and are not copied into
// the manifest.
func (s *Store) Manifest(ctx context.Context, id string) (Manifest, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT manifest_json FROM repo_index_generations WHERE id=?`, strings.TrimSpace(id)).Scan(&raw); err != nil {
		return Manifest{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return Manifest{}, fmt.Errorf("repoindex: generation %q has no manifest", id)
	}
	var manifest Manifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return Manifest{}, fmt.Errorf("repoindex: decode manifest %q: %w", id, err)
	}
	return manifest, nil
}

func (s *Store) Search(ctx context.Context, generationID, query string, limit int) ([]SearchHit, error) {
	generationID = strings.TrimSpace(generationID)
	if generationID == "" {
		return nil, fmt.Errorf("repoindex: generation id is required")
	}
	fts := ftsQuery(query)
	if fts == "" {
		return nil, fmt.Errorf("repoindex: search query is empty")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT c.generation_id, c.chunk_id, c.root, c.path, c.start_line, c.end_line,
       c.text, c.text_sha256, f.sha256, c.trust_label, bm25(repo_index_chunks_fts)
FROM repo_index_chunks_fts
JOIN repo_index_chunks c ON c.chunk_pk = repo_index_chunks_fts.rowid
JOIN repo_index_files f ON f.generation_id=c.generation_id AND f.root=c.root AND f.path=c.path
WHERE repo_index_chunks_fts MATCH ? AND c.generation_id=? AND f.status='indexed'
ORDER BY bm25(repo_index_chunks_fts), c.path, c.ordinal
LIMIT ?`, fts, generationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []SearchHit
	for rows.Next() {
		var hit SearchHit
		if err := rows.Scan(&hit.GenerationID, &hit.ChunkID, &hit.Root, &hit.Path, &hit.StartLine,
			&hit.EndLine, &hit.Text, &hit.TextSHA256, &hit.FileSHA256, &hit.TrustLabel, &hit.Rank); err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func ftsQuery(query string) string {
	var terms []string
	for _, term := range strings.Fields(query) {
		term = strings.Trim(term, `"'()[]{}:;,+-*?!`)
		if term == "" {
			continue
		}
		terms = append(terms, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(terms, " AND ")
}

func newGenerationID() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("repoindex: generation id: %w", err)
	}
	return "idx-" + hex.EncodeToString(random[:]), nil
}
