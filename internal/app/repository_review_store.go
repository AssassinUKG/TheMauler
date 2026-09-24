package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mauler/internal/repoindex"
)

type repositoryReviewSnapshot struct {
	Status      RepositoryReviewStatus
	Plan        repoindex.SplitReviewPlan
	Submissions []repoindex.ReviewSubmission
	Merge       repoindex.ReviewMergeResult
}

func (a *App) saveRepositoryReviewSnapshot(ctx context.Context, snapshot repositoryReviewSnapshot) error {
	if a == nil || a.db == nil {
		return fmt.Errorf("repository review database is unavailable")
	}
	status := cloneRepositoryReviewStatus(snapshot.Status)
	status.CanResume = repositoryReviewCanResume(status)
	snapshot.Status = status
	snapshot.Merge = repoindex.MergeReviewFindings(snapshot.Plan, snapshot.Submissions)
	statusJSON, err := json.Marshal(snapshot.Status)
	if err != nil {
		return err
	}
	planJSON, err := json.Marshal(snapshot.Plan)
	if err != nil {
		return err
	}
	submissionsJSON, err := json.Marshal(snapshot.Submissions)
	if err != nil {
		return err
	}
	mergeJSON, err := json.Marshal(snapshot.Merge)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
INSERT INTO repository_reviews(
  review_id, workspace, generation_id, manifest_digest, plan_digest, requested_shards,
  state, status_json, plan_json, submissions_json, merge_json, started_at, updated_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(review_id) DO UPDATE SET
  workspace=excluded.workspace,
  generation_id=excluded.generation_id,
  manifest_digest=excluded.manifest_digest,
  plan_digest=excluded.plan_digest,
  requested_shards=excluded.requested_shards,
  state=excluded.state,
  status_json=excluded.status_json,
  plan_json=excluded.plan_json,
  submissions_json=excluded.submissions_json,
  merge_json=excluded.merge_json,
  started_at=excluded.started_at,
  updated_at=excluded.updated_at,
  completed_at=excluded.completed_at`,
		status.ReviewID, status.Workspace, status.GenerationID, status.ManifestDigest, status.PlanDigest,
		status.RequestedShards, status.State, string(statusJSON), string(planJSON), string(submissionsJSON),
		string(mergeJSON), status.StartedAt, time.Now().UTC().Format(time.RFC3339Nano), status.CompletedAt,
	)
	return err
}

func (a *App) latestRepositoryReviewSnapshot(ctx context.Context, workspace string) (repositoryReviewSnapshot, error) {
	if a == nil || a.db == nil {
		return repositoryReviewSnapshot{}, fmt.Errorf("repository review database is unavailable")
	}
	var reviewID, storedWorkspace, generationID, manifestDigest, planDigest, state string
	var statusJSON, planJSON, submissionsJSON, mergeJSON string
	err := a.db.QueryRowContext(ctx, `
SELECT review_id, workspace, generation_id, manifest_digest, plan_digest, state,
       status_json, plan_json, submissions_json, merge_json
FROM repository_reviews
WHERE workspace = ? COLLATE NOCASE
ORDER BY updated_at DESC, review_id DESC
LIMIT 1`, filepathSlashClean(workspace)).Scan(
		&reviewID, &storedWorkspace, &generationID, &manifestDigest, &planDigest, &state,
		&statusJSON, &planJSON, &submissionsJSON, &mergeJSON,
	)
	if err != nil {
		return repositoryReviewSnapshot{}, err
	}
	var snapshot repositoryReviewSnapshot
	if err := json.Unmarshal([]byte(statusJSON), &snapshot.Status); err != nil {
		return snapshot, fmt.Errorf("decode repository review status: %w", err)
	}
	if err := json.Unmarshal([]byte(planJSON), &snapshot.Plan); err != nil {
		return snapshot, fmt.Errorf("decode repository review plan: %w", err)
	}
	if err := json.Unmarshal([]byte(submissionsJSON), &snapshot.Submissions); err != nil {
		return snapshot, fmt.Errorf("decode repository review submissions: %w", err)
	}
	var storedMerge repoindex.ReviewMergeResult
	if err := json.Unmarshal([]byte(mergeJSON), &storedMerge); err != nil {
		return snapshot, fmt.Errorf("decode repository review merge: %w", err)
	}
	if snapshot.Status.ReviewID != reviewID || !sameFilesystemPath(snapshot.Status.Workspace, storedWorkspace) ||
		snapshot.Status.GenerationID != generationID || snapshot.Status.ManifestDigest != manifestDigest ||
		snapshot.Status.PlanDigest != planDigest || snapshot.Status.State != state ||
		snapshot.Plan.GenerationID != generationID || snapshot.Plan.ManifestDigest != manifestDigest || snapshot.Plan.PlanDigest != planDigest {
		return snapshot, fmt.Errorf("repository review snapshot metadata does not match its durable envelope")
	}
	if err := validateRepositoryReviewSnapshot(snapshot); err != nil {
		return snapshot, err
	}
	recomputed := repoindex.MergeReviewFindings(snapshot.Plan, snapshot.Submissions)
	if !sameRepositoryReviewMerge(storedMerge, recomputed) {
		return snapshot, fmt.Errorf("repository review merge snapshot failed deterministic replay")
	}
	conflictCases := repoindex.BuildReviewConflictCases(recomputed)
	if err := validateRepositoryReviewConflictStatuses(snapshot.Status.ConflictChecks, conflictCases); err != nil {
		return snapshot, err
	}
	snapshot.Merge = recomputed
	snapshot.Status.Findings = append([]repoindex.MergedReviewFinding(nil), recomputed.Findings...)
	snapshot.Status.Accepted = recomputed.Accepted
	snapshot.Status.Rejected = recomputed.Rejected
	snapshot.Status.Duplicates = recomputed.Duplicates
	snapshot.Status.Conflicts = recomputed.Conflicts
	snapshot.Status.ConflictChecks = reconcileRepositoryReviewConflictStatuses(snapshot.Status.ConflictChecks, conflictCases)
	snapshot.Status.CanCancel = false
	snapshot.Status.CanResume = repositoryReviewCanResume(snapshot.Status)
	return snapshot, nil
}

func validateRepositoryReviewSnapshot(snapshot repositoryReviewSnapshot) error {
	if len(snapshot.Status.Shards) != len(snapshot.Plan.Shards) {
		return fmt.Errorf("repository review snapshot shard count does not match its sealed plan")
	}
	statuses := make(map[string]RepositoryReviewShardStatus, len(snapshot.Status.Shards))
	for _, status := range snapshot.Status.Shards {
		if _, duplicate := statuses[status.ID]; duplicate {
			return fmt.Errorf("repository review snapshot contains duplicate shard status %q", status.ID)
		}
		statuses[status.ID] = status
	}
	submissions := make(map[string]repoindex.ReviewSubmission, len(snapshot.Submissions))
	for _, submission := range snapshot.Submissions {
		if _, duplicate := submissions[submission.ShardID]; duplicate {
			return fmt.Errorf("repository review snapshot contains duplicate submission %q", submission.ShardID)
		}
		submissions[submission.ShardID] = submission
	}
	for _, shard := range snapshot.Plan.Shards {
		status, ok := statuses[shard.ID]
		if !ok || status.Digest != shard.Digest || status.Ordinal != shard.Ordinal ||
			status.FileCount != shard.FileCount || status.ChunkCount != shard.ChunkCount || status.Bytes != shard.Bytes ||
			!equalReviewPaths(status.Paths, shard.Paths) {
			return fmt.Errorf("repository review shard status %q does not match its sealed plan", shard.ID)
		}
		if status.State == "done" {
			if _, ok := submissions[shard.ID]; !ok {
				return fmt.Errorf("completed repository review shard %q has no durable submission", shard.ID)
			}
		}
	}
	for shardID, submission := range submissions {
		if _, ok := statuses[shardID]; !ok || strings.TrimSpace(submission.Claimant) == "" {
			return fmt.Errorf("repository review submission %q is not owned by the sealed plan", shardID)
		}
	}
	return nil
}

func equalReviewPaths(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (a *App) restoreRepositoryReviewForWorkspace(workspace string) error {
	workspace = filepathSlashClean(workspace)
	if workspace == "" || workspace == "." || a == nil || a.db == nil {
		return nil
	}
	a.repositoryReviewMu.Lock()
	alreadyLoaded := a.repositoryReviewRuntime.State != "" && sameFilesystemPath(a.repositoryReviewRuntime.Workspace, workspace)
	a.repositoryReviewMu.Unlock()
	if alreadyLoaded {
		return nil
	}
	snapshot, err := a.latestRepositoryReviewSnapshot(appOperationContext(a), workspace)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	interrupted := snapshot.Status.State == "running" || snapshot.Status.State == "cancelling"
	if interrupted {
		snapshot.Status.State = "interrupted"
		snapshot.Status.CanCancel = false
		snapshot.Status.CurrentShard = ""
		snapshot.Status.CurrentConflict = ""
		snapshot.Status.Error = "The previous application process stopped before this review finished. Resume revalidates the immutable generation before continuing."
		for i := range snapshot.Status.Shards {
			if snapshot.Status.Shards[i].State == "running" {
				snapshot.Status.Shards[i].State = "interrupted"
			}
		}
		for i := range snapshot.Status.ConflictChecks {
			if snapshot.Status.ConflictChecks[i].State == "running" {
				snapshot.Status.ConflictChecks[i].State = "interrupted"
			}
		}
		snapshot.Status.CanResume = repositoryReviewCanResume(snapshot.Status)
		if err := a.saveRepositoryReviewSnapshot(context.Background(), snapshot); err != nil {
			return err
		}
	}
	a.repositoryReviewMu.Lock()
	if !a.repositoryReviewRuntime.CanCancel {
		a.repositoryReviewRuntime = cloneRepositoryReviewStatus(snapshot.Status)
		a.repositoryReviewPlan = snapshot.Plan
		a.repositoryReviewSubmissions = append([]repoindex.ReviewSubmission(nil), snapshot.Submissions...)
	}
	a.repositoryReviewMu.Unlock()
	return nil
}

func repositoryReviewCanResume(status RepositoryReviewStatus) bool {
	if status.CanCancel || strings.TrimSpace(status.ReviewID) == "" {
		return false
	}
	for _, shard := range status.Shards {
		if shard.State != "done" {
			return true
		}
	}
	for _, check := range status.ConflictChecks {
		if check.State != "done" {
			return true
		}
	}
	return false
}

func validateRepositoryReviewConflictStatuses(statuses []RepositoryReviewConflictStatus, cases []repoindex.ReviewConflictCase) error {
	byID := make(map[string]repoindex.ReviewConflictCase, len(cases))
	for _, conflict := range cases {
		byID[conflict.ID] = conflict
	}
	seen := map[string]bool{}
	for _, status := range statuses {
		if seen[status.ID] {
			return fmt.Errorf("repository review contains duplicate conflict check %q", status.ID)
		}
		seen[status.ID] = true
		conflict, ok := byID[status.ID]
		if !ok || status.EvidenceDigest != conflict.EvidenceDigest || !sameJSONValue(status.Evidence, conflict.Evidence) || !sameJSONValue(status.Claims, conflict.Claims) {
			return fmt.Errorf("repository review conflict check %q does not match the deterministic merge", status.ID)
		}
		switch status.State {
		case "pending", "running", "done", "error", "cancelled", "interrupted":
		default:
			return fmt.Errorf("repository review conflict check %q has invalid state %q", status.ID, status.State)
		}
		if status.Attempt < 0 {
			return fmt.Errorf("repository review conflict check %q has an invalid attempt", status.ID)
		}
		if status.State == "done" {
			if _, err := validateRepositoryConflictResponse(repositoryConflictResponse{
				Verdict: status.Verdict, SupportedClaimIDs: status.SupportedClaimIDs, Reason: status.Reason,
			}, conflict); err != nil {
				return fmt.Errorf("repository review conflict check %q is invalid: %w", status.ID, err)
			}
		} else if status.Verdict != "" || len(status.SupportedClaimIDs) != 0 || status.Reason != "" {
			return fmt.Errorf("unfinished repository review conflict check %q contains a final verdict", status.ID)
		}
	}
	return nil
}

func reconcileRepositoryReviewConflictStatuses(statuses []RepositoryReviewConflictStatus, cases []repoindex.ReviewConflictCase) []RepositoryReviewConflictStatus {
	existing := make(map[string]RepositoryReviewConflictStatus, len(statuses))
	for _, status := range statuses {
		existing[status.ID] = status
	}
	out := make([]RepositoryReviewConflictStatus, 0, len(cases))
	for _, conflict := range cases {
		status, ok := existing[conflict.ID]
		if !ok {
			status = RepositoryReviewConflictStatus{ID: conflict.ID, State: "pending", SupportedClaimIDs: []string{}}
		}
		status.EvidenceDigest = conflict.EvidenceDigest
		status.Evidence = append([]repoindex.ReviewFindingEvidence(nil), conflict.Evidence...)
		status.Claims = cloneReviewConflictClaims(conflict.Claims)
		if status.SupportedClaimIDs == nil {
			status.SupportedClaimIDs = []string{}
		}
		out = append(out, status)
	}
	return out
}

func sameJSONValue(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func sameRepositoryReviewMerge(left, right repoindex.ReviewMergeResult) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}
