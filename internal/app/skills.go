package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

// Skill is a procedural-memory document in SKILL.md format.
// Each skill is stored as an individual Markdown file with YAML-style
// frontmatter so the agent can also read/write them as plain text.
type Skill struct {
	Name          string   `json:"name"`        // slug used as filename (e.g. "fix-go-tool-calls")
	Description   string   `json:"description"` // one-line trigger description
	Version       string   `json:"version"`     // semver string
	Tags          []string `json:"tags"`
	SourcePath    string   `json:"source_path"` // optional external file/folder backing this skill
	RequiredTools []string `json:"required_tools"`
	ShellBackend  string   `json:"shell_backend"` // optional: wsl | bash | powershell | cmd | auto
	NeedsNetwork  bool     `json:"needs_network"`
	NeedsWrite    bool     `json:"needs_write"`
	Body          string   `json:"body"` // full Markdown body after frontmatter
	Raw           string   `json:"raw"`  // full file content (frontmatter + body)
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
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
	saved, err := saveSkill(skill)
	if err != nil {
		return saved, err
	}
	a.recordLedger(ledger.Event{
		Kind:    "skill_write",
		Source:  "skills",
		Status:  "saved",
		Message: saved.Name,
		Detail:  saved.Description,
		Output:  saved.Raw,
		Metadata: map[string]string{
			"version": saved.Version,
			"tags":    strings.Join(saved.Tags, ","),
		},
	})
	return saved, nil
}

func (a *App) DeleteSkill(name string) error {
	dir, err := skillsDir()
	if err != nil {
		return err
	}
	slug := slugify(name)
	if err := os.Remove(filepath.Join(dir, slug+".md")); err != nil {
		return err
	}
	a.recordLedger(ledger.Event{
		Kind:    "skill_delete",
		Source:  "skills",
		Status:  "deleted",
		Message: slug,
	})
	return nil
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
	skill.RequiredTools = normaliseTags(skill.RequiredTools)
	skill.ShellBackend = strings.TrimSpace(skill.ShellBackend)

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
		Description: "Use as TheMauler's lazy master methodology reference for pentest, HTB, workflow, and agent anti-failure guidance.",
		Version:     "1.0.0",
		Tags:        []string{"master", "workflow", "instructions", "pentest", "htb", "local-llm"},
		SourcePath:  normalized,
		Body:        masterSkillAdapterBody(),
	}
	saved, err := saveSkill(skill)
	if err != nil {
		return saved, "", err
	}
	return saved, prompt, nil
}

func masterSkillAdapterBody() string {
	return strings.TrimSpace(`The external master/Navigator source is registered as a lazy reference library for TheMauler.

Adapter contract for local LLMs:
- TheMauler's system prompt, active project/box, access preset, evidence policy, shell backend, and latest user instruction have priority over any external master-skill text.
- Do not load or summarize the entire external source. Call skill with mode "view", name "master", and a focused query for the current phase.
- Prefer focused queries such as "htb methodology recon foothold", "web application enumeration", "linux privilege escalation", "active directory attack path", "report evidence workflow", "tool execution anti failure", or "context budget local llm".
- For Ops/pentest work, use terminal_send with command only for a genuine live/interactive session; use shell or http_probe for independent one-shot checks that should complete as separate local processes.
- Treat writeups/spoiler material as reference, not primary discovery, unless the project evidence policy explicitly allows spoiler-assisted work.
- Store only compact confirmed facts, working commands, and reusable lessons in memory. Do not store flags, secrets, huge logs, or unrelated target details.
- If a command or tactic fails twice, change tactic, inspect evidence, or ask the user instead of repeating.

Large external sources are loaded lazily. Use skill mode=list to discover this skill, then skill mode=view with name "master" and a focused query to read only the needed sections.`)
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
	sb.WriteString("\nUse skill mode=view with name `master` and a focused query when the task needs instructions from this source.")
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
	return relevantSkillsForSettings(cfg, settings.DefaultSettings(), prompt)
}

func relevantSkillsForSettings(cfg settings.SkillsConfig, appCfg settings.Settings, prompt string) []Skill {
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
		s = annotateSkillAvailability(s, appCfg)
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

func annotateSkillAvailability(skill Skill, cfg settings.Settings) Skill {
	problems := skillAvailabilityProblems(skill, cfg)
	if len(problems) == 0 {
		return skill
	}
	note := "Tool availability note: this skill may not be fully usable in the current run because " + strings.Join(problems, "; ") + ". If those capabilities are needed, ask the user to switch toolset/shell profile instead of repeatedly trying blocked tools."
	if strings.TrimSpace(skill.Body) == "" {
		skill.Body = note
		return skill
	}
	if !strings.Contains(skill.Body, "Tool availability note:") {
		skill.Body = note + "\n\n" + skill.Body
	}
	return skill
}

func skillAvailabilityProblems(skill Skill, cfg settings.Settings) []string {
	var problems []string
	effective := settings.EffectiveEnabledTools(cfg.Tools)
	for _, name := range skill.RequiredTools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !toolEnabled(effective, name) {
			problems = append(problems, fmt.Sprintf("required tool %q is not enabled by active toolset %q", name, cfg.Tools.ActiveToolset))
		}
	}
	if skill.NeedsWrite && !toolEnabled(effective, "write") && !toolEnabled(effective, "edit") {
		problems = append(problems, "write/edit tools are unavailable")
	}
	if skill.NeedsNetwork && !toolEnabled(effective, "web_search") && !toolEnabled(effective, "fetch_url") && !toolEnabled(effective, "shell") {
		problems = append(problems, "network-capable tools are unavailable")
	}
	if backend := strings.ToLower(strings.TrimSpace(skill.ShellBackend)); backend != "" && backend != "auto" {
		active := strings.ToLower(strings.TrimSpace(cfg.Tools.ShellBackend))
		if active == "" {
			active = "auto"
		}
		if active != backend {
			problems = append(problems, fmt.Sprintf("skill expects shell backend %q but active backend is %q", backend, active))
		}
	}
	return dedupeStrings(problems)
}

func masterSkillRequested(terms map[string]bool) bool {
	if terms["master"] || terms["master_skill"] || terms["master-skill"] || terms["master-skills"] || terms["master_skills"] {
		return true
	}
	if terms["masterskill"] || terms["masterskil"] {
		return true
	}
	for _, term := range []string{
		"pentest", "hacking", "exploit", "exploits", "exploitation", "foothold",
		"recon", "enumeration", "privsec", "privesc", "cve", "payload", "shell",
		"htb", "freepbx",
	} {
		if terms[term] {
			return true
		}
	}
	return false
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
		case "required_tools":
			s.RequiredTools = parseFrontmatterList(val)
		case "shell_backend":
			s.ShellBackend = val
		case "needs_network":
			s.NeedsNetwork = parseFrontmatterBool(val)
		case "needs_write":
			s.NeedsWrite = parseFrontmatterBool(val)
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
	if len(s.RequiredTools) > 0 {
		sb.WriteString("required_tools: [" + strings.Join(s.RequiredTools, ", ") + "]\n")
	}
	if strings.TrimSpace(s.ShellBackend) != "" {
		sb.WriteString("shell_backend: " + strings.TrimSpace(s.ShellBackend) + "\n")
	}
	if s.NeedsNetwork {
		sb.WriteString("needs_network: true\n")
	}
	if s.NeedsWrite {
		sb.WriteString("needs_write: true\n")
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

func parseFrontmatterList(val string) []string {
	val = strings.Trim(strings.TrimSpace(val), "[]")
	if val == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(val, ",") {
		item = strings.Trim(strings.TrimSpace(item), `"'`)
		if item != "" {
			out = append(out, item)
		}
	}
	return normaliseTags(out)
}

func parseFrontmatterBool(val string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(val), `"'`)) {
	case "true", "yes", "1", "on":
		return true
	default:
		return false
	}
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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
