package controlplane

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const CurrentContractVersion = 1

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// TaskContract is the immutable, code-validated operating contract for one run.
// User steering creates a new revision with ParentDigest pointing at the prior
// contract rather than relying on conflicting instructions in chat history.
type TaskContract struct {
	Version             int               `json:"version"`
	Revision            int               `json:"revision"`
	ParentDigest        string            `json:"parent_digest,omitempty"`
	RunID               string            `json:"run_id"`
	Objective           string            `json:"objective"`
	WorkspaceRoot       string            `json:"workspace_root"`
	Deliverables        []Deliverable     `json:"deliverables,omitempty"`
	Constraints         []string          `json:"constraints,omitempty"`
	ProtectedResources  []string          `json:"protected_resources,omitempty"`
	AllowedMutations    []PathRule        `json:"allowed_mutations,omitempty"`
	AcceptanceChecks    []AcceptanceCheck `json:"acceptance_checks,omitempty"`
	RequiredEvidence    []string          `json:"required_evidence,omitempty"`
	Risk                RiskLevel         `json:"risk"`
	InstructionRevision int               `json:"instruction_revision"`
	PlanRequired        bool              `json:"plan_required"`
	Budgets             RunBudgets        `json:"budgets"`
	ApprovalPolicy      string            `json:"approval_policy"`
	CompletionPolicy    string            `json:"completion_policy"`
	CreatedAt           string            `json:"created_at"`
	Digest              string            `json:"digest"`
}

type Deliverable struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Kind        string `json:"kind,omitempty"`
}

type PathRule struct {
	Root   string `json:"root"`
	Access string `json:"access"`
}

type AcceptanceCheck struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Verifier      string   `json:"verifier"`
	Blocking      bool     `json:"blocking"`
	EvidenceKinds []string `json:"evidence_kinds,omitempty"`
}

type RunBudgets struct {
	MaxToolCalls  int `json:"max_tool_calls,omitempty"`
	MaxRunSeconds int `json:"max_run_seconds,omitempty"`
}

type ContractInput struct {
	RunID               string
	Objective           string
	WorkspaceRoot       string
	Deliverables        []Deliverable
	Constraints         []string
	ProtectedResources  []string
	AllowedMutations    []PathRule
	AcceptanceChecks    []AcceptanceCheck
	RequiredEvidence    []string
	Risk                RiskLevel
	InstructionRevision int
	PlanRequired        bool
	Budgets             RunBudgets
	ApprovalPolicy      string
	CompletionPolicy    string
	CreatedAt           time.Time
}

func NewTaskContract(input ContractInput) (TaskContract, error) {
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	contract := TaskContract{
		Version:             CurrentContractVersion,
		Revision:            1,
		RunID:               strings.TrimSpace(input.RunID),
		Objective:           strings.TrimSpace(input.Objective),
		WorkspaceRoot:       cleanRoot(input.WorkspaceRoot),
		Deliverables:        normalizeDeliverables(input.Deliverables),
		Constraints:         uniqueStrings(input.Constraints),
		ProtectedResources:  uniqueCleanPaths(input.ProtectedResources),
		AllowedMutations:    normalizePathRules(input.AllowedMutations),
		AcceptanceChecks:    normalizeAcceptanceChecks(input.AcceptanceChecks),
		RequiredEvidence:    uniqueStrings(input.RequiredEvidence),
		Risk:                input.Risk,
		InstructionRevision: input.InstructionRevision,
		PlanRequired:        input.PlanRequired,
		Budgets:             input.Budgets,
		ApprovalPolicy:      strings.TrimSpace(input.ApprovalPolicy),
		CompletionPolicy:    strings.TrimSpace(input.CompletionPolicy),
		CreatedAt:           createdAt.UTC().Format(time.RFC3339Nano),
	}
	if contract.Risk == "" {
		contract.Risk = RiskLow
	}
	if contract.InstructionRevision <= 0 {
		contract.InstructionRevision = 1
	}
	if contract.ApprovalPolicy == "" {
		contract.ApprovalPolicy = "exact_action"
	}
	if contract.CompletionPolicy == "" {
		contract.CompletionPolicy = "evidence_owned"
	}
	contract.Digest = contract.calculatedDigest()
	if err := contract.Validate(); err != nil {
		return TaskContract{}, err
	}
	return contract, nil
}

// ReviseTaskContract creates a new immutable instruction revision. The prior
// digest remains linked for audit and checkpoint replay.
func ReviseTaskContract(current TaskContract, objective string, instructionRevision int, createdAt time.Time) (TaskContract, error) {
	if err := current.Validate(); err != nil {
		return TaskContract{}, fmt.Errorf("current contract: %w", err)
	}
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return TaskContract{}, errors.New("revised objective is required")
	}
	if instructionRevision <= current.InstructionRevision {
		return TaskContract{}, fmt.Errorf("instruction revision %d must be newer than %d", instructionRevision, current.InstructionRevision)
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	next := current
	next.Revision++
	next.ParentDigest = current.Digest
	next.Objective = objective
	next.InstructionRevision = instructionRevision
	next.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	next.Digest = next.calculatedDigest()
	if err := next.Validate(); err != nil {
		return TaskContract{}, err
	}
	return next, nil
}

func (c TaskContract) Validate() error {
	if c.Version != CurrentContractVersion {
		return fmt.Errorf("unsupported contract version %d", c.Version)
	}
	if c.Revision <= 0 {
		return errors.New("contract revision must be positive")
	}
	if strings.TrimSpace(c.RunID) == "" {
		return errors.New("run id is required")
	}
	if strings.TrimSpace(c.Objective) == "" {
		return errors.New("objective is required")
	}
	if strings.TrimSpace(c.WorkspaceRoot) == "" || !filepath.IsAbs(c.WorkspaceRoot) {
		return errors.New("workspace root must be absolute")
	}
	if c.InstructionRevision <= 0 {
		return errors.New("instruction revision must be positive")
	}
	switch c.Risk {
	case RiskLow, RiskMedium, RiskHigh:
	default:
		return fmt.Errorf("invalid risk level %q", c.Risk)
	}
	if c.Budgets.MaxToolCalls < 0 || c.Budgets.MaxRunSeconds < 0 {
		return errors.New("run budgets cannot be negative")
	}
	if strings.TrimSpace(c.ApprovalPolicy) == "" || strings.TrimSpace(c.CompletionPolicy) == "" {
		return errors.New("approval and completion policies are required")
	}
	if _, err := time.Parse(time.RFC3339Nano, c.CreatedAt); err != nil {
		return fmt.Errorf("created_at: %w", err)
	}
	if err := validateDeliverables(c.Deliverables); err != nil {
		return err
	}
	if err := validatePathRules(c.AllowedMutations); err != nil {
		return err
	}
	if err := validateAcceptanceChecks(c.AcceptanceChecks); err != nil {
		return err
	}
	if strings.TrimSpace(c.Digest) == "" {
		return errors.New("contract digest is required")
	}
	if expected := c.calculatedDigest(); !strings.EqualFold(c.Digest, expected) {
		return fmt.Errorf("contract digest mismatch: got %s want %s", c.Digest, expected)
	}
	return nil
}

func (c TaskContract) BlockingCheckIDs() []string {
	ids := make([]string, 0, len(c.AcceptanceChecks))
	for _, check := range c.AcceptanceChecks {
		if check.Blocking {
			ids = append(ids, check.ID)
		}
	}
	return ids
}

func (c TaskContract) calculatedDigest() string {
	copy := c
	copy.Digest = ""
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cleanRoot(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func normalizeDeliverables(in []Deliverable) []Deliverable {
	out := make([]Deliverable, 0, len(in))
	for _, item := range in {
		item.ID = strings.TrimSpace(item.ID)
		item.Description = strings.TrimSpace(item.Description)
		item.Kind = strings.TrimSpace(item.Kind)
		if item.ID != "" || item.Description != "" {
			out = append(out, item)
		}
	}
	return out
}

func normalizeAcceptanceChecks(in []AcceptanceCheck) []AcceptanceCheck {
	out := make([]AcceptanceCheck, 0, len(in))
	for _, check := range in {
		check.ID = strings.TrimSpace(check.ID)
		check.Description = strings.TrimSpace(check.Description)
		check.Verifier = strings.TrimSpace(check.Verifier)
		check.EvidenceKinds = uniqueStrings(check.EvidenceKinds)
		out = append(out, check)
	}
	return out
}

func normalizePathRules(in []PathRule) []PathRule {
	out := make([]PathRule, 0, len(in))
	for _, rule := range in {
		rule.Root = cleanRoot(rule.Root)
		rule.Access = strings.ToLower(strings.TrimSpace(rule.Access))
		if rule.Root != "" || rule.Access != "" {
			out = append(out, rule)
		}
	}
	return out
}

func uniqueCleanPaths(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, value := range in {
		value = cleanRoot(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, value := range in {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func validateDeliverables(items []Deliverable) error {
	seen := map[string]bool{}
	for _, item := range items {
		if item.ID == "" || item.Description == "" {
			return errors.New("deliverable id and description are required")
		}
		if seen[item.ID] {
			return fmt.Errorf("duplicate deliverable id %q", item.ID)
		}
		seen[item.ID] = true
	}
	return nil
}

func validatePathRules(rules []PathRule) error {
	for _, rule := range rules {
		if rule.Root == "" || !filepath.IsAbs(rule.Root) {
			return errors.New("allowed mutation roots must be absolute")
		}
		switch rule.Access {
		case "write", "create", "delete":
		default:
			return fmt.Errorf("invalid mutation access %q", rule.Access)
		}
	}
	return nil
}

func validateAcceptanceChecks(checks []AcceptanceCheck) error {
	seen := map[string]bool{}
	for _, check := range checks {
		if check.ID == "" || check.Description == "" || check.Verifier == "" {
			return errors.New("acceptance check id, description, and verifier are required")
		}
		if seen[check.ID] {
			return fmt.Errorf("duplicate acceptance check id %q", check.ID)
		}
		seen[check.ID] = true
	}
	return nil
}
