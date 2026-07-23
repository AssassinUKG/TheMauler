package engagement

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	IndefiniteRuns                 = "indefinite"
	CurrentDefinitionSchemaVersion = 2

	PackTrustOfficial  = "official"
	PackTrustCurated   = "curated"
	PackTrustCommunity = "community"
	PackTrustLocal     = "local"

	SafetyPassive     = "passive"
	SafetySafeActive  = "safe_active"
	SafetyIntrusive   = "intrusive"
	SafetyDestructive = "destructive"
)

// RunCount accepts the Shiftgrid-compatible JSON shape: a positive integer or
// the string "indefinite". An omitted value has an effective count of one.
type RunCount struct {
	Count      int
	Indefinite bool
}

func (r RunCount) EffectiveCount() int {
	if r.Indefinite {
		return 0
	}
	if r.Count <= 0 {
		return 1
	}
	return r.Count
}

func (r RunCount) ShouldRepeat(completedRuns int) bool {
	if r.Indefinite {
		return true
	}
	return completedRuns < r.EffectiveCount()
}

func (r *RunCount) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*r = RunCount{}
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(value), IndefiniteRuns) {
			return fmt.Errorf("runs must be a positive integer or %q", IndefiniteRuns)
		}
		*r = RunCount{Indefinite: true}
		return nil
	}
	value, err := strconv.Atoi(string(data))
	if err != nil || value < 1 {
		return fmt.Errorf("runs must be a positive integer or %q", IndefiniteRuns)
	}
	*r = RunCount{Count: value}
	return nil
}

func (r RunCount) MarshalJSON() ([]byte, error) {
	if r.Indefinite {
		return json.Marshal(IndefiniteRuns)
	}
	return []byte(strconv.Itoa(r.EffectiveCount())), nil
}

type WorkflowDefinition struct {
	SchemaVersion int               `json:"schema_version,omitempty"`
	ID            string            `json:"id"`
	Version       string            `json:"version,omitempty"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Color         string            `json:"color,omitempty"`
	Checklist     string            `json:"checklist,omitempty"`
	Source        DefinitionSource  `json:"source,omitempty"`
	Phases        []PhaseDefinition `json:"phases"`
	Digest        string            `json:"-"`
}

type PhaseDefinition struct {
	ID          string           `json:"id"`
	Icon        string           `json:"icon,omitempty"`
	Name        string           `json:"name"`
	Kind        string           `json:"kind,omitempty"`
	Description string           `json:"description,omitempty"`
	Optional    bool             `json:"optional,omitempty"`
	Free        bool             `json:"free,omitempty"`
	Runs        RunCount         `json:"runs,omitempty"`
	Steps       []StepDefinition `json:"steps"`
}

func (p PhaseDefinition) EffectiveKind() string {
	kind := strings.ToLower(strings.TrimSpace(p.Kind))
	if kind == "" {
		return "step"
	}
	return kind
}

type StepDefinition struct {
	ID          string   `json:"id,omitempty"`
	Check       string   `json:"check,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Examples    string   `json:"examples,omitempty"`
	Repeatable  bool     `json:"repeatable,omitempty"`
	Runs        RunCount `json:"runs,omitempty"`
}

func (s StepDefinition) EffectiveID() string {
	if id := strings.TrimSpace(s.ID); id != "" {
		return id
	}
	return strings.TrimSpace(s.Check)
}

type ChecklistDefinition struct {
	SchemaVersion int               `json:"schema_version,omitempty"`
	ID            string            `json:"id"`
	Version       string            `json:"version,omitempty"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Source        DefinitionSource  `json:"source,omitempty"`
	Items         []CheckDefinition `json:"items"`
	Digest        string            `json:"-"`
}

// DefinitionSource pins a workflow/checklist to the reviewed upstream input.
// Digest is calculated from the complete local JSON document during parsing;
// SourceDigest may hold an upstream file digest when one is available.
type DefinitionSource struct {
	ID           string `json:"id,omitempty"`
	Repository   string `json:"repository,omitempty"`
	Path         string `json:"path,omitempty"`
	Ref          string `json:"ref,omitempty"`
	Commit       string `json:"commit,omitempty"`
	License      string `json:"license,omitempty"`
	Trust        string `json:"trust,omitempty"`
	SourceDigest string `json:"source_digest,omitempty"`
}

type CheckMapping struct {
	Framework string `json:"framework"`
	ID        string `json:"id"`
	URL       string `json:"url,omitempty"`
}

// CheckApplicability limits a catalog check to targets where it makes sense.
// Values within one field are alternatives; different populated fields are
// combined as AND constraints. RequiresFeatures must all be present.
type CheckApplicability struct {
	TargetKinds      []string `json:"target_kinds,omitempty"`
	Protocols        []string `json:"protocols,omitempty"`
	EndpointKinds    []string `json:"endpoint_kinds,omitempty"`
	Technologies     []string `json:"technologies,omitempty"`
	AnyFeatures      []string `json:"any_features,omitempty"`
	RequiresFeatures []string `json:"requires_features,omitempty"`
	Authentication   string   `json:"authentication,omitempty"`
}

type CheckEvidencePolicy struct {
	RequiredKinds   []string `json:"required_kinds,omitempty"`
	Minimum         int      `json:"minimum,omitempty"`
	Reproduce       bool     `json:"reproduce,omitempty"`
	NegativeControl bool     `json:"negative_control,omitempty"`
}

type CheckAutomationReference struct {
	Adapter string `json:"adapter"`
	ID      string `json:"id"`
	Source  string `json:"source,omitempty"`
}

type CheckDefinition struct {
	ID                 string                     `json:"id"`
	Title              string                     `json:"title"`
	Category           string                     `json:"category,omitempty"`
	CategoryName       string                     `json:"category_name,omitempty"`
	Scope              string                     `json:"scope"`
	Description        string                     `json:"description,omitempty"`
	Examples           string                     `json:"examples,omitempty"`
	Mappings           []CheckMapping             `json:"mappings,omitempty"`
	AppliesWhen        CheckApplicability         `json:"applies_when,omitempty"`
	Prerequisites      []string                   `json:"prerequisites,omitempty"`
	Procedure          []string                   `json:"procedure,omitempty"`
	ExpectedSignals    []string                   `json:"expected_signals,omitempty"`
	NegativeSignals    []string                   `json:"negative_signals,omitempty"`
	FalsePositiveNotes []string                   `json:"false_positive_notes,omitempty"`
	Safety             string                     `json:"safety,omitempty"`
	Evidence           CheckEvidencePolicy        `json:"evidence,omitempty"`
	Automation         []CheckAutomationReference `json:"automation,omitempty"`
	Tags               []string                   `json:"tags,omitempty"`
	Maturity           string                     `json:"maturity,omitempty"`
	Verified           bool                       `json:"verified,omitempty"`
	Deprecated         bool                       `json:"deprecated,omitempty"`
	SupersededBy       string                     `json:"superseded_by,omitempty"`
	EstimatedMinutes   int                        `json:"estimated_minutes,omitempty"`
	Repeatable         bool                       `json:"repeatable,omitempty"`
	Runs               RunCount                   `json:"runs,omitempty"`
	ProducesEndpoints  bool                       `json:"produces_endpoints,omitempty"`
}

func ParseWorkflow(data []byte) (WorkflowDefinition, error) {
	var workflow WorkflowDefinition
	if err := json.Unmarshal(data, &workflow); err != nil {
		return WorkflowDefinition{}, fmt.Errorf("parse workflow: %w", err)
	}
	workflow.Digest = definitionDigest(data)
	if err := workflow.Validate(); err != nil {
		return WorkflowDefinition{}, err
	}
	return workflow, nil
}

func ParseChecklist(data []byte) (ChecklistDefinition, error) {
	var checklist ChecklistDefinition
	if err := json.Unmarshal(data, &checklist); err != nil {
		return ChecklistDefinition{}, fmt.Errorf("parse checklist: %w", err)
	}
	checklist.Digest = definitionDigest(data)
	if err := checklist.Validate(); err != nil {
		return ChecklistDefinition{}, err
	}
	return checklist, nil
}

func (w WorkflowDefinition) Validate() error {
	if err := validateDefinitionMetadata("workflow", w.SchemaVersion, w.ID, w.Version, w.Source); err != nil {
		return err
	}
	if strings.TrimSpace(w.ID) == "" {
		return fmt.Errorf("workflow id is required")
	}
	if strings.TrimSpace(w.Name) == "" {
		return fmt.Errorf("workflow %q name is required", w.ID)
	}
	if len(w.Phases) == 0 {
		return fmt.Errorf("workflow %q needs at least one phase", w.ID)
	}
	phaseIDs := map[string]bool{}
	for _, phase := range w.Phases {
		phaseID := strings.TrimSpace(phase.ID)
		if phaseID == "" {
			return fmt.Errorf("workflow %q contains a phase without an id", w.ID)
		}
		if phaseIDs[phaseID] {
			return fmt.Errorf("workflow %q has duplicate phase id %q", w.ID, phaseID)
		}
		phaseIDs[phaseID] = true
		if strings.TrimSpace(phase.Name) == "" {
			return fmt.Errorf("workflow %q phase %q name is required", w.ID, phaseID)
		}
		switch phase.EffectiveKind() {
		case "step", "checklist", "endpoint":
		default:
			return fmt.Errorf("workflow %q phase %q has unsupported kind %q", w.ID, phaseID, phase.Kind)
		}
		if len(phase.Steps) == 0 {
			return fmt.Errorf("workflow %q phase %q needs at least one step", w.ID, phaseID)
		}
		stepIDs := map[string]bool{}
		for _, step := range phase.Steps {
			stepID := step.EffectiveID()
			if stepID == "" {
				return fmt.Errorf("workflow %q phase %q contains a step without id/check", w.ID, phaseID)
			}
			if stepIDs[stepID] {
				return fmt.Errorf("workflow %q phase %q has duplicate step id %q", w.ID, phaseID, stepID)
			}
			stepIDs[stepID] = true
			if strings.TrimSpace(step.Title) == "" {
				return fmt.Errorf("workflow %q phase %q step %q title is required", w.ID, phaseID, stepID)
			}
		}
	}
	return nil
}

func (c ChecklistDefinition) Validate() error {
	if err := validateDefinitionMetadata("checklist", c.SchemaVersion, c.ID, c.Version, c.Source); err != nil {
		return err
	}
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("checklist id is required")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("checklist %q name is required", c.ID)
	}
	if len(c.Items) == 0 {
		return fmt.Errorf("checklist %q needs at least one item", c.ID)
	}
	ids := map[string]bool{}
	for _, item := range c.Items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return fmt.Errorf("checklist %q contains an item without an id", c.ID)
		}
		if ids[id] {
			return fmt.Errorf("checklist %q has duplicate item id %q", c.ID, id)
		}
		ids[id] = true
		if strings.TrimSpace(item.Title) == "" {
			return fmt.Errorf("checklist %q item %q title is required", c.ID, id)
		}
		if err := validateCheckDefinition(c.ID, item); err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(item.Scope)) {
		case "global", "per_endpoint":
		default:
			return fmt.Errorf("checklist %q item %q has unsupported scope %q", c.ID, id, item.Scope)
		}
	}
	return nil
}

func validateDefinitionMetadata(kind string, schemaVersion int, id, version string, source DefinitionSource) error {
	if schemaVersion < 0 || schemaVersion > CurrentDefinitionSchemaVersion {
		return fmt.Errorf("%s %q uses unsupported schema_version %d", kind, id, schemaVersion)
	}
	if schemaVersion < CurrentDefinitionSchemaVersion {
		return nil
	}
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("%s %q version is required for schema_version %d", kind, id, schemaVersion)
	}
	if strings.TrimSpace(source.ID) == "" || strings.TrimSpace(source.Repository) == "" || strings.TrimSpace(source.Ref) == "" || strings.TrimSpace(source.License) == "" {
		return fmt.Errorf("%s %q source id, repository, ref, and license are required for schema_version %d", kind, id, schemaVersion)
	}
	switch strings.ToLower(strings.TrimSpace(source.Trust)) {
	case PackTrustOfficial, PackTrustCurated, PackTrustCommunity, PackTrustLocal:
	default:
		return fmt.Errorf("%s %q has unsupported source trust %q", kind, id, source.Trust)
	}
	return nil
}

func validateCheckDefinition(checklistID string, item CheckDefinition) error {
	id := strings.TrimSpace(item.ID)
	if item.EstimatedMinutes < 0 {
		return fmt.Errorf("checklist %q item %q estimated_minutes cannot be negative", checklistID, id)
	}
	if item.Evidence.Minimum < 0 {
		return fmt.Errorf("checklist %q item %q evidence minimum cannot be negative", checklistID, id)
	}
	if safety := strings.ToLower(strings.TrimSpace(item.Safety)); safety != "" {
		switch safety {
		case SafetyPassive, SafetySafeActive, SafetyIntrusive, SafetyDestructive:
		default:
			return fmt.Errorf("checklist %q item %q has unsupported safety %q", checklistID, id, item.Safety)
		}
	}
	if maturity := strings.ToLower(strings.TrimSpace(item.Maturity)); maturity != "" {
		switch maturity {
		case "experimental", "stable", "deprecated":
		default:
			return fmt.Errorf("checklist %q item %q has unsupported maturity %q", checklistID, id, item.Maturity)
		}
	}
	if item.Verified {
		if !strings.EqualFold(strings.TrimSpace(item.Maturity), "stable") {
			return fmt.Errorf("checklist %q item %q must be stable before it can be fixture-verified", checklistID, id)
		}
		if len(item.Automation) == 0 {
			return fmt.Errorf("checklist %q item %q must pin an automation adapter before it can be fixture-verified", checklistID, id)
		}
	}
	if auth := strings.ToLower(strings.TrimSpace(item.AppliesWhen.Authentication)); auth != "" {
		switch auth {
		case "any", "anonymous", "authenticated", "both":
		default:
			return fmt.Errorf("checklist %q item %q has unsupported authentication applicability %q", checklistID, id, item.AppliesWhen.Authentication)
		}
	}
	if err := validateUniqueStrings("mapping", mappingKeys(item.Mappings)); err != nil {
		return fmt.Errorf("checklist %q item %q: %w", checklistID, id, err)
	}
	for _, mapping := range item.Mappings {
		if strings.TrimSpace(mapping.Framework) == "" || strings.TrimSpace(mapping.ID) == "" {
			return fmt.Errorf("checklist %q item %q has a mapping without framework/id", checklistID, id)
		}
	}
	if err := validateUniqueStrings("automation reference", automationKeys(item.Automation)); err != nil {
		return fmt.Errorf("checklist %q item %q: %w", checklistID, id, err)
	}
	for _, ref := range item.Automation {
		if strings.TrimSpace(ref.Adapter) == "" || strings.TrimSpace(ref.ID) == "" {
			return fmt.Errorf("checklist %q item %q has an automation reference without adapter/id", checklistID, id)
		}
	}
	for label, values := range map[string][]string{
		"target kind":      item.AppliesWhen.TargetKinds,
		"protocol":         item.AppliesWhen.Protocols,
		"endpoint kind":    item.AppliesWhen.EndpointKinds,
		"technology":       item.AppliesWhen.Technologies,
		"feature":          item.AppliesWhen.AnyFeatures,
		"required feature": item.AppliesWhen.RequiresFeatures,
		"evidence kind":    item.Evidence.RequiredKinds,
		"tag":              item.Tags,
	} {
		if err := validateUniqueStrings(label, values); err != nil {
			return fmt.Errorf("checklist %q item %q: %w", checklistID, id, err)
		}
	}
	return nil
}

func validateUniqueStrings(label string, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			return fmt.Errorf("%s cannot be empty", label)
		}
		if seen[key] {
			return fmt.Errorf("duplicate %s %q", label, value)
		}
		seen[key] = true
	}
	return nil
}

func mappingKeys(mappings []CheckMapping) []string {
	values := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		values = append(values, mapping.Framework+":"+mapping.ID)
	}
	return values
}

func automationKeys(refs []CheckAutomationReference) []string {
	values := make([]string, 0, len(refs))
	for _, ref := range refs {
		values = append(values, ref.Adapter+":"+ref.ID)
	}
	return values
}

func definitionDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func ValidateBundle(workflow WorkflowDefinition, checklist ChecklistDefinition) error {
	if err := workflow.Validate(); err != nil {
		return err
	}
	if err := checklist.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(workflow.Checklist) != strings.TrimSpace(checklist.ID) {
		return fmt.Errorf("workflow %q references checklist %q, got %q", workflow.ID, workflow.Checklist, checklist.ID)
	}
	needsGlobal := false
	needsEndpoint := false
	for _, phase := range workflow.Phases {
		switch phase.EffectiveKind() {
		case "checklist":
			needsGlobal = true
		case "endpoint":
			needsEndpoint = true
		}
	}
	hasGlobal := false
	hasEndpoint := false
	for _, item := range checklist.Items {
		switch strings.ToLower(strings.TrimSpace(item.Scope)) {
		case "global":
			hasGlobal = true
		case "per_endpoint":
			hasEndpoint = true
		}
	}
	if needsGlobal && !hasGlobal {
		return fmt.Errorf("workflow %q has a checklist phase but checklist %q has no global checks", workflow.ID, checklist.ID)
	}
	if needsEndpoint && !hasEndpoint {
		return fmt.Errorf("workflow %q has an endpoint phase but checklist %q has no per_endpoint checks", workflow.ID, checklist.ID)
	}
	return nil
}
