package repoindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	SplitReviewPlanVersion = 1
	MaxSplitReviewShards   = 16
)

// ReviewEvidenceRef is the smallest immutable source reference a child review
// may cite. The source body is deliberately absent: task context is assembled
// separately and remains bounded.
type ReviewEvidenceRef struct {
	ChunkID    string `json:"chunk_id"`
	Root       string `json:"root"`
	Path       string `json:"path"`
	FileSHA256 string `json:"file_sha256"`
	TextSHA256 string `json:"text_sha256"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
}

type ReviewChunkContent struct {
	ReviewEvidenceRef
	Text       string `json:"text"`
	TrustLabel string `json:"trust_label"`
}

type ReviewShard struct {
	ID             string              `json:"id"`
	Digest         string              `json:"digest"`
	Ordinal        int                 `json:"ordinal"`
	FileCount      int                 `json:"file_count"`
	ChunkCount     int                 `json:"chunk_count"`
	Bytes          int64               `json:"bytes"`
	Paths          []string            `json:"paths"`
	Languages      []string            `json:"languages,omitempty"`
	EvidenceDigest string              `json:"evidence_digest"`
	EvidenceRefs   []ReviewEvidenceRef `json:"evidence_refs"`
}

// SplitReviewPlan is deterministic for one immutable repository generation.
// Created-at timestamps and model state are intentionally excluded.
type SplitReviewPlan struct {
	Version         int           `json:"version"`
	GenerationID    string        `json:"generation_id"`
	ManifestDigest  string        `json:"manifest_digest"`
	PlanDigest      string        `json:"plan_digest"`
	RequestedShards int           `json:"requested_shards"`
	ShardCount      int           `json:"shard_count"`
	FileCount       int           `json:"file_count"`
	ChunkCount      int           `json:"chunk_count"`
	Bytes           int64         `json:"bytes"`
	Shards          []ReviewShard `json:"shards"`
}

type ReviewShardPreview struct {
	ID             string   `json:"id"`
	Digest         string   `json:"digest"`
	Ordinal        int      `json:"ordinal"`
	FileCount      int      `json:"file_count"`
	ChunkCount     int      `json:"chunk_count"`
	Bytes          int64    `json:"bytes"`
	Paths          []string `json:"paths"`
	Languages      []string `json:"languages,omitempty"`
	EvidenceDigest string   `json:"evidence_digest"`
}

type SplitReviewPreview struct {
	Version         int                  `json:"version"`
	GenerationID    string               `json:"generation_id"`
	ManifestDigest  string               `json:"manifest_digest"`
	PlanDigest      string               `json:"plan_digest"`
	RequestedShards int                  `json:"requested_shards"`
	ShardCount      int                  `json:"shard_count"`
	FileCount       int                  `json:"file_count"`
	ChunkCount      int                  `json:"chunk_count"`
	Bytes           int64                `json:"bytes"`
	Shards          []ReviewShardPreview `json:"shards"`
}

type ReviewFindingEvidence struct {
	ChunkID    string `json:"chunk_id"`
	FileSHA256 string `json:"file_sha256"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
}

type ReviewFinding struct {
	ID         string                  `json:"id,omitempty"`
	Claim      string                  `json:"claim"`
	Severity   string                  `json:"severity"`
	Confidence string                  `json:"confidence"`
	Evidence   []ReviewFindingEvidence `json:"evidence"`
	FollowUp   string                  `json:"follow_up,omitempty"`
}

type ReviewSubmission struct {
	ShardID  string          `json:"shard_id"`
	Claimant string          `json:"claimant"`
	Findings []ReviewFinding `json:"findings"`
}

type MergedReviewFinding struct {
	ReviewFinding
	State         string   `json:"state"`
	EvidenceValid bool     `json:"evidence_valid"`
	ShardIDs      []string `json:"shard_ids"`
	Claimants     []string `json:"claimants"`
}

type ReviewMergeDecision struct {
	ShardID   string `json:"shard_id"`
	Claimant  string `json:"claimant"`
	FindingID string `json:"finding_id,omitempty"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
}

type ReviewMergeResult struct {
	PlanDigest string                `json:"plan_digest"`
	Findings   []MergedReviewFinding `json:"findings"`
	Decisions  []ReviewMergeDecision `json:"decisions"`
	Accepted   int                   `json:"accepted"`
	Rejected   int                   `json:"rejected"`
	Duplicates int                   `json:"duplicates"`
	Conflicts  int                   `json:"conflicts"`
}

// ReviewConflictClaim is a deterministic, model-independent projection of one
// merged finding that disagrees with another finding over the same immutable
// evidence. ClaimID is derived from normalized claim text plus that evidence;
// child-supplied finding IDs are deliberately not trusted as identity.
type ReviewConflictClaim struct {
	ClaimID    string   `json:"claim_id"`
	Claim      string   `json:"claim"`
	Severity   string   `json:"severity"`
	Confidence string   `json:"confidence"`
	ShardIDs   []string `json:"shard_ids"`
	Claimants  []string `json:"claimants"`
}

// ReviewConflictCase is the sealed input for a later independent adjudication
// pass. It contains no source body: the controller resolves Evidence against
// the already sealed plan and exposes only those exact chunks.
type ReviewConflictCase struct {
	ID             string                  `json:"id"`
	EvidenceDigest string                  `json:"evidence_digest"`
	Evidence       []ReviewFindingEvidence `json:"evidence"`
	Claims         []ReviewConflictClaim   `json:"claims"`
}

type reviewFile struct {
	Root     string
	Path     string
	Language string
	Size     int64
	SHA256   string
	Evidence []ReviewEvidenceRef
}

type reviewGroup struct {
	Key    string
	Files  []reviewFile
	Bytes  int64
	Weight int64
}

func (s *Store) BuildSplitReviewPlan(ctx context.Context, generationID string, requestedShards int) (SplitReviewPlan, error) {
	if s == nil || s.db == nil {
		return SplitReviewPlan{}, fmt.Errorf("repoindex: database is required")
	}
	generationID = strings.TrimSpace(generationID)
	if generationID == "" {
		return SplitReviewPlan{}, fmt.Errorf("repoindex: generation id is required")
	}
	if requestedShards < 1 || requestedShards > MaxSplitReviewShards {
		return SplitReviewPlan{}, fmt.Errorf("repoindex: shard count must be between 1 and %d", MaxSplitReviewShards)
	}
	generation, err := s.Generation(ctx, generationID)
	if err != nil {
		return SplitReviewPlan{}, err
	}
	if !generation.Complete || generation.Status != "complete" {
		return SplitReviewPlan{}, fmt.Errorf("repoindex: generation %q is not complete", generationID)
	}
	manifest, err := s.Manifest(ctx, generationID)
	if err != nil {
		return SplitReviewPlan{}, err
	}
	if !manifest.Complete || manifest.Digest == "" || manifest.Digest != generation.ManifestDigest {
		return SplitReviewPlan{}, fmt.Errorf("repoindex: generation %q manifest is not sealed", generationID)
	}
	files, err := s.reviewFiles(ctx, generationID)
	if err != nil {
		return SplitReviewPlan{}, err
	}
	if len(files) == 0 {
		return SplitReviewPlan{}, fmt.Errorf("repoindex: generation %q has no indexed files", generationID)
	}
	return buildSplitReviewPlan(generationID, manifest.Digest, files, requestedShards)
}

func (s *Store) reviewFiles(ctx context.Context, generationID string) ([]reviewFile, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT f.root, f.path, f.language, f.size, f.sha256,
       c.chunk_id, c.text_sha256, c.start_line, c.end_line
FROM repo_index_files f
LEFT JOIN repo_index_chunks c ON c.generation_id=f.generation_id AND c.root=f.root AND c.path=f.path
WHERE f.generation_id=? AND f.status='indexed'
ORDER BY f.root, f.path, c.ordinal`, generationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []reviewFile
	var current *reviewFile
	for rows.Next() {
		var root, path, language, fileSHA string
		var chunkID, textSHA sql.NullString
		var size int64
		var startLine, endLine sql.NullInt64
		if err := rows.Scan(&root, &path, &language, &size, &fileSHA, &chunkID, &textSHA, &startLine, &endLine); err != nil {
			return nil, err
		}
		if current == nil || current.Root != root || current.Path != path {
			files = append(files, reviewFile{Root: root, Path: path, Language: language, Size: size, SHA256: fileSHA})
			current = &files[len(files)-1]
		}
		if chunkID.Valid {
			current.Evidence = append(current.Evidence, ReviewEvidenceRef{
				ChunkID: chunkID.String, Root: root, Path: path, FileSHA256: fileSHA,
				TextSHA256: textSHA.String, StartLine: int(startLine.Int64), EndLine: int(endLine.Int64),
			})
		}
	}
	return files, rows.Err()
}

// ReadReviewChunks returns source only when the requested ids belong to the
// exact sealed allowlist and every stored hash/line reference still matches.
func (s *Store) ReadReviewChunks(ctx context.Context, generationID string, allowed []ReviewEvidenceRef, chunkIDs []string) ([]ReviewChunkContent, error) {
	if len(chunkIDs) == 0 {
		return nil, fmt.Errorf("repoindex: at least one chunk id is required")
	}
	if len(chunkIDs) > 8 {
		return nil, fmt.Errorf("repoindex: at most 8 chunks may be read at once")
	}
	allow := make(map[string]ReviewEvidenceRef, len(allowed))
	for _, ref := range allowed {
		allow[ref.ChunkID] = ref
	}
	seen := map[string]struct{}{}
	out := make([]ReviewChunkContent, 0, len(chunkIDs))
	for _, rawID := range chunkIDs {
		id := strings.TrimSpace(rawID)
		ref, ok := allow[id]
		if !ok {
			return nil, fmt.Errorf("repoindex: chunk %q is outside the sealed shard", id)
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		var got ReviewChunkContent
		err := s.db.QueryRowContext(ctx, `
SELECT c.chunk_id, c.root, c.path, f.sha256, c.text_sha256, c.start_line, c.end_line,
       c.text, c.trust_label
FROM repo_index_chunks c
JOIN repo_index_files f ON f.generation_id=c.generation_id AND f.root=c.root AND f.path=c.path
WHERE c.generation_id=? AND c.chunk_id=? AND f.status='indexed'`, generationID, id).Scan(
			&got.ChunkID, &got.Root, &got.Path, &got.FileSHA256, &got.TextSHA256,
			&got.StartLine, &got.EndLine, &got.Text, &got.TrustLabel,
		)
		if err != nil {
			return nil, err
		}
		if got.Root != ref.Root || got.Path != ref.Path || got.FileSHA256 != ref.FileSHA256 ||
			got.TextSHA256 != ref.TextSHA256 || got.StartLine != ref.StartLine || got.EndLine != ref.EndLine {
			return nil, fmt.Errorf("repoindex: sealed evidence changed for chunk %q", id)
		}
		out = append(out, got)
	}
	return out, nil
}

// SearchReviewEvidence searches only chunk IDs owned by the sealed shard. The
// allowlist is batched to keep SQLite parameter counts bounded.
func (s *Store) SearchReviewEvidence(ctx context.Context, generationID string, allowed []ReviewEvidenceRef, query string, limit int) ([]SearchHit, error) {
	fts := ftsQuery(query)
	if fts == "" {
		return nil, fmt.Errorf("repoindex: search query is empty")
	}
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}
	if len(allowed) == 0 {
		return []SearchHit{}, nil
	}
	allow := make(map[string]ReviewEvidenceRef, len(allowed))
	for _, ref := range allowed {
		allow[ref.ChunkID] = ref
	}
	const batchSize = 200
	var hits []SearchHit
	seen := map[string]struct{}{}
	for start := 0; start < len(allowed); start += batchSize {
		end := start + batchSize
		if end > len(allowed) {
			end = len(allowed)
		}
		placeholders := make([]string, end-start)
		args := make([]any, 0, 3+end-start)
		args = append(args, fts, generationID)
		for i, ref := range allowed[start:end] {
			placeholders[i] = "?"
			args = append(args, ref.ChunkID)
		}
		args = append(args, limit)
		rows, err := s.db.QueryContext(ctx, `
SELECT c.generation_id, c.chunk_id, c.root, c.path, c.start_line, c.end_line,
       c.text, c.text_sha256, f.sha256, c.trust_label, bm25(repo_index_chunks_fts)
FROM repo_index_chunks_fts
JOIN repo_index_chunks c ON c.chunk_pk = repo_index_chunks_fts.rowid
JOIN repo_index_files f ON f.generation_id=c.generation_id AND f.root=c.root AND f.path=c.path
WHERE repo_index_chunks_fts MATCH ? AND c.generation_id=? AND f.status='indexed'
  AND c.chunk_id IN (`+strings.Join(placeholders, ",")+`)
ORDER BY bm25(repo_index_chunks_fts), c.path, c.ordinal
LIMIT ?`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var hit SearchHit
			if err := rows.Scan(&hit.GenerationID, &hit.ChunkID, &hit.Root, &hit.Path, &hit.StartLine,
				&hit.EndLine, &hit.Text, &hit.TextSHA256, &hit.FileSHA256, &hit.TrustLabel, &hit.Rank); err != nil {
				rows.Close()
				return nil, err
			}
			ref, owned := allow[hit.ChunkID]
			if !owned || hit.Root != ref.Root || hit.Path != ref.Path || hit.FileSHA256 != ref.FileSHA256 ||
				hit.TextSHA256 != ref.TextSHA256 || hit.StartLine != ref.StartLine || hit.EndLine != ref.EndLine {
				rows.Close()
				return nil, fmt.Errorf("repoindex: sealed evidence changed for search hit %q", hit.ChunkID)
			}
			if _, duplicate := seen[hit.ChunkID]; !duplicate {
				seen[hit.ChunkID] = struct{}{}
				hits = append(hits, hit)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Rank != hits[j].Rank {
			return hits[i].Rank < hits[j].Rank
		}
		if hits[i].Path != hits[j].Path {
			return hits[i].Path < hits[j].Path
		}
		return hits[i].StartLine < hits[j].StartLine
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func buildSplitReviewPlan(generationID, manifestDigest string, files []reviewFile, requested int) (SplitReviewPlan, error) {
	groups := groupReviewFiles(files)
	actual := requested
	if actual > len(files) {
		actual = len(files)
	}
	if len(groups) < actual {
		groups = make([]reviewGroup, 0, len(files))
		for _, file := range files {
			groups = append(groups, newReviewGroup(file.Root+"\x00"+file.Path, []reviewFile{file}))
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Weight != groups[j].Weight {
			return groups[i].Weight > groups[j].Weight
		}
		return groups[i].Key < groups[j].Key
	})
	shardFiles := make([][]reviewFile, actual)
	shardWeights := make([]int64, actual)
	for _, group := range groups {
		selected := 0
		for i := 1; i < actual; i++ {
			if shardWeights[i] < shardWeights[selected] {
				selected = i
			}
		}
		shardFiles[selected] = append(shardFiles[selected], group.Files...)
		shardWeights[selected] += group.Weight
	}
	plan := SplitReviewPlan{Version: SplitReviewPlanVersion, GenerationID: generationID, ManifestDigest: manifestDigest, RequestedShards: requested}
	for ordinal, assigned := range shardFiles {
		sort.Slice(assigned, func(i, j int) bool { return reviewFileKey(assigned[i]) < reviewFileKey(assigned[j]) })
		shard := ReviewShard{Ordinal: ordinal + 1}
		languageSet := map[string]struct{}{}
		for _, file := range assigned {
			shard.Paths = append(shard.Paths, displayReviewPath(file))
			shard.FileCount++
			shard.Bytes += file.Size
			shard.EvidenceRefs = append(shard.EvidenceRefs, file.Evidence...)
			if language := strings.TrimSpace(file.Language); language != "" {
				languageSet[language] = struct{}{}
			}
		}
		shard.ChunkCount = len(shard.EvidenceRefs)
		for language := range languageSet {
			shard.Languages = append(shard.Languages, language)
		}
		sort.Strings(shard.Languages)
		shard.EvidenceDigest = digestJSON(shard.EvidenceRefs)
		shard.Digest = digestJSON(struct {
			Version, Ordinal                             int
			GenerationID, ManifestDigest, EvidenceDigest string
			Paths                                        []string
		}{SplitReviewPlanVersion, shard.Ordinal, generationID, manifestDigest, shard.EvidenceDigest, shard.Paths})
		shard.ID = fmt.Sprintf("shard-%02d-%s", shard.Ordinal, shard.Digest[:12])
		plan.Shards = append(plan.Shards, shard)
		plan.FileCount += shard.FileCount
		plan.ChunkCount += shard.ChunkCount
		plan.Bytes += shard.Bytes
	}
	plan.ShardCount = len(plan.Shards)
	digests := make([]string, 0, len(plan.Shards))
	for _, shard := range plan.Shards {
		digests = append(digests, shard.Digest)
	}
	plan.PlanDigest = digestJSON(struct {
		Version, RequestedShards     int
		GenerationID, ManifestDigest string
		ShardDigests                 []string
	}{plan.Version, plan.RequestedShards, plan.GenerationID, plan.ManifestDigest, digests})
	return plan, nil
}

func groupReviewFiles(files []reviewFile) []reviewGroup {
	byKey := map[string][]reviewFile{}
	for _, file := range files {
		parts := strings.Split(strings.Trim(strings.ReplaceAll(file.Path, "\\", "/"), "/"), "/")
		top := file.Path
		if len(parts) > 1 {
			top = parts[0]
		}
		key := file.Root + "\x00" + top
		byKey[key] = append(byKey[key], file)
	}
	groups := make([]reviewGroup, 0, len(byKey))
	for key, grouped := range byKey {
		groups = append(groups, newReviewGroup(key, grouped))
	}
	return groups
}

func newReviewGroup(key string, files []reviewFile) reviewGroup {
	group := reviewGroup{Key: key, Files: files}
	for _, file := range files {
		group.Bytes += file.Size
		weight := file.Size
		if chunkWeight := int64(len(file.Evidence) * 1024); chunkWeight > weight {
			weight = chunkWeight
		}
		group.Weight += weight
	}
	return group
}

func (plan SplitReviewPlan) Preview() SplitReviewPreview {
	preview := SplitReviewPreview{
		Version: plan.Version, GenerationID: plan.GenerationID, ManifestDigest: plan.ManifestDigest,
		PlanDigest: plan.PlanDigest, RequestedShards: plan.RequestedShards, ShardCount: plan.ShardCount,
		FileCount: plan.FileCount, ChunkCount: plan.ChunkCount, Bytes: plan.Bytes,
	}
	for _, shard := range plan.Shards {
		preview.Shards = append(preview.Shards, ReviewShardPreview{
			ID: shard.ID, Digest: shard.Digest, Ordinal: shard.Ordinal, FileCount: shard.FileCount,
			ChunkCount: shard.ChunkCount, Bytes: shard.Bytes, Paths: append([]string(nil), shard.Paths...),
			Languages: append([]string(nil), shard.Languages...), EvidenceDigest: shard.EvidenceDigest,
		})
	}
	return preview
}

// MergeReviewFindings validates every cited chunk, file hash, and line range
// against the sealed shard before accepting it into the parent draft bundle.
// Evidence validation does not independently prove the child's prose claim.
func MergeReviewFindings(plan SplitReviewPlan, submissions []ReviewSubmission) ReviewMergeResult {
	result := ReviewMergeResult{PlanDigest: plan.PlanDigest}
	shards := make(map[string]ReviewShard, len(plan.Shards))
	for _, shard := range plan.Shards {
		shards[shard.ID] = shard
	}
	acceptedByKey := map[string]int{}
	evidenceClaims := map[string]map[string]struct{}{}
	for _, submission := range submissions {
		shard, ok := shards[strings.TrimSpace(submission.ShardID)]
		claimant := strings.TrimSpace(submission.Claimant)
		if !ok || claimant == "" {
			for _, finding := range submission.Findings {
				reason := "unknown shard"
				if ok {
					reason = "claimant is required"
				}
				result.Decisions = append(result.Decisions, mergeDecision(submission, finding, "rejected", reason))
				result.Rejected++
			}
			continue
		}
		allowed := make(map[string]ReviewEvidenceRef, len(shard.EvidenceRefs))
		for _, ref := range shard.EvidenceRefs {
			allowed[ref.ChunkID] = ref
		}
		for _, finding := range submission.Findings {
			canonical, evidenceKey, err := validateReviewFinding(finding, allowed)
			if err != nil {
				result.Decisions = append(result.Decisions, mergeDecision(submission, finding, "rejected", err.Error()))
				result.Rejected++
				continue
			}
			claimKey := normalizeClaim(canonical.Claim)
			findingKey := claimKey + "\x00" + evidenceKey
			if existing, duplicate := acceptedByKey[findingKey]; duplicate {
				merged := &result.Findings[existing]
				merged.ShardIDs = appendUniqueSorted(merged.ShardIDs, shard.ID)
				merged.Claimants = appendUniqueSorted(merged.Claimants, claimant)
				merged.Severity = strongerSeverity(merged.Severity, canonical.Severity)
				merged.Confidence = strongerConfidence(merged.Confidence, canonical.Confidence)
				result.Decisions = append(result.Decisions, mergeDecision(submission, finding, "duplicate", "same normalized claim and immutable evidence"))
				result.Duplicates++
				continue
			}
			state := "draft"
			if claims := evidenceClaims[evidenceKey]; len(claims) > 0 {
				if _, same := claims[claimKey]; !same {
					state = "conflict"
					result.Conflicts++
					for previousKey, index := range acceptedByKey {
						if strings.HasSuffix(previousKey, "\x00"+evidenceKey) {
							result.Findings[index].State = "conflict"
						}
					}
				}
			}
			if evidenceClaims[evidenceKey] == nil {
				evidenceClaims[evidenceKey] = map[string]struct{}{}
			}
			evidenceClaims[evidenceKey][claimKey] = struct{}{}
			merged := MergedReviewFinding{ReviewFinding: canonical, State: state, EvidenceValid: true, ShardIDs: []string{shard.ID}, Claimants: []string{claimant}}
			acceptedByKey[findingKey] = len(result.Findings)
			result.Findings = append(result.Findings, merged)
			result.Decisions = append(result.Decisions, mergeDecision(submission, finding, "accepted", "all evidence belongs to the sealed shard"))
			result.Accepted++
		}
	}
	return result
}

// BuildReviewConflictCases groups conflicting merged findings by their exact
// canonical evidence. The result is stable for deterministic checkpoint replay
// and does not claim that any model-authored statement is correct.
func BuildReviewConflictCases(result ReviewMergeResult) []ReviewConflictCase {
	type conflictGroup struct {
		evidence []ReviewFindingEvidence
		claims   map[string]ReviewConflictClaim
	}
	groups := map[string]*conflictGroup{}
	for _, finding := range result.Findings {
		if finding.State != "conflict" || !finding.EvidenceValid {
			continue
		}
		evidence := append([]ReviewFindingEvidence(nil), finding.Evidence...)
		sort.Slice(evidence, func(i, j int) bool {
			if evidence[i].ChunkID != evidence[j].ChunkID {
				return evidence[i].ChunkID < evidence[j].ChunkID
			}
			if evidence[i].StartLine != evidence[j].StartLine {
				return evidence[i].StartLine < evidence[j].StartLine
			}
			return evidence[i].EndLine < evidence[j].EndLine
		})
		evidenceDigest := digestJSON(evidence)
		group := groups[evidenceDigest]
		if group == nil {
			group = &conflictGroup{evidence: evidence, claims: map[string]ReviewConflictClaim{}}
			groups[evidenceDigest] = group
		}
		claimID := digestJSON(struct {
			Claim, EvidenceDigest string
		}{normalizeClaim(finding.Claim), evidenceDigest})
		group.claims[claimID] = ReviewConflictClaim{
			ClaimID: claimID, Claim: finding.Claim, Severity: finding.Severity, Confidence: finding.Confidence,
			ShardIDs: append([]string(nil), finding.ShardIDs...), Claimants: append([]string(nil), finding.Claimants...),
		}
	}
	cases := make([]ReviewConflictCase, 0, len(groups))
	for evidenceDigest, group := range groups {
		if len(group.claims) < 2 {
			continue
		}
		claimIDs := make([]string, 0, len(group.claims))
		for claimID := range group.claims {
			claimIDs = append(claimIDs, claimID)
		}
		sort.Strings(claimIDs)
		claims := make([]ReviewConflictClaim, 0, len(claimIDs))
		for _, claimID := range claimIDs {
			claims = append(claims, group.claims[claimID])
		}
		caseDigest := digestJSON(struct {
			PlanDigest, EvidenceDigest string
			ClaimIDs                   []string
		}{result.PlanDigest, evidenceDigest, claimIDs})
		cases = append(cases, ReviewConflictCase{
			ID: "conflict-" + caseDigest[:16], EvidenceDigest: evidenceDigest,
			Evidence: append([]ReviewFindingEvidence(nil), group.evidence...), Claims: claims,
		})
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases
}

func validateReviewFinding(finding ReviewFinding, allowed map[string]ReviewEvidenceRef) (ReviewFinding, string, error) {
	finding.Claim = strings.TrimSpace(finding.Claim)
	finding.Severity = strings.ToLower(strings.TrimSpace(finding.Severity))
	finding.Confidence = strings.ToLower(strings.TrimSpace(finding.Confidence))
	if finding.Claim == "" {
		return finding, "", fmt.Errorf("claim is required")
	}
	if !oneOf(finding.Severity, "info", "low", "medium", "high", "critical") {
		return finding, "", fmt.Errorf("invalid severity")
	}
	if !oneOf(finding.Confidence, "low", "medium", "high") {
		return finding, "", fmt.Errorf("invalid confidence")
	}
	if len(finding.Evidence) == 0 {
		return finding, "", fmt.Errorf("immutable chunk evidence is required")
	}
	seen := map[string]struct{}{}
	canonical := make([]ReviewFindingEvidence, 0, len(finding.Evidence))
	for _, evidence := range finding.Evidence {
		ref, ok := allowed[strings.TrimSpace(evidence.ChunkID)]
		if !ok {
			return finding, "", fmt.Errorf("chunk %q is outside the sealed shard", evidence.ChunkID)
		}
		if strings.TrimSpace(evidence.FileSHA256) != ref.FileSHA256 {
			return finding, "", fmt.Errorf("file hash does not match chunk %q", evidence.ChunkID)
		}
		if evidence.StartLine < ref.StartLine || evidence.EndLine > ref.EndLine || evidence.StartLine > evidence.EndLine {
			return finding, "", fmt.Errorf("line range is outside chunk %q", evidence.ChunkID)
		}
		item := ReviewFindingEvidence{ChunkID: ref.ChunkID, FileSHA256: ref.FileSHA256, StartLine: evidence.StartLine, EndLine: evidence.EndLine}
		key := fmt.Sprintf("%s:%s:%d:%d", item.ChunkID, item.FileSHA256, item.StartLine, item.EndLine)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		canonical = append(canonical, item)
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].ChunkID != canonical[j].ChunkID {
			return canonical[i].ChunkID < canonical[j].ChunkID
		}
		if canonical[i].StartLine != canonical[j].StartLine {
			return canonical[i].StartLine < canonical[j].StartLine
		}
		return canonical[i].EndLine < canonical[j].EndLine
	})
	finding.Evidence = canonical
	return finding, digestJSON(canonical), nil
}

func mergeDecision(submission ReviewSubmission, finding ReviewFinding, decision, reason string) ReviewMergeDecision {
	return ReviewMergeDecision{ShardID: submission.ShardID, Claimant: submission.Claimant, FindingID: finding.ID, Decision: decision, Reason: reason}
}

func reviewFileKey(file reviewFile) string { return file.Root + "\x00" + file.Path }
func displayReviewPath(file reviewFile) string {
	path := strings.Trim(strings.ReplaceAll(file.Path, "\\", "/"), "/")
	if path == "" {
		return strings.ReplaceAll(file.Root, "\\", "/")
	}
	return path
}

func normalizeClaim(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
func appendUniqueSorted(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

func strongerSeverity(left, right string) string {
	rank := map[string]int{"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}
	if rank[right] > rank[left] {
		return right
	}
	return left
}
func strongerConfidence(left, right string) string {
	rank := map[string]int{"low": 0, "medium": 1, "high": 2}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func digestJSON(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
