package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mauler/internal/settings"
	"mauler/internal/tools"
)

// Skill is a procedural-memory document in SKILL.md format.
// Each skill is stored as an individual Markdown file with YAML-style
// frontmatter so the agent can also read/write them as plain text.
type Skill struct {
	Name        string   `json:"name"`        // slug used as filename (e.g. "fix-go-tool-calls")
	Description string   `json:"description"` // one-line trigger description
	Version     string   `json:"version"`     // semver string
	Tags        []string `json:"tags"`
	SourcePath  string   `json:"source_path"` // optional external file/folder backing this skill
	Body        string   `json:"body"`        // full Markdown body after frontmatter
	Raw         string   `json:"raw"`         // full file content (frontmatter + body)
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// SkillSuggestion is emitted after a complex run as a learning prompt.
type SkillSuggestion struct {
	Type     string `json:"type"` // "skill" | "memory"
	Title    string `json:"title"`
	Reason   string `json:"reason"`
	Template string `json:"template"` // pre-filled SKILL.md content the user can edit
}

// ---------- Wails bindings ----------

func (a *App) ListSkills() ([]Skill, error) {
	return loadSkills()
}

func (a *App) GetSkill(name string) (Skill, error) {
	return loadSkill(name)
}

func (a *App) SaveSkill(skill Skill) (Skill, error) {
	return saveSkill(skill)
}

func (a *App) DeleteSkill(name string) error {
	dir, err := skillsDir()
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, slugify(name)+".md"))
}

// ---------- Internal helpers ----------

func loadSkills() ([]Skill, error) {
	dir, err := skillsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Skill{}, nil
	}
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		skill, err := loadSkill(name)
		if err != nil {
			continue
		}
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

func loadSkill(name string) (Skill, error) {
	dir, err := skillsDir()
	if err != nil {
		return Skill{}, err
	}
	path := filepath.Join(dir, slugify(name)+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	return parseSkillMD(name, string(data)), nil
}

func saveSkill(skill Skill) (Skill, error) {
	dir, err := skillsDir()
	if err != nil {
		return skill, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return skill, err
	}
	now := time.Now().Format(time.RFC3339)
	skill.Name = slugify(skill.Name)
	if skill.Name == "" {
		return skill, fmt.Errorf("skill name cannot be empty")
	}
	if skill.Version == "" {
		skill.Version = "1.0.0"
	}
	if skill.CreatedAt == "" {
		// Try to preserve existing creation time
		if existing, err := loadSkill(skill.Name); err == nil {
			skill.CreatedAt = existing.CreatedAt
		} else {
			skill.CreatedAt = now
		}
	}
	skill.UpdatedAt = now
	skill.Tags = normaliseTags(skill.Tags)
	skill.SourcePath = strings.TrimSpace(skill.SourcePath)

	content := renderSkillMD(skill)
	skill.Raw = content
	skill.Body = extractSkillBody(content)
	path := filepath.Join(dir, skill.Name+".md")
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		return skill, err
	}
	return skill, nil
}

func saveMasterSkillSource(path string) (Skill, string, error) {
	normalized, prompt, err := readMasterSkillSourcePrompt(path)
	if err != nil {
		return Skill{}, "", err
	}
	skill := Skill{
		Name:        "master",
		Description: "Use when the user explicitly asks to apply the selected master workflow/instruction source.",
		Version:     "1.0.0",
		Tags:        []string{"master", "workflow", "instructions"},
		SourcePath:  normalized,
		Body:        "External master skill source is registered for lazy use.\n\nLarge external sources are loaded lazily. Use `skills_list` to discover it, then `skill_view` with name `master` and an optional focused query to read only the needed sections.",
	}
	saved, err := saveSkill(skill)
	if err != nil {
		return saved, "", err
	}
	return saved, prompt, nil
}

func deleteMasterSkillSource() error {
	dir, err := skillsDir()
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(dir, "master.md"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func readMasterSkillSourcePrompt(path string) (string, string, error) {
	normalized := strings.TrimSpace(path)
	if normalized == "" {
		return "", "", fmt.Errorf("master skill path is required")
	}
	normalized = tools.NormalizeHostPath(normalized)
	abs, err := filepath.Abs(normalized)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", "", fmt.Errorf("master skill source: %w", err)
	}
	docs := readInstructionSource(abs, settings.DefaultSettings().Context.ProjectDocMaxBytes)
	if len(docs) == 0 {
		return "", "", fmt.Errorf("master skill source contains no readable markdown instructions")
	}
	var sb strings.Builder
	sb.WriteString("Master skill source registered (lazy-load):\n")
	for _, doc := range docs {
		sb.WriteString("\n--- Source: " + displayInstructionSourcePath(abs, doc.Path))
		if doc.Partial {
			sb.WriteString(" (truncated)")
		}
		sb.WriteString(" ---\n")
		sb.WriteString(firstMarkdownHeadings(doc.Content, 24))
		sb.WriteString("\n")
	}
	sb.WriteString("\nUse skill_view with name `master` and a focused query when the task needs instructions from this source.")
	return filepath.ToSlash(abs), sb.String(), nil
}

func displayInstructionSourcePath(root, path string) string {
	root = tools.NormalizeHostPath(root)
	path = tools.NormalizeHostPath(path)
	if rel, err := filepath.Rel(root, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}

func firstMarkdownHeadings(content string, limit int) string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			lines = append(lines, trimmed)
			if len(lines) >= limit {
				break
			}
		}
	}
	if len(lines) == 0 {
		return "(no markdown headings found)"
	}
	return strings.Join(lines, "\n")
}

// relevantSkills returns skills whose description/tags match the prompt.
func relevantSkills(cfg settings.SkillsConfig, prompt string) []Skill {
	if !cfg.Enabled {
		return nil
	}
	skills, err := loadSkills()
	if err != nil || len(skills) == 0 {
		return nil
	}
	terms := keywordSet(prompt)
	type scored struct {
		skill Skill
		score float64
	}
	var candidates []scored
	for _, s := range skills {
		if s.Name == "master" && !masterSkillRequested(terms) {
			continue
		}
		score := scoreSkill(s, terms)
		if score > 0 {
			candidates = append(candidates, scored{skill: s, score: score})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	limit := cfg.MaxInject
	if limit <= 0 {
		limit = 3
	}
	if len(candidates) < limit {
		limit = len(candidates)
	}
	out := make([]Skill, limit)
	for i := range out {
		out[i] = candidates[i].skill
	}
	return out
}

func masterSkillRequested(terms map[string]bool) bool {
	return terms["master"] || terms["master_skill"] || terms["master-skills"] || terms["master_skills"]
}

func scoreSkill(s Skill, terms map[string]bool) float64 {
	desc := strings.ToLower(s.Description)
	name := strings.ToLower(s.Name)
	tags := strings.ToLower(strings.Join(s.Tags, " "))
	body := strings.ToLower(s.Body)
	score := 0.0
	for term := range terms {
		switch {
		case containsWordish(tags, term):
			score += 4
		case containsWordish(name, term):
			score += 3
		case containsWordish(desc, term):
			score += 2
		case containsWordish(body, term):
			score += 0.5
		}
	}
	return score
}

// buildLearningSuggestion returns a SkillSuggestion if the run looks like it
// produced reusable procedural knowledge. Returns nil if not applicable.
func buildLearningSuggestion(run *TaskRun) *SkillSuggestion {
	if run == nil || run.Status != "done" {
		return nil
	}
	toolCalls := len(run.Tools)
	if toolCalls < 4 {
		return nil // too simple to be worth a skill
	}
	// Only suggest for modes that involve building/fixing/research patterns
	switch run.Mode {
	case "Builder", "Fixer", "Researcher", "Auto":
	default:
		return nil
	}
	// Derive a slug from the prompt
	words := strings.Fields(run.Prompt)
	if len(words) > 6 {
		words = words[:6]
	}
	slug := slugify(strings.Join(words, "-"))
	if len(slug) > 48 {
		slug = slug[:48]
	}
	// Build a pre-filled template the user can edit
	desc := truncate(run.Prompt, 80)
	template := fmt.Sprintf(`---
name: %s
description: Use when %s.
version: 1.0.0
tags: [%s]
---

## Overview

<!-- Describe what this skill covers in 1-3 sentences. -->

## When to Use

- <!-- Pattern or trigger that should activate this skill -->

## Steps

1. <!-- Step 1 -->
2. <!-- Step 2 -->

## Common Pitfalls

- <!-- Known traps or mistakes to avoid -->

## Verification

- [ ] <!-- How to confirm success -->
`, slug, desc, strings.ToLower(run.Mode))

	return &SkillSuggestion{
		Type:     "skill",
		Title:    fmt.Sprintf("Save %q as a skill?", truncate(run.Prompt, 50)),
		Reason:   fmt.Sprintf("This %s run used %d tools. Capturing it as a skill helps the agent repeat this pattern in future sessions.", run.Mode, toolCalls),
		Template: template,
	}
}

// ---------- SKILL.md parsing ----------

func parseSkillMD(name, content string) Skill {
	s := Skill{Name: name, Raw: content}
	if !strings.HasPrefix(content, "---") {
		s.Body = content
		return s
	}
	rest := content[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		s.Body = content
		return s
	}
	frontmatter := rest[:end]
	s.Body = strings.TrimSpace(rest[end+4:])
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "name":
			s.Name = slugify(val)
		case "description":
			s.Description = val
		case "version":
			s.Version = val
		case "tags":
			// accept both: tags: [a, b, c] and tags: a, b, c
			val = strings.Trim(val, "[]")
			for _, t := range strings.Split(val, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					s.Tags = append(s.Tags, t)
				}
			}
		case "source_path":
			s.SourcePath = val
		case "created_at":
			s.CreatedAt = val
		case "updated_at":
			s.UpdatedAt = val
		}
	}
	if s.Name == "" {
		s.Name = slugify(name)
	}
	return s
}

func renderSkillMD(s Skill) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: " + s.Name + "\n")
	sb.WriteString("description: " + s.Description + "\n")
	sb.WriteString("version: " + s.Version + "\n")
	if len(s.Tags) > 0 {
		sb.WriteString("tags: [" + strings.Join(s.Tags, ", ") + "]\n")
	}
	if strings.TrimSpace(s.SourcePath) != "" {
		sb.WriteString("source_path: " + strings.TrimSpace(s.SourcePath) + "\n")
	}
	if s.CreatedAt != "" {
		sb.WriteString("created_at: " + s.CreatedAt + "\n")
	}
	if s.UpdatedAt != "" {
		sb.WriteString("updated_at: " + s.UpdatedAt + "\n")
	}
	sb.WriteString("---\n\n")
	sb.WriteString(strings.TrimSpace(s.Body))
	sb.WriteString("\n")
	return sb.String()
}

func extractSkillBody(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	rest := content[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return content
	}
	return strings.TrimSpace(rest[end+4:])
}

// ---------- Path / util helpers ----------

func skillsDir() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills"), nil
}

func slugify(s string) string {
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
