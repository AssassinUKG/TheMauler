package engagement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrEngagementNotFound = errors.New("engagement not found")

type Record struct {
	Workspace         string                       `json:"workspace"`
	Workflow          WorkflowDefinition           `json:"workflow"`
	Checklist         ChecklistDefinition          `json:"checklist"`
	State             *State                       `json:"state"`
	EvidenceFreshness map[string]EvidenceFreshness `json:"evidence_freshness,omitempty"`
}

type Summary struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Workspace        string    `json:"workspace"`
	WorkflowID       string    `json:"workflow_id"`
	WorkflowVersion  string    `json:"workflow_version,omitempty"`
	ChecklistID      string    `json:"checklist_id"`
	ChecklistVersion string    `json:"checklist_version,omitempty"`
	CurrentPhase     string    `json:"current_phase"`
	Revision         uint64    `json:"revision"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, record Record) error {
	encoded, err := encodeRecord(record)
	if err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("engagement store is not configured")
	}
	_, err = s.db.ExecContext(ctx, `
insert into engagements (
  id, name, workspace, workflow_id, workflow_version, workflow_digest,
  checklist_id, checklist_version, checklist_digest,
  workflow_json, checklist_json, state_json, revision, created_at, updated_at
) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.State.ID, record.State.Name, strings.TrimSpace(record.Workspace),
		record.Workflow.ID, record.Workflow.Version, encoded.workflowDigest,
		record.Checklist.ID, record.Checklist.Version, encoded.checklistDigest,
		string(encoded.workflow), string(encoded.checklist), string(encoded.state), record.State.Revision,
		formatTime(record.State.CreatedAt), formatTime(record.State.UpdatedAt),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("engagement %q already exists", record.State.ID)
		}
		return fmt.Errorf("create engagement %q: %w", record.State.ID, err)
	}
	return nil
}

// Update applies a complete immutable-definition/state snapshot using an
// optimistic revision guard. Pack definitions cannot silently change during a
// normal state update because their ids, versions, and digests are compared in
// the WHERE clause.
func (s *Store) Update(ctx context.Context, record Record, expectedRevision uint64) error {
	encoded, err := encodeRecord(record)
	if err != nil {
		return err
	}
	if record.State.Revision <= expectedRevision {
		return fmt.Errorf("%w: updated revision %d must be greater than expected %d", ErrRevisionConflict, record.State.Revision, expectedRevision)
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("engagement store is not configured")
	}
	result, err := s.db.ExecContext(ctx, `
update engagements set
  name = ?, state_json = ?, revision = ?, updated_at = ?
where id = ? and revision = ?
  and workflow_id = ? and workflow_version = ? and workflow_digest = ?
  and checklist_id = ? and checklist_version = ? and checklist_digest = ?`,
		record.State.Name, string(encoded.state), record.State.Revision, formatTime(record.State.UpdatedAt),
		record.State.ID, expectedRevision,
		record.Workflow.ID, record.Workflow.Version, encoded.workflowDigest,
		record.Checklist.ID, record.Checklist.Version, encoded.checklistDigest,
	)
	if err != nil {
		return fmt.Errorf("update engagement %q: %w", record.State.ID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `select count(*) from engagements where id = ?`, record.State.ID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("%w: %s", ErrEngagementNotFound, record.State.ID)
		}
		return fmt.Errorf("%w: engagement %s expected revision %d or pinned pack changed", ErrRevisionConflict, record.State.ID, expectedRevision)
	}
	return nil
}

func (s *Store) Load(ctx context.Context, id string) (Record, error) {
	if s == nil || s.db == nil {
		return Record{}, fmt.Errorf("engagement store is not configured")
	}
	row := s.db.QueryRowContext(ctx, `
select workspace, workflow_id, workflow_version, workflow_digest,
       checklist_id, checklist_version, checklist_digest,
       workflow_json, checklist_json, state_json
from engagements where id = ?`, strings.TrimSpace(id))
	var workspace, workflowID, workflowVersion, workflowDigest string
	var checklistID, checklistVersion, checklistDigest, workflowJSON, checklistJSON, stateJSON string
	if err := row.Scan(&workspace, &workflowID, &workflowVersion, &workflowDigest,
		&checklistID, &checklistVersion, &checklistDigest,
		&workflowJSON, &checklistJSON, &stateJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, fmt.Errorf("%w: %s", ErrEngagementNotFound, id)
		}
		return Record{}, err
	}
	workflow, err := ParseWorkflow([]byte(workflowJSON))
	if err != nil {
		return Record{}, fmt.Errorf("decode stored workflow: %w", err)
	}
	checklist, err := ParseChecklist([]byte(checklistJSON))
	if err != nil {
		return Record{}, fmt.Errorf("decode stored checklist: %w", err)
	}
	if definitionDigest([]byte(workflowJSON)) != workflowDigest || definitionDigest([]byte(checklistJSON)) != checklistDigest {
		return Record{}, fmt.Errorf("stored engagement %q definition digest mismatch", id)
	}
	if workflow.ID != workflowID || workflow.Version != workflowVersion || checklist.ID != checklistID || checklist.Version != checklistVersion {
		return Record{}, fmt.Errorf("stored engagement %q definition identity mismatch", id)
	}
	workflow.Digest = workflowDigest
	checklist.Digest = checklistDigest
	var state State
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return Record{}, fmt.Errorf("decode stored engagement state: %w", err)
	}
	state.ensureCollections()
	record := Record{Workspace: workspace, Workflow: workflow, Checklist: checklist, State: &state}
	if err := validateRecord(record); err != nil {
		return Record{}, fmt.Errorf("stored engagement %q is invalid: %w", id, err)
	}
	return record, nil
}

func (s *Store) List(ctx context.Context, workspace string) ([]Summary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("engagement store is not configured")
	}
	query := `select id, name, workspace, workflow_id, workflow_version, checklist_id, checklist_version, state_json, revision, created_at, updated_at from engagements`
	args := []any{}
	if workspace = strings.TrimSpace(workspace); workspace != "" {
		query += ` where lower(workspace) = lower(?)`
		args = append(args, workspace)
	}
	query += ` order by updated_at desc, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var summary Summary
		var stateJSON, createdAt, updatedAt string
		if err := rows.Scan(&summary.ID, &summary.Name, &summary.Workspace, &summary.WorkflowID, &summary.WorkflowVersion,
			&summary.ChecklistID, &summary.ChecklistVersion, &stateJSON, &summary.Revision, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var state struct {
			CurrentPhase string `json:"current_phase"`
		}
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			return nil, fmt.Errorf("decode engagement %q summary state: %w", summary.ID, err)
		}
		summary.CurrentPhase = state.CurrentPhase
		summary.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		summary.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("engagement store is not configured")
	}
	result, err := s.db.ExecContext(ctx, `delete from engagements where id = ?`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrEngagementNotFound, id)
	}
	return nil
}

type encodedRecord struct {
	workflow        []byte
	checklist       []byte
	state           []byte
	workflowDigest  string
	checklistDigest string
}

func encodeRecord(record Record) (encodedRecord, error) {
	if err := validateRecord(record); err != nil {
		return encodedRecord{}, err
	}
	workflow, err := json.Marshal(record.Workflow)
	if err != nil {
		return encodedRecord{}, err
	}
	checklist, err := json.Marshal(record.Checklist)
	if err != nil {
		return encodedRecord{}, err
	}
	state, err := json.Marshal(record.State)
	if err != nil {
		return encodedRecord{}, err
	}
	return encodedRecord{
		workflow: workflow, checklist: checklist, state: state,
		workflowDigest: definitionDigest(workflow), checklistDigest: definitionDigest(checklist),
	}, nil
}

func validateRecord(record Record) error {
	if strings.TrimSpace(record.Workspace) == "" {
		return fmt.Errorf("engagement workspace is required")
	}
	if record.State == nil {
		return fmt.Errorf("engagement state is required")
	}
	if err := ValidateBundle(record.Workflow, record.Checklist); err != nil {
		return err
	}
	if record.State.ID == "" || record.State.Name == "" || record.State.Revision == 0 {
		return fmt.Errorf("engagement state id, name, and revision are required")
	}
	if record.State.WorkflowID != record.Workflow.ID || record.State.ChecklistID != record.Checklist.ID {
		return fmt.Errorf("engagement state definition ids do not match pinned definitions")
	}
	record.State.ensureCollections()
	if record.State.Steps == nil || record.State.GlobalChecks == nil || record.State.Endpoints == nil || record.State.EndpointChecks == nil {
		return fmt.Errorf("engagement state work maps are required")
	}
	return nil
}

func formatTime(value time.Time) string {
	return normaliseTime(value).Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse engagement time %q: %w", value, err)
	}
	return parsed.UTC(), nil
}
