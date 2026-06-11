package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mauler/internal/settings"
)

// SkillsList returns a compact list of all available skills (name + description).
type SkillsList struct{}

func (t *SkillsList) Name() string      { return "skills_list" }
func (t *SkillsList) Destructive() bool { return false }

func (t *SkillsList) Description() string {
	return "List all available procedural-memory skills (name and description). " +
		"Use this at the start of a complex task to discover whether a relevant skill exists, " +
		"then call skill_view to read its full instructions."
}

func (t *SkillsList) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "filter": {"type": "string", "description": "Optional keyword to filter skills by name/description/tags."}
  },
  "additionalProperties": false
}`)
}

type skillsListParams struct {
	Filter string `json:"filter"`
}

func (t *SkillsList) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p skillsListParams
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	skills, err := listSkillFiles()
	if err != nil {
		return "", fmt.Errorf("skills_list: %w", err)
	}
	if len(skills) == 0 {
		return "No skills found. You can create skills by saving SKILL.md files to the skills directory.", nil
	}
	filter := strings.ToLower(strings.TrimSpace(p.Filter))
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Available skills (%d):\n", len(skills)))
	count := 0
	for _, s := range skills {
		if filter != "" {
			haystack := strings.ToLower(s.name + " " + s.description + " " + strings.Join(s.tags, " "))
			if !strings.Contains(haystack, filter) {
				continue
			}
		}
		count++
		fmt.Fprintf(&sb, "\n- %s: %s", s.name, s.description)
		if len(s.tags) > 0 {
			fmt.Fprintf(&sb, " [%s]", strings.Join(s.tags, ", "))
		}
	}
	if count == 0 {
		return fmt.Sprintf("No skills matched filter %q.", p.Filter), nil
	}
	sb.WriteString("\n\nUse skill_view with a skill name to read its full instructions.")
	return sb.String(), nil
}

// SkillView reads the full content of a named skill.
type SkillView struct{}

func (t *SkillView) Name() string      { return "skill_view" }
func (t *SkillView) Destructive() bool { return false }

func (t *SkillView) Description() string {
	return "Read the full procedural instructions of a skill by name. " +
		"Skills contain step-by-step workflows, checklists, and pitfalls for common task patterns. " +
		"For large external skills, pass a focused query to read only matching sections."
}

func (t *SkillView) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "Skill slug name as listed by skills_list."},
    "query": {"type": "string", "description": "Optional focused topic/section keyword. For large external skills this returns matching sections instead of the entire source."},
    "max_bytes": {"type": "integer", "description": "Optional output cap in bytes. Defaults to 12000 and is bounded between 2000 and 50000."}
  },
  "required": ["name"],
  "additionalProperties": false
}`)
}

type skillViewParams struct {
	Name     string `json:"name"`
	Query    string `json:"query"`
	MaxBytes int    `json:"max_bytes"`
}

func (t *SkillView) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p skillViewParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("skill_view: bad params: %w", err)
	}
	if strings.TrimSpace(p.Name) == "" {
		return "", fmt.Errorf("skill_view: name is required")
	}
	content, err := readSkillFile(p.Name, p.Query, p.MaxBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Skill %q not found. Use skills_list to see available skills.", p.Name), nil
		}
		return "", fmt.Errorf("skill_view: %w", err)
	}
	return fmt.Sprintf("# Skill: %s\n\n%s", p.Name, content), nil
}

// ---------- Low-level file helpers (skills package-private) ----------

type skillMeta struct {
	name        string
	description string
	tags        []string
	sourcePath  string
}

func toolsSkillsDir() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills"), nil
}

func listSkillFiles() ([]skillMeta, error) {
	dir, err := toolsSkillsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []skillMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		meta := parseSkillMeta(name, string(data))
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

func readSkillFile(name, query string, maxBytes int) (string, error) {
	dir, err := toolsSkillsDir()
	if err != nil {
		return "", err
	}
	slug := toolsSlugify(name)
	data, err := os.ReadFile(filepath.Join(dir, slug+".md"))
	if err != nil {
		return "", err
	}
	meta := parseSkillMeta(slug, string(data))
	if strings.TrimSpace(meta.sourcePath) != "" {
		if content, err := readExternalSkillSource(meta.sourcePath, query, maxBytes); err == nil {
			return content, nil
		}
	}
	content := string(data)
	if strings.TrimSpace(query) != "" {
		content = relevantMarkdownExcerpt(content, query)
	}
	return clampSkillOutput(content, maxBytes), nil
}

func parseSkillMeta(name, content string) skillMeta {
	m := skillMeta{name: name}
	if !strings.HasPrefix(content, "---") {
		return m
	}
	rest := content[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return m
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "description":
			m.description = val
		case "tags":
			val = strings.Trim(val, "[]")
			for _, t := range strings.Split(val, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					m.tags = append(m.tags, t)
				}
			}
		case "name":
			if val != "" {
				m.name = val
			}
		case "source_path":
			m.sourcePath = val
		}
	}
	return m
}

func readExternalSkillSource(path, query string, maxBytes int) (string, error) {
	abs, err := filepath.Abs(NormalizeHostPath(strings.TrimSpace(path)))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	var files []string
	if info.IsDir() {
		if err := filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != abs && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasPrefix(d.Name(), ".") && strings.EqualFold(filepath.Ext(d.Name()), ".md") {
				files = append(files, path)
			}
			return nil
		}); err != nil {
			return "", err
		}
		sort.Slice(files, func(i, j int) bool {
			return strings.ToLower(filepath.ToSlash(files[i])) < strings.ToLower(filepath.ToSlash(files[j]))
		})
	} else {
		files = []string{abs}
	}
	if len(files) == 0 {
		return "", fmt.Errorf("external skill source contains no markdown files")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return externalSkillOutline(abs, files, maxBytes)
	}
	var sb strings.Builder
	sb.WriteString("Master skill excerpts (focused query: " + query + "):\n")
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil || strings.TrimSpace(string(data)) == "" {
			continue
		}
		excerpt := relevantMarkdownExcerpt(string(data), query)
		if strings.TrimSpace(excerpt) == "" {
			continue
		}
		sb.WriteString("\n--- Source: " + externalSkillDisplayPath(abs, file) + " ---\n")
		sb.WriteString(strings.TrimSpace(excerpt))
		sb.WriteString("\n")
	}
	if strings.TrimSpace(sb.String()) == "Master skill excerpts (focused query: "+query+"):" {
		return externalSkillOutline(abs, files, maxBytes)
	}
	return clampSkillOutput(sb.String(), maxBytes), nil
}

func externalSkillOutline(root string, files []string, maxBytes int) (string, error) {
	var sb strings.Builder
	sb.WriteString("Large external skill source. This outline is returned by default to avoid filling context.\n")
	sb.WriteString("Call skill_view again with a focused query to read matching sections.\n")
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil || strings.TrimSpace(string(data)) == "" {
			continue
		}
		sb.WriteString("\n--- Source: " + externalSkillDisplayPath(root, file) + " ---\n")
		headings := markdownHeadings(string(data), 40)
		if len(headings) == 0 {
			sb.WriteString("(no markdown headings found)\n")
			continue
		}
		for _, heading := range headings {
			sb.WriteString(heading + "\n")
		}
	}
	if strings.TrimSpace(sb.String()) == "Large external skill source. This outline is returned by default to avoid filling context.\nCall skill_view again with a focused query to read matching sections." {
		return "", fmt.Errorf("external skill source contains no readable instructions")
	}
	return clampSkillOutput(sb.String(), maxBytes), nil
}

func externalSkillDisplayPath(root, path string) string {
	root = NormalizeHostPath(root)
	path = NormalizeHostPath(path)
	if rel, err := filepath.Rel(root, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}

func relevantMarkdownExcerpt(content, query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return content
	}
	sections := splitMarkdownSections(content)
	var matches []string
	for _, section := range sections {
		if strings.Contains(strings.ToLower(section), query) {
			matches = append(matches, strings.TrimSpace(section))
		}
	}
	if len(matches) > 0 {
		return strings.Join(matches, "\n\n")
	}
	lines := strings.Split(content, "\n")
	var windows []string
	for i, line := range lines {
		if !strings.Contains(strings.ToLower(line), query) {
			continue
		}
		start := i - 4
		if start < 0 {
			start = 0
		}
		end := i + 5
		if end > len(lines) {
			end = len(lines)
		}
		windows = append(windows, strings.Join(lines[start:end], "\n"))
	}
	return strings.Join(windows, "\n\n---\n\n")
}

func splitMarkdownSections(content string) []string {
	lines := strings.Split(content, "\n")
	var sections []string
	var current []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && len(current) > 0 {
			sections = append(sections, strings.Join(current, "\n"))
			current = nil
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		sections = append(sections, strings.Join(current, "\n"))
	}
	return sections
}

func markdownHeadings(content string, limit int) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, trimmed)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func clampSkillOutput(content string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 12000
	}
	if maxBytes < 2000 {
		maxBytes = 2000
	}
	if maxBytes > 50000 {
		maxBytes = 50000
	}
	if len(content) <= maxBytes {
		return content
	}
	return content[:maxBytes] + "\n\n[skill_view output truncated. Call again with a narrower query for more focused context.]"
}

func toolsSlugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prev := '-'
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prev = r
		} else if prev != '-' {
			b.WriteRune('-')
			prev = '-'
		}
	}
	return strings.Trim(b.String(), "-")
}
