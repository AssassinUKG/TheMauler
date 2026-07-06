package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestProgressToolUpdateAppendRead(t *testing.T) {
	tmp := t.TempDir()
	restoreWorkingDir(t)
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	app := New()
	app.suppressEvents = true
	tool := &progressTool{app: app}

	_, err := tool.Run(context.Background(), json.RawMessage(`{"action":"update","content":"# Progress\n\n## Objective\n\nShip router.\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = tool.Run(context.Background(), json.RawMessage(`{"action":"append","section":"Next Steps","content":"Add run_script tests."}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Run(context.Background(), json.RawMessage(`{"action":"read"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# .mauler/progress.md", "Ship router.", "## Next Steps", "Add run_script tests."} {
		if !strings.Contains(out, want) {
			t.Fatalf("progress output missing %q:\n%s", want, out)
		}
	}
}
