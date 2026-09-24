package engagement

import (
	"fmt"
	"strings"
	"time"
)

const (
	EvidenceLedgerEvent  = "ledger_event"
	EvidenceArtifact     = "artifact"
	EvidenceScreenshot   = "screenshot"
	EvidenceHTTPCapture  = "http_capture"
	EvidenceExternalFile = "external_file"

	FindingDraft     = "draft"
	FindingConfirmed = "confirmed"
	FindingRejected  = "rejected"

	FingerprintFileSHA256          = "file_sha256"
	FingerprintLedgerPayloadSHA256 = "ledger_payload_sha256"

	EvidenceFresh        = "fresh"
	EvidenceStale        = "stale"
	EvidenceImmutable    = "immutable"
	EvidenceUnverifiable = "unverifiable"
)

type Evidence struct {
	ID              string    `json:"id"`
	Work            WorkRef   `json:"work"`
	FindingID       string    `json:"finding_id,omitempty"`
	SourceKind      string    `json:"source_kind"`
	LedgerEventID   string    `json:"ledger_event_id,omitempty"`
	Path            string    `json:"path,omitempty"`
	SHA256          string    `json:"sha256,omitempty"`
	Size            int64     `json:"size,omitempty"`
	FingerprintKind string    `json:"fingerprint_kind,omitempty"`
	AgentComposed   bool      `json:"agent_composed"`
	Description     string    `json:"description"`
	Run             int       `json:"run"`
	CreatedBy       Claimant  `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

// EvidenceFreshness is derived on read. The stored SHA-256 remains the
// authoritative fingerprint captured when evidence was attached; current_* is
// only a comparison result and is never accepted as replacement evidence.
type EvidenceFreshness struct {
	State         string `json:"state"`
	Detail        string `json:"detail,omitempty"`
	CurrentSHA256 string `json:"current_sha256,omitempty"`
	CurrentSize   int64  `json:"current_size,omitempty"`
}

type EvidenceInput struct {
	ID              string   `json:"id,omitempty"`
	EngagementID    string   `json:"engagement_id"`
	Work            WorkRef  `json:"work"`
	Claimant        Claimant `json:"claimant"`
	SourceKind      string   `json:"source_kind"`
	LedgerEventID   string   `json:"ledger_event_id,omitempty"`
	Path            string   `json:"path,omitempty"`
	Description     string   `json:"description"`
	OperatorTrusted bool     `json:"operator_trusted,omitempty"`
}

type Finding struct {
	ID             string    `json:"id"`
	Work           WorkRef   `json:"work"`
	Title          string    `json:"title"`
	Severity       string    `json:"severity"`
	State          string    `json:"state"`
	Description    string    `json:"description,omitempty"`
	Impact         string    `json:"impact,omitempty"`
	Recommendation string    `json:"recommendation,omitempty"`
	Confidence     string    `json:"confidence,omitempty"`
	Reproduction   string    `json:"reproduction,omitempty"`
	EvidenceIDs    []string  `json:"evidence_ids"`
	OperatorWaiver string    `json:"operator_waiver,omitempty"`
	CreatedBy      Claimant  `json:"created_by"`
	Revision       uint64    `json:"revision"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type FindingInput struct {
	ID               string   `json:"id,omitempty"`
	EngagementID     string   `json:"engagement_id"`
	Work             WorkRef  `json:"work"`
	Claimant         Claimant `json:"claimant"`
	Title            string   `json:"title"`
	Severity         string   `json:"severity"`
	Description      string   `json:"description,omitempty"`
	Impact           string   `json:"impact,omitempty"`
	Recommendation   string   `json:"recommendation,omitempty"`
	Confidence       string   `json:"confidence,omitempty"`
	Reproduction     string   `json:"reproduction,omitempty"`
	EvidenceIDs      []string `json:"evidence_ids,omitempty"`
	ExpectedRevision uint64   `json:"expected_revision,omitempty"`
}

func (s *State) AddEvidence(workflow WorkflowDefinition, input Evidence, claimantID string, now time.Time) (Evidence, error) {
	s.ensureCollections()
	work, err := s.resolveAccessibleWork(workflow, input.Work)
	if err != nil {
		return Evidence{}, err
	}
	now = normaliseTime(now)
	if err := requireClaim(work, claimantID, now); err != nil {
		return Evidence{}, err
	}
	input.ID = strings.TrimSpace(input.ID)
	input.SourceKind = strings.ToLower(strings.TrimSpace(input.SourceKind))
	input.Description = strings.TrimSpace(input.Description)
	if input.ID == "" || input.Description == "" {
		return Evidence{}, fmt.Errorf("evidence id and description are required")
	}
	if !validEvidenceKind(input.SourceKind) {
		return Evidence{}, fmt.Errorf("unsupported evidence source kind %q", input.SourceKind)
	}
	if _, exists := s.Evidence[input.ID]; exists {
		return Evidence{}, fmt.Errorf("evidence %q already exists", input.ID)
	}
	if input.LedgerEventID == "" && input.Path == "" {
		return Evidence{}, fmt.Errorf("evidence must reference a RunLedger event or a hashed file")
	}
	input.Work = work.Ref
	input.Run = work.RunsCompleted + 1
	input.CreatedBy = work.Claim.Claimant
	input.CreatedAt = now
	s.Evidence[input.ID] = &input
	s.EvidenceOrder = append(s.EvidenceOrder, input.ID)
	s.Revision++
	s.UpdatedAt = now
	return input, nil
}

func (s *State) UpsertFinding(workflow WorkflowDefinition, input Finding, claimantID string, expectedRevision uint64, now time.Time) (Finding, error) {
	s.ensureCollections()
	work, err := s.resolveAccessibleWork(workflow, input.Work)
	if err != nil {
		return Finding{}, err
	}
	now = normaliseTime(now)
	if err := requireClaim(work, claimantID, now); err != nil {
		return Finding{}, err
	}
	input.ID, input.Title = strings.TrimSpace(input.ID), strings.TrimSpace(input.Title)
	input.Severity = strings.ToLower(strings.TrimSpace(input.Severity))
	if input.ID == "" || input.Title == "" || !validSeverity(input.Severity) {
		return Finding{}, fmt.Errorf("finding id, title, and a valid severity are required")
	}
	for _, evidenceID := range input.EvidenceIDs {
		evidence := s.Evidence[evidenceID]
		if evidence == nil || evidence.Work != work.Ref {
			return Finding{}, fmt.Errorf("evidence %q is missing or belongs to another work item", evidenceID)
		}
	}
	existing := s.Findings[input.ID]
	if existing == nil {
		if expectedRevision != 0 {
			return Finding{}, fmt.Errorf("%w: new finding expects revision 0", ErrRevisionConflict)
		}
		input.State, input.Work, input.CreatedBy = FindingDraft, work.Ref, work.Claim.Claimant
		input.Revision, input.CreatedAt, input.UpdatedAt = 1, now, now
		s.Findings[input.ID] = &input
		s.FindingOrder = append(s.FindingOrder, input.ID)
	} else {
		if expectedRevision != existing.Revision {
			return Finding{}, fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expectedRevision, existing.Revision)
		}
		if existing.State == FindingConfirmed {
			return Finding{}, fmt.Errorf("confirmed finding %q must be returned to draft before editing", input.ID)
		}
		input.State, input.Work, input.CreatedBy = FindingDraft, work.Ref, existing.CreatedBy
		input.Revision, input.CreatedAt, input.UpdatedAt = existing.Revision+1, existing.CreatedAt, now
		s.Findings[input.ID] = &input
	}
	s.Revision++
	s.UpdatedAt = now
	return *s.Findings[input.ID], nil
}

func (s *State) ConfirmFinding(workflow WorkflowDefinition, id, claimantID string, expectedRevision uint64, operatorWaiver string, allowWaiver bool, now time.Time) (Finding, error) {
	s.ensureCollections()
	finding := s.Findings[strings.TrimSpace(id)]
	if finding == nil {
		return Finding{}, fmt.Errorf("unknown finding %q", id)
	}
	work, err := s.resolveAccessibleWork(workflow, finding.Work)
	if err != nil {
		return Finding{}, err
	}
	now = normaliseTime(now)
	if err := requireClaim(work, claimantID, now); err != nil {
		return Finding{}, err
	}
	if expectedRevision != finding.Revision {
		return Finding{}, fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expectedRevision, finding.Revision)
	}
	if strings.TrimSpace(finding.Reproduction) == "" {
		return Finding{}, fmt.Errorf("finding %q needs reproducible steps before confirmation", finding.ID)
	}
	if !s.findingHasRawEvidence(finding) {
		return Finding{}, fmt.Errorf("finding %q needs non-agent-composed evidence before confirmation", finding.ID)
	}
	waiver := strings.TrimSpace(operatorWaiver)
	if requiresScreenshot(finding.Severity) && !s.findingHasScreenshot(finding) {
		if !allowWaiver || waiver == "" {
			return Finding{}, fmt.Errorf("%s finding %q needs a screenshot or explicit operator waiver", finding.Severity, finding.ID)
		}
	}
	finding.State, finding.OperatorWaiver = FindingConfirmed, waiver
	finding.Revision++
	finding.UpdatedAt = now
	s.Revision++
	s.UpdatedAt = now
	return *finding, nil
}

func (s *State) requireCompletionEvidence(checklist ChecklistDefinition, work *WorkState) error {
	if work == nil || work.Ref.Kind == WorkStep {
		return nil
	}
	// A concise observation is the evidence for a not-applicable decision; do
	// not force the operator to fabricate a capture for a check that cannot run.
	if work.Status == StatusNotApplicable {
		return nil
	}
	policy := CheckEvidencePolicy{}
	for _, check := range checklist.Items {
		if check.ID == work.Ref.ID {
			policy = check.Evidence
			break
		}
	}
	required := policy.Minimum
	if work.Status == StatusVulnerable && required < 1 {
		required = 1
	}
	current := s.rawEvidenceForWork(work.Ref, work.RunsCompleted+1)
	if len(current) < required {
		return fmt.Errorf("completion blocked: %s/%s needs %d raw evidence reference(s) for run %d; has %d", work.Ref.Kind, work.Ref.ID, required, work.RunsCompleted+1, len(current))
	}
	for _, kind := range policy.RequiredKinds {
		found := false
		for _, evidence := range current {
			if evidence.SourceKind == strings.ToLower(strings.TrimSpace(kind)) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("completion blocked: %s/%s requires %s evidence for run %d", work.Ref.Kind, work.Ref.ID, kind, work.RunsCompleted+1)
		}
	}
	return nil
}

func (s *State) rawEvidenceForWork(ref WorkRef, run int) []*Evidence {
	out := []*Evidence{}
	for _, id := range s.EvidenceOrder {
		if evidence := s.Evidence[id]; evidence != nil && evidence.Work == ref && evidence.Run == run && !evidence.AgentComposed {
			out = append(out, evidence)
		}
	}
	return out
}

func (s *State) findingHasRawEvidence(finding *Finding) bool {
	for _, id := range finding.EvidenceIDs {
		if evidence := s.Evidence[id]; evidence != nil && !evidence.AgentComposed {
			return true
		}
	}
	return false
}

func (s *State) findingHasScreenshot(finding *Finding) bool {
	for _, id := range finding.EvidenceIDs {
		if evidence := s.Evidence[id]; evidence != nil && evidence.SourceKind == EvidenceScreenshot && !evidence.AgentComposed {
			return true
		}
	}
	return false
}

func (s *State) ensureCollections() {
	if s.NotesRevision == 0 {
		s.NotesRevision = 1
	}
	if s.NotesUpdatedAt.IsZero() {
		s.NotesUpdatedAt = s.CreatedAt
	}
	if s.Evidence == nil {
		s.Evidence = map[string]*Evidence{}
	}
	if s.EvidenceOrder == nil {
		s.EvidenceOrder = []string{}
	}
	if s.Findings == nil {
		s.Findings = map[string]*Finding{}
	}
	if s.FindingOrder == nil {
		s.FindingOrder = []string{}
	}
}

func validEvidenceKind(kind string) bool {
	switch kind {
	case EvidenceLedgerEvent, EvidenceArtifact, EvidenceScreenshot, EvidenceHTTPCapture, EvidenceExternalFile:
		return true
	}
	return false
}

func validSeverity(value string) bool {
	switch value {
	case "info", "low", "medium", "high", "critical":
		return true
	}
	return false
}

func requiresScreenshot(severity string) bool {
	return severity == "medium" || severity == "high" || severity == "critical"
}
