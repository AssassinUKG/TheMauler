package app

import (
	"context"
	"time"
)

// runEventOwner is appended to every live run-owned Wails event. Existing
// positional payloads stay compatible while the frontend can reject late
// deltas, tool results, stops, and completion events from a retired run.
type runEventOwner struct {
	RunID             string `json:"run_id"`
	Generation        uint64 `json:"generation"`
	ConversationEpoch uint64 `json:"conversation_epoch,omitempty"`
	Origin            string `json:"origin"`
}

type runEventOwnerContextKey struct{}

func eventOwnerForRun(run *TaskRun) (runEventOwner, bool) {
	if run == nil || run.ID == "" || run.Generation == 0 {
		return runEventOwner{}, false
	}
	return runEventOwner{
		RunID:             run.ID,
		Generation:        run.Generation,
		ConversationEpoch: run.ConversationEpoch,
		Origin:            firstNonEmpty(run.Origin, "desktop"),
	}, true
}

// currentConversationEpoch lazily initialises direct App values used by tests
// and binding generation while keeping production epochs process-monotonic.
func (a *App) currentConversationEpoch() uint64 {
	if a == nil {
		return 0
	}
	if current := a.conversationEpoch.Load(); current != 0 {
		return current
	}
	if a.conversationEpoch.CompareAndSwap(0, 1) {
		return 1
	}
	return a.conversationEpoch.Load()
}

// advanceConversationEpochLocked retires every run/event owner attached to the
// old transcript. Callers hold a.mu while replacing conversation-owned state.
func (a *App) advanceConversationEpochLocked() uint64 {
	a.currentConversationEpoch()
	return a.conversationEpoch.Add(1)
}

func (a *App) runOwnsCurrentConversation(run *TaskRun) bool {
	if a == nil || run == nil || run.ConversationEpoch == 0 {
		return true // backwards compatibility for imported pre-epoch runs/tests
	}
	return run.ConversationEpoch == a.currentConversationEpoch()
}

// withRunEventOwner carries immutable ownership into long-running tools whose
// secondary progress events are emitted below the main agent-loop call site.
func withRunEventOwner(ctx context.Context, run *TaskRun) context.Context {
	owner, ok := eventOwnerForRun(run)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, runEventOwnerContextKey{}, owner)
}

func runEventOwnerFromContext(ctx context.Context) (runEventOwner, bool) {
	if ctx == nil {
		return runEventOwner{}, false
	}
	owner, ok := ctx.Value(runEventOwnerContextKey{}).(runEventOwner)
	return owner, ok && owner.RunID != "" && owner.Generation != 0
}

func ownedEventArgs(owner runEventOwner, data ...interface{}) []interface{} {
	args := append([]interface{}{}, data...)
	if owner.RunID == "" || owner.Generation == 0 {
		return args
	}
	return append(args, owner)
}

// assistantTurnEvent commits one completed model response to the desktop
// transcript even when that response is followed by tools or another model
// turn. The final run response remains a separate terminal event.
type assistantTurnEvent struct {
	Content   string `json:"content"`
	Thinking  string `json:"thinking,omitempty"`
	Turn      int    `json:"turn"`
	ToolCalls int    `json:"tool_calls"`
}

func ownedRunEventArgs(run *TaskRun, data ...interface{}) []interface{} {
	owner, _ := eventOwnerForRun(run)
	return ownedEventArgs(owner, data...)
}

func (a *App) emitRun(run *TaskRun, event string, data ...interface{}) {
	if !a.runOwnsCurrentConversation(run) {
		a.recordStaleRunEvent(run, event)
		return
	}
	a.emit(event, ownedRunEventArgs(run, data...)...)
}

// emitRunContext is the secondary-event counterpart to emitRun. Long-running
// tools receive only a context after dispatch, so progress and artifact-refresh
// events must recover the immutable owner from that context and pass through
// the same backend epoch boundary. Tool calls outside an agent run retain their
// legacy unowned event shape.
func (a *App) emitRunContext(ctx context.Context, event string, data ...interface{}) {
	owner, ok := runEventOwnerFromContext(ctx)
	if !ok {
		a.emit(event, data...)
		return
	}
	if owner.ConversationEpoch != 0 && owner.ConversationEpoch != a.currentConversationEpoch() {
		a.recordStaleRunEvent(&TaskRun{
			ID: owner.RunID, Generation: owner.Generation,
			ConversationEpoch: owner.ConversationEpoch, Origin: owner.Origin,
		}, event)
		return
	}
	a.emit(event, ownedEventArgs(owner, data...)...)
}

// StaleRunEventRejection is the bounded, non-transcript audit record for the
// most recent event rejected by the backend conversation epoch.
type StaleRunEventRejection struct {
	Event             string `json:"event"`
	RunID             string `json:"run_id,omitempty"`
	Generation        uint64 `json:"generation,omitempty"`
	RunEpoch          uint64 `json:"run_epoch,omitempty"`
	ConversationEpoch uint64 `json:"conversation_epoch"`
	RejectedAt        string `json:"rejected_at"`
}

// RunEventDiagnostics is safe to expose in Doctor/Services. It contains no
// payload data and therefore cannot leak old transcript or tool content.
type RunEventDiagnostics struct {
	ConversationEpoch  uint64                 `json:"conversation_epoch"`
	StaleEventsDropped uint64                 `json:"stale_events_dropped"`
	LastRejection      StaleRunEventRejection `json:"last_rejection"`
}

func (a *App) recordStaleRunEvent(run *TaskRun, event string) {
	if a == nil {
		return
	}
	rejection := StaleRunEventRejection{
		Event:             event,
		ConversationEpoch: a.currentConversationEpoch(),
		RejectedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}
	if run != nil {
		rejection.RunID = run.ID
		rejection.Generation = run.Generation
		rejection.RunEpoch = run.ConversationEpoch
	}
	a.staleRunEventDrops.Add(1)
	a.runEventDiagMu.Lock()
	a.lastStaleRunEvent = rejection
	a.runEventDiagMu.Unlock()
}

// GetRunEventDiagnostics returns causal-ownership telemetry without adding any
// entries to Chat or the task transcript.
func (a *App) GetRunEventDiagnostics() RunEventDiagnostics {
	if a == nil {
		return RunEventDiagnostics{}
	}
	a.runEventDiagMu.RLock()
	last := a.lastStaleRunEvent
	a.runEventDiagMu.RUnlock()
	return RunEventDiagnostics{
		ConversationEpoch:  a.currentConversationEpoch(),
		StaleEventsDropped: a.staleRunEventDrops.Load(),
		LastRejection:      last,
	}
}
