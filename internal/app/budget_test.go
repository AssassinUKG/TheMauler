package app

import (
	"errors"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestTaskBudgetStopsAfterSearchLimit(t *testing.T) {
	b := newTaskBudget(settings.ToolsConfig{MaxSearches: 2, MaxFetches: 6, MaxFailedFetches: 3, MaxBrowserActions: 20})
	if msg := b.before("web_search"); msg != "" {
		t.Fatalf("first search blocked: %s", msg)
	}
	if msg := b.before("web_search"); msg != "" {
		t.Fatalf("second search blocked: %s", msg)
	}
	if msg := b.before("web_search"); !strings.Contains(msg, "budget exhausted") {
		t.Fatalf("third search was not budget-blocked: %q", msg)
	}
}

func TestTaskBudgetStopsAfterFailedWebAttempts(t *testing.T) {
	b := newTaskBudget(settings.ToolsConfig{MaxSearches: 4, MaxFetches: 6, MaxFailedFetches: 2, MaxBrowserActions: 20})
	b.after("web_search", `No results for "x" from DuckDuckGo.`, nil)
	b.after("fetch_url", "error: http 404 from https://example.com/nope", errors.New("http 404"))
	if msg := b.before("fetch_url"); !strings.Contains(msg, "web research stopped") {
		t.Fatalf("failed attempts did not stop web research: %q", msg)
	}
}

func TestTaskBudgetStopsBrowserActions(t *testing.T) {
	b := newTaskBudget(settings.ToolsConfig{MaxSearches: 4, MaxFetches: 6, MaxFailedFetches: 3, MaxBrowserActions: 1})
	if msg := b.before("browser_open"); msg != "" {
		t.Fatalf("first browser action blocked: %s", msg)
	}
	if msg := b.before("browser_snapshot"); !strings.Contains(msg, "browser automation budget exhausted") {
		t.Fatalf("second browser action was not budget-blocked: %q", msg)
	}
}
