package checkpacks

import (
	"reflect"
	"sort"
	"strings"

	"mauler/internal/engagement"
)

type ChecklistDiff struct {
	FromVersion     string         `json:"from_version,omitempty"`
	ToVersion       string         `json:"to_version,omitempty"`
	MetadataChanged bool           `json:"metadata_changed"`
	Added           []string       `json:"added,omitempty"`
	Removed         []string       `json:"removed,omitempty"`
	Changed         []ChangedCheck `json:"changed,omitempty"`
}

type ChangedCheck struct {
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
}

func Diff(from, to engagement.ChecklistDefinition) ChecklistDiff {
	diff := ChecklistDiff{
		FromVersion: from.Version,
		ToVersion:   to.Version,
		MetadataChanged: from.Name != to.Name || from.Description != to.Description ||
			!reflect.DeepEqual(from.Source, to.Source) || from.SchemaVersion != to.SchemaVersion,
	}
	oldItems := indexChecks(from.Items)
	newItems := indexChecks(to.Items)
	for id, oldCheck := range oldItems {
		newCheck, exists := newItems[id]
		if !exists {
			diff.Removed = append(diff.Removed, id)
			continue
		}
		if fields := changedFields(oldCheck, newCheck); len(fields) > 0 {
			diff.Changed = append(diff.Changed, ChangedCheck{ID: id, Fields: fields})
		}
	}
	for id := range newItems {
		if _, exists := oldItems[id]; !exists {
			diff.Added = append(diff.Added, id)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Slice(diff.Changed, func(i, j int) bool { return diff.Changed[i].ID < diff.Changed[j].ID })
	return diff
}

func (d ChecklistDiff) Empty() bool {
	return !d.MetadataChanged && len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}

func indexChecks(checks []engagement.CheckDefinition) map[string]engagement.CheckDefinition {
	indexed := make(map[string]engagement.CheckDefinition, len(checks))
	for _, check := range checks {
		indexed[strings.TrimSpace(check.ID)] = check
	}
	return indexed
}

func changedFields(a, b engagement.CheckDefinition) []string {
	fields := []struct {
		name    string
		changed bool
	}{
		{"title", a.Title != b.Title},
		{"category", a.Category != b.Category || a.CategoryName != b.CategoryName},
		{"scope", a.Scope != b.Scope},
		{"description", a.Description != b.Description},
		{"examples", a.Examples != b.Examples},
		{"mappings", !reflect.DeepEqual(a.Mappings, b.Mappings)},
		{"applicability", !reflect.DeepEqual(a.AppliesWhen, b.AppliesWhen)},
		{"procedure", !reflect.DeepEqual(a.Prerequisites, b.Prerequisites) || !reflect.DeepEqual(a.Procedure, b.Procedure)},
		{"signals", !reflect.DeepEqual(a.ExpectedSignals, b.ExpectedSignals) || !reflect.DeepEqual(a.NegativeSignals, b.NegativeSignals)},
		{"false_positive_notes", !reflect.DeepEqual(a.FalsePositiveNotes, b.FalsePositiveNotes)},
		{"safety", a.Safety != b.Safety},
		{"evidence", !reflect.DeepEqual(a.Evidence, b.Evidence)},
		{"automation", !reflect.DeepEqual(a.Automation, b.Automation)},
		{"lifecycle", a.Maturity != b.Maturity || a.Verified != b.Verified || a.Deprecated != b.Deprecated || a.SupersededBy != b.SupersededBy},
		{"execution", a.EstimatedMinutes != b.EstimatedMinutes || a.Repeatable != b.Repeatable || !reflect.DeepEqual(a.Runs, b.Runs) || a.ProducesEndpoints != b.ProducesEndpoints},
	}
	out := []string{}
	for _, field := range fields {
		if field.changed {
			out = append(out, field.name)
		}
	}
	return out
}
