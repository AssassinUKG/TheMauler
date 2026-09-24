package engagement

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const EngagementExportSchema = 1

type ExportEnvelope struct {
	SchemaVersion   int    `json:"schema_version"`
	ExportedAt      string `json:"exported_at"`
	WorkflowDigest  string `json:"workflow_digest"`
	ChecklistDigest string `json:"checklist_digest"`
	Record          Record `json:"record"`
}

func (s *Service) ExportJSON(ctx context.Context, id string, now time.Time) (string, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	clone, err := cloneRecordForExport(record)
	if err != nil {
		return "", err
	}
	if err := validatePortableState(clone.State); err != nil {
		return "", err
	}
	envelope := ExportEnvelope{
		SchemaVersion: EngagementExportSchema, ExportedAt: formatTime(now),
		WorkflowDigest: record.Workflow.Digest, ChecklistDigest: record.Checklist.Digest, Record: clone,
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Service) ImportJSON(ctx context.Context, raw, workspace string, now time.Time) (Record, error) {
	if s == nil || s.store == nil {
		return Record{}, fmt.Errorf("engagement service is not configured")
	}
	if len(raw) > 16<<20 {
		return Record{}, fmt.Errorf("engagement import exceeds 16 MiB")
	}
	var envelope ExportEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return Record{}, fmt.Errorf("decode engagement export: %w", err)
	}
	if envelope.SchemaVersion != EngagementExportSchema {
		return Record{}, fmt.Errorf("unsupported engagement export schema %d", envelope.SchemaVersion)
	}
	record := envelope.Record
	if record.State == nil {
		return Record{}, fmt.Errorf("engagement export has no state")
	}
	workflowJSON, _ := json.Marshal(record.Workflow)
	checklistJSON, _ := json.Marshal(record.Checklist)
	if envelope.WorkflowDigest == "" || definitionDigest(workflowJSON) != envelope.WorkflowDigest {
		return Record{}, fmt.Errorf("engagement workflow digest mismatch")
	}
	if envelope.ChecklistDigest == "" || definitionDigest(checklistJSON) != envelope.ChecklistDigest {
		return Record{}, fmt.Errorf("engagement checklist digest mismatch")
	}
	record.Workflow.Digest = envelope.WorkflowDigest
	record.Checklist.Digest = envelope.ChecklistDigest
	record.Workspace = strings.TrimSpace(workspace)
	if record.Workspace == "" {
		return Record{}, fmt.Errorf("current workspace is required for import")
	}
	record.State.ensureCollections()
	if err := validatePortableState(record.State); err != nil {
		return Record{}, err
	}
	if err := validateRecord(record); err != nil {
		return Record{}, err
	}
	if workflowPhaseIndex(record.Workflow, record.State.CurrentPhase) < 0 {
		return Record{}, fmt.Errorf("imported current phase %q is not in the pinned workflow", record.State.CurrentPhase)
	}
	clearImportedClaims(record.State, now)
	if err := s.store.Create(ctx, record); err != nil {
		return Record{}, err
	}
	stored, err := s.store.Load(ctx, record.State.ID)
	if err != nil {
		return Record{}, err
	}
	s.record("engagement_import", "imported", stored, nil, map[string]string{
		"schema_version": fmt.Sprintf("%d", envelope.SchemaVersion), "exported_at": envelope.ExportedAt,
	})
	return stored, nil
}

func cloneRecordForExport(record Record) (Record, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return Record{}, err
	}
	var clone Record
	if err := json.Unmarshal(data, &clone); err != nil {
		return Record{}, err
	}
	clone.Workflow.Digest = record.Workflow.Digest
	clone.Checklist.Digest = record.Checklist.Digest
	clone.EvidenceFreshness = nil
	clone.State.ensureCollections()
	root, _ := filepath.Abs(record.Workspace)
	for _, evidence := range clone.State.Evidence {
		if evidence == nil || strings.TrimSpace(evidence.Path) == "" || !filepath.IsAbs(evidence.Path) {
			continue
		}
		rel, relErr := filepath.Rel(root, evidence.Path)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			evidence.Path = filepath.ToSlash(rel)
		} else if evidence.LedgerEventID != "" {
			evidence.Path = ""
		} else {
			return Record{}, fmt.Errorf("evidence %q points outside the engagement workspace", evidence.ID)
		}
	}
	return clone, nil
}

func validatePortableState(state *State) error {
	if state == nil {
		return fmt.Errorf("engagement state is required")
	}
	state.ensureCollections()
	state.EvidenceOrder = completeOrder(state.EvidenceOrder, state.Evidence)
	state.FindingOrder = completeOrder(state.FindingOrder, state.Findings)
	for id, evidence := range state.Evidence {
		if evidence == nil || evidence.ID != id {
			return fmt.Errorf("invalid evidence entry %q", id)
		}
		if evidence.Path != "" {
			if filepath.IsAbs(evidence.Path) {
				return fmt.Errorf("evidence %q path must be workspace-relative", id)
			}
			clean := filepath.Clean(filepath.FromSlash(evidence.Path))
			if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return fmt.Errorf("evidence %q path escapes the workspace", id)
			}
		}
		if evidence.SHA256 != "" {
			decoded, err := hex.DecodeString(evidence.SHA256)
			if err != nil || len(decoded) != 32 {
				return fmt.Errorf("evidence %q has invalid SHA-256", id)
			}
		}
		if !stateHasWork(state, evidence.Work) {
			return fmt.Errorf("evidence %q references unknown work", id)
		}
	}
	for id, finding := range state.Findings {
		if finding == nil || finding.ID != id {
			return fmt.Errorf("invalid finding entry %q", id)
		}
		if !stateHasWork(state, finding.Work) {
			return fmt.Errorf("finding %q references unknown work", id)
		}
		for _, evidenceID := range finding.EvidenceIDs {
			if state.Evidence[evidenceID] == nil {
				return fmt.Errorf("finding %q references unknown evidence %q", id, evidenceID)
			}
		}
	}
	return nil
}

func stateHasWork(state *State, ref WorkRef) bool {
	if state == nil {
		return false
	}
	switch ref.Kind {
	case WorkStep:
		return state.Steps[ref.Key()] != nil
	case WorkGlobalCheck:
		return state.GlobalChecks[ref.Key()] != nil
	case WorkEndpointCheck:
		return state.EndpointChecks[ref.Key()] != nil
	default:
		return false
	}
}

func completeOrder[T any](order []string, values map[string]*T) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, id := range order {
		if values[id] != nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	missing := make([]string, 0, len(values)-len(out))
	for id, value := range values {
		if value != nil && !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return append(out, missing...)
}

func clearImportedClaims(state *State, now time.Time) {
	if state == nil {
		return
	}
	now = normaliseTime(now)
	changed := false
	for _, work := range state.allWork() {
		if work == nil || work.Claim == nil {
			continue
		}
		work.Claim = nil
		if !work.Finished && (work.Status == StatusFocused || isTerminalStatus(work.Ref.Kind, work.Status)) {
			work.Status = StatusPending
		}
		work.Revision++
		work.UpdatedAt = now
		changed = true
	}
	if changed {
		state.Revision++
		state.UpdatedAt = now
	}
}
