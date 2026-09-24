package repoindex

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	maulerstore "mauler/internal/store"
)

func TestSplitReviewPlanIsDeterministicAndCoversSealedGeneration(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "api", "handlers.go"), []byte("package api\nfunc Alpha() {}\nfunc Beta() {}\n"))
	writeFixture(t, filepath.Join(root, "api", "routes.go"), []byte("package api\nfunc Routes() {}\n"))
	writeFixture(t, filepath.Join(root, "ui", "app.ts"), []byte("export const app = 'ready'\n"))
	writeFixture(t, filepath.Join(root, "README.md"), []byte("# Review fixture\nsealed evidence\n"))
	writeFixture(t, filepath.Join(root, "EMPTY.txt"), nil)
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, _ := NewStore(db)
	policy := DefaultPolicy(root)
	policy.ExtractorPolicy.ChunkLines = 2
	indexed, err := index.Index(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}

	first, err := index.BuildSplitReviewPlan(context.Background(), indexed.GenerationID, 3)
	if err != nil {
		t.Fatal(err)
	}
	second, err := index.BuildSplitReviewPlan(context.Background(), indexed.GenerationID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("split review plan changed without an index change:\n%#v\n%#v", first, second)
	}
	if first.GenerationID != indexed.GenerationID || first.ManifestDigest != indexed.Manifest.Digest || first.ShardCount != 3 {
		t.Fatalf("plan identity = %#v", first)
	}
	if first.FileCount != indexed.Manifest.FilesIndexed || first.ChunkCount != indexed.Manifest.Chunks || len(first.PlanDigest) != 64 {
		t.Fatalf("plan coverage = files %d/%d chunks %d/%d digest %q", first.FileCount, indexed.Manifest.FilesIndexed, first.ChunkCount, indexed.Manifest.Chunks, first.PlanDigest)
	}
	seenFiles := map[string]struct{}{}
	seenChunks := map[string]struct{}{}
	for ordinal, shard := range first.Shards {
		if shard.Ordinal != ordinal+1 || shard.ID == "" || len(shard.Digest) != 64 || len(shard.EvidenceDigest) != 64 {
			t.Fatalf("unsealed shard = %#v", shard)
		}
		for _, path := range shard.Paths {
			if _, duplicate := seenFiles[path]; duplicate {
				t.Fatalf("file assigned twice: %s", path)
			}
			seenFiles[path] = struct{}{}
		}
		for _, ref := range shard.EvidenceRefs {
			if ref.FileSHA256 == "" || ref.TextSHA256 == "" {
				t.Fatalf("unhashed evidence = %#v", ref)
			}
			if _, duplicate := seenChunks[ref.ChunkID]; duplicate {
				t.Fatalf("chunk assigned twice: %s", ref.ChunkID)
			}
			seenChunks[ref.ChunkID] = struct{}{}
		}
	}
	preview := first.Preview()
	if preview.PlanDigest != first.PlanDigest || len(preview.Shards) != 3 {
		t.Fatalf("preview identity = %#v", preview)
	}
}

func TestSplitReviewPlanCapsEmptyShardsAndRejectsUnsealedGeneration(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "only.txt"), []byte("only evidence\n"))
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, _ := NewStore(db)
	result, err := index.Index(context.Background(), DefaultPolicy(root))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := index.BuildSplitReviewPlan(context.Background(), result.GenerationID, 8)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequestedShards != 8 || plan.ShardCount != 1 || len(plan.Shards) != 1 || plan.Shards[0].FileCount != 1 {
		t.Fatalf("single-file plan = %#v", plan)
	}
	if _, err := index.BuildSplitReviewPlan(context.Background(), result.GenerationID, 0); err == nil {
		t.Fatal("zero shard request unexpectedly accepted")
	}
}

func TestReviewEvidenceReadAndSearchStayInsideSealedShard(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "alpha", "one.txt"), []byte("AlphaOwnedMarker\nshared token\n"))
	writeFixture(t, filepath.Join(root, "beta", "two.txt"), []byte("BetaOtherMarker\nshared token\n"))
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, _ := NewStore(db)
	indexed, err := index.Index(context.Background(), DefaultPolicy(root))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := index.BuildSplitReviewPlan(context.Background(), indexed.GenerationID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Shards) != 2 {
		t.Fatalf("shards = %#v", plan.Shards)
	}
	shard := plan.Shards[0]
	if len(shard.EvidenceRefs) == 0 {
		t.Fatalf("shard has no evidence: %#v", shard)
	}
	owned := shard.EvidenceRefs[0]
	chunks, err := index.ReadReviewChunks(context.Background(), plan.GenerationID, shard.EvidenceRefs, []string{owned.ChunkID})
	if err != nil || len(chunks) != 1 || chunks[0].FileSHA256 != owned.FileSHA256 || chunks[0].TextSHA256 != owned.TextSHA256 {
		t.Fatalf("sealed read = %#v err=%v", chunks, err)
	}
	other := plan.Shards[1].EvidenceRefs[0]
	if _, err := index.ReadReviewChunks(context.Background(), plan.GenerationID, shard.EvidenceRefs, []string{other.ChunkID}); err == nil {
		t.Fatal("cross-shard chunk read unexpectedly succeeded")
	}
	query := "AlphaOwnedMarker"
	if owned.Path == "beta/two.txt" {
		query = "BetaOtherMarker"
	}
	hits, err := index.SearchReviewEvidence(context.Background(), plan.GenerationID, shard.EvidenceRefs, query, 5)
	if err != nil || len(hits) != 1 || hits[0].ChunkID != owned.ChunkID {
		t.Fatalf("sealed search = %#v err=%v", hits, err)
	}
	crossQuery := "BetaOtherMarker"
	if query == crossQuery {
		crossQuery = "AlphaOwnedMarker"
	}
	hits, err = index.SearchReviewEvidence(context.Background(), plan.GenerationID, shard.EvidenceRefs, crossQuery, 5)
	if err != nil || len(hits) != 0 {
		t.Fatalf("cross-shard search leaked results = %#v err=%v", hits, err)
	}
	if _, err := db.Exec(`UPDATE repo_index_chunks SET text_sha256='changed-after-seal' WHERE generation_id=? AND chunk_id=?`, plan.GenerationID, owned.ChunkID); err != nil {
		t.Fatal(err)
	}
	if _, err := index.SearchReviewEvidence(context.Background(), plan.GenerationID, shard.EvidenceRefs, query, 5); err == nil {
		t.Fatal("search accepted evidence whose sealed hash changed")
	}
}

func TestMergeReviewFindingsRequiresOwnedHashAndLineEvidence(t *testing.T) {
	refA := ReviewEvidenceRef{ChunkID: "chunk-a", Root: "/repo", Path: "api/a.go", FileSHA256: "file-a", TextSHA256: "text-a", StartLine: 10, EndLine: 20}
	refB := ReviewEvidenceRef{ChunkID: "chunk-b", Root: "/repo", Path: "ui/b.ts", FileSHA256: "file-b", TextSHA256: "text-b", StartLine: 1, EndLine: 8}
	plan := SplitReviewPlan{PlanDigest: "plan", Shards: []ReviewShard{
		{ID: "shard-a", EvidenceRefs: []ReviewEvidenceRef{refA}},
		{ID: "shard-b", EvidenceRefs: []ReviewEvidenceRef{refB}},
	}}
	result := MergeReviewFindings(plan, []ReviewSubmission{
		{ShardID: "shard-a", Claimant: "child-a", Findings: []ReviewFinding{
			{ID: "one", Claim: "Unchecked input reaches exec", Severity: "high", Confidence: "medium", Evidence: []ReviewFindingEvidence{{ChunkID: "chunk-a", FileSHA256: "file-a", StartLine: 12, EndLine: 14}}},
			{ID: "bad-hash", Claim: "Wrong hash", Severity: "low", Confidence: "low", Evidence: []ReviewFindingEvidence{{ChunkID: "chunk-a", FileSHA256: "changed", StartLine: 12, EndLine: 14}}},
			{ID: "bad-shard", Claim: "Cross shard", Severity: "low", Confidence: "low", Evidence: []ReviewFindingEvidence{{ChunkID: "chunk-b", FileSHA256: "file-b", StartLine: 1, EndLine: 2}}},
			{ID: "bad-lines", Claim: "Outside lines", Severity: "low", Confidence: "low", Evidence: []ReviewFindingEvidence{{ChunkID: "chunk-a", FileSHA256: "file-a", StartLine: 1, EndLine: 2}}},
			{ID: "no-proof", Claim: "Narrative only", Severity: "medium", Confidence: "high"},
		}},
	})
	if result.Accepted != 1 || result.Rejected != 4 || result.Duplicates != 0 || len(result.Findings) != 1 {
		t.Fatalf("merge result = %#v", result)
	}
	if !result.Findings[0].EvidenceValid || result.Findings[0].State != "draft" {
		t.Fatalf("accepted finding state = %#v", result.Findings[0])
	}
}

func TestMergeReviewFindingsDeduplicatesExactEvidenceAndPreservesConflicts(t *testing.T) {
	ref := ReviewEvidenceRef{ChunkID: "chunk-a", Root: "/repo", Path: "api/a.go", FileSHA256: "file-a", TextSHA256: "text-a", StartLine: 1, EndLine: 20}
	plan := SplitReviewPlan{PlanDigest: "plan", Shards: []ReviewShard{
		{ID: "shard-a", EvidenceRefs: []ReviewEvidenceRef{ref}},
		{ID: "shard-b", EvidenceRefs: []ReviewEvidenceRef{ref}},
	}}
	evidence := []ReviewFindingEvidence{{ChunkID: "chunk-a", FileSHA256: "file-a", StartLine: 4, EndLine: 6}}
	result := MergeReviewFindings(plan, []ReviewSubmission{
		{ShardID: "shard-a", Claimant: "child-a", Findings: []ReviewFinding{{ID: "a", Claim: "  Input reaches EXEC ", Severity: "medium", Confidence: "medium", Evidence: evidence}}},
		{ShardID: "shard-b", Claimant: "child-b", Findings: []ReviewFinding{
			{ID: "b", Claim: "input reaches exec", Severity: "high", Confidence: "high", Evidence: evidence},
			{ID: "c", Claim: "Input is escaped before exec", Severity: "info", Confidence: "medium", Evidence: evidence},
		}},
	})
	if result.Accepted != 2 || result.Duplicates != 1 || result.Conflicts != 1 || result.Rejected != 0 || len(result.Findings) != 2 {
		t.Fatalf("merge result = %#v", result)
	}
	if result.Findings[0].Severity != "high" || result.Findings[0].Confidence != "high" || len(result.Findings[0].Claimants) != 2 {
		t.Fatalf("deduplicated finding = %#v", result.Findings[0])
	}
	if result.Findings[0].State != "conflict" || result.Findings[1].State != "conflict" {
		t.Fatalf("conflict was not preserved = %#v", result.Findings)
	}
	cases := BuildReviewConflictCases(result)
	if len(cases) != 1 || len(cases[0].Claims) != 2 || len(cases[0].Evidence) != 1 {
		t.Fatalf("conflict cases = %#v", cases)
	}
	if cases[0].ID == "" || cases[0].EvidenceDigest == "" || cases[0].Claims[0].ClaimID >= cases[0].Claims[1].ClaimID {
		t.Fatalf("conflict contract is not stable and sorted = %#v", cases[0])
	}
	reversed := MergeReviewFindings(plan, []ReviewSubmission{
		{ShardID: "shard-b", Claimant: "child-b", Findings: []ReviewFinding{
			{ID: "c", Claim: "Input is escaped before exec", Severity: "info", Confidence: "medium", Evidence: evidence},
			{ID: "b", Claim: "input reaches exec", Severity: "high", Confidence: "high", Evidence: evidence},
		}},
		{ShardID: "shard-a", Claimant: "child-a", Findings: []ReviewFinding{{ID: "a", Claim: "Input reaches EXEC", Severity: "medium", Confidence: "medium", Evidence: evidence}}},
	})
	reversedCases := BuildReviewConflictCases(reversed)
	if len(reversedCases) != 1 || reversedCases[0].ID != cases[0].ID || reversedCases[0].EvidenceDigest != cases[0].EvidenceDigest {
		t.Fatalf("conflict identity changed with submission order: first=%#v reversed=%#v", cases, reversedCases)
	}
}
