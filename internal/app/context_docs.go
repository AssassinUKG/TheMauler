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

func buildProjectInstructionsPrompt(cfg settings.ContextConfig) string {
	docs := discoverProjectInstructionDocs(cfg)
	if len(docs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nProject instructions (layered, later files override earlier files):\n")
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
	return sb.String()
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
