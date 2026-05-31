package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mauler/internal/settings"
)

// MemoryEntry is a durable project note that can be injected into prompts.
type MemoryEntry struct {
	ID         string   `json:"id"`
	Scope      string   `json:"scope"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	Kind       string   `json:"kind"`
	Importance int      `json:"importance"`
	Pinned     bool     `json:"pinned"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
	LastUsedAt string   `json:"last_used_at"`
}

func (a *App) ListMemory() ([]MemoryEntry, error) {
	return loadMemory()
}

func (a *App) SaveMemoryEntry(entry MemoryEntry) (MemoryEntry, error) {
	a.mu.Lock()
	cfg := *a.cfg
	a.mu.Unlock()
	entries, err := loadMemory()
	if err != nil {
		return entry, err
	}
	now := time.Now().Format(time.RFC3339)
	if strings.TrimSpace(entry.ID) == "" {
		entry.ID = fmt.Sprintf("mem-%d", time.Now().UnixNano())
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	if entry.Scope == "" {
		entry.Scope = workspaceScope()
	}
	entry.Title = strings.TrimSpace(entry.Title)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.Kind = normaliseMemoryKind(entry.Kind)
	entry.Importance = clampInt(entry.Importance, 1, 5, 3)
	entry.Tags = normaliseTags(entry.Tags)
	if cfg.Memory.MaxEntryChars > 0 && len(entry.Content) > cfg.Memory.MaxEntryChars {
		entry.Content = entry.Content[:cfg.Memory.MaxEntryChars]
	}
	replaced := false
	for i := range entries {
		if entries[i].ID == entry.ID {
			if entry.CreatedAt == "" {
				entry.CreatedAt = entries[i].CreatedAt
			}
			entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}
	if cfg.Memory.MaxEntries > 0 && len(entries) > cfg.Memory.MaxEntries {
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].UpdatedAt > entries[j].UpdatedAt })
		entries = entries[:cfg.Memory.MaxEntries]
	}
	return entry, saveMemory(entries)
}

func (a *App) DeleteMemoryEntry(id string) error {
	entries, err := loadMemory()
	if err != nil {
		return err
	}
	next := entries[:0]
	for _, entry := range entries {
		if entry.ID != id {
			next = append(next, entry)
		}
	}
	return saveMemory(next)
}

func (a *App) ClearMemoryEntries() error {
	return saveMemory([]MemoryEntry{})
}

func (a *App) AddMemory(title, content string, tags []string) (MemoryEntry, error) {
	return a.SaveMemoryEntry(MemoryEntry{
		Title:      strings.TrimSpace(title),
		Content:    strings.TrimSpace(content),
		Tags:       tags,
		Scope:      workspaceScope(),
		Kind:       "note",
		Importance: 3,
	})
}

func relevantMemory(cfg settings.MemoryConfig, prompt string) []MemoryEntry {
	if !cfg.Enabled || !cfg.AutoInject {
		return nil
	}
	entries, err := loadMemory()
	if err != nil || len(entries) == 0 {
		return nil
	}
	scope := workspaceScope()
	terms := keywordSet(prompt)
	type scored struct {
		entry MemoryEntry
		score float64
	}
	scoredEntries := make([]scored, 0, len(entries))
	for _, entry := range entries {
		if entry.Scope != "" && entry.Scope != scope {
			continue
		}
		score := scoreMemory(entry, terms, prompt)
		if score <= 0 && len(terms) > 0 && !entry.Pinned {
			continue
		}
		scoredEntries = append(scoredEntries, scored{entry: entry, score: score})
	}
	sort.SliceStable(scoredEntries, func(i, j int) bool {
		if scoredEntries[i].score == scoredEntries[j].score {
			return scoredEntries[i].entry.UpdatedAt > scoredEntries[j].entry.UpdatedAt
		}
		return scoredEntries[i].score > scoredEntries[j].score
	})
	limit := cfg.MaxInject
	if limit <= 0 {
		limit = 8
	}
	if len(scoredEntries) < limit {
		limit = len(scoredEntries)
	}
	out := make([]MemoryEntry, 0, limit)
	for i := 0; i < limit; i++ {
		entry := scoredEntries[i].entry
		entry.LastUsedAt = time.Now().Format(time.RFC3339)
		out = append(out, entry)
	}
	if len(out) > 0 {
		markMemoryUsed(out)
	}
	return out
}

func keywordSet(text string) map[string]bool {
	out := map[string]bool{}
	for _, raw := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' && r != '.'
	}) {
		if len(raw) >= 4 && !memoryStopWords[raw] {
			out[raw] = true
		}
	}
	return out
}

var memoryStopWords = map[string]bool{
	"about": true, "after": true, "also": true, "because": true, "before": true,
	"build": true, "could": true, "from": true, "have": true, "into": true,
	"just": true, "like": true, "make": true, "need": true, "please": true,
	"should": true, "that": true, "there": true, "this": true, "what": true,
	"when": true, "with": true, "work": true, "would": true,
}

func scoreMemory(entry MemoryEntry, terms map[string]bool, prompt string) float64 {
	title := strings.ToLower(entry.Title)
	content := strings.ToLower(entry.Content)
	tags := strings.ToLower(strings.Join(entry.Tags, " "))
	kind := strings.ToLower(entry.Kind)
	score := 0.0
	for term := range terms {
		switch {
		case containsWordish(tags, term):
			score += 4
		case containsWordish(title, term):
			score += 3
		case containsWordish(content, term):
			score += 1
		}
	}
	if entry.Pinned {
		score += 2.5
	}
	if entry.Importance > 0 {
		score += float64(entry.Importance) * 0.35
	}
	if kind == "preference" || kind == "constraint" {
		score += 0.75
	}
	score += recencyBoost(entry.UpdatedAt)
	if strings.Contains(strings.ToLower(prompt), title) && title != "" {
		score += 2
	}
	return score
}

func containsWordish(haystack, term string) bool {
	return strings.Contains(haystack, term)
}

func recencyBoost(ts string) float64 {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return 0
	}
	age := time.Since(t)
	switch {
	case age < 24*time.Hour:
		return 1.0
	case age < 7*24*time.Hour:
		return 0.5
	case age < 30*24*time.Hour:
		return 0.2
	default:
		return 0
	}
}

func markMemoryUsed(used []MemoryEntry) {
	entries, err := loadMemory()
	if err != nil {
		return
	}
	usedAt := map[string]string{}
	for _, entry := range used {
		usedAt[entry.ID] = entry.LastUsedAt
	}
	changed := false
	for i := range entries {
		if ts, ok := usedAt[entries[i].ID]; ok {
			entries[i].LastUsedAt = ts
			changed = true
		}
	}
	if changed {
		_ = saveMemory(entries)
	}
}

func normaliseTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.Trim(strings.ToLower(strings.TrimSpace(tag)), "#")
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func normaliseMemoryKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "preference", "constraint", "fact", "workflow", "decision", "note":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "note"
	}
}

func clampInt(value, min, max, fallback int) int {
	if value == 0 {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func loadMemory() ([]MemoryEntry, error) {
	path, err := memoryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []MemoryEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []MemoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func saveMemory(entries []MemoryEntry) error {
	path, err := memoryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o640)
}

func memoryPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "memory.json"), nil
}

func workspaceScope() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.ToSlash(wd)
}
