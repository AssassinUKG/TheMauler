package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mauler/internal/engagement"
	"mauler/internal/ledger"
)

// enforceEngagementScope is deliberately placed at the structured tool
// boundary. Shell parsing remains advisory until it can be made reliable; an
// explicit URL tool can and should fail closed while a locked grid is active.
func (a *App) enforceEngagementScope(ctx context.Context, toolName, target string) error {
	if a == nil || a.engagements == nil {
		return nil
	}
	workspace := workspaceScope()
	if workspace == "" {
		return nil
	}
	items, err := a.engagements.List(ctx, workspace)
	if err != nil || len(items) == 0 {
		return err
	}
	record, err := a.engagements.Get(ctx, items[0].ID)
	if err != nil || record.State == nil || !record.State.ScopeLocked {
		return err
	}
	decision := engagement.CheckTargetScope(record.State.Scope, target)
	status := "allowed"
	if !decision.Allowed {
		status = "denied"
	}
	a.recordLedger(ledger.Event{
		Kind: "engagement_scope_decision", Source: "engagement", Tool: strings.TrimSpace(toolName), Status: status,
		State: record.State.CurrentPhase, Message: decision.Reason, Detail: strings.TrimSpace(target),
		Metadata: map[string]string{
			"engagement_id": record.State.ID, "workspace": record.Workspace, "target": strings.TrimSpace(target),
			"matched_scope": decision.Matched,
		}, Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if !decision.Allowed {
		return fmt.Errorf("%s: target blocked by locked engagement scope: %s", toolName, decision.Reason)
	}
	return nil
}
