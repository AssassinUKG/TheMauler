package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mauler/internal/engagement"
)

// ListEngagements returns project-scoped summaries for the future Grid/Home
// views. The current workspace is authoritative; callers cannot enumerate a
// different project's state through this binding.
func (a *App) ListEngagements() ([]engagement.Summary, error) {
	if a == nil || a.engagements == nil {
		return []engagement.Summary{}, nil
	}
	workspace := workspaceScope()
	if workspace == "" {
		return nil, fmt.Errorf("current workspace is unavailable")
	}
	items, err := a.engagements.List(context.Background(), workspace)
	if items == nil && err == nil {
		items = []engagement.Summary{}
	}
	return items, err
}

func (a *App) GetEngagement(id string) (engagement.Record, error) {
	if a == nil || a.engagements == nil {
		return engagement.Record{}, fmt.Errorf("engagement service is unavailable")
	}
	return (&engagementTool{app: a}).resolveRecord(context.Background(), id)
}

func (a *App) GetEngagementNext(id string) (engagement.NextAction, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return engagement.NextAction{}, err
	}
	return a.engagements.Now(context.Background(), record.State.ID, time.Now())
}

func (a *App) GetEngagementAvailable(id string, limit int) (engagement.AvailableWork, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return engagement.AvailableWork{}, err
	}
	return a.engagements.Available(context.Background(), record.State.ID, "", limit, time.Now())
}

func (a *App) CreateEngagement(name, workflowID string, scope []string) (engagement.Record, error) {
	if a == nil || a.engagements == nil {
		return engagement.Record{}, fmt.Errorf("engagement service is unavailable")
	}
	workspace := workspaceScope()
	if workspace == "" {
		return engagement.Record{}, fmt.Errorf("current workspace is unavailable")
	}
	lab := a.engagementLabContext()
	lockedScope, err := lockedAuthoritativeScope(lab, scope)
	if err != nil {
		return engagement.Record{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = firstNonEmpty(strings.TrimSpace(lab.Name), "Engagement")
	}
	workflowID = firstNonEmpty(strings.TrimSpace(workflowID), "webapp-simple")
	if err := a.refreshEngagementCatalog(); err != nil {
		return engagement.Record{}, err
	}
	return a.engagements.Create(context.Background(), engagement.CreateInput{
		Name: name, Workspace: workspace, WorkflowID: workflowID, Scope: lockedScope, ScopeLocked: true,
	}, time.Now())
}

func (a *App) AddEngagementEndpoint(id string, input engagement.EndpointInput) (engagement.Endpoint, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return engagement.Endpoint{}, err
	}
	return a.engagements.AddEndpoint(context.Background(), record.State.ID, input, time.Now())
}

func (a *App) SetEngagementEndpointGroup(id, endpointID, group string) (engagement.Endpoint, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return engagement.Endpoint{}, err
	}
	return a.engagements.SetEndpointGroup(context.Background(), record.State.ID, endpointID, group, time.Now())
}

func (a *App) SetEngagementNotes(id, notes string, expectedNotesRevision uint64) (uint64, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return 0, err
	}
	return a.engagements.SetNotes(context.Background(), record.State.ID, notes, expectedNotesRevision, time.Now())
}

func (a *App) AddEngagementEvidence(id string, input engagement.EvidenceInput) (engagement.Evidence, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return engagement.Evidence{}, err
	}
	work := engagementWorkState(record.State, input.Work)
	if work == nil || work.Claim == nil {
		return engagement.Evidence{}, fmt.Errorf("claim the work item before attaching evidence")
	}
	input.EngagementID = record.State.ID
	input.Claimant = work.Claim.Claimant
	input.OperatorTrusted = true
	return a.engagements.AddEvidence(context.Background(), input, time.Now())
}

func (a *App) UpsertEngagementFinding(id string, input engagement.FindingInput) (engagement.Finding, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return engagement.Finding{}, err
	}
	work := engagementWorkState(record.State, input.Work)
	if work == nil || work.Claim == nil {
		return engagement.Finding{}, fmt.Errorf("claim the work item before editing a finding")
	}
	input.EngagementID = record.State.ID
	input.Claimant = work.Claim.Claimant
	return a.engagements.UpsertFinding(context.Background(), input, time.Now())
}

func (a *App) ConfirmEngagementFinding(id, findingID string, expectedRevision uint64, operatorWaiver string) (engagement.Finding, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return engagement.Finding{}, err
	}
	finding := record.State.Findings[strings.TrimSpace(findingID)]
	if finding == nil {
		return engagement.Finding{}, fmt.Errorf("unknown finding %q", findingID)
	}
	work := engagementWorkState(record.State, finding.Work)
	if work == nil || work.Claim == nil {
		return engagement.Finding{}, fmt.Errorf("finding work item is not actively claimed")
	}
	return a.engagements.ConfirmFinding(context.Background(), record.State.ID, finding.ID, work.Claim.Claimant.ID, expectedRevision, operatorWaiver, true, time.Now())
}

func (a *App) ReleaseEngagementClaim(id, claimantID string) (engagement.WorkState, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return engagement.WorkState{}, err
	}
	work, released, err := a.engagements.ReleaseClaimant(context.Background(), record.State.ID, strings.TrimSpace(claimantID), time.Now())
	if err != nil {
		return engagement.WorkState{}, err
	}
	if !released {
		return engagement.WorkState{}, fmt.Errorf("claim %q is no longer active", claimantID)
	}
	a.emit("mauler:engagement_changed", map[string]any{"engagement_id": record.State.ID, "claimant_id": claimantID, "released_count": 1})
	return work, nil
}

func engagementWorkState(state *engagement.State, ref engagement.WorkRef) *engagement.WorkState {
	if state == nil {
		return nil
	}
	switch ref.Kind {
	case engagement.WorkStep:
		return state.Steps[ref.Key()]
	case engagement.WorkGlobalCheck:
		return state.GlobalChecks[ref.Key()]
	case engagement.WorkEndpointCheck:
		return state.EndpointChecks[ref.Key()]
	default:
		return nil
	}
}

func (a *App) DeleteEngagement(id string) error {
	record, err := a.GetEngagement(id)
	if err != nil {
		return err
	}
	return a.engagements.Delete(context.Background(), record.State.ID)
}

func (a *App) ExportEngagementJSON(id string) (string, error) {
	record, err := a.GetEngagement(id)
	if err != nil {
		return "", err
	}
	return a.engagements.ExportJSON(context.Background(), record.State.ID, time.Now())
}

func (a *App) ImportEngagementJSON(raw string) (engagement.Record, error) {
	if a == nil || a.engagements == nil {
		return engagement.Record{}, fmt.Errorf("engagement service is unavailable")
	}
	workspace := workspaceScope()
	if workspace == "" {
		return engagement.Record{}, fmt.Errorf("current workspace is unavailable")
	}
	return a.engagements.ImportJSON(context.Background(), raw, workspace, time.Now())
}
