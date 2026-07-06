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
		if req := formatSkillRequirements(s); req != "" {
			fmt.Fprintf(&sb, " Requirements: %s", req)
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
	name          string
	description   string
	tags          []string
	sourcePath    string
	requiredTools []string
	shellBackend  string
	needsNetwork  bool
	needsWrite    bool
}

type externalSkillMatch struct {
	file    string
	excerpt string
	score   float64
}

type markdownSectionMatch struct {
	text  string
	score float64
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
		case "required_tools":
			m.requiredTools = parseToolsSkillList(val)
		case "shell_backend":
			m.shellBackend = strings.TrimSpace(val)
		case "needs_network":
			m.needsNetwork = parseToolsSkillBool(val)
		case "needs_write":
			m.needsWrite = parseToolsSkillBool(val)
		}
	}
	return m
}

func formatSkillRequirements(meta skillMeta) string {
	var parts []string
	if len(meta.requiredTools) > 0 {
		parts = append(parts, "tools="+strings.Join(meta.requiredTools, ","))
	}
	if strings.TrimSpace(meta.shellBackend) != "" {
		parts = append(parts, "shell="+strings.TrimSpace(meta.shellBackend))
	}
	if meta.needsNetwork {
		parts = append(parts, "network")
	}
	if meta.needsWrite {
		parts = append(parts, "write")
	}
	return strings.Join(parts, "; ")
}

func parseToolsSkillList(val string) []string {
	val = strings.Trim(strings.TrimSpace(val), "[]")
	var out []string
	seen := map[string]bool{}
	for _, item := range strings.Split(val, ",") {
		item = strings.Trim(strings.ToLower(strings.TrimSpace(item)), `"'`)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func parseToolsSkillBool(val string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(val), `"'`)) {
	case "true", "yes", "1", "on":
		return true
	default:
		return false
	}
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
	terms := skillQueryTerms(query)
	var matches []externalSkillMatch
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil || strings.TrimSpace(string(data)) == "" {
			continue
		}
		for _, section := range relevantMarkdownMatches(string(data), query, 3) {
			matches = append(matches, externalSkillMatch{
				file:    file,
				excerpt: section.text,
				score:   section.score + externalSkillFileScore(abs, file, terms),
			})
		}
	}
	if len(matches) == 0 {
		return externalSkillOutline(abs, files, maxBytes)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return strings.ToLower(filepath.ToSlash(matches[i].file)) < strings.ToLower(filepath.ToSlash(matches[j].file))
		}
		return matches[i].score > matches[j].score
	})
	var sb strings.Builder
	sb.WriteString("Master skill excerpts (focused query: " + query + "):\n")
	limit := len(matches)
	if limit > 12 {
		limit = 12
	}
	for _, match := range matches[:limit] {
		sb.WriteString("\n--- Source: " + externalSkillDisplayPath(abs, match.file) + " ---\n")
		sb.WriteString(strings.TrimSpace(match.excerpt))
		sb.WriteString("\n")
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
	if strings.TrimSpace(query) == "" {
		return content
	}
	matches := relevantMarkdownMatches(content, query, 4)
	if len(matches) > 0 {
		parts := make([]string, 0, len(matches))
		for _, match := range matches {
			parts = append(parts, strings.TrimSpace(match.text))
		}
		return strings.Join(parts, "\n\n")
	}
	sections := splitMarkdownSections(content)
	for _, section := range sections {
		if strings.Contains(strings.ToLower(section), strings.ToLower(strings.TrimSpace(query))) {
			return strings.TrimSpace(section)
		}
	}
	lines := strings.Split(content, "\n")
	var windows []string
	for i, line := range lines {
		if !strings.Contains(strings.ToLower(line), strings.ToLower(strings.TrimSpace(query))) {
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

func relevantMarkdownMatches(content, query string, limit int) []markdownSectionMatch {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return []markdownSectionMatch{{text: content, score: 1}}
	}
	terms := skillQueryTerms(query)
	if len(terms) == 0 {
		return nil
	}
	var matches []markdownSectionMatch
	for _, section := range splitMarkdownSections(content) {
		score := scoreMarkdownSection(section, query, terms)
		if score <= 0 {
			continue
		}
		matches = append(matches, markdownSectionMatch{
			text:  strings.TrimSpace(section),
			score: score,
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func scoreMarkdownSection(section, query string, terms []string) float64 {
	lower := strings.ToLower(section)
	heading := strings.ToLower(firstSectionHeading(section))
	score := 0.0
	if strings.Contains(lower, query) {
		score += 12
	}
	if heading != "" && strings.Contains(heading, query) {
		score += 10
	}
	for _, term := range terms {
		if term == "" {
			continue
		}
		if strings.Contains(heading, term) {
			score += 6
		}
		count := strings.Count(lower, term)
		if count > 6 {
			count = 6
		}
		score += float64(count)
	}
	return score
}

func firstSectionHeading(section string) string {
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			return trimmed
		}
	}
	return ""
}

func skillQueryTerms(query string) []string {
	stop := map[string]bool{
		"a": true, "an": true, "and": true, "as": true, "at": true, "for": true,
		"from": true, "in": true, "of": true, "on": true, "or": true, "the": true,
		"to": true, "use": true, "with": true,
	}
	seen := map[string]bool{}
	var terms []string
	for _, raw := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-')
	}) {
		term := strings.Trim(raw, "_-")
		if len(term) < 2 || stop[term] || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

func externalSkillFileScore(root, path string, terms []string) float64 {
	rel := strings.ToLower(externalSkillDisplayPath(root, path))
	score := 0.0
	for _, term := range terms {
		if strings.Contains(rel, term) {
			score += 4
		}
	}
	has := func(words ...string) bool {
		for _, word := range words {
			for _, term := range terms {
				if term == word {
					return true
				}
			}
		}
		return false
	}
	switch {
	case strings.Contains(rel, "maps/htb_methodology"):
		if has("htb", "box", "foothold", "recon", "enumeration", "enum", "privesc", "flag", "root", "user") {
			score += 16
		}
	case strings.Contains(rel, "skills/build/htb_challenge_solver"):
		if has("htb", "box", "foothold", "privesc", "root", "user") {
			score += 14
		}
	case strings.Contains(rel, "maps/web_application"):
		if has("web", "http", "https", "admin", "fuzz", "vhost", "directory", "injection", "sqli", "rce") {
			score += 14
		}
	case strings.Contains(rel, "maps/linux_unix"):
		if has("linux", "unix", "shell", "privesc", "privilege", "sudo", "cron", "root") {
			score += 12
		}
	case strings.Contains(rel, "skills/rules/engagement"):
		if has("scope", "evidence", "report", "reporting", "rules", "engagement") {
			score += 10
		}
	case strings.Contains(rel, "anti_failure/tool_execution"):
		if has("tool", "tools", "execution", "command", "repeat", "failure", "terminal") {
			score += 10
		}
	case strings.Contains(rel, "anti_failure/context_budget"):
		if has("context", "budget", "local", "llm", "memory") {
			score += 10
		}
	}
	if strings.Contains(rel, "prototyping/func_encyclopedia") || strings.Contains(rel, "evasion") {
		if !has("evasion", "malware", "prototype", "prototyping", "c", "cpp") {
			score -= 8
		}
	}
	return score
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
