package engagement

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mauler/internal/ledger"
)

type EventRecorder interface {
	Record(ledger.Event) (ledger.Event, error)
}

type EventReader interface {
	List(limit int) ([]ledger.Event, error)
}

type Service struct {
	store     *Store
	catalogMu sync.RWMutex
	catalog   Catalog
	recorder  EventRecorder
}

type CreateInput struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name"`
	Workspace   string   `json:"workspace"`
	WorkflowID  string   `json:"workflow_id"`
	Scope       []string `json:"scope,omitempty"`
	ScopeLocked bool     `json:"scope_locked"`
}

type ObservationInput struct {
	EngagementID     string  `json:"engagement_id"`
	Ref              WorkRef `json:"ref"`
	ClaimantID       string  `json:"claimant_id"`
	Status           string  `json:"status"`
	Observation      string  `json:"observation"`
	ExpectedRevision uint64  `json:"expected_revision"`
}

func NewService(db *sql.DB, catalog Catalog, recorder EventRecorder) *Service {
	return &Service{store: NewStore(db), catalog: catalog, recorder: recorder}
}

func NewServiceWithStore(store *Store, catalog Catalog, recorder EventRecorder) *Service {
	return &Service{store: store, catalog: catalog, recorder: recorder}
}

// SetCatalog atomically replaces the definitions available to future
// engagement creation. Existing engagements are unaffected because their exact
// workflow/checklist snapshots live in SQLite.
func (s *Service) SetCatalog(catalog Catalog) {
	if s == nil {
		return
	}
	s.catalogMu.Lock()
	s.catalog = catalog
	s.catalogMu.Unlock()
}

func (s *Service) bundle(workflowID string) (WorkflowDefinition, ChecklistDefinition, error) {
	if s == nil {
		return WorkflowDefinition{}, ChecklistDefinition{}, fmt.Errorf("engagement service is not configured")
	}
	s.catalogMu.RLock()
	defer s.catalogMu.RUnlock()
	return s.catalog.Bundle(workflowID)
}

func (s *Service) Create(ctx context.Context, input CreateInput, now time.Time) (Record, error) {
	if s == nil || s.store == nil {
		return Record{}, fmt.Errorf("engagement service is not configured")
	}
	workflowID := strings.TrimSpace(input.WorkflowID)
	if workflowID == "" {
		return Record{}, fmt.Errorf("workflow id is required")
	}
	workflow, checklist, err := s.bundle(workflowID)
	if err != nil {
		return Record{}, err
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id, err = newEngagementID()
		if err != nil {
			return Record{}, err
		}
	}
	state, err := NewState(id, input.Name, workflow, checklist, input.Scope, input.ScopeLocked, now)
	if err != nil {
		return Record{}, err
	}
	record := Record{Workspace: strings.TrimSpace(input.Workspace), Workflow: workflow, Checklist: checklist, State: state}
	if err := s.store.Create(ctx, record); err != nil {
		return Record{}, err
	}
	stored, err := s.store.Load(ctx, state.ID)
	if err != nil {
		return Record{}, err
	}
	s.record("engagement_create", "created", stored, nil, map[string]string{
		"workflow_id": workflow.ID, "workflow_version": workflow.Version,
		"checklist_id": checklist.ID, "checklist_version": checklist.Version,
	})
	return stored, nil
}

func (s *Service) Get(ctx context.Context, id string) (Record, error) {
	if s == nil || s.store == nil {
		return Record{}, fmt.Errorf("engagement service is not configured")
	}
	record, err := s.store.Load(ctx, id)
	if err != nil {
		return Record{}, err
	}
	record.EvidenceFreshness = evaluateEvidenceFreshness(record)
	return record, nil
}

func (s *Service) List(ctx context.Context, workspace string) ([]Summary, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("engagement service is not configured")
	}
	return s.store.List(ctx, workspace)
}

func (s *Service) Now(ctx context.Context, id string, now time.Time) (NextAction, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return NextAction{}, err
	}
	expected := record.State.Revision
	next, actionErr := record.State.Now(record.Workflow, now)
	if record.State.Revision != expected {
		if err := s.store.Update(ctx, record, expected); err != nil {
			return NextAction{}, err
		}
		s.record("engagement_claim_expired", "updated", record, nil, nil)
	}
	return next, actionErr
}

func (s *Service) Available(ctx context.Context, id, claimantID string, limit int, now time.Time) (AvailableWork, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return AvailableWork{}, err
	}
	expected := record.State.Revision
	available, actionErr := record.State.Available(record.Workflow, claimantID, limit, now)
	if record.State.Revision != expected {
		if err := s.store.Update(ctx, record, expected); err != nil {
			return AvailableWork{}, err
		}
		s.record("engagement_claim_expired", "updated", record, nil, nil)
	}
	return available, actionErr
}

func (s *Service) Claim(ctx context.Context, id string, ref WorkRef, claimant Claimant, lease time.Duration, now time.Time) (WorkState, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return WorkState{}, err
	}
	expected := record.State.Revision
	work, err := record.State.ClaimWork(record.Workflow, ref, claimant, lease, now)
	if err != nil {
		return WorkState{}, err
	}
	if err := s.store.Update(ctx, record, expected); err != nil {
		return WorkState{}, err
	}
	s.record("engagement_claim", "focused", record, &ref, map[string]string{"claimant_id": claimant.ID})
	return work, nil
}

// HeartbeatClaimant extends a live claim while preserving the work revision
// that the claimant must send with its eventual observation. Snapshot-level
// conflicts are retried because another operator or agent may update unrelated
// engagement state between the read and write.
func (s *Service) HeartbeatClaimant(ctx context.Context, id, claimantID string, lease time.Duration, now time.Time) (WorkState, bool, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		record, err := s.Get(ctx, id)
		if err != nil {
			return WorkState{}, false, err
		}
		expected := record.State.Revision
		work, changed, err := record.State.HeartbeatClaimant(claimantID, lease, now)
		if err != nil || !changed {
			return work, changed, err
		}
		if err := s.store.Update(ctx, record, expected); err != nil {
			if errors.Is(err, ErrRevisionConflict) {
				lastErr = err
				continue
			}
			return WorkState{}, false, err
		}
		s.record("engagement_claim_heartbeat", "active", record, &work.Ref, map[string]string{
			"claimant_id": strings.TrimSpace(claimantID), "lease_until": formatTime(work.Claim.LeaseUntil),
		})
		return work, true, nil
	}
	return WorkState{}, false, firstNonNilEngagement(lastErr, ErrRevisionConflict)
}

// ReleaseClaimant frees unfinished work on a clean run exit. It is also used
// by the operator's stale-claim recovery control.
func (s *Service) ReleaseClaimant(ctx context.Context, id, claimantID string, now time.Time) (WorkState, bool, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		record, err := s.Get(ctx, id)
		if err != nil {
			return WorkState{}, false, err
		}
		expected := record.State.Revision
		work, changed, err := record.State.ReleaseClaimant(claimantID, now)
		if err != nil || !changed {
			return work, changed, err
		}
		if err := s.store.Update(ctx, record, expected); err != nil {
			if errors.Is(err, ErrRevisionConflict) {
				lastErr = err
				continue
			}
			return WorkState{}, false, err
		}
		s.record("engagement_claim_release", "released", record, &work.Ref, map[string]string{
			"claimant_id": strings.TrimSpace(claimantID),
		})
		return work, true, nil
	}
	return WorkState{}, false, firstNonNilEngagement(lastErr, ErrRevisionConflict)
}

func firstNonNilEngagement(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func (s *Service) RecordResult(ctx context.Context, input ObservationInput, now time.Time) (WorkState, error) {
	record, err := s.Get(ctx, input.EngagementID)
	if err != nil {
		return WorkState{}, err
	}
	expectedStateRevision := record.State.Revision
	work, err := record.State.RecordWork(record.Workflow, input.Ref, input.ClaimantID, input.Status, input.Observation, input.ExpectedRevision, now)
	if err != nil {
		return WorkState{}, err
	}
	if err := s.store.Update(ctx, record, expectedStateRevision); err != nil {
		return WorkState{}, err
	}
	s.record("engagement_observation", input.Status, record, &input.Ref, map[string]string{"claimant_id": input.ClaimantID})
	return work, nil
}

func (s *Service) Finish(ctx context.Context, id string, ref WorkRef, claimantID string, expectedWorkRevision uint64, now time.Time) (FinishResult, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return FinishResult{}, err
	}
	expectedStateRevision := record.State.Revision
	result, err := record.State.FinishWork(record.Workflow, record.Checklist, ref, claimantID, expectedWorkRevision, now)
	if err != nil {
		return FinishResult{}, err
	}
	if err := s.store.Update(ctx, record, expectedStateRevision); err != nil {
		return FinishResult{}, err
	}
	s.record("engagement_finish", result.Work.Status, record, &ref, map[string]string{
		"claimant_id": claimantID, "runs_completed": fmt.Sprintf("%d", result.Work.RunsCompleted),
	})
	return result, nil
}

func (s *Service) AddEvidence(ctx context.Context, input EvidenceInput, now time.Time) (Evidence, error) {
	record, err := s.Get(ctx, input.EngagementID)
	if err != nil {
		return Evidence{}, err
	}
	if strings.TrimSpace(input.ID) == "" {
		input.ID, err = newResourceID("evidence")
		if err != nil {
			return Evidence{}, err
		}
	}
	evidence := Evidence{
		ID: input.ID, Work: input.Work, SourceKind: strings.ToLower(strings.TrimSpace(input.SourceKind)),
		LedgerEventID: strings.TrimSpace(input.LedgerEventID), Description: strings.TrimSpace(input.Description),
	}
	if evidence.LedgerEventID != "" {
		event, findErr := s.findLedgerEvent(evidence.LedgerEventID)
		if findErr != nil {
			return Evidence{}, findErr
		}
		payload := firstNonEmptyEngagement(event.Output, event.Detail, event.Input, event.Message)
		sum := sha256.Sum256([]byte(payload))
		evidence.SHA256, evidence.Size = hex.EncodeToString(sum[:]), int64(len(payload))
		evidence.FingerprintKind = FingerprintLedgerPayloadSHA256
		evidence.AgentComposed = ledgerEventAgentComposed(event)
		evidence.SourceKind = evidenceKindForEvent(event)
		if evidence.Path == "" {
			evidence.Path = firstLedgerPath(event)
		}
	}
	if strings.TrimSpace(input.Path) != "" {
		path, relative, size, digest, pathErr := hashWorkspaceEvidence(record.Workspace, input.Path)
		if pathErr != nil {
			return Evidence{}, pathErr
		}
		evidence.Path, evidence.Size, evidence.SHA256 = relative, size, digest
		evidence.FingerprintKind = FingerprintFileSHA256
		matched, found := s.findLedgerEventForPath(path)
		if found {
			evidence.LedgerEventID = matched.ID
			evidence.AgentComposed = ledgerEventAgentComposed(matched)
			evidence.SourceKind = evidenceKindForEvent(matched)
		} else {
			evidence.AgentComposed = !input.OperatorTrusted
		}
	}
	if evidence.SourceKind == "" {
		evidence.SourceKind = EvidenceArtifact
	}
	expected := record.State.Revision
	created, err := record.State.AddEvidence(record.Workflow, evidence, input.Claimant.ID, now)
	if err != nil {
		return Evidence{}, err
	}
	if err := s.store.Update(ctx, record, expected); err != nil {
		return Evidence{}, err
	}
	s.record("engagement_evidence_add", "created", record, &input.Work, map[string]string{
		"evidence_id": created.ID, "source_kind": created.SourceKind, "ledger_event_id": created.LedgerEventID,
	})
	return created, nil
}

func (s *Service) UpsertFinding(ctx context.Context, input FindingInput, now time.Time) (Finding, error) {
	record, err := s.Get(ctx, input.EngagementID)
	if err != nil {
		return Finding{}, err
	}
	if strings.TrimSpace(input.ID) == "" {
		input.ID, err = newResourceID("finding")
		if err != nil {
			return Finding{}, err
		}
	}
	finding := Finding{
		ID: input.ID, Work: input.Work, Title: input.Title, Severity: input.Severity,
		Description: input.Description, Impact: input.Impact, Recommendation: input.Recommendation,
		Confidence: input.Confidence, Reproduction: input.Reproduction, EvidenceIDs: append([]string(nil), input.EvidenceIDs...),
	}
	expected := record.State.Revision
	updated, err := record.State.UpsertFinding(record.Workflow, finding, input.Claimant.ID, input.ExpectedRevision, now)
	if err != nil {
		return Finding{}, err
	}
	if err := s.store.Update(ctx, record, expected); err != nil {
		return Finding{}, err
	}
	s.record("engagement_finding_upsert", updated.State, record, &updated.Work, map[string]string{
		"finding_id": updated.ID, "severity": updated.Severity, "finding_revision": fmt.Sprintf("%d", updated.Revision),
	})
	return updated, nil
}

func (s *Service) ConfirmFinding(ctx context.Context, engagementID, findingID, claimantID string, expectedRevision uint64, operatorWaiver string, allowWaiver bool, now time.Time) (Finding, error) {
	record, err := s.Get(ctx, engagementID)
	if err != nil {
		return Finding{}, err
	}
	finding := record.State.Findings[strings.TrimSpace(findingID)]
	if finding == nil {
		return Finding{}, fmt.Errorf("unknown finding %q", findingID)
	}
	if err := ensureFindingEvidenceFresh(record, finding); err != nil {
		return Finding{}, err
	}
	expected := record.State.Revision
	confirmed, err := record.State.ConfirmFinding(record.Workflow, findingID, claimantID, expectedRevision, operatorWaiver, allowWaiver, now)
	if err != nil {
		return Finding{}, err
	}
	if err := s.store.Update(ctx, record, expected); err != nil {
		return Finding{}, err
	}
	s.record("engagement_finding_confirm", "confirmed", record, &confirmed.Work, map[string]string{
		"finding_id": confirmed.ID, "severity": confirmed.Severity, "operator_waiver": fmt.Sprintf("%t", confirmed.OperatorWaiver != ""),
	})
	return confirmed, nil
}

func evaluateEvidenceFreshness(record Record) map[string]EvidenceFreshness {
	result := make(map[string]EvidenceFreshness)
	if record.State == nil {
		return result
	}
	for id, evidence := range record.State.Evidence {
		if evidence == nil {
			continue
		}
		result[id] = evaluateOneEvidenceFreshness(record.Workspace, evidence)
	}
	return result
}

func evaluateOneEvidenceFreshness(workspace string, evidence *Evidence) EvidenceFreshness {
	if evidence == nil {
		return EvidenceFreshness{State: EvidenceUnverifiable, Detail: "evidence record is missing"}
	}
	kind := strings.TrimSpace(evidence.FingerprintKind)
	if kind == FingerprintLedgerPayloadSHA256 || (kind == "" && evidence.LedgerEventID != "" && filepath.IsAbs(evidence.Path)) {
		return EvidenceFreshness{State: EvidenceImmutable, Detail: "RunLedger payload fingerprint is immutable"}
	}
	if strings.TrimSpace(evidence.Path) == "" {
		if evidence.LedgerEventID != "" && evidence.SHA256 != "" {
			return EvidenceFreshness{State: EvidenceImmutable, Detail: "RunLedger payload fingerprint is immutable"}
		}
		return EvidenceFreshness{State: EvidenceUnverifiable, Detail: "no file path is attached to this fingerprint"}
	}
	if strings.TrimSpace(evidence.SHA256) == "" {
		return EvidenceFreshness{State: EvidenceUnverifiable, Detail: "stored SHA-256 fingerprint is missing"}
	}
	_, _, size, digest, err := hashWorkspaceEvidence(workspace, evidence.Path)
	if err != nil {
		return EvidenceFreshness{State: EvidenceStale, Detail: err.Error()}
	}
	current := EvidenceFreshness{CurrentSHA256: digest, CurrentSize: size}
	if !strings.EqualFold(digest, evidence.SHA256) || size != evidence.Size {
		current.State = EvidenceStale
		current.Detail = "file bytes changed after evidence was attached"
		return current
	}
	current.State = EvidenceFresh
	current.Detail = "file fingerprint matches the attached evidence"
	return current
}

func ensureFindingEvidenceFresh(record Record, finding *Finding) error {
	if finding == nil {
		return fmt.Errorf("finding is required")
	}
	freshness := record.EvidenceFreshness
	if freshness == nil {
		freshness = evaluateEvidenceFreshness(record)
	}
	for _, evidenceID := range finding.EvidenceIDs {
		status, ok := freshness[evidenceID]
		if !ok {
			return fmt.Errorf("finding %q references missing evidence %q", finding.ID, evidenceID)
		}
		if status.State == EvidenceStale || status.State == EvidenceUnverifiable {
			return fmt.Errorf("finding %q cannot be confirmed: evidence %q is %s (%s); attach fresh evidence for the current artifact bytes", finding.ID, evidenceID, status.State, status.Detail)
		}
	}
	return nil
}

func (s *Service) findLedgerEvent(id string) (ledger.Event, error) {
	reader, ok := s.recorder.(EventReader)
	if !ok {
		return ledger.Event{}, fmt.Errorf("RunLedger lookup is unavailable")
	}
	events, err := reader.List(5000)
	if err != nil {
		return ledger.Event{}, err
	}
	for _, event := range events {
		if event.ID == id {
			return event, nil
		}
	}
	return ledger.Event{}, fmt.Errorf("RunLedger event %q was not found", id)
}

func (s *Service) findLedgerEventForPath(path string) (ledger.Event, bool) {
	reader, ok := s.recorder.(EventReader)
	if !ok {
		return ledger.Event{}, false
	}
	events, err := reader.List(5000)
	if err != nil {
		return ledger.Event{}, false
	}
	wanted, _ := filepath.Abs(path)
	for _, event := range events {
		for _, candidate := range append(append([]string{}, event.Files...), event.Artifacts...) {
			absolute, absErr := filepath.Abs(candidate)
			if absErr == nil && strings.EqualFold(filepath.Clean(absolute), filepath.Clean(wanted)) {
				return event, true
			}
		}
	}
	return ledger.Event{}, false
}

func hashWorkspaceEvidence(workspace, inputPath string) (absolute, relative string, size int64, digest string, err error) {
	root, err := filepath.Abs(strings.TrimSpace(workspace))
	if err != nil {
		return "", "", 0, "", err
	}
	path := strings.TrimSpace(inputPath)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", "", 0, "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", 0, "", fmt.Errorf("evidence file must be inside engagement workspace %q", root)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", "", 0, "", fmt.Errorf("open evidence file: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err = io.Copy(hash, file)
	if err != nil {
		return "", "", 0, "", err
	}
	return path, filepath.ToSlash(rel), size, hex.EncodeToString(hash.Sum(nil)), nil
}

func ledgerEventAgentComposed(event ledger.Event) bool {
	source, kind := strings.ToLower(event.Source), strings.ToLower(event.Kind)
	if source == "model" || source == "assistant" || source == "engagement" || strings.Contains(kind, "model_response") {
		return true
	}
	if strings.TrimSpace(event.Tool) != "" || len(event.Files) > 0 || len(event.Artifacts) > 0 || source == "operator" {
		return false
	}
	return true
}

func evidenceKindForEvent(event ledger.Event) string {
	tool := strings.ToLower(event.Tool)
	if tool == "http_probe" {
		return EvidenceHTTPCapture
	}
	if strings.Contains(tool, "browser") && len(event.Artifacts) > 0 {
		return EvidenceScreenshot
	}
	if len(event.Artifacts) > 0 || len(event.Files) > 0 {
		return EvidenceArtifact
	}
	return EvidenceLedgerEvent
}

func firstLedgerPath(event ledger.Event) string {
	if len(event.Artifacts) > 0 {
		return event.Artifacts[0]
	}
	if len(event.Files) > 0 {
		return event.Files[0]
	}
	return ""
}

func firstNonEmptyEngagement(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Service) AddEndpoint(ctx context.Context, id string, input EndpointInput, now time.Time) (Endpoint, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return Endpoint{}, err
	}
	if strings.TrimSpace(input.ID) == "" {
		input.ID, err = newResourceID("endpoint")
		if err != nil {
			return Endpoint{}, err
		}
	}
	expected := record.State.Revision
	endpoint, err := record.State.AddEndpoint(record.Checklist, input, now)
	if err != nil {
		return Endpoint{}, err
	}
	if err := s.store.Update(ctx, record, expected); err != nil {
		return Endpoint{}, err
	}
	s.record("engagement_endpoint_add", "created", record, nil, map[string]string{
		"endpoint_id": endpoint.ID, "method": endpoint.Method, "url": endpoint.URL,
	})
	return endpoint, nil
}

func (s *Service) SetEndpointGroup(ctx context.Context, id, endpointID, group string, now time.Time) (Endpoint, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return Endpoint{}, err
	}
	expected := record.State.Revision
	endpoint, err := record.State.SetEndpointGroup(endpointID, group, now)
	if err != nil {
		return Endpoint{}, err
	}
	if err := s.store.Update(ctx, record, expected); err != nil {
		return Endpoint{}, err
	}
	s.record("engagement_endpoint_group", "updated", record, nil, map[string]string{
		"endpoint_id": endpoint.ID, "feature_group": endpoint.FeatureGroup,
	})
	return endpoint, nil
}

func (s *Service) SetNotes(ctx context.Context, id, notes string, expectedNotesRevision uint64, now time.Time) (uint64, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	expectedStateRevision := record.State.Revision
	notesRevision, err := record.State.SetNotes(notes, expectedNotesRevision, now)
	if err != nil {
		return 0, err
	}
	if err := s.store.Update(ctx, record, expectedStateRevision); err != nil {
		return 0, err
	}
	s.record("engagement_notes_update", "updated", record, nil, map[string]string{
		"notes_revision": fmt.Sprintf("%d", notesRevision),
		"notes_runes":    fmt.Sprintf("%d", len([]rune(notes))),
	})
	return notesRevision, nil
}

func (s *Service) Advance(ctx context.Context, id string, now time.Time) (string, error) {
	record, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	expected := record.State.Revision
	from := record.State.CurrentPhase
	phase, err := record.State.Advance(record.Workflow, now)
	if err != nil {
		return from, err
	}
	if record.State.Revision == expected {
		return phase, nil
	}
	if err := s.store.Update(ctx, record, expected); err != nil {
		return from, err
	}
	s.record("engagement_advance", "advanced", record, nil, map[string]string{"from_phase": from, "to_phase": phase})
	return phase, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	record, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.record("engagement_delete", "deleted", record, nil, nil)
	return nil
}

func (s *Service) record(kind, status string, record Record, ref *WorkRef, metadata map[string]string) {
	if s == nil || s.recorder == nil || record.State == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["engagement_id"] = record.State.ID
	metadata["workspace"] = record.Workspace
	metadata["phase_id"] = record.State.CurrentPhase
	if ref != nil {
		metadata["work_kind"] = ref.Kind
		metadata["work_id"] = ref.ID
		if ref.PhaseID != "" {
			metadata["work_phase_id"] = ref.PhaseID
		}
	}
	_, _ = s.recorder.Record(ledger.Event{
		Kind: kind, Source: "engagement", Status: status, State: record.State.CurrentPhase,
		Message: record.State.Name, Metadata: metadata, Timestamp: formatTime(record.State.UpdatedAt),
	})
}

func newEngagementID() (string, error) {
	return newResourceID("eng")
}

func newResourceID(prefix string) (string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return strings.TrimSpace(prefix) + "-" + hex.EncodeToString(buffer), nil
}
