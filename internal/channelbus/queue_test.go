package channelbus

import (
	"path/filepath"
	"testing"

	"mauler/internal/store"
)

func TestPersistentQueueSurvivesNewInstance(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	q1 := NewPersistentQueue(db)
	item := q1.Enqueue(Envelope{Source: "telegram", SessionID: "telegram:direct:1", Text: "/run test"}, Route{Lane: LaneWork, Command: "run", Policy: WorkQueueIfBusy})
	if item.ID == "" {
		t.Fatal("expected queued item id")
	}

	q2 := NewPersistentQueue(db)
	items := q2.List()
	if len(items) != 1 {
		t.Fatalf("expected one persisted item, got %d", len(items))
	}
	if items[0].Envelope.Text != "/run test" || items[0].Route.Command != "run" {
		t.Fatalf("persisted item mismatch: %+v", items[0])
	}

	popped, ok := q2.PopNext()
	if !ok {
		t.Fatal("expected pop")
	}
	if popped.ID != item.ID || popped.Status != "dispatching" {
		t.Fatalf("unexpected popped item: %+v", popped)
	}

	q2.Mark(item.ID, "done")
	items = q2.List()
	if len(items) != 1 || items[0].Status != "done" {
		t.Fatalf("mark did not persist: %+v", items)
	}
	if active := q2.ListActive(); len(active) != 0 {
		t.Fatalf("done items should not appear in active queue: %+v", active)
	}
}
