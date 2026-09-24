package app

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTaskRunGenerationsAreStrictlyMonotonic(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	first := nextTaskRunGeneration(now)
	second := nextTaskRunGeneration(now)
	if second <= first {
		t.Fatalf("generation did not advance: first=%d second=%d", first, second)
	}
	if second > 9_007_199_254_740_991 {
		t.Fatalf("generation is not JavaScript-safe: %d", second)
	}
}

func TestRunEventOwnerRoundTripsThroughToolContext(t *testing.T) {
	run := &TaskRun{ID: "task-image", Generation: 77, ConversationEpoch: 9, Origin: "desktop"}
	ctx := withRunEventOwner(context.Background(), run)
	owner, ok := runEventOwnerFromContext(ctx)
	if !ok || owner.RunID != run.ID || owner.Generation != run.Generation || owner.ConversationEpoch != run.ConversationEpoch || owner.Origin != run.Origin {
		t.Fatalf("unexpected contextual event owner: %#v ok=%t", owner, ok)
	}
	args := ownedEventArgs(owner, map[string]any{"status": "working"})
	if len(args) != 2 || args[1] != owner {
		t.Fatalf("secondary event did not preserve owner: %#v", args)
	}
}

func TestOwnedRunEventArgsPreservePayloadAndAppendOwner(t *testing.T) {
	run := &TaskRun{ID: "task-1", Generation: 42, ConversationEpoch: 3, Origin: "telegram"}
	args := ownedRunEventArgs(run, "answer", "done")
	if len(args) != 3 || args[0] != "answer" || args[1] != "done" {
		t.Fatalf("unexpected owned event args: %#v", args)
	}
	owner, ok := args[2].(runEventOwner)
	if !ok || owner.RunID != run.ID || owner.Generation != run.Generation || owner.ConversationEpoch != run.ConversationEpoch || owner.Origin != run.Origin {
		t.Fatalf("unexpected owner metadata: %#v", args[2])
	}
}

func TestConversationEpochRetiresOldRunOwners(t *testing.T) {
	app := &App{}
	first := app.currentConversationEpoch()
	run := &TaskRun{ID: "task-old", Generation: 1, ConversationEpoch: first}
	if !app.runOwnsCurrentConversation(run) {
		t.Fatal("new run did not own the current conversation")
	}
	app.mu.Lock()
	next := app.advanceConversationEpochLocked()
	app.mu.Unlock()
	if next <= first {
		t.Fatalf("conversation epoch did not advance: first=%d next=%d", first, next)
	}
	if app.runOwnsCurrentConversation(run) {
		t.Fatal("retired run still owns the replacement conversation")
	}
	current := &TaskRun{ID: "task-current", Generation: 2, ConversationEpoch: next}
	if !app.runOwnsCurrentConversation(current) {
		t.Fatal("current run was rejected by conversation ownership")
	}
}

func TestStaleRunEventsProduceDiagnosticsWithoutReopeningOwnership(t *testing.T) {
	app := &App{suppressEvents: true}
	oldEpoch := app.currentConversationEpoch()
	oldRun := &TaskRun{ID: "task-old", Generation: 10, ConversationEpoch: oldEpoch, Origin: "desktop"}
	app.mu.Lock()
	app.advanceConversationEpochLocked()
	app.mu.Unlock()

	app.emitRun(oldRun, "mauler:delta", "must not appear")
	diagnostics := app.GetRunEventDiagnostics()
	if diagnostics.StaleEventsDropped != 1 || diagnostics.LastRejection.Event != "mauler:delta" || diagnostics.LastRejection.RunID != oldRun.ID {
		t.Fatalf("unexpected stale-event diagnostics: %#v", diagnostics)
	}
	if diagnostics.LastRejection.RunEpoch != oldEpoch || diagnostics.LastRejection.ConversationEpoch <= oldEpoch {
		t.Fatalf("diagnostics lost epoch ownership: %#v", diagnostics)
	}
}

func TestConversationEpochConcurrentClearSwitchStopStyleRace(t *testing.T) {
	app := &App{suppressEvents: true}
	oldEpoch := app.currentConversationEpoch()
	oldRun := &TaskRun{ID: "task-race-old", Generation: 20, ConversationEpoch: oldEpoch, Origin: "desktop"}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < 100; n++ {
				app.emitRun(oldRun, "mauler:delta", "retired")
			}
		}()
	}
	close(start)
	app.mu.Lock()
	newEpoch := app.advanceConversationEpochLocked()
	app.mu.Unlock()
	wg.Wait()

	currentRun := &TaskRun{ID: "task-race-current", Generation: 21, ConversationEpoch: newEpoch, Origin: "desktop"}
	before := app.GetRunEventDiagnostics().StaleEventsDropped
	app.emitRun(currentRun, "mauler:stream_done", "current")
	after := app.GetRunEventDiagnostics().StaleEventsDropped
	if before == 0 {
		t.Fatal("race did not reject any retired events")
	}
	if after != before {
		t.Fatalf("current run was counted as stale: before=%d after=%d", before, after)
	}
}

func TestOwnedRunEventArgsRemainBackwardCompatibleWithoutOwner(t *testing.T) {
	args := ownedRunEventArgs(nil, "answer")
	if len(args) != 1 || args[0] != "answer" {
		t.Fatalf("nil run changed positional payload: %#v", args)
	}
}

func TestContextOwnedSecondaryEventRejectsRetiredEpoch(t *testing.T) {
	app := &App{suppressEvents: true}
	oldEpoch := app.currentConversationEpoch()
	oldRun := &TaskRun{ID: "task-secondary-old", Generation: 31, ConversationEpoch: oldEpoch, Origin: "desktop"}
	ctx := withRunEventOwner(context.Background(), oldRun)

	app.mu.Lock()
	app.advanceConversationEpochLocked()
	app.mu.Unlock()
	app.emitRunContext(ctx, "mauler:image_progress", map[string]any{"status": "late"})

	diagnostics := app.GetRunEventDiagnostics()
	if diagnostics.StaleEventsDropped != 1 || diagnostics.LastRejection.Event != "mauler:image_progress" || diagnostics.LastRejection.RunID != oldRun.ID {
		t.Fatalf("late secondary event was not rejected: %#v", diagnostics)
	}
}

func TestContextOwnedSecondaryEventAcceptsCurrentAndLegacyEvents(t *testing.T) {
	app := &App{suppressEvents: true}
	epoch := app.currentConversationEpoch()
	run := &TaskRun{ID: "task-secondary-current", Generation: 32, ConversationEpoch: epoch, Origin: "desktop"}

	app.emitRunContext(withRunEventOwner(context.Background(), run), "mauler:workspace_files_changed", "workspace")
	app.emitRunContext(context.Background(), "mauler:workspace_files_changed", "workspace")
	if diagnostics := app.GetRunEventDiagnostics(); diagnostics.StaleEventsDropped != 0 {
		t.Fatalf("current or legacy event was rejected: %#v", diagnostics)
	}
}
