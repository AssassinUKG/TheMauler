package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"mauler/internal/repoindex"
	"mauler/internal/settings"
	maulerstore "mauler/internal/store"
)

func TestTaskToolExposesExpectedSubagentTypes(t *testing.T) {
	specs := subagentSpecs()
	got := map[string]bool{}
	for _, spec := range specs {
		got[subagentTypeName(spec.ToolName)] = true
		if spec.TimeoutSecs <= 0 || spec.MaxTurns <= 0 || spec.MaxOutput <= 0 || spec.ContextBudget <= 0 {
			t.Fatalf("subagent spec has invalid bounds: %#v", spec)
		}
		if strings.TrimSpace(spec.Toolset) == "" || strings.TrimSpace(spec.Contract) == "" {
			t.Fatalf("subagent spec missing toolset/contract: %#v", spec)
		}
	}
	for _, name := range []string{"explore", "research", "review", "testfix", "summarize"} {
		if !got[name] {
			t.Fatalf("missing task type %q in %#v", name, got)
		}
	}
}

func TestSubagentExploreReadOnlyToolset(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "explore"
	effective := settings.EffectiveEnabledTools(cfg)

	for _, name := range []string{"read", "glob", "grep"} {
		if !effective[name] {
			t.Fatalf("explore toolset should include %s: %#v", name, effective)
		}
	}
	for _, name := range []string{"write", "edit", "shell", "web_search", "fetch_url"} {
		if effective[name] {
			t.Fatalf("explore toolset should exclude %s: %#v", name, effective)
		}
	}
}

func TestBuildSubagentSystemPromptIncludesBoundsAndWorkspace(t *testing.T) {
	spec := mustSubagentSpec(t, "subagent_research")
	profile := settings.Profile{Name: "qwen-test", CtxTokens: 32768}
	prompt := buildSubagentSystemPrompt(spec, profile, 30, 2)

	for _, want := range []string{
		"bounded Researcher subagent",
		"Profile: qwen-test",
		"Toolset: web-research",
		"Timeout: 30s",
		"Tool-call budget: 2",
		"Current workspace context",
		spec.Contract,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSubagentFinalReportIncludesMetadata(t *testing.T) {
	spec := mustSubagentSpec(t, "subagent_review")
	out := finalSubagentReport(spec, "found issue", nil, 3, "turn budget exhausted")

	for _, want := range []string{"Subagent: Reviewer", "Toolset: safe", "Tool calls used: 3", "Stop: turn budget exhausted", "found issue"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
}

func TestSubagentFinalReportFallsBackToEvidence(t *testing.T) {
	spec := mustSubagentSpec(t, "subagent_research")
	out := finalSubagentReport(spec, "", []string{
		`web_search: No results found for "FreePBX 16.0.40.7 exploit"`,
		`fetch_url: blocked by timeout`,
	}, 4, "turn budget exhausted")

	for _, want := range []string{
		"Subagent: Researcher",
		"Stop: turn budget exhausted",
		"The subagent stopped before writing a synthesis",
		`web_search: No results found for "FreePBX 16.0.40.7 exploit"`,
		"fetch_url: blocked by timeout",
		"Recommended next step",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("fallback report missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "No subagent output was produced") {
		t.Fatalf("fallback report should not return the old blank-output message:\n%s", out)
	}
}

func TestRepositoryReviewResponseParsing(t *testing.T) {
	raw := "```json\n{\"findings\":[{\"id\":\"f-1\",\"claim\":\"input reaches exec\",\"severity\":\"high\",\"confidence\":\"medium\",\"evidence\":[{\"chunk_id\":\"c-1\",\"file_sha256\":\"hash\",\"start_line\":2,\"end_line\":3}]}]}\n```"
	parsed, err := parseRepositoryReviewResponse(raw)
	if err != nil || len(parsed.Findings) != 1 || parsed.Findings[0].Evidence[0].ChunkID != "c-1" {
		t.Fatalf("parsed = %#v err=%v", parsed, err)
	}
	if _, err := parseRepositoryReviewResponse("review looks fine"); err == nil {
		t.Fatal("narrative-only review response unexpectedly accepted")
	}
}

func TestRepositoryConflictResponseParsingAndEvidenceScope(t *testing.T) {
	conflict := repoindex.ReviewConflictCase{
		ID: "conflict-1", EvidenceDigest: "evidence-1",
		Evidence: []repoindex.ReviewFindingEvidence{{ChunkID: "chunk-1", FileSHA256: "file-1", StartLine: 2, EndLine: 4}},
		Claims: []repoindex.ReviewConflictClaim{
			{ClaimID: "claim-a", Claim: "input reaches exec"},
			{ClaimID: "claim-b", Claim: "input is escaped"},
		},
	}
	parsed, err := parseRepositoryConflictResponse(`{"verdict":"prefer","supported_claim_ids":["claim-a"],"reason":"The cited call has no escaping step."}`, conflict)
	if err != nil || parsed.Verdict != "prefer" || len(parsed.SupportedClaimIDs) != 1 || parsed.SupportedClaimIDs[0] != "claim-a" {
		t.Fatalf("parsed conflict response = %#v err=%v", parsed, err)
	}
	for _, invalid := range []string{
		`{"verdict":"compatible","supported_claim_ids":["claim-a"],"reason":"missing one"}`,
		`{"verdict":"unsupported","supported_claim_ids":["claim-a"],"reason":"contradictory selection"}`,
		`{"verdict":"prefer","supported_claim_ids":["unknown"],"reason":"unknown id"}`,
		`{"verdict":"inconclusive","supported_claim_ids":[],"reason":""}`,
	} {
		if _, err := parseRepositoryConflictResponse(invalid, conflict); err == nil {
			t.Fatalf("invalid conflict response unexpectedly accepted: %s", invalid)
		}
	}
	plan := repoindex.SplitReviewPlan{Shards: []repoindex.ReviewShard{{EvidenceRefs: []repoindex.ReviewEvidenceRef{
		{ChunkID: "chunk-1", FileSHA256: "file-1", StartLine: 1, EndLine: 10},
		{ChunkID: "chunk-2", FileSHA256: "file-2", StartLine: 1, EndLine: 10},
	}}}}
	shard, err := repositoryConflictEvidenceShard(plan, conflict)
	if err != nil || len(shard.EvidenceRefs) != 1 || shard.EvidenceRefs[0].ChunkID != "chunk-1" {
		t.Fatalf("conflict evidence shard = %#v err=%v", shard, err)
	}
	conflict.Evidence[0].EndLine = 11
	if _, err := repositoryConflictEvidenceShard(plan, conflict); err == nil {
		t.Fatal("out-of-range conflict evidence unexpectedly accepted")
	}
}

func TestRepositoryReviewEvidenceToolExposesOnlySealedCapability(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "review.txt")
	if err := os.WriteFile(path, []byte("ReviewMarker\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, _ := repoindex.NewStore(db)
	indexed, err := store.Index(context.Background(), repoindex.DefaultPolicy(root))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.BuildSplitReviewPlan(context.Background(), indexed.GenerationID, 1)
	if err != nil {
		t.Fatal(err)
	}
	tool := &repositoryReviewEvidenceTool{store: store, generation: plan.GenerationID, shard: plan.Shards[0]}
	catalog, err := tool.Run(context.Background(), []byte(`{"action":"catalog","limit":5}`))
	if err != nil || !strings.Contains(catalog, plan.Shards[0].EvidenceRefs[0].ChunkID) || strings.Contains(catalog, "ReviewMarker") {
		t.Fatalf("catalog = %q err=%v", catalog, err)
	}
	read, err := tool.Run(context.Background(), []byte(`{"action":"read","chunk_ids":["`+plan.Shards[0].EvidenceRefs[0].ChunkID+`"]}`))
	if err != nil || !strings.Contains(read, "ReviewMarker") {
		t.Fatalf("read = %q err=%v", read, err)
	}
}

func TestRepositoryReviewRuntimeCopiesAndTruncatesSafely(t *testing.T) {
	original := RepositoryReviewStatus{Findings: []repoindex.MergedReviewFinding{{
		ReviewFinding: repoindex.ReviewFinding{Evidence: []repoindex.ReviewFindingEvidence{{ChunkID: "chunk-1"}}},
		ShardIDs:      []string{"shard-1"},
		Claimants:     []string{"review/one"},
	}}, ConflictChecks: []RepositoryReviewConflictStatus{{
		ID: "conflict-1", Evidence: []repoindex.ReviewFindingEvidence{{ChunkID: "chunk-1"}},
		Claims:            []repoindex.ReviewConflictClaim{{ClaimID: "claim-1", ShardIDs: []string{"shard-1"}, Claimants: []string{"review/one"}}},
		SupportedClaimIDs: []string{"claim-1"},
	}}}
	cloned := cloneRepositoryReviewStatus(original)
	cloned.Findings[0].Evidence[0].ChunkID = "changed"
	cloned.Findings[0].ShardIDs[0] = "changed"
	cloned.Findings[0].Claimants[0] = "changed"
	cloned.ConflictChecks[0].Evidence[0].ChunkID = "changed"
	cloned.ConflictChecks[0].Claims[0].ShardIDs[0] = "changed"
	cloned.ConflictChecks[0].SupportedClaimIDs[0] = "changed"
	if original.Findings[0].Evidence[0].ChunkID != "chunk-1" || original.Findings[0].ShardIDs[0] != "shard-1" || original.Findings[0].Claimants[0] != "review/one" {
		t.Fatalf("clone mutated live review state: %#v", original)
	}
	if original.ConflictChecks[0].Evidence[0].ChunkID != "chunk-1" || original.ConflictChecks[0].Claims[0].ShardIDs[0] != "shard-1" || original.ConflictChecks[0].SupportedClaimIDs[0] != "claim-1" {
		t.Fatalf("clone mutated conflict state: %#v", original.ConflictChecks)
	}
	truncated := truncateReviewText("a€b", 2)
	if !utf8.ValidString(truncated) || !strings.HasPrefix(truncated, "a\n") {
		t.Fatalf("bounded excerpt split UTF-8: %q", truncated)
	}
}

func TestRepositoryReviewSnapshotReconcilesAndValidatesConflictChecks(t *testing.T) {
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ref := repoindex.ReviewEvidenceRef{ChunkID: "chunk-1", Root: "C:/repo", Path: "main.go", FileSHA256: "file-hash", TextSHA256: "text-hash", StartLine: 1, EndLine: 10}
	plan := repoindex.SplitReviewPlan{
		Version: 1, GenerationID: "generation-1", ManifestDigest: "manifest-1", PlanDigest: "plan-1", RequestedShards: 2, ShardCount: 2,
		Shards: []repoindex.ReviewShard{
			{ID: "shard-1", Digest: "digest-1", Ordinal: 1, FileCount: 1, ChunkCount: 1, Paths: []string{"main.go"}, EvidenceRefs: []repoindex.ReviewEvidenceRef{ref}},
			{ID: "shard-2", Digest: "digest-2", Ordinal: 2, FileCount: 1, ChunkCount: 1, Paths: []string{"main.go"}, EvidenceRefs: []repoindex.ReviewEvidenceRef{ref}},
		},
	}
	evidence := []repoindex.ReviewFindingEvidence{{ChunkID: ref.ChunkID, FileSHA256: ref.FileSHA256, StartLine: 2, EndLine: 4}}
	submissions := []repoindex.ReviewSubmission{
		{ShardID: "shard-1", Claimant: "child-1", Findings: []repoindex.ReviewFinding{{Claim: "input reaches exec", Severity: "high", Confidence: "medium", Evidence: evidence}}},
		{ShardID: "shard-2", Claimant: "child-2", Findings: []repoindex.ReviewFinding{{Claim: "input is escaped", Severity: "info", Confidence: "medium", Evidence: evidence}}},
	}
	status := RepositoryReviewStatus{
		Available: true, Workspace: "C:/repo", ReviewID: "review-conflict", GenerationID: plan.GenerationID,
		ManifestDigest: plan.ManifestDigest, PlanDigest: plan.PlanDigest, RequestedShards: 2, State: "complete", Phase: "complete",
		Shards: []RepositoryReviewShardStatus{
			{ID: "shard-1", Digest: "digest-1", Ordinal: 1, State: "done", FileCount: 1, ChunkCount: 1, Paths: []string{"main.go"}},
			{ID: "shard-2", Digest: "digest-2", Ordinal: 2, State: "done", FileCount: 1, ChunkCount: 1, Paths: []string{"main.go"}},
		},
		Findings: []repoindex.MergedReviewFinding{}, ConflictChecks: []RepositoryReviewConflictStatus{},
	}
	app := &App{db: db}
	if err := app.saveRepositoryReviewSnapshot(context.Background(), repositoryReviewSnapshot{Status: status, Plan: plan, Submissions: submissions}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := app.latestRepositoryReviewSnapshot(context.Background(), "C:/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Status.ConflictChecks) != 1 || snapshot.Status.ConflictChecks[0].State != "pending" || !snapshot.Status.CanResume {
		t.Fatalf("reconciled conflict status = %#v", snapshot.Status)
	}
	check := snapshot.Status.ConflictChecks[0]
	check.State = "done"
	check.Attempt = 1
	check.CheckerID = "review-conflict/check-1"
	check.Verdict = "prefer"
	check.SupportedClaimIDs = []string{check.Claims[0].ClaimID}
	check.Reason = "The exact cited lines support only one draft claim."
	snapshot.Status.ConflictChecks = []RepositoryReviewConflictStatus{check}
	if err := app.saveRepositoryReviewSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	checked, err := app.latestRepositoryReviewSnapshot(context.Background(), "C:/repo")
	if err != nil || checked.Status.CanResume || checked.Status.ConflictChecks[0].Verdict != "prefer" {
		t.Fatalf("checked conflict snapshot = %#v err=%v", checked.Status, err)
	}
	checked.Status.ConflictChecks[0].SupportedClaimIDs = []string{"not-a-claim"}
	if err := app.saveRepositoryReviewSnapshot(context.Background(), checked); err != nil {
		t.Fatal(err)
	}
	if _, err := app.latestRepositoryReviewSnapshot(context.Background(), "C:/repo"); err == nil {
		t.Fatal("tampered conflict verdict unexpectedly passed durable replay validation")
	}
}

func TestRepositoryReviewUnavailableWithoutAppOrDatabase(t *testing.T) {
	var missing *App
	if status := missing.GetRepositoryReviewStatus(); status.State != "unavailable" || status.Available {
		t.Fatalf("nil app status = %#v", status)
	}
	app := &App{}
	if _, err := app.RetryRepositoryReviewShard("shard-01"); err == nil {
		t.Fatal("retry without a repository database unexpectedly succeeded")
	}
}

func TestRepositoryReviewSnapshotRestoresInterruptedRunAndReplaysMerge(t *testing.T) {
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ref := repoindex.ReviewEvidenceRef{ChunkID: "chunk-1", Root: "C:/repo", Path: "main.go", FileSHA256: "file-hash", TextSHA256: "text-hash", StartLine: 1, EndLine: 10}
	plan := repoindex.SplitReviewPlan{
		Version: 1, GenerationID: "generation-1", ManifestDigest: "manifest-1", PlanDigest: "plan-1", RequestedShards: 1, ShardCount: 1,
		Shards: []repoindex.ReviewShard{{ID: "shard-1", Digest: "shard-digest", Ordinal: 1, FileCount: 1, ChunkCount: 1, Paths: []string{"main.go"}, EvidenceRefs: []repoindex.ReviewEvidenceRef{ref}}},
	}
	status := RepositoryReviewStatus{
		Available: true, Workspace: "C:/repo", ReviewID: "review-1", GenerationID: plan.GenerationID,
		ManifestDigest: plan.ManifestDigest, PlanDigest: plan.PlanDigest, RequestedShards: 1,
		State: "running", CanCancel: true, StartedAt: "2026-09-24T00:00:00Z",
		Shards:   []RepositoryReviewShardStatus{{ID: "shard-1", Digest: "shard-digest", Ordinal: 1, State: "running", Attempt: 1, FileCount: 1, ChunkCount: 1, Paths: []string{"main.go"}}},
		Findings: []repoindex.MergedReviewFinding{},
	}
	submissions := []repoindex.ReviewSubmission{{ShardID: "shard-1", Claimant: "review-1/shard-1", Findings: []repoindex.ReviewFinding{{
		ID: "finding-1", Claim: "input reaches sink", Severity: "high", Confidence: "medium",
		Evidence: []repoindex.ReviewFindingEvidence{{ChunkID: ref.ChunkID, FileSHA256: ref.FileSHA256, StartLine: 2, EndLine: 4}},
	}}}}
	writer := &App{db: db}
	if err := writer.saveRepositoryReviewSnapshot(context.Background(), repositoryReviewSnapshot{Status: status, Plan: plan, Submissions: submissions}); err != nil {
		t.Fatal(err)
	}
	restored := &App{db: db}
	if err := restored.restoreRepositoryReviewForWorkspace("C:/repo"); err != nil {
		t.Fatal(err)
	}
	restored.repositoryReviewMu.Lock()
	got := cloneRepositoryReviewStatus(restored.repositoryReviewRuntime)
	restored.repositoryReviewMu.Unlock()
	if got.State != "interrupted" || !got.CanResume || got.CanCancel || len(got.Findings) != 1 || got.Accepted != 1 {
		t.Fatalf("restored review = %#v", got)
	}
	if got.Shards[0].State != "interrupted" {
		t.Fatalf("restored shard = %#v", got.Shards[0])
	}
	if _, err := db.Exec(`UPDATE repository_reviews SET merge_json='{}' WHERE review_id='review-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.latestRepositoryReviewSnapshot(context.Background(), "C:/repo"); err == nil {
		t.Fatal("tampered merge snapshot unexpectedly passed deterministic replay")
	}
}

func mustSubagentSpec(t *testing.T, toolName string) subagentSpec {
	t.Helper()
	for _, spec := range subagentSpecs() {
		if spec.ToolName == toolName {
			return spec
		}
	}
	t.Fatalf("missing subagent spec %q", toolName)
	return subagentSpec{}
}
