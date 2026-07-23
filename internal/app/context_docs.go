package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mauler/internal/settings"
)

type projectInstructionDoc struct {
	Path    string
	Content string
	Partial bool
}

type projectInstructionPacket struct {
	Policy            string                         `json:"policy"`
	RequestedClass    string                         `json:"requested_class"`
	EffectiveClass    string                         `json:"effective_class"`
	Prompt            string                         `json:"-"`
	Sources           []string                       `json:"sources,omitempty"`
	SourceBytes       int                            `json:"source_bytes"`
	PromptBytes       int                            `json:"prompt_bytes"`
	EstimatedTokens   int                            `json:"estimated_tokens"`
	PacketLimitBytes  int                            `json:"packet_limit_bytes"`
	PacketLimitTokens int                            `json:"packet_limit_tokens"`
	ManifestStatus    string                         `json:"manifest_status,omitempty"`
	ManifestPath      string                         `json:"manifest_path,omitempty"`
	ManifestSHA256    string                         `json:"manifest_sha256,omitempty"`
	RouteID           string                         `json:"route_id,omitempty"`
	FallbackReason    string                         `json:"fallback_reason,omitempty"`
	Provenance        []projectInstructionProvenance `json:"provenance,omitempty"`
}

// Project files may be arbitrarily large and remain fully available through
// read. Only a bounded compiled packet belongs in every model request.
const projectInstructionPromptMaxBytes = 16 * 1024

func buildProjectInstructionsPrompt(cfg settings.ContextConfig) string {
	return buildProjectInstructionPacket(cfg, "").Prompt
}

func buildProjectInstructionsPromptForTask(cfg settings.ContextConfig, taskText string) string {
	return buildProjectInstructionPacket(cfg, taskText).Prompt
}

func buildProjectInstructionPacket(cfg settings.ContextConfig, taskText string) projectInstructionPacket {
	return buildProjectInstructionPacketForClass(cfg, taskText, "auto")
}

func buildProjectInstructionPacketForClass(cfg settings.ContextConfig, taskText, requestedClass string) projectInstructionPacket {
	requestedClass = normalizeContextPacketClass(requestedClass)
	policy := projectInstructionPolicyForTask(taskText)
	if requestedClass == "relevant" || requestedClass == "expanded" || requestedClass == "core" {
		policy = "relevant_workspace"
	}
	var packet projectInstructionPacket
	if strings.TrimSpace(cfg.MAULERMDPath) == "" {
		if wd, err := os.Getwd(); err == nil {
			root := findProjectInstructionRoot(wd)
			selection := selectProjectContextManifest(root, policy, taskText)
			switch requestedClass {
			case "core":
				selection = selectProjectContextManifestPacket(root, "relevant_workspace", policy, taskText, false)
				selection = contextManifestCoreSelection(selection, "explicit-core")
			case "expanded":
				selection = selectProjectContextManifestPacket(root, "expanded_workspace", policy, taskText, false)
			case "relevant":
				selection = selectProjectContextManifestPacket(root, "relevant_workspace", policy, taskText, true)
			}
			switch selection.Status {
			case contextManifestStatusActive:
				builtPacket, buildErr := buildManifestProjectInstructionPacket(cfg, taskText, selection)
				if buildErr == nil {
					return finalizeProjectInstructionPacketClass(builtPacket, requestedClass)
				}
				selection.Status = contextManifestStatusInvalid
				selection.FallbackReason = "selected manifest document became unreadable: " + buildErr.Error()
				if fallback, ok := buildManifestFallbackCorePacket(cfg, taskText, selection); ok {
					return finalizeProjectInstructionPacketClass(fallback, requestedClass)
				}
			case contextManifestStatusInvalid:
				if fallback, ok := buildManifestFallbackCorePacket(cfg, taskText, selection); ok {
					return finalizeProjectInstructionPacketClass(fallback, requestedClass)
				}
			case contextManifestStatusMissing:
				if requestedClass == "core" {
					selection = contextManifestCoreSelection(selection, "explicit-core")
					if core, buildErr := buildManifestProjectInstructionPacket(cfg, taskText, selection); buildErr == nil {
						return finalizeProjectInstructionPacketClass(core, requestedClass)
					}
				}
			}
		}
	}
	packet = buildLegacyProjectInstructionPacket(cfg, taskText, policy)
	return finalizeProjectInstructionPacketClass(packet, requestedClass)
}

func normalizeContextPacketClass(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "core", "relevant", "expanded":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "auto"
	}
}

func finalizeProjectInstructionPacketClass(packet projectInstructionPacket, requestedClass string) projectInstructionPacket {
	packet.RequestedClass = normalizeContextPacketClass(requestedClass)
	switch {
	case packet.RouteID == "fallback-compact-core" || packet.RouteID == "explicit-core":
		packet.EffectiveClass = "core"
	case packet.RequestedClass == "expanded" && packet.ManifestStatus == contextManifestStatusActive:
		packet.EffectiveClass = "expanded"
	case packet.Policy == "minimal_external_research":
		packet.EffectiveClass = "minimal"
	default:
		packet.EffectiveClass = "relevant"
	}
	return packet
}

func buildLegacyProjectInstructionPacket(cfg settings.ContextConfig, taskText, policy string) projectInstructionPacket {
	sourceDocs := discoverProjectInstructionDocs(cfg)
	packet := projectInstructionPacket{Policy: policy, ManifestStatus: contextManifestStatusMissing}
	packet.PacketLimitBytes = projectInstructionPromptMaxBytes
	packet.PacketLimitTokens = projectInstructionPromptMaxBytes / 4
	if strings.TrimSpace(cfg.MAULERMDPath) != "" {
		packet.ManifestStatus = contextManifestStatusBypassed
		packet.FallbackReason = "explicit project instruction source configured"
	} else {
		packet.FallbackReason = "no active context manifest; used bounded project instruction discovery"
	}
	for _, doc := range sourceDocs {
		packet.SourceBytes += len(doc.Content)
		packet.Sources = append(packet.Sources, filepath.ToSlash(doc.Path))
	}
	if len(sourceDocs) == 0 {
		return packet
	}
	if packet.Policy == "minimal_external_research" {
		packet.PacketLimitBytes = 500 * 4
		packet.PacketLimitTokens = 500
		var sb strings.Builder
		sb.WriteString("\n\nProject context policy: minimal external research.\n")
		sb.WriteString("This task requests current public information without project inspection or mutation, so repository handoff contents were not copied into the prompt. Code-owned safety, scope, tool, and evidence rules still apply.\n")
		sb.WriteString("Project instruction sources remain available for targeted reading if the user connects the research to workspace work:\n")
		for _, source := range packet.Sources {
			sb.WriteString("- " + source + "\n")
		}
		packet.Prompt = sb.String()
		packet.PromptBytes = len(packet.Prompt)
		packet.EstimatedTokens = estimateContextTokens(packet.PromptBytes)
		return packet
	}

	docs := compileProjectInstructionDocs(sourceDocs, projectInstructionPromptMaxBytes)
	if len(docs) == 0 {
		return packet
	}
	var sb strings.Builder
	sb.WriteString("\n\nProject instructions (layered, later files override earlier files):\n")
	sb.WriteString("Large instruction files are represented by bounded excerpts and a heading index. Use read with a targeted line range only when omitted detail is relevant; do not reread an entire large instruction file.\n")
	if strings.TrimSpace(cfg.MAULERMDPath) != "" {
		sb.WriteString("The configured project instruction source is already loaded below. Do not search the active workspace for master_skill.md, master_skills.md, MAULER.md, or AGENTS.md unless the user explicitly asks for another instruction file.\n")
		sb.WriteString("If this is a Navigator/framework source, read at most 1-3 targeted follow-up files or line ranges needed for routing, then move to the user's operational task. Do not reread the same framework file or crawl methodology docs after the next action is clear.\n")
	}
	for _, doc := range docs {
		sb.WriteString("\n--- Source: " + filepath.ToSlash(doc.Path))
		if doc.Partial {
			sb.WriteString(" (truncated)")
		}
		sb.WriteString(" ---\n")
		sb.WriteString(strings.TrimSpace(doc.Content))
		sb.WriteString("\n")
	}
	packet.Prompt = sb.String()
	packet.PromptBytes = len(packet.Prompt)
	packet.EstimatedTokens = estimateContextTokens(packet.PromptBytes)
	return packet
}

func projectInstructionPolicyForTask(taskText string) string {
	lower := strings.ToLower(strings.TrimSpace(taskText))
	if lower == "" {
		return "relevant_workspace"
	}
	externalResearch := looksPublicExploitLookup(lower) || explicitWebResearchIntent(lower) || looksResearchTask(lower)
	if externalResearch && !taskNeedsWorkspaceInstructions(lower) {
		return "minimal_external_research"
	}
	return "relevant_workspace"
}

func taskNeedsWorkspaceInstructions(lower string) bool {
	return hasAny(lower,
		"this repo", "the repo", "repository", "codebase", "code base", "workspace",
		"this project", "project files", "local file", "local files", "folder", "directory",
		"readme", "documentation file", "writeup", "notes file", "report file",
		"implement", "fix", "patch", "refactor", "edit", "update", "write", "create",
		"save", "download", "clone", "run this", "test this", "build", "compile",
		"frontend", "backend", "component", "mauler", "helixclaw",
		"bug bounty", "bug-bounty", "post-recon", "post recon", "manual assessment",
		"burp request", "burp response", "web application pentest", "web app pentest",
	)
}

func compileProjectInstructionDocs(docs []projectInstructionDoc, maxBytes int) []projectInstructionDoc {
	if len(docs) == 0 || maxBytes <= 0 {
		return nil
	}
	remaining := maxBytes
	out := make([]projectInstructionDoc, 0, len(docs))
	for i, doc := range docs {
		if remaining <= 0 {
			break
		}
		share := remaining / (len(docs) - i)
		if share <= 0 {
			break
		}
		compiled := doc
		if len(compiled.Content) > share {
			compiled.Content = compileInstructionExcerpt(compiled.Content, share)
			compiled.Partial = true
		}
		if len(compiled.Content) > remaining {
			compiled.Content = compileInstructionExcerpt(compiled.Content, remaining)
			compiled.Partial = true
		}
		if strings.TrimSpace(compiled.Content) == "" {
			continue
		}
		out = append(out, compiled)
		remaining -= len(compiled.Content)
	}
	return out
}

func compileInstructionExcerpt(content string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(content) <= maxBytes {
		return content
	}
	if maxBytes < 512 {
		return content[:maxBytes]
	}

	marker := "\n\n[... middle omitted from the always-on prompt; use targeted read if needed ...]\n\n"
	outlineBudget := maxBytes / 4
	outline := instructionHeadingOutline(content, outlineBudget)
	if outline != "" {
		outline = "Heading index:\n" + outline + "\n\n"
	}
	tailBudget := maxBytes / 5
	headBudget := maxBytes - len(marker) - len(outline) - tailBudget
	if headBudget < maxBytes/3 {
		headBudget = maxBytes / 2
		tailBudget = maxBytes - len(marker) - headBudget
		outline = ""
	}
	if tailBudget < 0 {
		tailBudget = 0
	}
	if headBudget < 0 {
		headBudget = 0
	}

	excerpt := content[:headBudget] + marker + outline
	if tailBudget > 0 {
		excerpt += content[len(content)-tailBudget:]
	}
	if len(excerpt) > maxBytes {
		excerpt = excerpt[:maxBytes]
	}
	return excerpt
}

func instructionHeadingOutline(content string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	var sb strings.Builder
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		candidate := line + "\n"
		if sb.Len()+len(candidate) > maxBytes {
			break
		}
		sb.WriteString(candidate)
	}
	return strings.TrimSpace(sb.String())
}

func discoverProjectInstructionDocs(cfg settings.ContextConfig) []projectInstructionDoc {
	maxBytes := cfg.ProjectDocMaxBytes
	if maxBytes <= 0 {
		maxBytes = settings.DefaultSettings().Context.ProjectDocMaxBytes
	}
	if explicit := strings.TrimSpace(cfg.MAULERMDPath); explicit != "" {
		return readInstructionSource(explicit, maxBytes)
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil
	}
	root := findProjectInstructionRoot(wd)
	dirs := dirsFromRoot(root, wd)
	names := projectInstructionFilenames(cfg.ProjectDocFallbackFilenames)
	remaining := maxBytes
	var docs []projectInstructionDoc
	for _, dir := range dirs {
		for _, name := range names {
			path := filepath.Join(dir, name)
			sourceDocs := readInstructionSource(path, remaining)
			if len(sourceDocs) == 0 {
				continue
			}
			for _, doc := range sourceDocs {
				docs = append(docs, doc)
				remaining -= len(doc.Content)
				if remaining <= 0 {
					return docs
				}
			}
		}
	}
	return docs
}

func projectInstructionFilenames(fallback []string) []string {
	if len(fallback) == 0 {
		fallback = settings.DefaultSettings().Context.ProjectDocFallbackFilenames
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(fallback)+2)
	for _, name := range fallback {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if isMasterSkillSourceName(name) {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		names = append(names, name)
		seen[key] = true
	}
	for _, name := range []string{"MAULER.override.md", "AGENTS.override.md"} {
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		names = append(names, name)
		seen[key] = true
	}
	return names
}

func readInstructionSource(path string, maxBytes int) []projectInstructionDoc {
	if maxBytes <= 0 {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		return readInstructionDir(abs, maxBytes)
	}
	if info.Size() == 0 {
		return nil
	}
	if doc, ok := readInstructionDoc(abs, maxBytes); ok {
		return []projectInstructionDoc{doc}
	}
	return nil
}

func readInstructionDir(dir string, maxBytes int) []projectInstructionDoc {
	remaining := maxBytes
	var docs []projectInstructionDoc
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != dir && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	sort.SliceStable(files, func(i, j int) bool {
		left := instructionFilePriority(dir, files[i])
		right := instructionFilePriority(dir, files[j])
		if left != right {
			return left < right
		}
		return strings.ToLower(filepath.ToSlash(files[i])) < strings.ToLower(filepath.ToSlash(files[j]))
	})
	for _, path := range files {
		if remaining <= 0 {
			break
		}
		doc, ok := readInstructionDoc(path, remaining)
		if !ok {
			continue
		}
		docs = append(docs, doc)
		remaining -= len(doc.Content)
	}
	return docs
}

func instructionFilePriority(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = strings.ToLower(filepath.ToSlash(rel))
	base := strings.ToLower(filepath.Base(rel))
	switch base {
	case "master_skill.md":
		return 0
	case "master_skills.md":
		return 1
	case "skill.md":
		return 2
	case "mauler.md", "agents.md":
		return 3
	case "map_index.md":
		return 4
	case "htb_methodology.md":
		return 5
	case "engagement.md":
		return 6
	case "red_team_mode.md":
		return 7
	case "mode_registry.md":
		return 8
	}
	if strings.Contains(rel, "/maps/") {
		return 40
	}
	if strings.Contains(rel, "/skills/") || strings.Contains(rel, "/modes/") {
		return 50
	}
	return 100
}

func readInstructionDoc(path string, maxBytes int) (projectInstructionDoc, bool) {
	if maxBytes <= 0 {
		return projectInstructionDoc{}, false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	data, err := os.ReadFile(abs)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return projectInstructionDoc{}, false
	}
	partial := false
	if len(data) > maxBytes {
		data = data[:maxBytes]
		partial = true
	}
	return projectInstructionDoc{
		Path:    abs,
		Content: string(data),
		Partial: partial,
	}, true
}

func findProjectInstructionRoot(wd string) string {
	abs, err := filepath.Abs(wd)
	if err != nil {
		return wd
	}
	for {
		for _, marker := range []string{".git", "wails.json", "go.mod", "MAULER.md", "AGENTS.md"} {
			if _, err := os.Stat(filepath.Join(abs, marker)); err == nil {
				return abs
			}
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return wd
		}
		abs = parent
	}
}

func dirsFromRoot(root, wd string) []string {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		rootAbs = root
	}
	wdAbs, err := filepath.Abs(wd)
	if err != nil {
		wdAbs = wd
	}
	rel, err := filepath.Rel(rootAbs, wdAbs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return []string{wdAbs}
	}
	dirs := []string{rootAbs}
	if rel == "." {
		return dirs
	}
	cur := rootAbs
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		dirs = append(dirs, cur)
	}
	return dirs
}

func instructionDocsSummary(cfg settings.ContextConfig) string {
	docs := discoverProjectInstructionDocs(cfg)
	if len(docs) == 0 {
		return "no project instruction files loaded"
	}
	parts := make([]string, 0, len(docs))
	for _, doc := range docs {
		suffix := ""
		if doc.Partial {
			suffix = " truncated"
		}
		parts = append(parts, fmt.Sprintf("%s (%d chars%s)", filepath.ToSlash(doc.Path), len(doc.Content), suffix))
	}
	return strings.Join(parts, "\n")
}

func isMasterSkillSourceName(name string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	switch base {
	case "master_skill.md", "master_skills.md", "master_skill", "master_skills":
		return true
	default:
		return false
	}
}
