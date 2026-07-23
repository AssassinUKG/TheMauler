package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mauler/internal/engagement"
	"mauler/internal/ledger"
)

const engagementClaimHeartbeatInterval = 45 * time.Second

type engagementClaimPulse struct {
	EngagementID string
	Work         engagement.WorkState
}

// startEngagementClaimHeartbeat keeps claims owned by one active run alive and
// releases unfinished work when that run exits cleanly. If the process crashes,
// no release runs and the persisted lease remains the recovery boundary.
func (a *App) startEngagementClaimHeartbeat(ctx context.Context, claimantID, claimantAlias, origin string) func() {
	claimantID = strings.TrimSpace(claimantID)
	if a == nil || a.engagements == nil || claimantID == "" || workspaceScope() == "" {
		return func() {}
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(engagementClaimHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				pulses, err := a.heartbeatWorkspaceClaims(heartbeatCtx, claimantID, engagement.DefaultClaimLease, time.Now())
				if err != nil {
					if heartbeatCtx.Err() == nil {
						a.recordEngagementHeartbeatError(claimantID, origin, err)
					}
					continue
				}
				if len(pulses) == 0 {
					continue
				}
				a.emit("mauler:engagement_changed", map[string]any{"claimant_id": claimantID, "heartbeat_count": len(pulses)})
				work := pulses[0].Work
				detail := fmt.Sprintf("Grid claim active: %s (%s via %s)", work.Title, firstNonEmpty(strings.TrimSpace(claimantAlias), claimantID), firstNonEmpty(strings.TrimSpace(origin), "desktop"))
				a.telegramNotifyRunState("working", detail)
			}
		}
	}()

	return func() {
		cancel()
		<-done
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer releaseCancel()
		pulses, err := a.releaseWorkspaceClaims(releaseCtx, claimantID, time.Now())
		if err != nil {
			a.recordEngagementHeartbeatError(claimantID, origin, err)
			return
		}
		if len(pulses) > 0 {
			a.emit("mauler:engagement_changed", map[string]any{"claimant_id": claimantID, "released_count": len(pulses)})
		}
	}
}

func (a *App) heartbeatWorkspaceClaims(ctx context.Context, claimantID string, lease time.Duration, now time.Time) ([]engagementClaimPulse, error) {
	if a == nil || a.engagements == nil {
		return nil, nil
	}
	items, err := a.engagements.List(ctx, workspaceScope())
	if err != nil {
		return nil, err
	}
	var pulses []engagementClaimPulse
	for _, item := range items {
		work, changed, err := a.engagements.HeartbeatClaimant(ctx, item.ID, claimantID, lease, now)
		if err != nil {
			return pulses, err
		}
		if changed {
			pulses = append(pulses, engagementClaimPulse{EngagementID: item.ID, Work: work})
		}
	}
	return pulses, nil
}

func (a *App) releaseWorkspaceClaims(ctx context.Context, claimantID string, now time.Time) ([]engagementClaimPulse, error) {
	if a == nil || a.engagements == nil {
		return nil, nil
	}
	items, err := a.engagements.List(ctx, workspaceScope())
	if err != nil {
		return nil, err
	}
	var pulses []engagementClaimPulse
	for _, item := range items {
		work, changed, err := a.engagements.ReleaseClaimant(ctx, item.ID, claimantID, now)
		if err != nil {
			return pulses, err
		}
		if changed {
			pulses = append(pulses, engagementClaimPulse{EngagementID: item.ID, Work: work})
		}
	}
	return pulses, nil
}

func (a *App) recordEngagementHeartbeatError(claimantID, origin string, err error) {
	if err == nil {
		return
	}
	a.recordLedger(ledger.Event{
		Kind: "engagement_claim_heartbeat", Source: "engagement", Status: "error",
		Message: strings.TrimSpace(claimantID), Error: err.Error(),
		Metadata: map[string]string{"claimant_id": strings.TrimSpace(claimantID), "origin": strings.TrimSpace(origin)},
	})
}
