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
		"Skills contain step-by-step workflows, checklists, and pitfalls for common task patterns."
}

func (t *SkillView) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "Skill slug name as listed by skills_list."}
  },
  "required": ["name"],
  "additionalProperties": false
}`)
}

type skillViewParams struct {
	Name string `json:"name"`
}

func (t *SkillView) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p skillViewParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("skill_view: bad params: %w", err)
	}
	if strings.TrimSpace(p.Name) == "" {
		return "", fmt.Errorf("skill_view: name is required")
	}
	content, err := readSkillFile(p.Name)
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

func readSkillFile(name string) (string, error) {
	dir, err := toolsSkillsDir()
	if err != nil {
		return "", err
	}
	slug := toolsSlugify(name)
	data, err := os.ReadFile(filepath.Join(dir, slug+".md"))
	if err != nil {
		return "", err
	}
	return string(data), nil
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
		}
	}
	return m
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
