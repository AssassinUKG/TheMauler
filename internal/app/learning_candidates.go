package app

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/store"
)

type LearningCandidate struct {
	ID         string   `json:"id"`
	RunID      string   `json:"run_id,omitempty"`
	Type       string   `json:"type"`
	Title      string   `json:"title"`
	Reason     string   `json:"reason"`
	Content    string   `json:"content"`
	Kind       string   `json:"kind"`
	Importance int      `json:"importance"`
	Tags       []string `json:"tags"`
	Evidence   []string `json:"evidence,omitempty"`
	Template   string   `json:"template,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

func (a *App) ListLearningCandidates(limit int) ([]LearningCandidate, error) {
	if limit <= 0 {
		limit = 1000
	}
	events, err := a.ListLedgerEvents(limit)
	if err != nil {
		return nil, err
	}
	candidates := buildLearningCandidates(events)
	decisions, err := a.learningDecisionMap()
	if err != nil {
		return nil, err
	}
	if len(decisions) == 0 {
		return candidates, nil
	}
	filtered := candidates[:0]
	for _, candidate := range candidates {
		switch decisions[candidate.ID] {
		case "approved", "rejected", "deferred":
			continue
		default:
			filtered = append(filtered, candidate)
		}
	}
	return filtered, nil
}

func (a *App) RecordLearningDecision(candidate LearningCandidate, decision, reason string) error {
	decision = normaliseLearningDecision(decision)
	if decision == "" {
		return fmt.Errorf("learning decision must be approved, rejected, or deferred")
	}
	candidate.ID = strings.TrimSpace(candidate.ID)
	if candidate.ID == "" {
		return fmt.Errorf("learning candidate id is required")
	}
	db, cleanup, err := a.learningDB()
	if err != nil {
		return err
	}
	defer cleanup()
	_, err = db.Exec(`
INSERT INTO learning_decisions (candidate_id, run_id, type, title, decision, reason, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(candidate_id) DO UPDATE SET
  run_id=excluded.run_id,
  type=excluded.type,
  title=excluded.title,
  decision=excluded.decision,
  reason=excluded.reason,
  created_at=excluded.created_at`,
		candidate.ID,
		candidate.RunID,
		candidate.Type,
		candidate.Title,
		decision,
		strings.TrimSpace(reason),
		time.Now().Format(time.RFC3339),
	)
	if err != nil {
		return err
	}
	a.recordLedger(ledger.Event{
		Kind:    "learning_decision",
		Source:  "learning",
		Status:  decision,
		RunID:   candidate.RunID,
		Message: candidate.Title,
		Detail:  strings.TrimSpace(reason),
		Metadata: map[string]string{
			"id":   candidate.ID,
			"type": candidate.Type,
		},
	})
	return nil
}

func (a *App) learningDecisionMap() (map[string]string, error) {
	db, cleanup, err := a.learningDB()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	rows, err := db.Query(`SELECT candidate_id, decision FROM learning_decisions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, decision string
		if err := rows.Scan(&id, &decision); err != nil {
			return nil, err
		}
		out[id] = decision
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *App) learningDB() (*sql.DB, func(), error) {
	if a != nil && a.db != nil {
		return a.db, func() {}, nil
	}
	db, err := store.OpenDefault()
	if err != nil {
		return nil, nil, err
	}
	return db, func() { _ = db.Close() }, nil
}

func normaliseLearningDecision(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approve", "approved", "save", "saved":
		return "approved"
	case "reject", "rejected", "dismiss", "dismissed":
		return "rejected"
	case "defer", "deferred":
		return "deferred"
	default:
		return ""
	}
}

func buildLearningCandidates(events []ledger.Event) []LearningCandidate {
	out := make([]LearningCandidate, 0)
	seen := map[string]bool{}
	add := func(c LearningCandidate) {
		c.Title = strings.TrimSpace(c.Title)
		c.Content = sanitizeMilestoneMemory(strings.TrimSpace(c.Content))
		if c.Title == "" || c.Content == "" {
			return
		}
		if c.ID == "" {
			c.ID = "learn-" + slugify(c.Type+"-"+c.Title)
		}
		key := c.Type + "|" + c.RunID + "|" + c.Title + "|" + c.Content
		if seen[key] {
			return
		}
		seen[key] = true
		if c.Kind == "" {
			c.Kind = "note"
		}
		if c.Importance <= 0 {
			c.Importance = 3
		}
		c.Tags = normaliseTags(c.Tags)
		if c.CreatedAt == "" {
			c.CreatedAt = time.Now().Format(time.RFC3339)
		}
		out = append(out, c)
	}

	for _, event := range events {
		switch {
		case event.Kind == "learning_suggestion":
			add(skillCandidate(event))
		case isLearningProblemEvent(event):
			add(reflectionCandidate(event))
		case event.Kind == "web_research" && event.Status == "done":
			add(evidenceCandidate(event))
		case event.Kind == "artifact_done" && event.Status == "done":
			add(evidenceCandidate(event))
		case event.Kind == "memory_write":
			continue
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Importance == out[j].Importance {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].Importance > out[j].Importance
	})
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

func skillCandidate(event ledger.Event) LearningCandidate {
	title := strings.TrimSpace(event.Message)
	if title == "" {
		title = "Reusable skill suggestion"
	}
	return LearningCandidate{
		ID:         "learn-" + event.ID,
		RunID:      event.RunID,
		Type:       "skill",
		Title:      title,
		Reason:     firstNonEmpty(event.Detail, "This run looked reusable enough to capture as a skill."),
		Content:    firstNonEmpty(event.Output, event.Detail, event.Message),
		Kind:       "workflow",
		Importance: 4,
		Tags:       []string{"skill", "procedure", "suggested"},
		Template:   event.Output,
		CreatedAt:  event.Timestamp,
	}
}

func reflectionCandidate(event ledger.Event) LearningCandidate {
	title := reflectionTitle(event)
	content := []string{
		"Observed: " + title,
	}
	if event.Tool != "" {
		content = append(content, "Tool: "+event.Tool)
	}
	if event.Status != "" {
		content = append(content, "Status: "+event.Status)
	}
	if event.Error != "" {
		content = append(content, "Error: "+truncateLine(event.Error, 300))
	}
	if event.Detail != "" {
		content = append(content, "Detail: "+truncateLine(event.Detail, 400))
	}
	if event.Input != "" {
		content = append(content, "Input: "+truncateLine(event.Input, 220))
	}
	content = append(content, "Lesson: check this pattern before repeating the same action in a future run.")
	return LearningCandidate{
		ID:         "learn-" + event.ID,
		RunID:      event.RunID,
		Type:       "reflection",
		Title:      title,
		Reason:     "Problem event found in the ledger; saving it helps avoid repeating the same failure.",
		Content:    strings.Join(content, "\n"),
		Kind:       "constraint",
		Importance: reflectionImportance(event),
		Tags:       []string{"reflection", "failure", strings.ReplaceAll(event.Kind, "_", "-")},
		Evidence:   compactEvidence(event),
		CreatedAt:  event.Timestamp,
	}
}

func evidenceCandidate(event ledger.Event) LearningCandidate {
	title := "Evidence: " + firstNonEmpty(event.Tool, event.Kind)
	content := []string{
		"Source: " + firstNonEmpty(event.Source, "ledger"),
		"Kind: " + event.Kind,
	}
	if event.Tool != "" {
		content = append(content, "Tool: "+event.Tool)
	}
	if event.Input != "" {
		content = append(content, "Input: "+truncateLine(event.Input, 300))
	}
	if event.Output != "" {
		content = append(content, "Output summary: "+truncateLine(event.Output, 500))
	}
	return LearningCandidate{
		ID:         "learn-" + event.ID,
		RunID:      event.RunID,
		Type:       "evidence",
		Title:      title,
		Reason:     "Potential evidence or source material from a successful research/artifact event.",
		Content:    strings.Join(content, "\n"),
		Kind:       "fact",
		Importance: 3,
		Tags:       []string{"evidence", strings.ReplaceAll(event.Kind, "_", "-")},
		Evidence:   compactEvidence(event),
		CreatedAt:  event.Timestamp,
	}
}

func isLearningProblemEvent(event ledger.Event) bool {
	status := strings.ToLower(strings.TrimSpace(event.Status))
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	return event.Error != "" ||
		kind == "run_stop" ||
		kind == "tool_error" ||
		kind == "model_load" && (status == "retry" || status == "error" || status == "cancelled") ||
		status == "blocked" ||
		status == "denied" ||
		status == "unreachable" ||
		status == "exhausted"
}

func reflectionTitle(event ledger.Event) string {
	switch {
	case event.Kind == "model_load":
		return "Model load " + firstNonEmpty(event.Status, "problem")
	case event.Tool != "":
		return fmt.Sprintf("%s %s", event.Tool, firstNonEmpty(event.Status, "problem"))
	case event.Message != "":
		return truncateLine(event.Message, 80)
	default:
		return event.Kind + " " + firstNonEmpty(event.Status, "problem")
	}
}

func reflectionImportance(event ledger.Event) int {
	if event.Kind == "model_load" || event.Kind == "run_stop" {
		return 4
	}
	if event.Error != "" {
		return 4
	}
	return 3
}

func compactEvidence(event ledger.Event) []string {
	var out []string
	for _, value := range []string{
		event.ID,
		event.RunID,
		event.Tool,
		event.Error,
		event.Detail,
		event.Output,
	} {
		value = sanitizeMilestoneMemory(truncateLine(value, 240))
		if value != "" {
			out = append(out, value)
		}
		if len(out) >= 5 {
			break
		}
	}
	return out
}
