package packlibrary

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"mauler/internal/engagement"
	"mauler/internal/engagement/checkpacks"
)

const (
	BundleSchemaVersion = 1
	ScopeBuiltin        = "builtin"
	ScopePersonal       = "personal"
	ScopeProject        = "project"
)

var (
	packIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)
	packVersionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)
)

// DefinitionPin preserves the exact source definition a local clone started
// from without letting the clone continue to claim the source pack's trust.
type DefinitionPin struct {
	WorkflowID       string `json:"workflow_id"`
	WorkflowVersion  string `json:"workflow_version"`
	WorkflowDigest   string `json:"workflow_digest"`
	ChecklistID      string `json:"checklist_id"`
	ChecklistVersion string `json:"checklist_version"`
	ChecklistDigest  string `json:"checklist_digest"`
}

// Bundle is the portable, versioned Pack Library file format. Definitions are
// kept together so a workflow can never silently resolve to a different
// checklist version.
type Bundle struct {
	SchemaVersion int                            `json:"schema_version"`
	ID            string                         `json:"id"`
	Version       string                         `json:"version"`
	Name          string                         `json:"name"`
	Description   string                         `json:"description,omitempty"`
	BasedOn       *DefinitionPin                 `json:"based_on,omitempty"`
	Workflow      engagement.WorkflowDefinition  `json:"workflow"`
	Checklist     engagement.ChecklistDefinition `json:"checklist"`
}

type Summary struct {
	Key              string                    `json:"key"`
	ID               string                    `json:"id"`
	Version          string                    `json:"version"`
	Name             string                    `json:"name"`
	Description      string                    `json:"description,omitempty"`
	Scope            string                    `json:"scope"`
	Trust            string                    `json:"trust"`
	License          string                    `json:"license"`
	Path             string                    `json:"path,omitempty"`
	BuiltIn          bool                      `json:"built_in"`
	Archived         bool                      `json:"archived"`
	Active           bool                      `json:"active"`
	Valid            bool                      `json:"valid"`
	ValidationError  string                    `json:"validation_error,omitempty"`
	WorkflowID       string                    `json:"workflow_id"`
	WorkflowVersion  string                    `json:"workflow_version"`
	ChecklistID      string                    `json:"checklist_id"`
	ChecklistVersion string                    `json:"checklist_version"`
	WorkflowDigest   string                    `json:"workflow_digest"`
	ChecklistDigest  string                    `json:"checklist_digest"`
	PhaseCount       int                       `json:"phase_count"`
	CheckCount       int                       `json:"check_count"`
	GlobalChecks     int                       `json:"global_checks"`
	EndpointChecks   int                       `json:"endpoint_checks"`
	AutomatedChecks  int                       `json:"automated_checks"`
	QualityScore     int                       `json:"quality_score"`
	QualityReady     bool                      `json:"quality_ready"`
	QualityIssues    []checkpacks.QualityIssue `json:"quality_issues,omitempty"`
}

type Snapshot struct {
	Packs         []Summary `json:"packs"`
	PersonalRoot  string    `json:"personal_root"`
	ProjectRoot   string    `json:"project_root,omitempty"`
	BuiltInCount  int       `json:"built_in_count"`
	ActiveCount   int       `json:"active_count"`
	ArchivedCount int       `json:"archived_count"`
	InvalidCount  int       `json:"invalid_count"`
}

type CloneInput struct {
	SourceKey string `json:"source_key"`
	Scope     string `json:"scope"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
}

type Library struct {
	personalRoot string
	builtins     engagement.Catalog
	registry     *checkpacks.Registry
}

type entry struct {
	bundle  Bundle
	summary Summary
}

func New(personalRoot string) (*Library, error) {
	personalRoot = strings.TrimSpace(personalRoot)
	if personalRoot == "" {
		return nil, fmt.Errorf("personal pack root is required")
	}
	builtins, err := engagement.LoadEmbeddedCatalog()
	if err != nil {
		return nil, err
	}
	registry, err := checkpacks.DefaultRegistry()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(personalRoot, 0o750); err != nil {
		return nil, fmt.Errorf("create personal pack root: %w", err)
	}
	return &Library{personalRoot: filepath.Clean(personalRoot), builtins: builtins, registry: registry}, nil
}

func (l *Library) List(workspace string) (Snapshot, error) {
	entries, err := l.entries(workspace)
	if err != nil {
		return Snapshot{}, err
	}
	markActive(entries)
	snapshot := Snapshot{
		Packs: make([]Summary, 0, len(entries)), PersonalRoot: filepath.ToSlash(l.personalRoot),
		ProjectRoot: filepath.ToSlash(projectRoot(workspace)),
	}
	for _, item := range entries {
		snapshot.Packs = append(snapshot.Packs, item.summary)
		if item.summary.BuiltIn {
			snapshot.BuiltInCount++
		}
		if item.summary.Active {
			snapshot.ActiveCount++
		}
		if item.summary.Archived {
			snapshot.ArchivedCount++
		}
		if !item.summary.Valid {
			snapshot.InvalidCount++
		}
	}
	sort.Slice(snapshot.Packs, func(i, j int) bool {
		if snapshot.Packs[i].Active != snapshot.Packs[j].Active {
			return snapshot.Packs[i].Active
		}
		if snapshot.Packs[i].Name != snapshot.Packs[j].Name {
			return strings.ToLower(snapshot.Packs[i].Name) < strings.ToLower(snapshot.Packs[j].Name)
		}
		return compareVersions(snapshot.Packs[i].Version, snapshot.Packs[j].Version) > 0
	})
	return snapshot, nil
}

// Catalog returns one active version of every non-archived workflow. Project
// packs win ties over personal packs, which win ties over built-ins.
func (l *Library) Catalog(workspace string) (engagement.Catalog, error) {
	entries, err := l.entries(workspace)
	if err != nil {
		return engagement.Catalog{}, err
	}
	markActive(entries)
	catalog := engagement.Catalog{Workflows: map[string]engagement.WorkflowDefinition{}, Checklists: map[string]engagement.ChecklistDefinition{}}
	for _, item := range entries {
		if !item.summary.Valid || !item.summary.Active || item.summary.Archived {
			continue
		}
		catalog.Workflows[item.bundle.Workflow.ID] = item.bundle.Workflow
		catalog.Checklists[item.bundle.Checklist.ID] = item.bundle.Checklist
	}
	return catalog, nil
}

func (l *Library) Clone(workspace string, input CloneInput) (Summary, error) {
	input.ID = strings.ToLower(strings.TrimSpace(input.ID))
	input.Name = strings.TrimSpace(input.Name)
	input.Version = firstNonEmpty(strings.TrimSpace(input.Version), "0.1.0")
	if err := validatePackIdentity(input.ID, input.Version); err != nil {
		return Summary{}, err
	}
	if input.Name == "" {
		return Summary{}, fmt.Errorf("pack name is required")
	}
	source, err := l.resolve(workspace, input.SourceKey)
	if err != nil {
		return Summary{}, err
	}
	if !source.summary.Valid {
		return Summary{}, fmt.Errorf("pack %q is quarantined: %s", input.SourceKey, source.summary.ValidationError)
	}
	workflow, checklist, err := cloneDefinitions(source.bundle.Workflow, source.bundle.Checklist)
	if err != nil {
		return Summary{}, err
	}
	workflow.ID = input.ID
	workflow.Name = input.Name
	workflow.Version = input.Version
	checklist.ID = input.ID + "-checks"
	checklist.Name = input.Name + " Checks"
	checklist.Version = input.Version
	workflow.Checklist = checklist.ID
	license := firstNonEmpty(strings.TrimSpace(checklist.Source.License), strings.TrimSpace(workflow.Source.License), "Local")
	sourceMeta := engagement.DefinitionSource{
		ID: "local-" + input.ID, Repository: "mauler://pack-library", Ref: input.Version,
		License: license, Trust: engagement.PackTrustLocal,
	}
	workflow.Source = sourceMeta
	checklist.Source = sourceMeta
	bundle := Bundle{
		SchemaVersion: BundleSchemaVersion, ID: input.ID, Version: input.Version, Name: input.Name,
		Description: source.bundle.Description, Workflow: workflow, Checklist: checklist,
		BasedOn: &DefinitionPin{
			WorkflowID: source.bundle.Workflow.ID, WorkflowVersion: source.bundle.Workflow.Version, WorkflowDigest: source.summary.WorkflowDigest,
			ChecklistID: source.bundle.Checklist.ID, ChecklistVersion: source.bundle.Checklist.Version, ChecklistDigest: source.summary.ChecklistDigest,
		},
	}
	return l.write(workspace, normaliseWritableScope(input.Scope), bundle)
}

func (l *Library) Import(workspace, scope, raw string) (Summary, error) {
	var bundle Bundle
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return Summary{}, fmt.Errorf("parse pack bundle: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Summary{}, fmt.Errorf("parse pack bundle: multiple JSON values are not allowed")
		}
		return Summary{}, fmt.Errorf("parse pack bundle: %w", err)
	}
	if err := bundle.Validate(); err != nil {
		return Summary{}, err
	}
	if !strings.EqualFold(bundle.Checklist.Source.Trust, engagement.PackTrustLocal) {
		if err := l.registry.ValidateProvenance(bundle.Checklist.Source); err != nil {
			return Summary{}, fmt.Errorf("unapproved checklist source: %w", err)
		}
	}
	if !strings.EqualFold(bundle.Workflow.Source.Trust, engagement.PackTrustLocal) {
		if err := l.registry.ValidateProvenance(bundle.Workflow.Source); err != nil {
			return Summary{}, fmt.Errorf("unapproved workflow source: %w", err)
		}
	}
	return l.write(workspace, normaliseWritableScope(scope), bundle)
}

func (l *Library) Export(workspace, key string) (string, error) {
	item, err := l.resolve(workspace, key)
	if err != nil {
		return "", err
	}
	if !item.summary.Valid {
		return "", fmt.Errorf("pack %q is quarantined: %s", key, item.summary.ValidationError)
	}
	data, err := json.MarshalIndent(item.bundle, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func (l *Library) SetArchived(workspace, key string, archived bool) (Summary, error) {
	item, err := l.resolve(workspace, key)
	if err != nil {
		return Summary{}, err
	}
	if item.summary.BuiltIn {
		return Summary{}, fmt.Errorf("built-in packs are immutable")
	}
	marker := archiveMarker(item.summary.Path)
	if archived {
		if err := os.WriteFile(marker, []byte("archived\n"), 0o600); err != nil {
			return Summary{}, fmt.Errorf("archive pack: %w", err)
		}
	} else if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return Summary{}, fmt.Errorf("restore pack: %w", err)
	}
	snapshot, err := l.List(workspace)
	if err != nil {
		return Summary{}, err
	}
	for _, summary := range snapshot.Packs {
		if summary.Key == key {
			return summary, nil
		}
	}
	return Summary{}, fmt.Errorf("pack %q disappeared after archive update", key)
}

func (b Bundle) Validate() error {
	if b.SchemaVersion != BundleSchemaVersion {
		return fmt.Errorf("pack %q uses unsupported schema_version %d", b.ID, b.SchemaVersion)
	}
	if err := validatePackIdentity(strings.TrimSpace(b.ID), strings.TrimSpace(b.Version)); err != nil {
		return err
	}
	if strings.TrimSpace(b.Name) == "" {
		return fmt.Errorf("pack %q name is required", b.ID)
	}
	if strings.TrimSpace(b.Workflow.ID) != strings.TrimSpace(b.ID) {
		return fmt.Errorf("pack id %q must match workflow id %q", b.ID, b.Workflow.ID)
	}
	if strings.TrimSpace(b.Workflow.Version) != strings.TrimSpace(b.Version) {
		return fmt.Errorf("pack version %q must match workflow version %q", b.Version, b.Workflow.Version)
	}
	if err := engagement.ValidateBundle(b.Workflow, b.Checklist); err != nil {
		return err
	}
	return nil
}

func (l *Library) write(workspace, scope string, bundle Bundle) (Summary, error) {
	if err := bundle.Validate(); err != nil {
		return Summary{}, err
	}
	if _, exists := l.builtins.Workflows[bundle.Workflow.ID]; exists {
		return Summary{}, fmt.Errorf("workflow id %q is reserved by a built-in pack", bundle.Workflow.ID)
	}
	items, err := l.entries(workspace)
	if err != nil {
		return Summary{}, err
	}
	for _, item := range items {
		if !item.summary.Valid || item.bundle.Workflow.ID == bundle.Workflow.ID {
			continue
		}
		if item.bundle.Checklist.ID == bundle.Checklist.ID {
			return Summary{}, fmt.Errorf("checklist id %q is already owned by workflow %q", bundle.Checklist.ID, item.bundle.Workflow.ID)
		}
	}
	root, err := l.rootForScope(workspace, scope)
	if err != nil {
		return Summary{}, err
	}
	dir := filepath.Join(root, bundle.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Summary{}, fmt.Errorf("create pack directory: %w", err)
	}
	path := filepath.Join(dir, bundle.Version+".pack.json")
	if _, err := os.Stat(path); err == nil {
		return Summary{}, fmt.Errorf("pack %s@%s already exists in %s scope", bundle.ID, bundle.Version, scope)
	} else if !os.IsNotExist(err) {
		return Summary{}, err
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return Summary{}, err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, append(data, '\n'), 0o600); err != nil {
		return Summary{}, fmt.Errorf("write pack: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return Summary{}, fmt.Errorf("publish pack: %w", err)
	}
	snapshot, err := l.List(workspace)
	if err != nil {
		return Summary{}, err
	}
	key := packKey(scope, bundle.ID, bundle.Version)
	for _, summary := range snapshot.Packs {
		if summary.Key == key {
			return summary, nil
		}
	}
	return Summary{}, fmt.Errorf("pack %q was written but not indexed", key)
}

func (l *Library) entries(workspace string) ([]entry, error) {
	items := make([]entry, 0, len(l.builtins.Workflows)+4)
	workflowIDs := make([]string, 0, len(l.builtins.Workflows))
	for id := range l.builtins.Workflows {
		workflowIDs = append(workflowIDs, id)
	}
	sort.Strings(workflowIDs)
	for _, id := range workflowIDs {
		workflow, checklist, err := l.builtins.Bundle(id)
		if err != nil {
			return nil, err
		}
		bundle := Bundle{
			SchemaVersion: BundleSchemaVersion, ID: workflow.ID, Version: workflow.Version,
			Name: workflow.Name, Description: workflow.Description, Workflow: workflow, Checklist: checklist,
		}
		items = append(items, entry{bundle: bundle, summary: l.summary(bundle, ScopeBuiltin, "", false)})
	}
	for _, source := range []struct{ scope, root string }{{ScopePersonal, l.personalRoot}, {ScopeProject, projectRoot(workspace)}} {
		loaded, err := l.loadRoot(source.root, source.scope)
		if err != nil {
			return nil, err
		}
		items = append(items, loaded...)
	}
	return items, nil
}

func (l *Library) loadRoot(root, scope string) ([]entry, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("pack root %s is not a directory", root)
	}
	items := []entry{}
	err = filepath.WalkDir(root, func(path string, dir os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if dir.IsDir() {
			if path != root && filepath.Dir(path) != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(dir.Name()), ".pack.json") {
			return nil
		}
		archived := false
		if _, err := os.Stat(archiveMarker(path)); err == nil {
			archived = true
		} else if !os.IsNotExist(err) {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			items = append(items, invalidEntry(scope, path, archived, fmt.Errorf("read pack: %w", err)))
			return nil
		}
		var bundle Bundle
		if err := json.Unmarshal(data, &bundle); err != nil {
			items = append(items, invalidEntry(scope, path, archived, fmt.Errorf("parse pack: %w", err)))
			return nil
		}
		if err := bundle.Validate(); err != nil {
			items = append(items, invalidEntry(scope, path, archived, fmt.Errorf("validate pack: %w", err)))
			return nil
		}
		items = append(items, entry{bundle: bundle, summary: l.summary(bundle, scope, path, archived)})
		return nil
	})
	return items, err
}

func (l *Library) summary(bundle Bundle, scope, path string, archived bool) Summary {
	quality := checkpacks.ReviewChecklist(bundle.Checklist, l.registry)
	global, endpoint, automated := 0, 0, 0
	for _, check := range bundle.Checklist.Items {
		if strings.EqualFold(check.Scope, "per_endpoint") {
			endpoint++
		} else {
			global++
		}
		if len(check.Automation) > 0 {
			automated++
		}
	}
	return Summary{
		Key: packKey(scope, bundle.ID, bundle.Version), ID: bundle.ID, Version: bundle.Version,
		Name: bundle.Name, Description: bundle.Description, Scope: scope,
		Trust:   firstNonEmpty(bundle.Checklist.Source.Trust, bundle.Workflow.Source.Trust, engagement.PackTrustLocal),
		License: firstNonEmpty(bundle.Checklist.Source.License, bundle.Workflow.Source.License, "Unknown"),
		Path:    filepath.ToSlash(path), BuiltIn: scope == ScopeBuiltin, Archived: archived, Valid: true,
		WorkflowID: bundle.Workflow.ID, WorkflowVersion: bundle.Workflow.Version,
		ChecklistID: bundle.Checklist.ID, ChecklistVersion: bundle.Checklist.Version,
		WorkflowDigest: definitionDigest(bundle.Workflow), ChecklistDigest: definitionDigest(bundle.Checklist),
		PhaseCount: len(bundle.Workflow.Phases), CheckCount: len(bundle.Checklist.Items),
		GlobalChecks: global, EndpointChecks: endpoint, AutomatedChecks: automated,
		QualityScore: quality.Score, QualityReady: quality.Ready, QualityIssues: quality.Issues,
	}
}

func invalidEntry(scope, path string, archived bool, validationErr error) entry {
	digest := sha256.Sum256([]byte(filepath.Clean(path)))
	id := fmt.Sprintf("invalid-%x", digest[:6])
	return entry{summary: Summary{
		Key: packKey(scope, id, "unknown"), ID: id, Version: "unknown",
		Name: filepath.Base(path), Scope: scope, Trust: engagement.PackTrustLocal,
		Path: filepath.ToSlash(path), Archived: archived, Valid: false,
		ValidationError: validationErr.Error(),
	}}
}

func (l *Library) resolve(workspace, key string) (entry, error) {
	items, err := l.entries(workspace)
	if err != nil {
		return entry{}, err
	}
	for _, item := range items {
		if item.summary.Key == strings.TrimSpace(key) {
			return item, nil
		}
	}
	return entry{}, fmt.Errorf("unknown pack %q", key)
}

func (l *Library) rootForScope(workspace, scope string) (string, error) {
	switch scope {
	case ScopePersonal:
		return l.personalRoot, nil
	case ScopeProject:
		root := projectRoot(workspace)
		if root == "" {
			return "", fmt.Errorf("project pack scope requires an active workspace")
		}
		return root, nil
	default:
		return "", fmt.Errorf("pack scope must be personal or project")
	}
}

func markActive(items []entry) {
	selected := map[string]int{}
	for index := range items {
		if !items[index].summary.Valid || items[index].summary.Archived {
			continue
		}
		id := items[index].bundle.Workflow.ID
		current, exists := selected[id]
		if !exists || entryPreferred(items[index], items[current]) {
			selected[id] = index
		}
	}
	for _, index := range selected {
		items[index].summary.Active = true
	}
}

func entryPreferred(candidate, current entry) bool {
	if comparison := compareVersions(candidate.bundle.Version, current.bundle.Version); comparison != 0 {
		return comparison > 0
	}
	return scopePriority(candidate.summary.Scope) > scopePriority(current.summary.Scope)
}

func compareVersions(a, b string) int {
	partsA := strings.FieldsFunc(a, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	partsB := strings.FieldsFunc(b, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	limit := len(partsA)
	if len(partsB) > limit {
		limit = len(partsB)
	}
	for index := 0; index < limit; index++ {
		left, right := "0", "0"
		if index < len(partsA) {
			left = partsA[index]
		}
		if index < len(partsB) {
			right = partsB[index]
		}
		li, lerr := strconv.Atoi(left)
		ri, rerr := strconv.Atoi(right)
		if lerr == nil && rerr == nil {
			if li < ri {
				return -1
			}
			if li > ri {
				return 1
			}
			continue
		}
		if comparison := strings.Compare(strings.ToLower(left), strings.ToLower(right)); comparison != 0 {
			return comparison
		}
	}
	return 0
}

func scopePriority(scope string) int {
	switch scope {
	case ScopeProject:
		return 3
	case ScopePersonal:
		return 2
	default:
		return 1
	}
}

func validatePackIdentity(id, version string) error {
	if !packIDPattern.MatchString(id) {
		return fmt.Errorf("pack id must use 2-64 lowercase letters, numbers, dots, dashes, or underscores")
	}
	if !packVersionPattern.MatchString(version) || strings.Contains(version, "..") {
		return fmt.Errorf("pack version contains unsupported characters")
	}
	return nil
}

func normaliseWritableScope(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), ScopeProject) {
		return ScopeProject
	}
	return ScopePersonal
}

func projectRoot(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(workspace), ".mauler", "engagement-packs")
}

func packKey(scope, id, version string) string { return scope + ":" + id + "@" + version }
func archiveMarker(path string) string         { return filepath.FromSlash(path) + ".archived" }

func cloneDefinitions(workflow engagement.WorkflowDefinition, checklist engagement.ChecklistDefinition) (engagement.WorkflowDefinition, engagement.ChecklistDefinition, error) {
	data, err := json.Marshal(struct {
		Workflow  engagement.WorkflowDefinition  `json:"workflow"`
		Checklist engagement.ChecklistDefinition `json:"checklist"`
	}{workflow, checklist})
	if err != nil {
		return workflow, checklist, err
	}
	var copy struct {
		Workflow  engagement.WorkflowDefinition  `json:"workflow"`
		Checklist engagement.ChecklistDefinition `json:"checklist"`
	}
	if err := json.Unmarshal(data, &copy); err != nil {
		return workflow, checklist, err
	}
	return copy.Workflow, copy.Checklist, nil
}

func definitionDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
