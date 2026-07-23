package controlplane

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const CurrentMachineVersion = 1

type Phase string

const (
	PhaseIntake           Phase = "intake"
	PhasePlanning         Phase = "planning"
	PhaseActing           Phase = "acting"
	PhaseObserving        Phase = "observing"
	PhaseVerifying        Phase = "verifying"
	PhaseRepairing        Phase = "repairing"
	PhaseAwaitingApproval Phase = "awaiting_approval"
	PhaseComplete         Phase = "complete"
	PhaseBlocked          Phase = "blocked"
	PhaseCancelled        Phase = "cancelled"
	PhaseFailed           Phase = "failed"
)

type EventKind string

const (
	EventContractValidated     EventKind = "contract_validated"
	EventPlanAccepted          EventKind = "plan_accepted"
	EventActionStarted         EventKind = "action_started"
	EventActionSucceeded       EventKind = "action_succeeded"
	EventObservationRecorded   EventKind = "observation_recorded"
	EventActionFailed          EventKind = "action_failed"
	EventRepairReady           EventKind = "repair_ready"
	EventRepairConcluded       EventKind = "repair_concluded"
	EventVerificationRequested EventKind = "verification_requested"
	EventVerificationPassed    EventKind = "verification_passed"
	EventVerificationFailed    EventKind = "verification_failed"
	EventApprovalRequired      EventKind = "approval_required"
	EventApprovalGranted       EventKind = "approval_granted"
	EventApprovalDenied        EventKind = "approval_denied"
	EventBlocked               EventKind = "blocked"
	EventCancelled             EventKind = "cancelled"
	EventFailed                EventKind = "failed"
)

type Event struct {
	Kind            EventKind           `json:"kind"`
	Detail          string              `json:"detail,omitempty"`
	SatisfiedChecks map[string][]string `json:"satisfied_checks,omitempty"`
	At              time.Time           `json:"-"`
}

// MachineState is the authoritative control-plane state. It deliberately sits
// beside, rather than replaces, TaskRun.State (which remains descriptive UI
// telemetry such as thinking, reading, or testing).
type MachineState struct {
	Version          int                 `json:"version"`
	ContractDigest   string              `json:"contract_digest"`
	ContractRevision int                 `json:"contract_revision"`
	Phase            Phase               `json:"phase"`
	ResumePhase      Phase               `json:"resume_phase,omitempty"`
	Revision         int                 `json:"revision"`
	PlanRequired     bool                `json:"plan_required"`
	PlanAccepted     bool                `json:"plan_accepted"`
	BlockingCheckIDs []string            `json:"blocking_check_ids,omitempty"`
	SatisfiedChecks  map[string][]string `json:"satisfied_checks,omitempty"`
	LastEvent        EventKind           `json:"last_event,omitempty"`
	LastDetail       string              `json:"last_detail,omitempty"`
	UpdatedAt        string              `json:"updated_at"`
}

func NewMachineState(contract TaskContract, at time.Time) (MachineState, error) {
	if err := contract.Validate(); err != nil {
		return MachineState{}, err
	}
	if at.IsZero() {
		at = time.Now()
	}
	return MachineState{
		Version:          CurrentMachineVersion,
		ContractDigest:   contract.Digest,
		ContractRevision: contract.Revision,
		Phase:            PhaseIntake,
		Revision:         1,
		PlanRequired:     contract.PlanRequired,
		BlockingCheckIDs: append([]string(nil), contract.BlockingCheckIDs()...),
		SatisfiedChecks:  map[string][]string{},
		UpdatedAt:        at.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (s MachineState) Validate() error {
	if s.Version != CurrentMachineVersion {
		return fmt.Errorf("unsupported machine version %d", s.Version)
	}
	if strings.TrimSpace(s.ContractDigest) == "" || s.ContractRevision <= 0 {
		return errors.New("machine contract identity is required")
	}
	if !validPhase(s.Phase) {
		return fmt.Errorf("invalid control phase %q", s.Phase)
	}
	if s.Revision <= 0 {
		return errors.New("machine revision must be positive")
	}
	if _, err := time.Parse(time.RFC3339Nano, s.UpdatedAt); err != nil {
		return fmt.Errorf("updated_at: %w", err)
	}
	return nil
}

func (s MachineState) Apply(event Event) (MachineState, error) {
	if err := s.Validate(); err != nil {
		return s, err
	}
	if event.Kind == "" {
		return s, errors.New("control event kind is required")
	}
	if s.Terminal() {
		return s, fmt.Errorf("control phase %s is terminal", s.Phase)
	}
	next := cloneMachineState(s)
	if err := next.applyTransition(event); err != nil {
		return s, err
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	next.Revision++
	next.LastEvent = event.Kind
	next.LastDetail = strings.TrimSpace(event.Detail)
	next.UpdatedAt = event.At.UTC().Format(time.RFC3339Nano)
	return next, nil
}

// Rebase applies a newer user-instruction contract and returns the run to
// planning. Prior acceptance evidence is intentionally invalidated.
func (s MachineState) Rebase(contract TaskContract, at time.Time) (MachineState, error) {
	if err := s.Validate(); err != nil {
		return s, err
	}
	if err := contract.Validate(); err != nil {
		return s, err
	}
	if contract.ParentDigest != s.ContractDigest || contract.Revision <= s.ContractRevision {
		return s, errors.New("revised contract must link to and supersede the active contract")
	}
	if s.Terminal() {
		return s, fmt.Errorf("control phase %s is terminal", s.Phase)
	}
	if at.IsZero() {
		at = time.Now()
	}
	next := cloneMachineState(s)
	next.ContractDigest = contract.Digest
	next.ContractRevision = contract.Revision
	next.Phase = PhasePlanning
	next.ResumePhase = ""
	next.PlanRequired = contract.PlanRequired
	next.PlanAccepted = false
	next.BlockingCheckIDs = append([]string(nil), contract.BlockingCheckIDs()...)
	next.SatisfiedChecks = map[string][]string{}
	next.Revision++
	next.LastEvent = "user_correction"
	next.LastDetail = "contract revised; affected work returned to planning"
	next.UpdatedAt = at.UTC().Format(time.RFC3339Nano)
	return next, nil
}

func (s MachineState) Terminal() bool {
	switch s.Phase {
	case PhaseComplete, PhaseBlocked, PhaseCancelled, PhaseFailed:
		return true
	default:
		return false
	}
}

func (s MachineState) MissingBlockingChecks() []string {
	var missing []string
	for _, id := range s.BlockingCheckIDs {
		if len(s.SatisfiedChecks[id]) == 0 {
			missing = append(missing, id)
		}
	}
	return missing
}

type ToolDecision struct {
	Controlled bool
	Allowed    bool
	Reason     string
}

// CheckTool applies the deliberately narrow first-slice policy. Other tools
// continue through the existing Mauler gates until M3 adds complete metadata.
func (s MachineState) CheckTool(name string) ToolDecision {
	name = strings.ToLower(strings.TrimSpace(name))
	controlled := name == "read" || name == "write" || name == "edit" || name == "shell"
	if !controlled {
		return ToolDecision{Allowed: true}
	}
	if s.Terminal() || s.Phase == PhaseIntake || s.Phase == PhaseAwaitingApproval || s.Phase == PhaseVerifying {
		return ToolDecision{Controlled: true, Reason: fmt.Sprintf("%s is not legal during control phase %s", name, s.Phase)}
	}
	if name == "read" {
		switch s.Phase {
		case PhasePlanning, PhaseActing, PhaseRepairing:
			return ToolDecision{Controlled: true, Allowed: true}
		default:
			return ToolDecision{Controlled: true, Reason: fmt.Sprintf("read is not legal during control phase %s", s.Phase)}
		}
	}
	if s.Phase != PhaseActing {
		return ToolDecision{Controlled: true, Reason: fmt.Sprintf("%s requires control phase acting; current phase is %s", name, s.Phase)}
	}
	if s.PlanRequired && !s.PlanAccepted {
		return ToolDecision{Controlled: true, Reason: fmt.Sprintf("%s requires an accepted plan before mutation or execution", name)}
	}
	return ToolDecision{Controlled: true, Allowed: true}
}

func (s *MachineState) applyTransition(event Event) error {
	switch event.Kind {
	case EventContractValidated:
		if s.Phase != PhaseIntake {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.Phase = PhasePlanning
	case EventPlanAccepted:
		if s.Phase != PhasePlanning {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.PlanAccepted = true
		s.Phase = PhaseActing
	case EventActionStarted:
		if s.Phase != PhaseActing {
			return illegalTransition(s.Phase, event.Kind)
		}
	case EventActionSucceeded:
		if s.Phase != PhaseActing {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.Phase = PhaseObserving
	case EventObservationRecorded:
		if s.Phase != PhaseObserving {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.Phase = PhaseActing
	case EventActionFailed:
		if s.Phase != PhaseActing && s.Phase != PhaseObserving {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.Phase = PhaseRepairing
	case EventRepairReady:
		if s.Phase != PhaseRepairing {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.Phase = PhaseActing
	case EventRepairConcluded:
		if s.Phase != PhaseRepairing {
			return illegalTransition(s.Phase, event.Kind)
		}
		// Concluding a repair does not waive any acceptance check. It only
		// returns control to acting so the controller can run every blocking
		// verifier again against the evidence that is actually available.
		s.Phase = PhaseActing
	case EventVerificationRequested:
		if s.Phase != PhaseActing && s.Phase != PhaseObserving {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.Phase = PhaseVerifying
	case EventVerificationPassed:
		if s.Phase != PhaseVerifying {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.mergeSatisfiedChecks(event.SatisfiedChecks)
		if missing := s.MissingBlockingChecks(); len(missing) > 0 {
			return fmt.Errorf("blocking acceptance checks remain unsatisfied: %s", strings.Join(missing, ", "))
		}
		s.Phase = PhaseComplete
	case EventVerificationFailed:
		if s.Phase != PhaseVerifying {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.mergeSatisfiedChecks(event.SatisfiedChecks)
		s.Phase = PhaseRepairing
	case EventApprovalRequired:
		if s.Phase != PhasePlanning && s.Phase != PhaseActing && s.Phase != PhaseRepairing {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.ResumePhase = s.Phase
		s.Phase = PhaseAwaitingApproval
	case EventApprovalGranted:
		if s.Phase != PhaseAwaitingApproval || !validActivePhase(s.ResumePhase) {
			return illegalTransition(s.Phase, event.Kind)
		}
		s.Phase = s.ResumePhase
		s.ResumePhase = ""
	case EventApprovalDenied, EventBlocked:
		s.Phase = PhaseBlocked
		s.ResumePhase = ""
	case EventCancelled:
		s.Phase = PhaseCancelled
		s.ResumePhase = ""
	case EventFailed:
		s.Phase = PhaseFailed
		s.ResumePhase = ""
	default:
		return fmt.Errorf("unknown control event %q", event.Kind)
	}
	return nil
}

func (s *MachineState) mergeSatisfiedChecks(checks map[string][]string) {
	if s.SatisfiedChecks == nil {
		s.SatisfiedChecks = map[string][]string{}
	}
	allowed := map[string]bool{}
	for _, id := range s.BlockingCheckIDs {
		allowed[id] = true
	}
	for id, evidence := range checks {
		id = strings.TrimSpace(id)
		if !allowed[id] {
			continue
		}
		s.SatisfiedChecks[id] = uniqueEvidenceIDs(append(s.SatisfiedChecks[id], evidence...))
	}
}

func cloneMachineState(s MachineState) MachineState {
	next := s
	next.BlockingCheckIDs = append([]string(nil), s.BlockingCheckIDs...)
	next.SatisfiedChecks = make(map[string][]string, len(s.SatisfiedChecks))
	for id, evidence := range s.SatisfiedChecks {
		next.SatisfiedChecks[id] = append([]string(nil), evidence...)
	}
	return next
}

func uniqueEvidenceIDs(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func validPhase(phase Phase) bool {
	switch phase {
	case PhaseIntake, PhasePlanning, PhaseActing, PhaseObserving, PhaseVerifying,
		PhaseRepairing, PhaseAwaitingApproval, PhaseComplete, PhaseBlocked, PhaseCancelled, PhaseFailed:
		return true
	default:
		return false
	}
}

func validActivePhase(phase Phase) bool {
	switch phase {
	case PhasePlanning, PhaseActing, PhaseRepairing:
		return true
	default:
		return false
	}
}

func illegalTransition(phase Phase, event EventKind) error {
	return fmt.Errorf("control event %s is not legal from phase %s", event, phase)
}
