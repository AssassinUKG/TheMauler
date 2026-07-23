package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mauler/internal/engagement"
)

const (
	maxEngagementSetupArtifacts = 80
	maxEngagementSetupDepth     = 5
)

type EngagementSetupWorkflow struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Description      string `json:"description"`
	PhaseCount       int    `json:"phase_count"`
	ChecklistName    string `json:"checklist_name"`
	ChecklistVersion string `json:"checklist_version"`
	CheckCount       int    `json:"check_count"`
}

type EngagementSetupArtifact struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

type EngagementSetupCheck struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Status   string `json:"status"` // ready | info | warning | blocked | unchecked
	Detail   string `json:"detail"`
	Blocking bool   `json:"blocking"`
}

type EngagementSetupPreview struct {
	ProjectName        string                    `json:"project_name"`
	ProjectID          string                    `json:"project_id"`
	Workspace          string                    `json:"workspace"`
	Target             string                    `json:"target"`
	Hostname           string                    `json:"hostname"`
	Scope              []string                  `json:"scope"`
	ScopeLocked        bool                      `json:"scope_locked"`
	ActiveProfile      string                    `json:"active_profile"`
	ModelID            string                    `json:"model_id"`
	Provider           string                    `json:"provider"`
	ProviderURL        string                    `json:"provider_url"`
	Shell              string                    `json:"shell"`
	VPN                string                    `json:"vpn"`
	Workflows          []EngagementSetupWorkflow `json:"workflows"`
	CandidateArtifacts []EngagementSetupArtifact `json:"candidate_artifacts"`
	Checks             []EngagementSetupCheck    `json:"checks"`
	CanCreate          bool                      `json:"can_create"`
	CanStart           bool                      `json:"can_start"`
}

type EngagementTargetProbe struct {
	Target     string   `json:"target"`
	Status     string   `json:"status"` // ready | warning | blocked
	Detail     string   `json:"detail"`
	Attempted  []string `json:"attempted,omitempty"`
	LatencyMS  int64    `json:"latency_ms,omitempty"`
	HTTPStatus int      `json:"http_status,omitempty"`
	ScopeMatch string   `json:"scope_match,omitempty"`
	CheckedAt  string   `json:"checked_at"`
}

// GetEngagementSetupPreview returns the operator-owned inputs used by the
// setup wizard. It never creates or mutates engagement state.
func (a *App) GetEngagementSetupPreview() EngagementSetupPreview {
	cfg := a.GetSettings()
	profiles := a.GetProfiles()
	lab := cfg.Context.Lab
	workspace := strings.TrimSpace(cfg.Context.WorkspaceDir)
	if workspace == "" {
		workspace = workspaceScope()
	}
	projectName := strings.TrimSpace(lab.Name)
	if projectName == "" && workspace != "" {
		projectName = filepath.Base(filepath.Clean(workspace))
	}
	if projectName == "" {
		projectName = "Engagement"
	}

	preview := EngagementSetupPreview{
		ProjectName:   projectName,
		ProjectID:     strings.TrimSpace(lab.ID),
		Workspace:     filepath.ToSlash(workspace),
		Target:        strings.TrimSpace(lab.Target),
		Hostname:      strings.TrimSpace(lab.Hostname),
		ScopeLocked:   true,
		ActiveProfile: strings.TrimSpace(cfg.ActiveProfile),
		VPN:           strings.TrimSpace(lab.VPNInterface),
	}

	profile, profileOK := profiles.Profiles[preview.ActiveProfile]
	if profileOK {
		preview.ModelID = strings.TrimSpace(profile.ModelID)
		preview.Provider = strings.TrimSpace(profile.Provider)
		if provider, ok := profiles.Providers[profile.Provider]; ok {
			preview.ProviderURL = strings.TrimSpace(provider.BaseURL)
		}
	}
	preview.Shell = engagementSetupShell(cfg.Environment.AIShellBackend, cfg.Environment.AIShellDistro, cfg.Environment.AIShellUser)
	preview.CandidateArtifacts = discoverEngagementSetupArtifacts(workspace)

	if scope, err := lockedAuthoritativeScope(lab, nil); err == nil {
		preview.Scope = scope
	}
	preview.Workflows = a.engagementSetupWorkflows()
	preview.Checks = engagementSetupChecks(preview, profileOK)
	preview.CanCreate = setupChecksAllow(preview.Checks, "workspace", "target")
	preview.CanStart = preview.CanCreate && setupChecksAllow(preview.Checks, "model", "provider")
	return preview
}

// CheckEngagementTarget performs a small, read-only desktop-side readiness
// probe. Failure is a warning rather than a creation blocker because .htb and
// lab hosts may deliberately resolve only inside the configured WSL/VPN path.
func (a *App) CheckEngagementTarget() EngagementTargetProbe {
	started := time.Now()
	checkedAt := started.UTC().Format(time.RFC3339)
	lab := a.engagementLabContext()
	scope, err := lockedAuthoritativeScope(lab, nil)
	if err != nil {
		return EngagementTargetProbe{Status: "blocked", Detail: err.Error(), CheckedAt: checkedAt}
	}
	target := strings.TrimSpace(lab.Target)
	if target == "" {
		target = strings.TrimSpace(lab.Hostname)
	}
	decision := engagement.CheckTargetScope(scope, target)
	if !decision.Allowed {
		return EngagementTargetProbe{Target: target, Status: "blocked", Detail: decision.Reason, CheckedAt: checkedAt}
	}

	if parsed, parseErr := url.Parse(target); parseErr == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" {
		return probeEngagementHTTP(scope, target, decision.Matched, started)
	}
	return probeEngagementTCP(target, decision.Matched, started)
}

func (a *App) engagementSetupWorkflows() []EngagementSetupWorkflow {
	catalog, err := a.engagementCatalog()
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(catalog.Workflows))
	for id := range catalog.Workflows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]EngagementSetupWorkflow, 0, len(ids))
	for _, id := range ids {
		workflow := catalog.Workflows[id]
		checklist := catalog.Checklists[workflow.Checklist]
		out = append(out, EngagementSetupWorkflow{
			ID: id, Name: workflow.Name, Version: workflow.Version, Description: workflow.Description,
			PhaseCount: len(workflow.Phases), ChecklistName: checklist.Name,
			ChecklistVersion: checklist.Version, CheckCount: len(checklist.Items),
		})
	}
	return out
}

func engagementSetupChecks(preview EngagementSetupPreview, profileOK bool) []EngagementSetupCheck {
	checks := make([]EngagementSetupCheck, 0, 7)
	workspaceStatus, workspaceDetail := "ready", preview.Workspace
	if preview.Workspace == "" {
		workspaceStatus, workspaceDetail = "blocked", "Open a project workspace before creating a Grid."
	} else if info, err := os.Stat(filepath.Clean(preview.Workspace)); err != nil || !info.IsDir() {
		workspaceStatus, workspaceDetail = "blocked", "The active workspace folder is unavailable."
	}
	checks = append(checks, EngagementSetupCheck{ID: "workspace", Label: "Project workspace", Status: workspaceStatus, Detail: workspaceDetail, Blocking: workspaceStatus == "blocked"})

	targetStatus, targetDetail := "ready", strings.Join(preview.Scope, " · ")
	if len(preview.Scope) == 0 {
		targetStatus, targetDetail = "blocked", "Set the project target IP/URL or hostname first."
	}
	checks = append(checks, EngagementSetupCheck{ID: "target", Label: "Authorised target", Status: targetStatus, Detail: targetDetail, Blocking: targetStatus == "blocked"})

	modelStatus, modelDetail := "ready", preview.ActiveProfile+" · "+preview.ModelID
	if !profileOK || preview.ModelID == "" {
		modelStatus, modelDetail = "blocked", "Select a valid model profile before starting the first run."
	}
	checks = append(checks, EngagementSetupCheck{ID: "model", Label: "Agent profile", Status: modelStatus, Detail: modelDetail, Blocking: modelStatus == "blocked"})

	providerStatus, providerDetail := "ready", preview.Provider+" · "+preview.ProviderURL
	if preview.Provider == "" || preview.ProviderURL == "" {
		providerStatus, providerDetail = "blocked", "The active profile has no configured provider endpoint."
	}
	checks = append(checks, EngagementSetupCheck{ID: "provider", Label: "Inference provider", Status: providerStatus, Detail: providerDetail, Blocking: providerStatus == "blocked"})

	checks = append(checks, EngagementSetupCheck{ID: "shell", Label: "Agent shell", Status: "ready", Detail: firstNonEmpty(preview.Shell, "automatic")})
	vpnStatus, vpnDetail := "ready", preview.VPN
	if preview.VPN == "" {
		vpnStatus, vpnDetail = "info", "No VPN selected; this is fine for public or local targets."
	}
	checks = append(checks, EngagementSetupCheck{ID: "vpn", Label: "VPN / interface", Status: vpnStatus, Detail: vpnDetail})
	checks = append(checks, EngagementSetupCheck{ID: "reachability", Label: "Desktop reachability", Status: "unchecked", Detail: "Run the optional check before creating the Grid. A warning does not block WSL/VPN testing."})
	return checks
}

func setupChecksAllow(checks []EngagementSetupCheck, required ...string) bool {
	wanted := map[string]bool{}
	for _, id := range required {
		wanted[id] = true
	}
	for _, check := range checks {
		if wanted[check.ID] && check.Blocking {
			return false
		}
	}
	return true
}

func engagementSetupShell(backend, distro, user string) string {
	backend = firstNonEmpty(strings.TrimSpace(backend), "auto")
	parts := []string{backend}
	if strings.TrimSpace(distro) != "" {
		parts = append(parts, strings.TrimSpace(distro))
	}
	if strings.TrimSpace(user) != "" {
		parts = append(parts, strings.TrimSpace(user))
	}
	return strings.Join(parts, " / ")
}

func discoverEngagementSetupArtifacts(root string) []EngagementSetupArtifact {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	artifacts := make([]EngagementSetupArtifact, 0, 24)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}
		segments := strings.Split(filepath.ToSlash(rel), "/")
		if entry.IsDir() {
			name := strings.ToLower(entry.Name())
			if len(segments) > maxEngagementSetupDepth || name == ".git" || name == "node_modules" || name == "vendor" || name == ".mauler" {
				return filepath.SkipDir
			}
			return nil
		}
		kind := engagementSetupArtifactKind(rel)
		if kind == "" {
			return nil
		}
		fileInfo, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		artifacts = append(artifacts, EngagementSetupArtifact{
			Path: filepath.ToSlash(rel), Name: entry.Name(), Kind: kind, Size: fileInfo.Size(),
			ModifiedAt: fileInfo.ModTime().UTC().Format(time.RFC3339),
		})
		if len(artifacts) >= maxEngagementSetupArtifacts {
			return filepath.SkipAll
		}
		return nil
	})
	sort.SliceStable(artifacts, func(i, j int) bool {
		if artifacts[i].ModifiedAt == artifacts[j].ModifiedAt {
			return artifacts[i].Path < artifacts[j].Path
		}
		return artifacts[i].ModifiedAt > artifacts[j].ModifiedAt
	})
	return artifacts
}

func engagementSetupArtifactKind(rel string) string {
	lower := strings.ToLower(filepath.ToSlash(rel))
	name := strings.ToLower(filepath.Base(rel))
	ext := strings.ToLower(filepath.Ext(name))
	inDir := func(dir string) bool { return strings.Contains("/"+lower, "/"+dir+"/") }
	switch {
	case inDir("screenshots") || ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp":
		return "screenshot"
	case inDir("scans") || ext == ".nmap" || ext == ".gnmap" || ext == ".nessus" || ext == ".har" || ext == ".http":
		return "scan"
	case inDir("reports") || strings.Contains(name, "report") || ext == ".pdf" || ext == ".html":
		return "report"
	case inDir("notes") || strings.Contains(name, "note") || ext == ".md":
		return "note"
	case inDir("loot") || inDir(".mauler_artifacts") || ext == ".xml" || ext == ".json" || ext == ".txt":
		return "artifact"
	default:
		return ""
	}
}

func probeEngagementHTTP(scope []string, target, matched string, started time.Time) EngagementTargetProbe {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return engagementProbeWarning(target, matched, []string{target}, started, err)
	}
	request.Header.Set("User-Agent", "TheMauler-Engagement-Setup/1.0")
	request.Header.Set("Range", "bytes=0-1023")
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // authorised lab targets commonly use self-signed TLS
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			decision := engagement.CheckTargetScope(scope, req.URL.String())
			if !decision.Allowed {
				return fmt.Errorf("redirect blocked by locked scope: %s", decision.Reason)
			}
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return engagementProbeWarning(target, matched, []string{target}, started, err)
	}
	defer response.Body.Close()
	_, _ = io.CopyN(io.Discard, response.Body, 1024)
	return EngagementTargetProbe{
		Target: target, Status: "ready", Detail: fmt.Sprintf("HTTP %d from the authorised target", response.StatusCode),
		Attempted: []string{target}, LatencyMS: time.Since(started).Milliseconds(), HTTPStatus: response.StatusCode,
		ScopeMatch: matched, CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func probeEngagementTCP(target, matched string, started time.Time) EngagementTargetProbe {
	host := scopeAuthorityKey(target)
	if host == "" {
		return engagementProbeWarning(target, matched, nil, started, fmt.Errorf("target host could not be parsed"))
	}
	ports := []string{"443", "80"}
	if parsed, err := url.Parse("//" + target); err == nil && parsed.Port() != "" {
		ports = []string{parsed.Port()}
	}
	attempted := make([]string, 0, len(ports))
	var lastErr error
	for _, port := range ports {
		address := net.JoinHostPort(host, port)
		attempted = append(attempted, address)
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		cancel()
		if err == nil {
			_ = conn.Close()
			return EngagementTargetProbe{
				Target: target, Status: "ready", Detail: "TCP connection succeeded on " + address,
				Attempted: attempted, LatencyMS: time.Since(started).Milliseconds(), ScopeMatch: matched,
				CheckedAt: time.Now().UTC().Format(time.RFC3339),
			}
		}
		lastErr = err
	}
	return engagementProbeWarning(target, matched, attempted, started, lastErr)
}

func engagementProbeWarning(target, matched string, attempted []string, started time.Time, err error) EngagementTargetProbe {
	detail := "Desktop-side reachability was not confirmed. WSL/VPN-only lab routes may still work during the first agent claim."
	if err != nil {
		detail += " " + err.Error()
	}
	return EngagementTargetProbe{
		Target: target, Status: "warning", Detail: detail, Attempted: attempted,
		LatencyMS: time.Since(started).Milliseconds(), ScopeMatch: matched,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
}
