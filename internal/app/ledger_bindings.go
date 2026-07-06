package app

import (
	"strings"
	"time"

	"mauler/internal/ledger"
)

func (a *App) recordLedger(event ledger.Event) {
	if a == nil || a.ledger == nil {
		return
	}
	if event.Timestamp == "" {
		event.Timestamp = time.Now().Format(time.RFC3339)
	}
	_, _ = a.ledger.Record(event)
}

func (a *App) ListLedgerEvents(limit int) ([]ledger.Event, error) {
	if limit <= 0 {
		limit = 200
	}
	if a == nil || a.ledger == nil {
		return []ledger.Event{}, nil
	}
	events, err := a.ledger.List(limit)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return []ledger.Event{}, nil
	}
	return events, nil
}

func (a *App) ClearLedgerEvents() error {
	if a == nil || a.ledger == nil {
		return nil
	}
	return a.ledger.Clear()
}

func (a *App) PruneLedgerEvents(scope string, ids []string) (int, error) {
	if a == nil || a.ledger == nil {
		return 0, nil
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	idSet := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			idSet[id] = true
		}
	}
	return a.ledger.Prune(func(event ledger.Event) bool {
		switch scope {
		case "ids", "filtered":
			return idSet[event.ID]
		case "problems", "errors":
			return isLedgerProblemEvent(event)
		case "tool_errors":
			return isLedgerProblemEvent(event) && (event.Source == "tool" || event.Kind == "tool_error" || event.Kind == "tool_result" || event.Tool != "")
		case "model_errors":
			return isLedgerProblemEvent(event) && (event.Kind == "model_load" || strings.HasPrefix(event.Kind, "provider_") || event.Source == "model")
		case "learning":
			return event.Kind == "learning_suggestion"
		default:
			return false
		}
	})
}

func isLedgerProblemEvent(event ledger.Event) bool {
	status := strings.ToLower(strings.TrimSpace(event.Status))
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	return event.Error != "" ||
		status == "error" ||
		status == "failed" ||
		status == "cancelled" ||
		status == "blocked" ||
		status == "denied" ||
		status == "unreachable" ||
		status == "exhausted" ||
		kind == "run_stop" ||
		kind == "tool_error" ||
		strings.Contains(kind, "error")
}
