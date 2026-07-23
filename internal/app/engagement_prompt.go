package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mauler/internal/engagement"
)

const maxEngagementPromptRunes = 1600

// buildEngagementPromptPacket is evaluated for every model turn, alongside the
// terminal execution-state packet. It deliberately includes only identity,
// compact progress, scope, notes excerpt, and the deterministic next action.
// Definitions, observations, raw evidence, and full endpoint/check lists remain
// on demand.
func (a *App) buildEngagementPromptPacket() string {
	if a == nil || a.engagements == nil {
		return ""
	}
	workspace := workspaceScope()
	if workspace == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	summaries, err := a.engagements.List(ctx, workspace)
	if err != nil || len(summaries) == 0 {
		return ""
	}
	record, err := a.engagements.Get(ctx, summaries[0].ID)
	if err != nil || record.State == nil {
		return ""
	}
	next, nextErr := a.engagements.Now(ctx, record.State.ID, time.Now())
	if refreshed, refreshErr := a.engagements.Get(ctx, record.State.ID); refreshErr == nil {
		record = refreshed
	}
	state := record.State
	stepsDone, globalDone, endpointDone := finishedEngagementCounts(state)
	phaseName := state.CurrentPhase
	for _, phase := range record.Workflow.Phases {
		if phase.ID == state.CurrentPhase {
			phaseName = phase.Name
			break
		}
	}

	var sb strings.Builder
	sb.WriteString("Active Engagement Grid packet (fresh, authoritative; do not infer progress from chat):\n")
	fmt.Fprintf(&sb, "- engagement: id=%s name=%s workflow=%s@%s checklist=%s@%s\n",
		state.ID, engagementPromptValue(state.Name, 80), record.Workflow.ID, record.Workflow.Version, record.Checklist.ID, record.Checklist.Version)
	fmt.Fprintf(&sb, "- phase: id=%s name=%s state_revision=%d\n", state.CurrentPhase, engagementPromptValue(phaseName, 80), state.Revision)
	fmt.Fprintf(&sb, "- progress: steps=%d/%d global_checks=%d/%d endpoint_checks=%d/%d endpoints=%d\n",
		stepsDone, len(state.Steps), globalDone, len(state.GlobalChecks), endpointDone, len(state.EndpointChecks), len(state.EndpointOrder))
	if len(state.Scope) > 0 {
		scope := append([]string(nil), state.Scope...)
		if len(scope) > 6 {
			scope = append(scope[:6], fmt.Sprintf("+%d more", len(state.Scope)-6))
		}
		fmt.Fprintf(&sb, "- scope: locked=%v entries=%s\n", state.ScopeLocked, engagementPromptValue(strings.Join(scope, ", "), 260))
	}
	if strings.TrimSpace(state.Notes) != "" {
		fmt.Fprintf(&sb, "- notes: revision=%d excerpt=%s\n", state.NotesRevision, engagementPromptValue(state.Notes, 360))
	} else {
		fmt.Fprintf(&sb, "- notes: revision=%d empty=true (use engagement get_notes/set_notes for the shared project risk model)\n", state.NotesRevision)
	}
	if nextErr != nil {
		label := "state_error"
		if errors.Is(nextErr, engagement.ErrPhaseBlocked) {
			label = "blocked"
		}
		fmt.Fprintf(&sb, "- next: %s=%s\n", label, engagementPromptValue(nextErr.Error(), 260))
	} else if next.Work != nil {
		fmt.Fprintf(&sb, "- next: action=%s kind=%s phase=%s endpoint=%s work_id=%s work_revision=%d title=%s\n",
			next.Action, next.Work.Ref.Kind, next.Work.Ref.PhaseID, next.Work.Ref.EndpointID,
			next.Work.Ref.ID, next.Work.Revision, engagementPromptValue(next.Work.Title, 140))
		if next.Work.Claim != nil {
			fmt.Fprintf(&sb, "- claim: claimant=%s lease_until=%s\n", next.Work.Claim.Claimant.ID, next.Work.Claim.LeaseUntil.UTC().Format(time.RFC3339))
		}
	} else {
		fmt.Fprintf(&sb, "- next: action=%s phase_complete=%v workflow_done=%v\n", next.Action, next.PhaseComplete, next.WorkflowDone)
	}
	sb.WriteString("- discipline: use engagement now/status/get_notes/set_notes/claim/observe/finish/advance for grid state. Perform the actual test with normal Mauler tools between claim and observe. Keep notes as a compact risk/context model rather than a command log. Do not use todo_write as a second copy of this workflow, do not finish without a terminal status plus concise observation, and do not expand locked scope.\n")
	return truncateRunes(sb.String(), maxEngagementPromptRunes)
}

func engagementPromptValue(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	return truncateRunes(value, limit)
}
