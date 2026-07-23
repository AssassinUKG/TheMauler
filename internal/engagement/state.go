package engagement

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	WorkStep          = "step"
	WorkGlobalCheck   = "global_check"
	WorkEndpointCheck = "endpoint_check"

	StatusPending       = "pending"
	StatusFocused       = "focused"
	StatusDone          = "done"
	StatusSkipped       = "skipped"
	StatusDisabled      = "disabled"
	StatusFailed        = "failed"
	StatusPassed        = "passed"
	StatusWarning       = "warning"
	StatusVulnerable    = "vulnerable"
	StatusNotApplicable = "not_applicable"

	DefaultClaimLease   = 5 * time.Minute
	MaxObservationWords = 200
	MaxNotesRunes       = 24000
)

var (
	ErrClaimRequired    = errors.New("engagement work item must be claimed before writing a result")
	ErrAlreadyClaimed   = errors.New("engagement work item is claimed by another agent")
	ErrClaimLimit       = errors.New("claimant already holds another engagement work item")
	ErrRevisionConflict = errors.New("engagement work item revision conflict")
	ErrPhaseBlocked     = errors.New("engagement phase is incomplete")
)

type WorkRef struct {
	Kind       string `json:"kind"`
	PhaseID    string `json:"phase_id,omitempty"`
	EndpointID string `json:"endpoint_id,omitempty"`
	ID         string `json:"id"`
}

func (r WorkRef) Key() string {
	if r.Kind == WorkStep {
		return r.PhaseID + "/" + r.ID
	}
	if r.Kind == WorkEndpointCheck {
		return r.EndpointID + "/" + r.ID
	}
	return r.ID
}

type Claimant struct {
	ID    string `json:"id"`
	Alias string `json:"alias,omitempty"`
}

type Claim struct {
	Claimant    Claimant  `json:"claimant"`
	ClaimedAt   time.Time `json:"claimed_at"`
	HeartbeatAt time.Time `json:"heartbeat_at,omitempty"`
	LeaseUntil  time.Time `json:"lease_until"`
}

type Observation struct {
	Run       int       `json:"run"`
	Status    string    `json:"status"`
	Text      string    `json:"text"`
	Claimant  Claimant  `json:"claimant"`
	CreatedAt time.Time `json:"created_at"`
}

type WorkState struct {
	Ref           WorkRef       `json:"ref"`
	Title         string        `json:"title"`
	Status        string        `json:"status"`
	Observation   string        `json:"observation,omitempty"`
	Observations  []Observation `json:"observations,omitempty"`
	Runs          RunCount      `json:"runs"`
	RunsCompleted int           `json:"runs_completed"`
	Finished      bool          `json:"finished"`
	Claim         *Claim        `json:"claim,omitempty"`
	Revision      uint64        `json:"revision"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type Endpoint struct {
	ID           string    `json:"id"`
	Method       string    `json:"method"`
	URL          string    `json:"url"`
	Name         string    `json:"name,omitempty"`
	FeatureGroup string    `json:"feature_group,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type EndpointInput struct {
	ID           string `json:"id"`
	Method       string `json:"method,omitempty"`
	URL          string `json:"url"`
	Name         string `json:"name,omitempty"`
	FeatureGroup string `json:"feature_group,omitempty"`
}

type State struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	WorkflowID     string                `json:"workflow_id"`
	ChecklistID    string                `json:"checklist_id"`
	CurrentPhase   string                `json:"current_phase"`
	Scope          []string              `json:"scope"`
	ScopeLocked    bool                  `json:"scope_locked"`
	Notes          string                `json:"notes,omitempty"`
	NotesRevision  uint64                `json:"notes_revision"`
	NotesUpdatedAt time.Time             `json:"notes_updated_at,omitempty"`
	Steps          map[string]*WorkState `json:"steps"`
	GlobalChecks   map[string]*WorkState `json:"global_checks"`
	// GlobalCheckOrder preserves checklist author order while states remain
	// directly addressable by id.
	GlobalCheckOrder   []string              `json:"global_check_order"`
	Endpoints          map[string]*Endpoint  `json:"endpoints"`
	EndpointOrder      []string              `json:"endpoint_order"`
	EndpointChecks     map[string]*WorkState `json:"endpoint_checks"`
	EndpointCheckOrder []string              `json:"endpoint_check_order"`
	Evidence           map[string]*Evidence  `json:"evidence"`
	EvidenceOrder      []string              `json:"evidence_order"`
	Findings           map[string]*Finding   `json:"findings"`
	FindingOrder       []string              `json:"finding_order"`
	Revision           uint64                `json:"revision"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
}

type NextAction struct {
	Action        string     `json:"action"`
	PhaseID       string     `json:"phase_id,omitempty"`
	PhaseName     string     `json:"phase_name,omitempty"`
	Work          *WorkState `json:"work,omitempty"`
	PhaseComplete bool       `json:"phase_complete"`
	WorkflowDone  bool       `json:"workflow_done"`
}

// AvailableWork is the deterministic queue for the active phase. Setup and
// summary steps remain single-file gates; checklist and endpoint checks may be
// returned together so distinct claimants can work them in parallel.
type AvailableWork struct {
	PhaseID   string      `json:"phase_id"`
	PhaseName string      `json:"phase_name"`
	Parallel  bool        `json:"parallel"`
	Items     []WorkState `json:"items"`
	Blocker   string      `json:"blocker,omitempty"`
}

type FinishResult struct {
	Work       WorkState `json:"work"`
	RunAgain   bool      `json:"run_again"`
	NextRun    int       `json:"next_run,omitempty"`
	TargetRuns RunCount  `json:"target_runs"`
}

func NewState(id, name string, workflow WorkflowDefinition, checklist ChecklistDefinition, scope []string, scopeLocked bool, now time.Time) (*State, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("engagement id is required")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("engagement name is required")
	}
	if err := ValidateBundle(workflow, checklist); err != nil {
		return nil, err
	}
	now = normaliseTime(now)
	state := &State{
		ID:                 strings.TrimSpace(id),
		Name:               strings.TrimSpace(name),
		WorkflowID:         workflow.ID,
		ChecklistID:        checklist.ID,
		CurrentPhase:       workflow.Phases[0].ID,
		Scope:              normaliseScope(scope),
		ScopeLocked:        scopeLocked,
		NotesRevision:      1,
		NotesUpdatedAt:     now,
		Steps:              map[string]*WorkState{},
		GlobalChecks:       map[string]*WorkState{},
		GlobalCheckOrder:   []string{},
		Endpoints:          map[string]*Endpoint{},
		EndpointOrder:      []string{},
		EndpointChecks:     map[string]*WorkState{},
		EndpointCheckOrder: []string{},
		Evidence:           map[string]*Evidence{},
		EvidenceOrder:      []string{},
		Findings:           map[string]*Finding{},
		FindingOrder:       []string{},
		Revision:           1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	for _, phase := range workflow.Phases {
		for _, step := range phase.Steps {
			ref := WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: step.EffectiveID()}
			state.Steps[ref.Key()] = &WorkState{
				Ref:       ref,
				Title:     strings.TrimSpace(step.Title),
				Status:    StatusPending,
				Runs:      step.Runs,
				Revision:  1,
				UpdatedAt: now,
			}
		}
	}
	for _, check := range checklist.Items {
		if !strings.EqualFold(strings.TrimSpace(check.Scope), "global") {
			continue
		}
		ref := WorkRef{Kind: WorkGlobalCheck, ID: check.ID}
		state.GlobalChecks[ref.Key()] = &WorkState{
			Ref:       ref,
			Title:     strings.TrimSpace(check.Title),
			Status:    StatusPending,
			Runs:      check.Runs,
			Revision:  1,
			UpdatedAt: now,
		}
		state.GlobalCheckOrder = append(state.GlobalCheckOrder, check.ID)
	}
	return state, nil
}

func (s *State) AddEndpoint(checklist ChecklistDefinition, input EndpointInput, now time.Time) (Endpoint, error) {
	if s == nil {
		return Endpoint{}, fmt.Errorf("engagement state is nil")
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return Endpoint{}, fmt.Errorf("endpoint id is required")
	}
	if s.Endpoints == nil {
		s.Endpoints = map[string]*Endpoint{}
	}
	if s.EndpointChecks == nil {
		s.EndpointChecks = map[string]*WorkState{}
	}
	if _, exists := s.Endpoints[id]; exists {
		return Endpoint{}, fmt.Errorf("endpoint %q already exists", id)
	}
	url := strings.TrimSpace(input.URL)
	if url == "" {
		return Endpoint{}, fmt.Errorf("endpoint URL/path is required")
	}
	if s.ScopeLocked {
		if decision := CheckTargetScope(s.Scope, url); !decision.Allowed {
			return Endpoint{}, fmt.Errorf("endpoint is outside locked engagement scope: %s", decision.Reason)
		}
	}
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if method == "" {
		method = "GET"
	}
	now = normaliseTime(now)
	endpoint := &Endpoint{
		ID: id, Method: method, URL: url, Name: strings.TrimSpace(input.Name),
		FeatureGroup: strings.TrimSpace(input.FeatureGroup), CreatedAt: now, UpdatedAt: now,
	}
	s.Endpoints[id] = endpoint
	s.EndpointOrder = append(s.EndpointOrder, id)
	if len(s.EndpointCheckOrder) == 0 {
		for _, check := range checklist.Items {
			if strings.EqualFold(strings.TrimSpace(check.Scope), "per_endpoint") {
				s.EndpointCheckOrder = append(s.EndpointCheckOrder, check.ID)
			}
		}
	}
	for _, check := range checklist.Items {
		if !strings.EqualFold(strings.TrimSpace(check.Scope), "per_endpoint") {
			continue
		}
		ref := WorkRef{Kind: WorkEndpointCheck, EndpointID: id, ID: check.ID}
		s.EndpointChecks[ref.Key()] = &WorkState{
			Ref: ref, Title: strings.TrimSpace(check.Title), Status: StatusPending,
			Runs: check.Runs, Revision: 1, UpdatedAt: now,
		}
	}
	s.Revision++
	s.UpdatedAt = now
	return *endpoint, nil
}

func (s *State) SetEndpointGroup(endpointID, group string, now time.Time) (Endpoint, error) {
	if s == nil {
		return Endpoint{}, fmt.Errorf("engagement state is nil")
	}
	endpoint := s.Endpoints[strings.TrimSpace(endpointID)]
	if endpoint == nil {
		return Endpoint{}, fmt.Errorf("unknown endpoint %q", endpointID)
	}
	endpoint.FeatureGroup = strings.TrimSpace(group)
	endpoint.UpdatedAt = normaliseTime(now)
	s.Revision++
	s.UpdatedAt = endpoint.UpdatedAt
	return *endpoint, nil
}

// SetNotes updates the compact project-wide risk/context note. Notes have
// their own revision so an operator draft cannot silently overwrite a newer
// agent or channel edit while unrelated engagement work continues.
func (s *State) SetNotes(notes string, expectedRevision uint64, now time.Time) (uint64, error) {
	if s == nil {
		return 0, fmt.Errorf("engagement state is nil")
	}
	if expectedRevision != s.NotesRevision {
		return s.NotesRevision, fmt.Errorf("%w: notes expected revision %d, current %d", ErrRevisionConflict, expectedRevision, s.NotesRevision)
	}
	notes = strings.ReplaceAll(notes, "\r\n", "\n")
	if count := len([]rune(notes)); count > MaxNotesRunes {
		return s.NotesRevision, fmt.Errorf("notes have %d characters; maximum is %d", count, MaxNotesRunes)
	}
	now = normaliseTime(now)
	s.Notes = notes
	s.NotesRevision++
	s.NotesUpdatedAt = now
	s.Revision++
	s.UpdatedAt = now
	return s.NotesRevision, nil
}

func (s *State) Now(workflow WorkflowDefinition, now time.Time) (NextAction, error) {
	if s == nil {
		return NextAction{}, fmt.Errorf("engagement state is nil")
	}
	s.expireClaims(normaliseTime(now))
	phaseIndex := workflowPhaseIndex(workflow, s.CurrentPhase)
	if phaseIndex < 0 {
		return NextAction{}, fmt.Errorf("current phase %q is not in workflow %q", s.CurrentPhase, workflow.ID)
	}
	phase := workflow.Phases[phaseIndex]
	if phase.EffectiveKind() == "checklist" && len(phase.Steps) > 0 {
		for _, step := range phase.Steps[:len(phase.Steps)-1] {
			if next := nextForState(s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: step.EffectiveID()}.Key()]); next != nil {
				return NextAction{Action: nextActionForWork(next), PhaseID: phase.ID, PhaseName: phase.Name, Work: next}, nil
			}
		}
		for _, check := range orderedGlobalChecks(s, phase) {
			if next := nextForState(check); next != nil {
				return NextAction{Action: nextActionForWork(next), PhaseID: phase.ID, PhaseName: phase.Name, Work: next}, nil
			}
		}
		last := phase.Steps[len(phase.Steps)-1]
		if next := nextForState(s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: last.EffectiveID()}.Key()]); next != nil {
			return NextAction{Action: nextActionForWork(next), PhaseID: phase.ID, PhaseName: phase.Name, Work: next}, nil
		}
	} else if phase.EffectiveKind() == "endpoint" && len(phase.Steps) > 0 {
		for _, step := range phase.Steps[:len(phase.Steps)-1] {
			if next := nextForState(s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: step.EffectiveID()}.Key()]); next != nil {
				return NextAction{Action: nextActionForWork(next), PhaseID: phase.ID, PhaseName: phase.Name, Work: next}, nil
			}
		}
		if len(s.EndpointOrder) == 0 {
			return NextAction{}, fmt.Errorf("%w: add at least one endpoint before endpoint testing", ErrPhaseBlocked)
		}
		for _, check := range orderedEndpointChecks(s) {
			if next := nextForState(check); next != nil {
				return NextAction{Action: nextActionForWork(next), PhaseID: phase.ID, PhaseName: phase.Name, Work: next}, nil
			}
		}
		last := phase.Steps[len(phase.Steps)-1]
		if next := nextForState(s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: last.EffectiveID()}.Key()]); next != nil {
			return NextAction{Action: nextActionForWork(next), PhaseID: phase.ID, PhaseName: phase.Name, Work: next}, nil
		}
	} else {
		for _, step := range phase.Steps {
			if next := nextForState(s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: step.EffectiveID()}.Key()]); next != nil {
				return NextAction{Action: nextActionForWork(next), PhaseID: phase.ID, PhaseName: phase.Name, Work: next}, nil
			}
		}
	}
	workflowDone := phaseIndex == len(workflow.Phases)-1
	action := "advance"
	if workflowDone {
		action = "done"
	}
	return NextAction{Action: action, PhaseID: phase.ID, PhaseName: phase.Name, PhaseComplete: true, WorkflowDone: workflowDone}, nil
}

// Available returns claimable work in stable workflow/checklist order. A
// claimant that already holds a live lease sees only that lease, matching the
// one-active-claim invariant enforced by ClaimWork. An empty claimant is a
// read-only queue view and never sees work leased by another run.
func (s *State) Available(workflow WorkflowDefinition, claimantID string, limit int, now time.Time) (AvailableWork, error) {
	if s == nil {
		return AvailableWork{}, fmt.Errorf("engagement state is nil")
	}
	now = normaliseTime(now)
	s.expireClaims(now)
	phaseIndex := workflowPhaseIndex(workflow, s.CurrentPhase)
	if phaseIndex < 0 {
		return AvailableWork{}, fmt.Errorf("current phase %q is not in workflow %q", s.CurrentPhase, workflow.ID)
	}
	phase := workflow.Phases[phaseIndex]
	queue := AvailableWork{PhaseID: phase.ID, PhaseName: phase.Name, Items: []WorkState{}}
	claimantID = strings.TrimSpace(claimantID)
	if claimantID != "" {
		for _, work := range s.allWork() {
			if work != nil && work.Claim != nil && work.Claim.Claimant.ID == claimantID && work.Claim.LeaseUntil.After(now) {
				queue.Items = append(queue.Items, cloneWork(work))
				return queue, nil
			}
		}
	}

	limit = normaliseAvailableLimit(limit)
	appendCandidate := func(work *WorkState) bool {
		if work == nil || work.Finished || work.Status == StatusDisabled {
			return false
		}
		if work.Claim != nil && work.Claim.LeaseUntil.After(now) {
			if len(queue.Items) == 0 {
				queue.Blocker = fmt.Sprintf("%s/%s is claimed by %s", work.Ref.Kind, work.Ref.ID, work.Claim.Claimant.ID)
			}
			return false
		}
		queue.Blocker = ""
		queue.Items = append(queue.Items, cloneWork(work))
		return len(queue.Items) >= limit
	}
	appendGate := func(work *WorkState) AvailableWork {
		appendCandidate(work)
		return queue
	}

	kind := phase.EffectiveKind()
	if (kind == "checklist" || kind == "endpoint") && len(phase.Steps) > 0 {
		for _, step := range phase.Steps[:len(phase.Steps)-1] {
			work := s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: step.EffectiveID()}.Key()]
			if nextForState(work) != nil {
				return appendGate(work), nil
			}
		}
		queue.Parallel = true
		if kind == "endpoint" && len(s.EndpointOrder) == 0 {
			queue.Blocker = "add at least one endpoint before endpoint testing"
			return queue, fmt.Errorf("%w: %s", ErrPhaseBlocked, queue.Blocker)
		}
		var checks []*WorkState
		if kind == "checklist" {
			checks = orderedGlobalChecks(s, phase)
		} else {
			checks = orderedEndpointChecks(s)
		}
		unfinished := false
		for _, work := range checks {
			if nextForState(work) == nil {
				continue
			}
			unfinished = true
			if appendCandidate(work) {
				return queue, nil
			}
		}
		if unfinished {
			if len(queue.Items) == 0 && queue.Blocker == "" {
				queue.Blocker = "all active checks are currently claimed"
			}
			return queue, nil
		}
		queue.Parallel = false
		last := phase.Steps[len(phase.Steps)-1]
		return appendGate(s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: last.EffectiveID()}.Key()]), nil
	}

	for _, step := range phase.Steps {
		work := s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: step.EffectiveID()}.Key()]
		if nextForState(work) != nil {
			return appendGate(work), nil
		}
	}
	return queue, nil
}

func normaliseAvailableLimit(limit int) int {
	if limit <= 0 {
		return 6
	}
	if limit > 12 {
		return 12
	}
	return limit
}

func (s *State) ClaimWork(workflow WorkflowDefinition, ref WorkRef, claimant Claimant, lease time.Duration, now time.Time) (WorkState, error) {
	if strings.TrimSpace(claimant.ID) == "" {
		return WorkState{}, fmt.Errorf("claimant id is required")
	}
	now = normaliseTime(now)
	s.expireClaims(now)
	work, err := s.resolveAccessibleWork(workflow, ref)
	if err != nil {
		return WorkState{}, err
	}
	for _, other := range s.allWork() {
		if other.Claim == nil || other.Ref == ref || !other.Claim.LeaseUntil.After(now) {
			continue
		}
		if other.Claim.Claimant.ID == claimant.ID {
			return WorkState{}, fmt.Errorf("%w: %s already holds %s/%s", ErrClaimLimit, claimant.ID, other.Ref.Kind, other.Ref.ID)
		}
	}
	if work.Claim != nil && work.Claim.LeaseUntil.After(now) {
		if work.Claim.Claimant.ID != claimant.ID {
			return WorkState{}, fmt.Errorf("%w: %s/%s held by %s", ErrAlreadyClaimed, ref.Kind, ref.ID, work.Claim.Claimant.ID)
		}
		if lease <= 0 {
			lease = DefaultClaimLease
		}
		work.Claim.LeaseUntil = now.Add(lease)
		work.Claim.HeartbeatAt = now
		work.UpdatedAt = now
		s.bump(work)
		return cloneWork(work), nil
	}
	if work.Finished || isTerminalStatus(ref.Kind, work.Status) {
		return WorkState{}, fmt.Errorf("work item %s/%s is already settled", ref.Kind, ref.ID)
	}
	if lease <= 0 {
		lease = DefaultClaimLease
	}
	work.Status = StatusFocused
	work.Claim = &Claim{Claimant: claimant, ClaimedAt: now, HeartbeatAt: now, LeaseUntil: now.Add(lease)}
	work.UpdatedAt = now
	s.bump(work)
	return cloneWork(work), nil
}

// HeartbeatClaimant extends one live claim without changing the work revision.
// Result writes use the work revision returned by claim, so background lease
// maintenance must only advance the containing state revision used by SQLite's
// optimistic snapshot guard.
func (s *State) HeartbeatClaimant(claimantID string, lease time.Duration, now time.Time) (WorkState, bool, error) {
	if s == nil {
		return WorkState{}, false, fmt.Errorf("engagement state is nil")
	}
	claimantID = strings.TrimSpace(claimantID)
	if claimantID == "" {
		return WorkState{}, false, fmt.Errorf("claimant id is required")
	}
	now = normaliseTime(now)
	if lease <= 0 {
		lease = DefaultClaimLease
	}
	for _, work := range s.allWork() {
		if work == nil || work.Claim == nil || work.Claim.Claimant.ID != claimantID {
			continue
		}
		if !work.Claim.LeaseUntil.After(now) {
			return cloneWork(work), false, nil
		}
		leaseUntil := now.Add(lease)
		if !leaseUntil.After(work.Claim.LeaseUntil) {
			return cloneWork(work), false, nil
		}
		work.Claim.HeartbeatAt = now
		work.Claim.LeaseUntil = leaseUntil
		work.UpdatedAt = now
		s.Revision++
		s.UpdatedAt = now
		return cloneWork(work), true, nil
	}
	return WorkState{}, false, nil
}

// ReleaseClaimant returns unfinished focused work to pending immediately when
// a run exits normally. Crashed runs are recovered by lease expiry instead.
func (s *State) ReleaseClaimant(claimantID string, now time.Time) (WorkState, bool, error) {
	if s == nil {
		return WorkState{}, false, fmt.Errorf("engagement state is nil")
	}
	claimantID = strings.TrimSpace(claimantID)
	if claimantID == "" {
		return WorkState{}, false, fmt.Errorf("claimant id is required")
	}
	now = normaliseTime(now)
	for _, work := range s.allWork() {
		if work == nil || work.Claim == nil || work.Claim.Claimant.ID != claimantID {
			continue
		}
		work.Claim = nil
		if !work.Finished && (work.Status == StatusFocused || isTerminalStatus(work.Ref.Kind, work.Status)) {
			work.Status = StatusPending
		}
		work.UpdatedAt = now
		s.bump(work)
		return cloneWork(work), true, nil
	}
	return WorkState{}, false, nil
}

func (s *State) RecordWork(workflow WorkflowDefinition, ref WorkRef, claimantID, status, observation string, expectedRevision uint64, now time.Time) (WorkState, error) {
	now = normaliseTime(now)
	s.expireClaims(now)
	work, err := s.resolveAccessibleWork(workflow, ref)
	if err != nil {
		return WorkState{}, err
	}
	if err := requireClaim(work, claimantID, now); err != nil {
		return WorkState{}, err
	}
	if expectedRevision != work.Revision {
		return WorkState{}, fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expectedRevision, work.Revision)
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if !isTerminalStatus(ref.Kind, status) {
		return WorkState{}, fmt.Errorf("invalid terminal status %q for %s", status, ref.Kind)
	}
	observation = strings.TrimSpace(observation)
	if observation == "" {
		return WorkState{}, fmt.Errorf("observation is required before settling %s/%s", ref.Kind, ref.ID)
	}
	if words := len(strings.Fields(observation)); words > MaxObservationWords {
		return WorkState{}, fmt.Errorf("observation has %d words; maximum is %d", words, MaxObservationWords)
	}
	work.Status = status
	work.Observation = observation
	work.Observations = append(work.Observations, Observation{
		Run:       work.RunsCompleted + 1,
		Status:    status,
		Text:      observation,
		Claimant:  work.Claim.Claimant,
		CreatedAt: now,
	})
	work.UpdatedAt = now
	s.bump(work)
	return cloneWork(work), nil
}

func (s *State) FinishWork(workflow WorkflowDefinition, checklist ChecklistDefinition, ref WorkRef, claimantID string, expectedRevision uint64, now time.Time) (FinishResult, error) {
	now = normaliseTime(now)
	s.expireClaims(now)
	work, err := s.resolveAccessibleWork(workflow, ref)
	if err != nil {
		return FinishResult{}, err
	}
	if err := requireClaim(work, claimantID, now); err != nil {
		return FinishResult{}, err
	}
	if expectedRevision != work.Revision {
		return FinishResult{}, fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expectedRevision, work.Revision)
	}
	if !isTerminalStatus(ref.Kind, work.Status) || strings.TrimSpace(work.Observation) == "" {
		return FinishResult{}, fmt.Errorf("record a terminal status and observation before finishing %s/%s", ref.Kind, ref.ID)
	}
	if err := s.requireCompletionEvidence(checklist, work); err != nil {
		return FinishResult{}, err
	}
	work.RunsCompleted++
	work.Claim = nil
	work.UpdatedAt = now
	if work.Runs.ShouldRepeat(work.RunsCompleted) {
		work.Status = StatusPending
		work.Finished = false
		s.bump(work)
		return FinishResult{Work: cloneWork(work), RunAgain: true, NextRun: work.RunsCompleted + 1, TargetRuns: work.Runs}, nil
	}
	work.Finished = true
	s.bump(work)
	return FinishResult{Work: cloneWork(work), TargetRuns: work.Runs}, nil
}

func (s *State) Advance(workflow WorkflowDefinition, now time.Time) (string, error) {
	next, err := s.Now(workflow, now)
	if err != nil {
		return s.CurrentPhase, err
	}
	if !next.PhaseComplete {
		return s.CurrentPhase, fmt.Errorf("%w: next action is %s", ErrPhaseBlocked, next.Action)
	}
	phaseIndex := workflowPhaseIndex(workflow, s.CurrentPhase)
	if phaseIndex == len(workflow.Phases)-1 {
		return s.CurrentPhase, nil
	}
	s.CurrentPhase = workflow.Phases[phaseIndex+1].ID
	s.UpdatedAt = normaliseTime(now)
	s.Revision++
	return s.CurrentPhase, nil
}

func (s *State) resolveAccessibleWork(workflow WorkflowDefinition, ref WorkRef) (*WorkState, error) {
	phaseIndex := workflowPhaseIndex(workflow, s.CurrentPhase)
	if phaseIndex < 0 {
		return nil, fmt.Errorf("unknown current phase %q", s.CurrentPhase)
	}
	phase := workflow.Phases[phaseIndex]
	var work *WorkState
	switch ref.Kind {
	case WorkStep:
		if ref.PhaseID != s.CurrentPhase {
			return nil, fmt.Errorf("step %s belongs to phase %q; current phase is %q", ref.ID, ref.PhaseID, s.CurrentPhase)
		}
		work = s.Steps[ref.Key()]
		if work == nil {
			return nil, fmt.Errorf("unknown workflow step %s/%s", ref.PhaseID, ref.ID)
		}
		if err := s.requirePriorWorkFinished(phase, ref); err != nil {
			return nil, err
		}
	case WorkGlobalCheck:
		if phase.EffectiveKind() != "checklist" {
			return nil, fmt.Errorf("global checks are not active in phase %q", phase.ID)
		}
		work = s.GlobalChecks[ref.Key()]
		if work == nil {
			return nil, fmt.Errorf("unknown global check %q", ref.ID)
		}
		if len(phase.Steps) > 1 {
			for _, step := range phase.Steps[:len(phase.Steps)-1] {
				state := s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: step.EffectiveID()}.Key()]
				if state != nil && !state.Finished {
					return nil, fmt.Errorf("%w: finish setup step %s before checklist work", ErrPhaseBlocked, step.EffectiveID())
				}
			}
		}
	case WorkEndpointCheck:
		if phase.EffectiveKind() != "endpoint" {
			return nil, fmt.Errorf("endpoint checks are not active in phase %q", phase.ID)
		}
		if strings.TrimSpace(ref.EndpointID) == "" || s.Endpoints[ref.EndpointID] == nil {
			return nil, fmt.Errorf("unknown endpoint %q", ref.EndpointID)
		}
		work = s.EndpointChecks[ref.Key()]
		if work == nil {
			return nil, fmt.Errorf("unknown endpoint check %q for endpoint %q", ref.ID, ref.EndpointID)
		}
		if len(phase.Steps) > 1 {
			for _, step := range phase.Steps[:len(phase.Steps)-1] {
				state := s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: step.EffectiveID()}.Key()]
				if state != nil && !state.Finished {
					return nil, fmt.Errorf("%w: finish endpoint setup step %s before endpoint checks", ErrPhaseBlocked, step.EffectiveID())
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported work kind %q", ref.Kind)
	}
	return work, nil
}

func (s *State) requirePriorWorkFinished(phase PhaseDefinition, ref WorkRef) error {
	for index, step := range phase.Steps {
		if step.EffectiveID() != ref.ID {
			continue
		}
		if phase.EffectiveKind() == "checklist" && index == len(phase.Steps)-1 {
			for _, check := range s.GlobalChecks {
				if !check.Finished {
					return fmt.Errorf("%w: finish global checks before checklist summary step", ErrPhaseBlocked)
				}
			}
		}
		if phase.EffectiveKind() == "endpoint" && index == len(phase.Steps)-1 {
			if len(s.EndpointOrder) == 0 {
				return fmt.Errorf("%w: add at least one endpoint before endpoint summary", ErrPhaseBlocked)
			}
			for _, check := range s.EndpointChecks {
				if !check.Finished {
					return fmt.Errorf("%w: finish endpoint checks before endpoint summary step", ErrPhaseBlocked)
				}
			}
		}
		for _, prior := range phase.Steps[:index] {
			state := s.Steps[WorkRef{Kind: WorkStep, PhaseID: phase.ID, ID: prior.EffectiveID()}.Key()]
			if state != nil && !state.Finished {
				return fmt.Errorf("%w: prior step %s is incomplete", ErrPhaseBlocked, prior.EffectiveID())
			}
		}
		return nil
	}
	return fmt.Errorf("unknown workflow step %s/%s", phase.ID, ref.ID)
}

func (s *State) expireClaims(now time.Time) {
	for _, work := range s.allWork() {
		if work.Claim == nil || work.Claim.LeaseUntil.After(now) {
			continue
		}
		work.Claim = nil
		if !work.Finished && (work.Status == StatusFocused || isTerminalStatus(work.Ref.Kind, work.Status)) {
			work.Status = StatusPending
		}
		work.UpdatedAt = now
		s.bump(work)
	}
}

func (s *State) allWork() []*WorkState {
	items := make([]*WorkState, 0, len(s.Steps)+len(s.GlobalChecks)+len(s.EndpointChecks))
	for _, work := range s.Steps {
		items = append(items, work)
	}
	for _, work := range s.GlobalChecks {
		items = append(items, work)
	}
	for _, work := range s.EndpointChecks {
		items = append(items, work)
	}
	return items
}

func (s *State) bump(work *WorkState) {
	work.Revision++
	s.Revision++
	s.UpdatedAt = work.UpdatedAt
}

func requireClaim(work *WorkState, claimantID string, now time.Time) error {
	if work.Claim == nil || !work.Claim.LeaseUntil.After(now) {
		return ErrClaimRequired
	}
	if work.Claim.Claimant.ID != strings.TrimSpace(claimantID) {
		return fmt.Errorf("%w: held by %s", ErrAlreadyClaimed, work.Claim.Claimant.ID)
	}
	return nil
}

func isTerminalStatus(kind, status string) bool {
	switch kind {
	case WorkStep:
		return status == StatusDone || status == StatusSkipped
	case WorkGlobalCheck, WorkEndpointCheck:
		switch status {
		case StatusFailed, StatusPassed, StatusWarning, StatusVulnerable, StatusNotApplicable:
			return true
		}
	}
	return false
}

func nextForState(work *WorkState) *WorkState {
	if work == nil || work.Finished || work.Status == StatusDisabled {
		return nil
	}
	copy := cloneWork(work)
	return &copy
}

func nextActionForWork(work *WorkState) string {
	if work == nil {
		return ""
	}
	if work.Status == StatusPending {
		return "claim"
	}
	if work.Status == StatusFocused {
		return "record"
	}
	if isTerminalStatus(work.Ref.Kind, work.Status) && !work.Finished {
		return "finish"
	}
	return "claim"
}

func orderedGlobalChecks(s *State, _ PhaseDefinition) []*WorkState {
	selected := make([]*WorkState, 0, len(s.GlobalCheckOrder))
	for _, id := range s.GlobalCheckOrder {
		if work := s.GlobalChecks[id]; work != nil {
			selected = append(selected, work)
		}
	}
	return selected
}

func orderedEndpointChecks(s *State) []*WorkState {
	selected := make([]*WorkState, 0, len(s.EndpointOrder)*len(s.EndpointCheckOrder))
	for _, endpointID := range s.EndpointOrder {
		if s.Endpoints[endpointID] == nil {
			continue
		}
		for _, checkID := range s.EndpointCheckOrder {
			ref := WorkRef{Kind: WorkEndpointCheck, EndpointID: endpointID, ID: checkID}
			if work := s.EndpointChecks[ref.Key()]; work != nil {
				selected = append(selected, work)
			}
		}
	}
	return selected
}

func workflowPhaseIndex(workflow WorkflowDefinition, phaseID string) int {
	for i, phase := range workflow.Phases {
		if phase.ID == phaseID {
			return i
		}
	}
	return -1
}

func normaliseScope(scope []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(scope))
	for _, value := range scope {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func normaliseTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func cloneWork(work *WorkState) WorkState {
	copy := *work
	if work.Claim != nil {
		claim := *work.Claim
		copy.Claim = &claim
	}
	copy.Observations = append([]Observation(nil), work.Observations...)
	return copy
}
