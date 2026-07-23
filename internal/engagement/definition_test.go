package engagement

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadEmbeddedCatalogIncludesReviewedShiftgridPack(t *testing.T) {
	catalog, err := LoadEmbeddedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	workflow, checklist, err := catalog.Bundle("webapp-simple")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(workflow.Phases); got != 7 {
		t.Fatalf("workflow phases = %d, want 7", got)
	}
	if got := len(checklist.Items); got != 20 {
		t.Fatalf("checklist items = %d, want 20", got)
	}
	global, endpoint := 0, 0
	for _, item := range checklist.Items {
		switch item.Scope {
		case "global":
			global++
		case "per_endpoint":
			endpoint++
		}
	}
	if global != 12 || endpoint != 8 {
		t.Fatalf("checklist scopes global=%d endpoint=%d, want 12/8", global, endpoint)
	}
}

func TestRunCountAcceptsPositiveIntegerOrIndefinite(t *testing.T) {
	for _, tc := range []struct {
		input      string
		count      int
		indefinite bool
	}{
		{input: `1`, count: 1},
		{input: `3`, count: 3},
		{input: `"indefinite"`, indefinite: true},
	} {
		var got RunCount
		if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.input, err)
		}
		if got.EffectiveCount() != tc.count || got.Indefinite != tc.indefinite {
			t.Fatalf("unmarshal %s = %#v, want count=%d indefinite=%v", tc.input, got, tc.count, tc.indefinite)
		}
	}
	for _, input := range []string{`0`, `-1`, `1.5`, `true`, `"forever"`} {
		var got RunCount
		if err := json.Unmarshal([]byte(input), &got); err == nil {
			t.Fatalf("unmarshal %s unexpectedly succeeded: %#v", input, got)
		}
	}
}

func TestDefinitionValidationRejectsAmbiguousPacks(t *testing.T) {
	workflow := testWorkflow("step", RunCount{})
	checklist := testChecklist()
	workflow.Phases = append(workflow.Phases, workflow.Phases[0])
	if err := workflow.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate phase") {
		t.Fatalf("duplicate phase validation = %v", err)
	}

	workflow = testWorkflow("step", RunCount{})
	checklist.Items = append(checklist.Items, checklist.Items[0])
	if err := checklist.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate item") {
		t.Fatalf("duplicate checklist validation = %v", err)
	}

	workflow = testWorkflow("step", RunCount{})
	checklist = testChecklist()
	checklist.ID = "wrong"
	if err := ValidateBundle(workflow, checklist); err == nil || !strings.Contains(err.Error(), "references checklist") {
		t.Fatalf("bundle mismatch validation = %v", err)
	}
}

func TestCatalogRejectsMissingReferencedChecklist(t *testing.T) {
	files := fstest.MapFS{
		"workflows/w.json":  &fstest.MapFile{Data: []byte(`{"id":"w","name":"W","checklist":"missing","phases":[{"id":"p","name":"P","steps":[{"check":"s","title":"S"}]}]}`)},
		"checklists/c.json": &fstest.MapFile{Data: []byte(`{"id":"other","name":"Other","items":[{"id":"c","title":"C","scope":"global"}]}`)},
	}
	_, err := LoadCatalog(files, "workflows/*.json", "checklists/*.json")
	if err == nil || !strings.Contains(err.Error(), "missing checklist") {
		t.Fatalf("missing checklist load = %v", err)
	}
}

func testWorkflow(kind string, runs RunCount) WorkflowDefinition {
	return WorkflowDefinition{
		ID:        "workflow",
		Name:      "Workflow",
		Checklist: "checklist",
		Phases: []PhaseDefinition{
			{
				ID:   "phase-one",
				Name: "Phase One",
				Kind: kind,
				Steps: []StepDefinition{
					{Check: "first", Title: "First", Runs: runs},
					{Check: "second", Title: "Second"},
				},
			},
			{
				ID:    "phase-two",
				Name:  "Phase Two",
				Steps: []StepDefinition{{Check: "last", Title: "Last"}},
			},
		},
	}
}

func testChecklist() ChecklistDefinition {
	return ChecklistDefinition{
		ID:   "checklist",
		Name: "Checklist",
		Items: []CheckDefinition{
			{ID: "headers", Title: "Headers", Scope: "global"},
			{ID: "idor", Title: "IDOR", Scope: "per_endpoint"},
		},
	}
}
