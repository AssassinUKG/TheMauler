package checkpacks

import (
	"fmt"
	"sort"
	"strings"

	"mauler/internal/engagement"
)

const (
	SourceKindStandard  = "standard"
	SourceKindDetector  = "detector"
	SourceKindAdvisory  = "advisory"
	SourceKindWorkflow  = "workflow"
	MinimumPublishScore = 80
)

// SourceSpec is an operator-reviewed allow-list entry. The future updater may
// download only repositories and paths represented here; it must never execute
// files from an upstream repository.
type SourceSpec struct {
	ID           string   `json:"id"`
	Repository   string   `json:"repository"`
	Kind         string   `json:"kind"`
	Trust        string   `json:"trust"`
	License      string   `json:"license"`
	AllowedPaths []string `json:"allowed_paths,omitempty"`
}

type Registry struct {
	sources map[string]SourceSpec
}

func DefaultRegistry() (*Registry, error) {
	registry := NewRegistry()
	for _, source := range []SourceSpec{
		{ID: "shiftgrid", Repository: "https://github.com/BuFuuu/shiftgrid", Kind: SourceKindWorkflow, Trust: engagement.PackTrustCurated, License: "MIT", AllowedPaths: []string{"Workflows/", "Checklists/"}},
		{ID: "owasp-wstg", Repository: "https://github.com/OWASP/wstg", Kind: SourceKindStandard, Trust: engagement.PackTrustOfficial, License: "CC-BY-SA-4.0", AllowedPaths: []string{"document/"}},
		{ID: "owasp-asvs", Repository: "https://github.com/OWASP/ASVS", Kind: SourceKindStandard, Trust: engagement.PackTrustOfficial, License: "CC-BY-SA-4.0", AllowedPaths: []string{"5.0/", "en/"}},
		{ID: "owasp-api-security", Repository: "https://github.com/OWASP/API-Security", Kind: SourceKindStandard, Trust: engagement.PackTrustOfficial, License: "CC-BY-SA-4.0", AllowedPaths: []string{"editions/"}},
		{ID: "owasp-top10", Repository: "https://github.com/OWASP/Top10", Kind: SourceKindStandard, Trust: engagement.PackTrustOfficial, License: "CC-BY-SA-4.0"},
		{ID: "nuclei-templates", Repository: "https://github.com/projectdiscovery/nuclei-templates", Kind: SourceKindDetector, Trust: engagement.PackTrustCurated, License: "MIT", AllowedPaths: []string{"http/", "network/", "ssl/", "workflows/"}},
		{ID: "payloads-all-the-things", Repository: "https://github.com/swisskyrepo/PayloadsAllTheThings", Kind: SourceKindAdvisory, Trust: engagement.PackTrustCommunity, License: "MIT"},
	} {
		if err := registry.Add(source); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func NewRegistry() *Registry {
	return &Registry{sources: map[string]SourceSpec{}}
}

func (r *Registry) Add(source SourceSpec) error {
	if r == nil {
		return fmt.Errorf("check-pack registry is nil")
	}
	source.ID = strings.TrimSpace(source.ID)
	source.Repository = strings.TrimRight(strings.TrimSpace(source.Repository), "/")
	source.Kind = strings.ToLower(strings.TrimSpace(source.Kind))
	source.Trust = strings.ToLower(strings.TrimSpace(source.Trust))
	source.License = strings.TrimSpace(source.License)
	if source.ID == "" || source.Repository == "" || source.License == "" {
		return fmt.Errorf("source id, repository, and license are required")
	}
	if !strings.HasPrefix(strings.ToLower(source.Repository), "https://github.com/") {
		return fmt.Errorf("source %q repository must be an https GitHub URL", source.ID)
	}
	switch source.Kind {
	case SourceKindStandard, SourceKindDetector, SourceKindAdvisory, SourceKindWorkflow:
	default:
		return fmt.Errorf("source %q has unsupported kind %q", source.ID, source.Kind)
	}
	switch source.Trust {
	case engagement.PackTrustOfficial, engagement.PackTrustCurated, engagement.PackTrustCommunity:
	default:
		return fmt.Errorf("source %q has unsupported trust %q", source.ID, source.Trust)
	}
	if _, exists := r.sources[source.ID]; exists {
		return fmt.Errorf("duplicate check-pack source %q", source.ID)
	}
	for index, path := range source.AllowedPaths {
		path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
		if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			return fmt.Errorf("source %q has unsafe allowed path %q", source.ID, source.AllowedPaths[index])
		}
		source.AllowedPaths[index] = path
	}
	r.sources[source.ID] = source
	return nil
}

func (r *Registry) Get(id string) (SourceSpec, bool) {
	if r == nil {
		return SourceSpec{}, false
	}
	source, ok := r.sources[strings.TrimSpace(id)]
	return source, ok
}

func (r *Registry) List() []SourceSpec {
	if r == nil {
		return nil
	}
	out := make([]SourceSpec, 0, len(r.sources))
	for _, source := range r.sources {
		copy := source
		copy.AllowedPaths = append([]string(nil), source.AllowedPaths...)
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) ValidateProvenance(source engagement.DefinitionSource) error {
	approved, ok := r.Get(source.ID)
	if !ok {
		return fmt.Errorf("source %q is not in the check-pack allow-list", source.ID)
	}
	repository := strings.TrimRight(strings.TrimSpace(source.Repository), "/")
	if !strings.EqualFold(repository, approved.Repository) {
		return fmt.Errorf("source %q repository %q does not match approved %q", source.ID, repository, approved.Repository)
	}
	if !strings.EqualFold(strings.TrimSpace(source.License), approved.License) {
		return fmt.Errorf("source %q license %q does not match approved %q", source.ID, source.License, approved.License)
	}
	if !strings.EqualFold(strings.TrimSpace(source.Trust), approved.Trust) {
		return fmt.Errorf("source %q trust %q does not match approved %q", source.ID, source.Trust, approved.Trust)
	}
	if strings.TrimSpace(source.Ref) == "" {
		return fmt.Errorf("source %q must be pinned to a release, tag, or commit", source.ID)
	}
	if source.Path != "" && !approvedPath(source.Path, approved.AllowedPaths) {
		return fmt.Errorf("source %q path %q is outside the approved paths", source.ID, source.Path)
	}
	return nil
}

func approvedPath(path string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	path = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
	if path == "" || strings.Contains(path, "..") {
		return false
	}
	for _, prefix := range allowed {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
