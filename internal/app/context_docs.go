package app

import (
	"fmt"
	"os"
	"path/filepath"
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
		if doc, ok := readInstructionDoc(explicit, maxBytes); ok {
			return []projectInstructionDoc{doc}
		}
		return nil
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
			info, err := os.Stat(path)
			if err != nil || info.IsDir() || info.Size() == 0 {
				continue
			}
			doc, ok := readInstructionDoc(path, remaining)
			if !ok {
				continue
			}
			docs = append(docs, doc)
			remaining -= len(doc.Content)
			if remaining <= 0 {
				return docs
			}
			break
		}
	}
	return docs
}

func projectInstructionFilenames(fallback []string) []string {
	names := []string{"MAULER.override.md", "AGENTS.override.md"}
	if len(fallback) == 0 {
		fallback = settings.DefaultSettings().Context.ProjectDocFallbackFilenames
	}
	seen := map[string]bool{}
	for _, name := range names {
		seen[strings.ToLower(name)] = true
	}
	for _, name := range fallback {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		names = append(names, name)
		seen[key] = true
	}
	return names
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
