package engagement

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

//go:embed workflows/*.json checklists/*.json
var embeddedPacks embed.FS

type Catalog struct {
	Workflows  map[string]WorkflowDefinition
	Checklists map[string]ChecklistDefinition
}

func LoadEmbeddedCatalog() (Catalog, error) {
	return LoadCatalog(embeddedPacks, "workflows/*.json", "checklists/*.json")
}

func LoadCatalog(fsys fs.FS, workflowPattern, checklistPattern string) (Catalog, error) {
	catalog := Catalog{
		Workflows:  map[string]WorkflowDefinition{},
		Checklists: map[string]ChecklistDefinition{},
	}
	workflowPaths, err := fs.Glob(fsys, workflowPattern)
	if err != nil {
		return Catalog{}, fmt.Errorf("glob workflows: %w", err)
	}
	checklistPaths, err := fs.Glob(fsys, checklistPattern)
	if err != nil {
		return Catalog{}, fmt.Errorf("glob checklists: %w", err)
	}
	sort.Strings(workflowPaths)
	sort.Strings(checklistPaths)
	if len(workflowPaths) == 0 {
		return Catalog{}, fmt.Errorf("no workflows matched %q", workflowPattern)
	}
	if len(checklistPaths) == 0 {
		return Catalog{}, fmt.Errorf("no checklists matched %q", checklistPattern)
	}
	for _, path := range checklistPaths {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return Catalog{}, fmt.Errorf("read checklist %s: %w", path, err)
		}
		checklist, err := ParseChecklist(data)
		if err != nil {
			return Catalog{}, fmt.Errorf("checklist %s: %w", path, err)
		}
		if _, exists := catalog.Checklists[checklist.ID]; exists {
			return Catalog{}, fmt.Errorf("duplicate checklist id %q", checklist.ID)
		}
		catalog.Checklists[checklist.ID] = checklist
	}
	for _, path := range workflowPaths {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return Catalog{}, fmt.Errorf("read workflow %s: %w", path, err)
		}
		workflow, err := ParseWorkflow(data)
		if err != nil {
			return Catalog{}, fmt.Errorf("workflow %s: %w", path, err)
		}
		if _, exists := catalog.Workflows[workflow.ID]; exists {
			return Catalog{}, fmt.Errorf("duplicate workflow id %q", workflow.ID)
		}
		checklist, ok := catalog.Checklists[workflow.Checklist]
		if !ok {
			return Catalog{}, fmt.Errorf("workflow %q references missing checklist %q", workflow.ID, workflow.Checklist)
		}
		if err := ValidateBundle(workflow, checklist); err != nil {
			return Catalog{}, err
		}
		catalog.Workflows[workflow.ID] = workflow
	}
	return catalog, nil
}

func (c Catalog) Bundle(workflowID string) (WorkflowDefinition, ChecklistDefinition, error) {
	workflow, ok := c.Workflows[workflowID]
	if !ok {
		return WorkflowDefinition{}, ChecklistDefinition{}, fmt.Errorf("unknown workflow %q", workflowID)
	}
	checklist, ok := c.Checklists[workflow.Checklist]
	if !ok {
		return WorkflowDefinition{}, ChecklistDefinition{}, fmt.Errorf("workflow %q references missing checklist %q", workflowID, workflow.Checklist)
	}
	return workflow, checklist, nil
}
