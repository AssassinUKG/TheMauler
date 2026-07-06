package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/sessionstore"
	"mauler/internal/settings"
	"mauler/internal/store"
)

// MemoryEntry is a durable project note that can be injected into prompts.
type MemoryEntry struct {
	ID         string   `json:"id"`
	Scope      string   `json:"scope"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	Kind       string   `json:"kind"`
	Confidence string   `json:"confidence"` // confirmed | likely | hypothesis | stale
	Source     string   `json:"source"`     // user | agent | tool | model | previous_run | auto_distill
	Importance int      `json:"importance"`
	Pinned     bool     `json:"pinned"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
	LastUsedAt string   `json:"last_used_at"`
}

var (
	memoryDBMu sync.RWMutex
	memoryDB   *sql.DB
)

func setMemoryDB(db *sql.DB) {
	memoryDBMu.Lock()
	memoryDB = db
	memoryDBMu.Unlock()
}

func (a *App) ListMemory() ([]MemoryEntry, error) {
	return loadMemory()
}

func (a *App) ExportMemoryJSON() (string, error) {
	entries, err := loadMemory()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) ImportMemoryJSON(raw string) (int, error) {
	var entries []MemoryEntry
	if err := json.Unmarshal(stripJSONBOM([]byte(raw)), &entries); err != nil {
		return 0, err
	}
	if err := saveMemory(entries); err != nil {
		return 0, err
	}
	return len(entries), nil
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
	entry.Confidence = normaliseMemoryConfidence(entry.Confidence)
	entry.Source = normaliseMemorySource(entry.Source)
	entry.Importance = clampInt(entry.Importance, 1, 5, 3)
	entry.Tags = normaliseTags(entry.Tags)
	entry.Tags = addLabMemoryTags(entry.Tags, cfg.Context.Lab, entry)
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
		sort.SliceStable(entries, func(i, j int) bool {
			si, sj := evictionScore(entries[i]), evictionScore(entries[j])
			if si == sj {
				return entries[i].UpdatedAt > entries[j].UpdatedAt
			}
			return si > sj
		})
		entries = entries[:cfg.Memory.MaxEntries]
	}
	if err := saveMemory(entries); err != nil {
		return entry, err
	}
	status := "created"
	if replaced {
		status = "updated"
	}
	a.recordLedger(ledger.Event{
		Kind:    "memory_write",
		Source:  "memory",
		Status:  status,
		Message: entry.Title,
		Detail:  entry.Content,
		Metadata: map[string]string{
			"id":         entry.ID,
			"scope":      entry.Scope,
			"kind":       entry.Kind,
			"confidence": entry.Confidence,
			"source":     entry.Source,
			"importance": fmt.Sprintf("%d", entry.Importance),
			"tags":       strings.Join(entry.Tags, ","),
		},
	})
	return entry, nil
}

func (a *App) DeleteMemoryEntry(id string) error {
	entries, err := loadMemory()
	if err != nil {
		return err
	}
	var deleted MemoryEntry
	next := entries[:0]
	for _, entry := range entries {
		if entry.ID != id {
			next = append(next, entry)
		} else {
			deleted = entry
		}
	}
	if err := saveMemory(next); err != nil {
		return err
	}
	a.recordLedger(ledger.Event{
		Kind:    "memory_delete",
		Source:  "memory",
		Status:  "deleted",
		Message: deleted.Title,
		Metadata: map[string]string{
			"id":    strings.TrimSpace(id),
			"scope": deleted.Scope,
			"kind":  deleted.Kind,
		},
	})
	return nil
}

func (a *App) ClearMemoryEntries() error {
	if err := saveMemory([]MemoryEntry{}); err != nil {
		return err
	}
	a.recordLedger(ledger.Event{
		Kind:    "memory_clear",
		Source:  "memory",
		Status:  "cleared",
		Message: workspaceScope(),
	})
	return nil
}

func (a *App) AddMemory(title, content string, tags []string) (MemoryEntry, error) {
	return a.SaveMemoryEntry(MemoryEntry{
		Title:      strings.TrimSpace(title),
		Content:    strings.TrimSpace(content),
		Tags:       tags,
		Scope:      workspaceScope(),
		Kind:       "note",
		Confidence: "confirmed",
		Source:     "user",
		Importance: 3,
	})
}

type memorySelection struct {
	Entries   []MemoryEntry
	Withheld  []MemoryEntry
	Conflicts []string
	Plan      memoryRetrievalPlan
}

func relevantMemory(cfg settings.MemoryConfig, prompt string) []MemoryEntry {
	return selectRelevantMemory(settings.Settings{Memory: cfg}, prompt).Entries
}

type memoryRetrievalPlan struct {
	Intent     string
	QueryTerms []string
	Slots      map[string]int
	Selected   []memoryPlanSelection
}

type memoryPlanSelection struct {
	ID     string
	Title  string
	Layer  string
	Reason string
}

func (s *memorySelection) AddSessionRecall(results []sessionstore.SearchResult) {
	for _, entry := range sessionRecallMemoryEntries(results) {
		s.Entries = append(s.Entries, entry)
		s.Plan.Selected = append(s.Plan.Selected, memoryPlanSelection{
			ID:     entry.ID,
			Title:  memoryLabel(entry),
			Layer:  "session_recall",
			Reason: "matched prior saved session; pointer only",
		})
	}
}

func (s *memorySelection) AddEvidencePointers(events []ledger.Event, prompt string) {
	terms := sortedKeywordTerms(prompt)
	for _, entry := range evidenceMemoryEntries(events, terms) {
		s.Entries = append(s.Entries, entry)
		s.Plan.Selected = append(s.Plan.Selected, memoryPlanSelection{
			ID:     entry.ID,
			Title:  memoryLabel(entry),
			Layer:  "evidence",
			Reason: memorySelectionReason(entry, "evidence", terms),
		})
	}
}

func sessionRecallMemoryEntries(results []sessionstore.SearchResult) []MemoryEntry {
	const limit = 2
	out := make([]MemoryEntry, 0, minInt(len(results), limit))
	seen := map[string]bool{}
	for _, result := range results {
		key := result.SessionID + ":" + fmt.Sprint(result.MessageID)
		if seen[key] {
			continue
		}
		seen[key] = true
		title := strings.TrimSpace(result.SessionName)
		if title == "" {
			title = "Prior session"
		}
		content := compactSessionRecallContent(result)
		if content == "" {
			continue
		}
		out = append(out, MemoryEntry{
			ID:         "session:" + key,
			Scope:      workspaceScope(),
			Title:      "Session recall: " + title,
			Content:    content,
			Tags:       normaliseTags([]string{"session-recall", "pointer"}),
			Kind:       "note",
			Confidence: "likely",
			Source:     "previous_run",
			Importance: 3,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func compactSessionRecallContent(result sessionstore.SearchResult) string {
	snippet := compactOneLine(result.Content, 260)
	if snippet == "" {
		return ""
	}
	parts := []string{"Prior saved chat match"}
	if result.Role != "" {
		parts = append(parts, "role="+result.Role)
	}
	if result.ToolName != "" {
		parts = append(parts, "tool="+result.ToolName)
	}
	if result.UpdatedAt != "" {
		parts = append(parts, "updated="+result.UpdatedAt)
	}
	parts = append(parts, "snippet="+snippet)
	return strings.Join(parts, "; ")
}

func evidenceMemoryEntries(events []ledger.Event, terms []string) []MemoryEntry {
	const limit = 3
	out := make([]MemoryEntry, 0, limit)
	seen := map[string]bool{}
	for _, event := range events {
		if !isEvidencePointerEvent(event) || !eventMatchesTerms(event, terms) {
			continue
		}
		key := firstNonEmpty(event.ID, event.Kind+":"+event.Timestamp)
		if seen[key] {
			continue
		}
		seen[key] = true
		content := compactEvidenceContent(event)
		if content == "" {
			continue
		}
		out = append(out, MemoryEntry{
			ID:         "evidence:" + key,
			Scope:      workspaceScope(),
			Title:      "Evidence pointer: " + firstNonEmpty(event.Message, event.Kind),
			Content:    content,
			Tags:       normaliseTags(append([]string{"evidence", "pointer"}, event.Kind)),
			Kind:       "fact",
			Confidence: "confirmed",
			Source:     "tool",
			Importance: 4,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func isEvidencePointerEvent(event ledger.Event) bool {
	if len(event.Artifacts) > 0 || len(event.Files) > 0 {
		switch event.Kind {
		case "tool_result", "artifact_done", "artifact_output", "file_change", "web_event", "browser_event", "subagent_done":
			return event.Status == "" || event.Status == "done" || event.Status == "success" || event.Status == "completed"
		default:
			return strings.Contains(event.Kind, "artifact") || strings.Contains(event.Kind, "evidence") || strings.Contains(event.Kind, "file")
		}
	}
	return false
}

func eventMatchesTerms(event ledger.Event, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		event.Kind,
		event.Tool,
		event.Message,
		event.Detail,
		event.Output,
		strings.Join(event.Files, " "),
		strings.Join(event.Artifacts, " "),
	}, "\n"))
	for _, term := range terms {
		if containsWordish(haystack, term) {
			return true
		}
	}
	return false
}

func compactEvidenceContent(event ledger.Event) string {
	parts := []string{"Ledger evidence"}
	if event.Kind != "" {
		parts = append(parts, "kind="+event.Kind)
	}
	if event.Tool != "" {
		parts = append(parts, "tool="+event.Tool)
	}
	if event.Timestamp != "" {
		parts = append(parts, "time="+event.Timestamp)
	}
	if msg := compactOneLine(firstNonEmpty(event.Message, event.Detail, event.Output), 220); msg != "" {
		parts = append(parts, "summary="+msg)
	}
	if len(event.Files) > 0 {
		parts = append(parts, "files="+strings.Join(event.Files[:minInt(len(event.Files), 3)], ","))
	}
	if len(event.Artifacts) > 0 {
		parts = append(parts, "artifacts="+strings.Join(event.Artifacts[:minInt(len(event.Artifacts), 3)], ","))
	}
	return strings.Join(parts, "; ")
}

func compactOneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit > 0 && len(text) > limit {
		return text[:limit] + "..."
	}
	return text
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func selectRelevantMemory(cfg settings.Settings, prompt string) memorySelection {
	if !cfg.Memory.Enabled || !cfg.Memory.AutoInject {
		return memorySelection{}
	}
	entries, err := loadMemory()
	if err != nil || len(entries) == 0 {
		return memorySelection{}
	}
	filtered, withheld, conflicts := filterMemoryInjectionConflicts(entries, cfg, prompt)
	limit := cfg.Memory.MaxInject
	if limit <= 0 {
		limit = 8
	}
	plan := planMemoryRetrieval(filtered, prompt, limit)
	if len(plan.Selected) > 0 {
		markMemoryUsed(planEntries(filtered, plan))
	}
	return memorySelection{
		Entries:   planEntries(filtered, plan),
		Withheld:  withheld,
		Conflicts: conflicts,
		Plan:      plan,
	}
}

func planMemoryRetrieval(entries []MemoryEntry, prompt string, limit int) memoryRetrievalPlan {
	if limit <= 0 {
		limit = 8
	}
	terms := sortedKeywordTerms(prompt)
	intent := memoryRetrievalIntent(prompt)
	ranked := rankMemoryCandidates(entries, prompt, 0)
	ranked = filterAutoInjectMemoryByIntent(ranked, intent, terms, prompt)
	slots := memoryRetrievalSlots(intent, limit)
	selected := make([]MemoryEntry, 0, limit)
	used := map[string]bool{}
	for _, layer := range []string{"preferences", "confirmed", "previous_run", "unverified"} {
		want := slots[layer]
		if want <= 0 {
			continue
		}
		for _, entry := range ranked {
			if used[entry.ID] || memoryLayer(entry) != layer {
				continue
			}
			selected = append(selected, entry)
			used[entry.ID] = true
			if countMemoryLayer(selected, layer) >= want || len(selected) >= limit {
				break
			}
		}
	}
	for _, entry := range ranked {
		if len(selected) >= limit {
			break
		}
		if used[entry.ID] {
			continue
		}
		selected = append(selected, entry)
		used[entry.ID] = true
	}
	plan := memoryRetrievalPlan{
		Intent:     intent,
		QueryTerms: terms,
		Slots:      slots,
		Selected:   make([]memoryPlanSelection, 0, len(selected)),
	}
	for _, entry := range selected {
		layer := memoryLayer(entry)
		plan.Selected = append(plan.Selected, memoryPlanSelection{
			ID:     entry.ID,
			Title:  memoryLabel(entry),
			Layer:  layer,
			Reason: memorySelectionReason(entry, layer, terms),
		})
	}
	return plan
}

func filterAutoInjectMemoryByIntent(entries []MemoryEntry, intent string, terms []string, prompt string) []MemoryEntry {
	if len(entries) == 0 {
		return entries
	}
	out := make([]MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		if autoInjectMemoryAllowed(entry, intent, terms, prompt) {
			out = append(out, entry)
		}
	}
	return out
}

func autoInjectMemoryAllowed(entry MemoryEntry, intent string, terms []string, prompt string) bool {
	layer := memoryLayer(entry)
	kind := normaliseMemoryKind(entry.Kind)
	if kind == "preference" || kind == "constraint" || entry.Pinned {
		return true
	}
	if intent == "recall" || intent == "ops" {
		return true
	}
	if memoryLooksOpsScoped(entry) && !memoryOverlapsPromptTarget(entry, prompt) {
		return false
	}
	if layer == "previous_run" || layer == "unverified" || layer == "evidence" || layer == "session_recall" {
		return firstMemoryTermHit(entry, terms) != ""
	}
	if layer == "confirmed" && (intent == "code" || intent == "research") {
		return true
	}
	return firstMemoryTermHit(entry, terms) != "" || intent == "research"
}

func memoryLooksOpsScoped(entry MemoryEntry) bool {
	if len(memoryTargetRefs(entry)) > 0 {
		return true
	}
	for _, tag := range entry.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "htb" || tag == "ctf" || tag == "ops" || tag == "milestone" || tag == "evidence" {
			return true
		}
	}
	text := strings.ToLower(entry.Title + "\n" + entry.Content)
	source := normaliseMemorySource(entry.Source)
	if source != "user" && source != "manual" {
		for _, tag := range entry.Tags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if strings.HasPrefix(tag, "target-") || strings.HasPrefix(tag, "host-") || strings.HasPrefix(tag, "lab-") {
				return true
			}
		}
	}
	return containsAny(text, "htb", "ctf", "target:", "target ", "root flag", "user flag", "foothold", "webshell", "reverse shell", "10.")
}

func memoryOverlapsPromptTarget(entry MemoryEntry, prompt string) bool {
	refs := memoryTargetRefs(entry)
	if len(refs) == 0 {
		return false
	}
	promptRefs := map[string]bool{}
	addTargetRefs(promptRefs, prompt)
	return targetRefsOverlap(refs, promptRefs)
}

func planEntries(entries []MemoryEntry, plan memoryRetrievalPlan) []MemoryEntry {
	if len(plan.Selected) == 0 {
		return nil
	}
	byID := map[string]MemoryEntry{}
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	out := make([]MemoryEntry, 0, len(plan.Selected))
	now := time.Now().Format(time.RFC3339)
	for _, item := range plan.Selected {
		if entry, ok := byID[item.ID]; ok {
			entry.LastUsedAt = now
			out = append(out, entry)
		}
	}
	return out
}

func memoryRetrievalPlanDetail(plan memoryRetrievalPlan) string {
	if len(plan.Selected) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("intent=" + plan.Intent)
	if len(plan.QueryTerms) > 0 {
		sb.WriteString("; query_terms=" + strings.Join(plan.QueryTerms, ","))
	}
	for _, item := range plan.Selected {
		sb.WriteString("\n- ")
		sb.WriteString(item.Layer)
		sb.WriteString(": ")
		sb.WriteString(item.Title)
		if item.Reason != "" {
			sb.WriteString(" (")
			sb.WriteString(item.Reason)
			sb.WriteString(")")
		}
	}
	return sb.String()
}

func memoryRetrievalIntent(prompt string) string {
	lower := strings.ToLower(prompt)
	switch {
	case containsAny(lower, "previous", "prior", "remember", "last time", "resume", "where did we leave"):
		return "recall"
	case containsAny(lower, "pentest", "htb", "ctf", "exploit", "cve", "recon", "foothold", "privilege", "shell", "target"):
		return "ops"
	case containsAny(lower, "fix", "bug", "test", "build", "compile", "refactor", "implement", "code"):
		return "code"
	case containsAny(lower, "research", "latest", "current", "compare", "look up"):
		return "research"
	default:
		return "general"
	}
}

func memoryRetrievalSlots(intent string, limit int) map[string]int {
	slots := map[string]int{
		"preferences":  2,
		"confirmed":    3,
		"previous_run": 2,
		"unverified":   1,
	}
	switch intent {
	case "recall":
		slots["previous_run"] = 3
		slots["confirmed"] = 2
	case "ops":
		slots["confirmed"] = 3
		slots["previous_run"] = 2
		slots["unverified"] = 1
	case "code":
		slots["confirmed"] = 4
		slots["previous_run"] = 1
	case "research":
		slots["confirmed"] = 3
		slots["unverified"] = 2
		slots["previous_run"] = 1
	}
	total := 0
	for _, n := range slots {
		total += n
	}
	for total > limit {
		for _, layer := range []string{"unverified", "previous_run", "confirmed", "preferences"} {
			if total <= limit {
				break
			}
			if slots[layer] > 0 {
				slots[layer]--
				total--
			}
		}
	}
	return slots
}

func memoryLayer(entry MemoryEntry) string {
	if strings.HasPrefix(entry.ID, "session:") || hasTag(entry, "session-recall") {
		return "session_recall"
	}
	if strings.HasPrefix(entry.ID, "evidence:") || hasTag(entry, "evidence") {
		return "evidence"
	}
	confidence := normaliseMemoryConfidence(entry.Confidence)
	kind := normaliseMemoryKind(entry.Kind)
	source := normaliseMemorySource(entry.Source)
	switch {
	case confidence == "hypothesis" || confidence == "likely" || confidence == "stale":
		return "unverified"
	case kind == "preference" || kind == "constraint":
		return "preferences"
	case source == "previous_run" || source == "auto_distill":
		return "previous_run"
	default:
		return "confirmed"
	}
}

func countMemoryLayer(entries []MemoryEntry, layer string) int {
	n := 0
	for _, entry := range entries {
		if memoryLayer(entry) == layer {
			n++
		}
	}
	return n
}

func memorySelectionReason(entry MemoryEntry, layer string, terms []string) string {
	reasons := []string{layer}
	if entry.Pinned {
		reasons = append(reasons, "pinned")
	}
	if entry.Importance >= 4 {
		reasons = append(reasons, fmt.Sprintf("importance=%d", entry.Importance))
	}
	if hit := firstMemoryTermHit(entry, terms); hit != "" {
		reasons = append(reasons, "matched="+hit)
	}
	return strings.Join(reasons, ", ")
}

func firstMemoryTermHit(entry MemoryEntry, terms []string) string {
	title := strings.ToLower(entry.Title)
	content := strings.ToLower(entry.Content)
	tags := strings.ToLower(strings.Join(entry.Tags, " "))
	for _, term := range terms {
		if containsWordish(tags, term) || containsWordish(title, term) || containsWordish(content, term) {
			return term
		}
	}
	return ""
}

func sortedKeywordTerms(text string) []string {
	terms := keywordSet(text)
	out := make([]string, 0, len(terms))
	for term := range terms {
		out = append(out, term)
	}
	sort.Strings(out)
	return out
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// searchMemory powers the memory tool's recall action. Unlike relevantMemory it
// is not gated on AutoInject — the agent asked for it explicitly — but it shares
// the same scoring and usage-marking so recall and auto-injection stay coherent.
func searchMemory(query string, limit int) ([]MemoryEntry, error) {
	entries, err := loadMemory()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 6
	}
	return rankMemory(entries, query, limit), nil
}

func filterMemoryInjectionConflicts(entries []MemoryEntry, cfg settings.Settings, prompt string) ([]MemoryEntry, []MemoryEntry, []string) {
	current := currentTargetRefs(cfg, prompt)
	if len(current) == 0 {
		return entries, nil, nil
	}
	filtered := make([]MemoryEntry, 0, len(entries))
	var withheld []MemoryEntry
	var conflicts []string
	for _, entry := range entries {
		refs := memoryTargetRefs(entry)
		if len(refs) > 0 && !targetRefsOverlap(current, refs) {
			withheld = append(withheld, entry)
			conflicts = append(conflicts, fmt.Sprintf("%s references %s while current target context is %s", memoryLabel(entry), strings.Join(sortedRefKeys(refs), ","), strings.Join(sortedRefKeys(current), ",")))
			continue
		}
		if shouldWithholdSpoilerMemory(entry, cfg, prompt) {
			withheld = append(withheld, entry)
			conflicts = append(conflicts, fmt.Sprintf("%s looks like exact-machine spoiler/writeup reference and evidence policy is %s", memoryLabel(entry), normaliseEvidencePolicy(cfg.Context.Lab.EvidencePolicy, cfg.Context.Lab.OpsProfile)))
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered, withheld, conflicts
}

func currentTargetRefs(cfg settings.Settings, prompt string) map[string]bool {
	refs := map[string]bool{}
	addTargetRefs(refs, cfg.Context.Lab.Target)
	addTargetRefs(refs, cfg.Context.Lab.Hostname)
	addTargetRefs(refs, prompt)
	return refs
}

func shouldWithholdSpoilerMemory(entry MemoryEntry, cfg settings.Settings, prompt string) bool {
	if !memoryLooksLikeSpoiler(entry) {
		return false
	}
	lowerPrompt := strings.ToLower(prompt)
	if containsAny(lowerPrompt, "spoiler", "writeup", "walkthrough", "use reference", "fastest path", "cheat") {
		return false
	}
	switch normaliseEvidencePolicy(cfg.Context.Lab.EvidencePolicy, cfg.Context.Lab.OpsProfile) {
	case "discovery_first", "research_assisted":
		return true
	default:
		return false
	}
}

func memoryLooksLikeSpoiler(entry MemoryEntry) bool {
	if hasTag(entry, "spoiler") || hasTag(entry, "writeup") || hasTag(entry, "walkthrough") || hasTag(entry, "flag") {
		return true
	}
	text := strings.ToLower(strings.Join([]string{entry.Title, entry.Content, strings.Join(entry.Tags, " ")}, "\n"))
	return containsAny(text, "walkthrough", "write-up", "writeup", "spoiler", "flag path", "root flag", "user flag", "exact box")
}

func memoryTargetRefs(entry MemoryEntry) map[string]bool {
	refs := map[string]bool{}
	for _, tag := range entry.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		switch {
		case strings.HasPrefix(tag, "target-"):
			addTargetRefs(refs, strings.TrimPrefix(tag, "target-"))
		case strings.HasPrefix(tag, "target:"):
			addTargetRefs(refs, strings.TrimPrefix(tag, "target:"))
		case strings.HasPrefix(tag, "host-"):
			addTargetRefs(refs, strings.TrimPrefix(tag, "host-"))
		}
	}
	text := entry.Title + "\n" + entry.Content
	lower := strings.ToLower(text)
	if strings.Contains(lower, "target") || strings.Contains(lower, "host") || strings.Contains(lower, "engagement") || strings.Contains(lower, "htb") || hasTag(entry, "htb") || hasTag(entry, "run") || hasTag(entry, "milestone") {
		addTargetRefs(refs, text)
	}
	return refs
}

var ipv4RefRE = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
var hostRefRE = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9-]*(?:\.[a-z0-9][a-z0-9-]*)+\b`)

func addTargetRefs(refs map[string]bool, text string) {
	text = expandDashedIPv4Refs(text)
	for _, match := range ipv4RefRE.FindAllStringIndex(text, -1) {
		ip := text[match[0]:match[1]]
		if looksLikeVersionIPRef(text, match[0], match[1]) {
			continue
		}
		if validIPv4Ref(ip) {
			refs[strings.ToLower(ip)] = true
		}
	}
	for _, host := range hostRefRE.FindAllString(text, -1) {
		host = strings.Trim(strings.ToLower(host), ".,;:()[]{}<>\"'")
		if isTargetHostRef(host) {
			refs[host] = true
		}
	}
}

var dashedIPv4RefRE = regexp.MustCompile(`\b(?:\d{1,3}-){3}\d{1,3}\b`)

func expandDashedIPv4Refs(text string) string {
	return dashedIPv4RefRE.ReplaceAllStringFunc(text, func(match string) string {
		return strings.ReplaceAll(match, "-", ".")
	})
}

func validIPv4Ref(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 3 {
			return false
		}
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
			n = n*10 + int(r-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}

func isTargetHostRef(host string) bool {
	if host == "" {
		return false
	}
	if strings.HasSuffix(host, ".htb") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".test") || strings.Contains(host, ".htb.") {
		return true
	}
	return false
}

func targetRefsOverlap(a, b map[string]bool) bool {
	for ref := range a {
		if b[ref] {
			return true
		}
	}
	return false
}

func sortedRefKeys(refs map[string]bool) []string {
	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func memoryLabel(entry MemoryEntry) string {
	if strings.TrimSpace(entry.Title) != "" {
		return strings.TrimSpace(entry.Title)
	}
	if strings.TrimSpace(entry.ID) != "" {
		return strings.TrimSpace(entry.ID)
	}
	return "memory"
}

func hasTag(entry MemoryEntry, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, tag := range entry.Tags {
		if strings.ToLower(strings.TrimSpace(tag)) == want {
			return true
		}
	}
	return false
}

// rankMemory scopes, scores, sorts, and trims entries against a query, then marks
// the returned set as used. Shared by auto-injection and explicit recall.
func rankMemory(entries []MemoryEntry, query string, limit int) []MemoryEntry {
	out := rankMemoryCandidates(entries, query, limit)
	if len(out) > 0 {
		markMemoryUsed(out)
	}
	return out
}

func rankMemoryCandidates(entries []MemoryEntry, query string, limit int) []MemoryEntry {
	scope := workspaceScope()
	terms := keywordSet(query)
	type scored struct {
		entry MemoryEntry
		score float64
	}
	scoredEntries := make([]scored, 0, len(entries))
	for _, entry := range entries {
		if entry.Scope != "" && entry.Scope != scope {
			continue
		}
		score := scoreMemory(entry, terms, query)
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
	if limit <= 0 || len(scoredEntries) < limit {
		limit = len(scoredEntries)
	}
	out := make([]MemoryEntry, 0, limit)
	for i := 0; i < limit; i++ {
		entry := scoredEntries[i].entry
		entry.LastUsedAt = time.Now().Format(time.RFC3339)
		out = append(out, entry)
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
	"then": true, "attack": true, "diff": true, "different": true,
	"run": true, "runs": true, "prompt": true, "status": true, "stopped": true,
	"memory": true, "model": true, "file": true, "files": true, "inspect": true,
	"summarize": true, "summarise": true, "involved": true, "anything": true,
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
	switch normaliseMemoryConfidence(entry.Confidence) {
	case "confirmed":
		score += 0.35
	case "hypothesis":
		score -= 0.2
	case "stale":
		score -= 0.5
	}
	score += recencyBoost(entry.UpdatedAt)
	score += usageBoost(entry.LastUsedAt)
	if strings.Contains(strings.ToLower(prompt), title) && title != "" {
		score += 2
	}
	if refs := memoryTargetRefs(entry); len(refs) > 0 {
		promptRefs := map[string]bool{}
		addTargetRefs(promptRefs, prompt)
		if targetRefsOverlap(refs, promptRefs) {
			score += 5
		}
	}
	if memoryLooksLikeSpoiler(entry) {
		score -= 3
	}
	return score
}

// containsWordish reports whether term appears in haystack bounded by
// non-alphanumeric characters (or string edges). This avoids the substring trap
// where "cat" matched "category", while still matching "connected" inside
// "connected.htb" because "." is not alphanumeric. haystack is expected lowercased.
// memoryHasTermHit reports whether the entry textually matches at least one query
// term. Re-injection uses this to require a real keyword overlap rather than the
// importance/recency boosts alone, which would otherwise surface unrelated entries.
func memoryHasTermHit(entry MemoryEntry, terms map[string]bool) bool {
	if len(terms) == 0 {
		return false
	}
	title := strings.ToLower(entry.Title)
	content := strings.ToLower(entry.Content)
	tags := strings.ToLower(strings.Join(entry.Tags, " "))
	for term := range terms {
		if containsWordish(tags, term) || containsWordish(title, term) || containsWordish(content, term) {
			return true
		}
	}
	return false
}

func containsWordish(haystack, term string) bool {
	if term == "" {
		return false
	}
	from := 0
	for from <= len(haystack)-len(term) {
		idx := strings.Index(haystack[from:], term)
		if idx < 0 {
			return false
		}
		start := from + idx
		end := start + len(term)
		leftOK := start == 0 || !isAlphaNumByte(haystack[start-1])
		rightOK := end == len(haystack) || !isAlphaNumByte(haystack[end])
		if leftOK && rightOK {
			return true
		}
		from = start + 1
	}
	return false
}

func isAlphaNumByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
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

// usageBoost rewards entries that have actually been recalled/injected recently,
// so a frequently-used fact outranks a never-touched one of equal text relevance.
func usageBoost(lastUsedAt string) float64 {
	t, err := time.Parse(time.RFC3339, lastUsedAt)
	if err != nil {
		return 0
	}
	age := time.Since(t)
	switch {
	case age < 24*time.Hour:
		return 0.6
	case age < 7*24*time.Hour:
		return 0.3
	case age < 30*24*time.Hour:
		return 0.1
	default:
		return 0
	}
}

// evictionScore decides which entries survive when MaxEntries is exceeded. It
// favors pinned, important, recently-updated, and recently-used entries instead
// of the old UpdatedAt-only rule that dropped a heavily-used old fact before a
// never-touched recent one. Pinned entries get a dominating boost so they are
// effectively never evicted by capacity pressure.
func evictionScore(entry MemoryEntry) float64 {
	score := float64(entry.Importance)
	if entry.Pinned {
		score += 1000
	}
	score += recencyBoost(entry.UpdatedAt)
	score += usageBoost(entry.LastUsedAt)
	return score
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

func addLabMemoryTags(tags []string, lab settings.LabContext, entry MemoryEntry) []string {
	kind := normaliseMemoryKind(entry.Kind)
	source := normaliseMemorySource(entry.Source)
	if kind == "preference" && source == "user" {
		return tags
	}
	next := append([]string{}, tags...)
	if id := strings.TrimSpace(lab.ID); id != "" {
		next = append(next, "lab-"+slugMemoryTag(id))
	}
	if target := strings.TrimSpace(lab.Target); target != "" && !hasAnyTargetTag(entry) {
		for ref := range refsFromText(target) {
			next = append(next, "target-"+slugMemoryTag(ref))
			break
		}
	}
	if host := strings.TrimSpace(lab.Hostname); host != "" && !hasAnyHostTag(entry) {
		for ref := range refsFromText(host) {
			if strings.Contains(ref, ".") {
				next = append(next, "host-"+slugMemoryTag(ref))
				break
			}
		}
	}
	return normaliseTags(next)
}

func refsFromText(text string) map[string]bool {
	refs := map[string]bool{}
	addTargetRefs(refs, text)
	return refs
}

func hasAnyTargetTag(entry MemoryEntry) bool {
	for _, tag := range entry.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if strings.HasPrefix(tag, "target-") || strings.HasPrefix(tag, "target:") {
			return true
		}
	}
	return false
}

func hasAnyHostTag(entry MemoryEntry) bool {
	for _, tag := range entry.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if strings.HasPrefix(tag, "host-") || strings.HasPrefix(tag, "host:") {
			return true
		}
	}
	return false
}

func slugMemoryTag(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, ".", "-")
	value = strings.ReplaceAll(value, "_", "-")
	return strings.Trim(value, "-")
}

func normaliseMemoryKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "preference", "constraint", "fact", "workflow", "decision", "note":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "note"
	}
}

func normaliseMemoryConfidence(confidence string) string {
	switch strings.ToLower(strings.TrimSpace(confidence)) {
	case "confirmed", "likely", "hypothesis", "stale":
		return strings.ToLower(strings.TrimSpace(confidence))
	default:
		return "confirmed"
	}
}

func normaliseMemorySource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	source = strings.ReplaceAll(source, " ", "_")
	source = strings.ReplaceAll(source, "-", "_")
	switch source {
	case "user", "agent", "tool", "model", "previous_run", "auto_distill", "manual", "system":
		if source == "manual" {
			return "user"
		}
		return source
	default:
		return "user"
	}
}

func memoryPromptPrefix(entry MemoryEntry) string {
	switch normaliseMemoryConfidence(entry.Confidence) {
	case "hypothesis":
		return "UNVERIFIED hypothesis: "
	case "likely":
		return "Likely but unverified: "
	case "stale":
		return "STALE memory, verify before use: "
	default:
		return ""
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
	db, cleanup, err := memoryStore()
	if err == nil {
		defer cleanup()
		if _, err := migrateMemoryJSONToDB(db); err != nil {
			return nil, err
		}
		return loadMemoryDB(db)
	}
	return loadMemoryJSON()
}

func saveMemory(entries []MemoryEntry) error {
	db, cleanup, err := memoryStore()
	if err == nil {
		defer cleanup()
		if err := saveMemoryDB(db, entries); err != nil {
			return err
		}
		// Keep the legacy JSON file in sync with the SQLite store. The loader
		// still uses memory.json as a migration source when the DB is empty, so
		// an intentional clear must also blank the JSON copy or old memories will
		// be re-imported on the next app start.
		return saveMemoryJSON(entries)
	}
	return saveMemoryJSON(entries)
}

func memoryStore() (*sql.DB, func(), error) {
	memoryDBMu.RLock()
	db := memoryDB
	memoryDBMu.RUnlock()
	if db != nil {
		return db, func() {}, nil
	}
	db, err := store.OpenDefault()
	if err != nil {
		return nil, nil, err
	}
	return db, func() { _ = db.Close() }, nil
}

func migrateMemoryJSONToDB(db *sql.DB) (int, error) {
	if db == nil {
		return 0, nil
	}
	count, err := memoryCountDB(db)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	entries, err := loadMemoryJSON()
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	if err := saveMemoryDB(db, entries); err != nil {
		return 0, err
	}
	return len(entries), nil
}

func loadMemoryDB(db *sql.DB) ([]MemoryEntry, error) {
	rows, err := db.Query(`
SELECT id, scope, title, content, kind, confidence, source, importance, pinned, created_at, updated_at, last_used_at
FROM memory_entries
ORDER BY updated_at DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	var entries []MemoryEntry
	for rows.Next() {
		var entry MemoryEntry
		var pinned int
		if err := rows.Scan(&entry.ID, &entry.Scope, &entry.Title, &entry.Content, &entry.Kind, &entry.Confidence, &entry.Source, &entry.Importance, &pinned, &entry.CreatedAt, &entry.UpdatedAt, &entry.LastUsedAt); err != nil {
			rows.Close()
			return nil, err
		}
		entry.Pinned = pinned != 0
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	tags, err := loadMemoryTagsDB(db)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].Tags = tags[entries[i].ID]
	}
	if entries == nil {
		return []MemoryEntry{}, nil
	}
	return entries, nil
}

func loadMemoryTagsDB(db *sql.DB) (map[string][]string, error) {
	rows, err := db.Query(`SELECT memory_id, tag FROM memory_tags ORDER BY memory_id ASC, tag ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var id, tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, err
		}
		out[id] = append(out[id], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func saveMemoryDB(db *sql.DB, entries []MemoryEntry) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM memory_entries`); err != nil {
		_ = tx.Rollback()
		return err
	}
	entryStmt, err := tx.Prepare(`
INSERT INTO memory_entries (
  id, scope, title, content, kind, confidence, source, importance, pinned, created_at, updated_at, last_used_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer entryStmt.Close()
	tagStmt, err := tx.Prepare(`INSERT OR IGNORE INTO memory_tags (memory_id, tag) VALUES (?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer tagStmt.Close()
	for _, entry := range entries {
		if _, err := entryStmt.Exec(
			entry.ID,
			entry.Scope,
			entry.Title,
			entry.Content,
			normaliseMemoryKind(entry.Kind),
			normaliseMemoryConfidence(entry.Confidence),
			normaliseMemorySource(entry.Source),
			clampInt(entry.Importance, 1, 5, 3),
			boolInt(entry.Pinned),
			entry.CreatedAt,
			entry.UpdatedAt,
			entry.LastUsedAt,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		for _, tag := range normaliseTags(entry.Tags) {
			if _, err := tagStmt.Exec(entry.ID, tag); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

func memoryCountDB(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_entries`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func loadMemoryJSON() ([]MemoryEntry, error) {
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
	if err := json.Unmarshal(stripJSONBOM(data), &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		return []MemoryEntry{}, nil
	}
	return entries, nil
}

// stripJSONBOM removes a leading UTF-8 byte-order mark. Windows editors/tools can
// write memory.json with a BOM, which encoding/json rejects ("invalid character
// '?'") — that silently broke every memory recall (and the JSON→DB migration)
// until this was added.
func stripJSONBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

func saveMemoryJSON(entries []MemoryEntry) error {
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
