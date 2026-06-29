package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestToolResultOffloadRoundTrip(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := &App{}
	full := strings.Repeat("alpha ", 3000) + "MIDDLE-SECRET-NEEDLE " + strings.Repeat("omega ", 3000)
	handle, err := app.saveToolResult("run-1", "grep", full)
	if err != nil {
		t.Fatalf("saveToolResult: %v", err)
	}
	out, ok := app.loadToolResultSlice(handle, 0, 200)
	if !ok {
		t.Fatal("expected saved tool result to be readable")
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "total_chars=") {
		t.Fatalf("unexpected first slice: %q", out)
	}
	mid := strings.Index(full, "MIDDLE-SECRET-NEEDLE")
	out, ok = app.loadToolResultSlice(handle, mid, 80)
	if !ok || !strings.Contains(out, "MIDDLE-SECRET-NEEDLE") {
		t.Fatalf("middle slice missing needle: ok=%v out=%q", ok, out)
	}
}

func TestOffloadPreviewHasHeadTailAndHandle(t *testing.T) {
	full := "HEAD-" + strings.Repeat("x", 5000) + "-TAIL"
	preview := toolResultPreview(full, "run-1/result-1", 400)
	for _, want := range []string{"HEAD-", "-TAIL", "tool result offloaded", "result_id=run-1/result-1", "read_tool_result"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
	if strings.Contains(preview, strings.Repeat("x", 1000)) {
		t.Fatalf("preview retained too much middle data")
	}
}

func TestReadToolResultPaging(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := &App{}
	handle, err := app.saveToolResult("run-2", "shell", "0123456789abcdefghijklmnopqrstuvwxyz")
	if err != nil {
		t.Fatal(err)
	}
	tool := &readToolResultTool{app: app}
	raw, _ := json.Marshal(readToolResultArgs{ResultID: handle, Offset: 10, Limit: 5})
	out, err := tool.Run(nil, raw)
	if err != nil {
		t.Fatalf("read tool result: %v", err)
	}
	if !strings.Contains(out, "abcde") || strings.Contains(out, "01234") {
		t.Fatalf("unexpected paged output: %q", out)
	}
}

func TestToolResultForContextOffloadsLargeResult(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := &App{}
	cfg := settings.DefaultSettings().Tools
	cfg.MaxToolResultChars = 100
	cfg.ToolResultPreviewChars = 180
	full := "START-" + strings.Repeat("middle-", 80) + "END"
	got := app.toolResultForContext("run-3", "grep", full, cfg)
	if !strings.Contains(got, "result_id=run-3/grep-") || !strings.Contains(got, "START-") || !strings.Contains(got, "END") {
		t.Fatalf("offload preview missing expected pieces: %q", got)
	}
	idStart := strings.Index(got, "result_id=")
	if idStart < 0 {
		t.Fatal("missing result_id")
	}
	handle := strings.Fields(strings.TrimPrefix(got[idStart:], "result_id="))[0]
	handle = strings.TrimRight(handle, ".]")
	out, ok := app.loadToolResultSlice(handle, 0, len(full)+100)
	if !ok || !strings.Contains(out, full) {
		t.Fatalf("offloaded full result not recoverable: ok=%v out=%q", ok, out)
	}
}

func TestAggregateToolResultOffloadsLargestMessages(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := &App{}
	cfg := settings.DefaultSettings().Tools
	cfg.MaxToolResultChars = 10000
	cfg.ToolResultPreviewChars = 180
	cfg.ToolResultAggregateChars = 600
	bigA := "AAA-" + strings.Repeat("a", 900) + "-AEND"
	bigB := "BBB-" + strings.Repeat("b", 700) + "-BEND"
	msgs := []llm.Message{
		newToolResultMsg("call-a", "grep", bigA),
		newToolResultMsg("call-b", "read_file", bigB),
	}

	got := app.offloadToolResultMessagesForAggregate("run-aggregate", msgs, cfg)
	offloaded := 0
	var handle string
	for _, msg := range got {
		content, _ := msg.Content.(string)
		if strings.Contains(content, "[tool result offloaded:") {
			offloaded++
			idStart := strings.Index(content, "result_id=")
			handle = strings.Fields(strings.TrimPrefix(content[idStart:], "result_id="))[0]
			handle = strings.TrimRight(handle, ".]")
		}
	}
	if offloaded == 0 {
		t.Fatalf("expected aggregate policy to offload at least one message: %#v", got)
	}
	out, ok := app.loadToolResultSlice(handle, 0, 2000)
	if !ok || (!strings.Contains(out, "-AEND") && !strings.Contains(out, "-BEND")) {
		t.Fatalf("aggregate-offloaded result was not recoverable: ok=%v out=%q", ok, out)
	}
}

func TestAggregatePolicyLeavesExistingOffloadPreviewAlone(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := &App{}
	cfg := settings.DefaultSettings().Tools
	cfg.ToolResultPreviewChars = 180
	cfg.ToolResultAggregateChars = 100
	preview := toolResultPreview("HEAD-"+strings.Repeat("x", 1000)+"-TAIL", "run/existing", 180)
	msgs := []llm.Message{newToolResultMsg("call-a", "grep", preview)}

	got := app.offloadToolResultMessagesForAggregate("run-aggregate", msgs, cfg)
	if got[0].Content != preview {
		t.Fatalf("existing preview should not be re-offloaded:\n%q\n!=\n%q", got[0].Content, preview)
	}
}

func TestReadToolResultToolDefExposedThroughToolset(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	registry := tools.New()
	registry.Register(&readToolResultTool{})
	defs, _ := toolDefsAndChoiceForTurn(registry, cfg, "inspect the project", 0, 0)
	for _, def := range defs {
		if def.Function.Name == "read_tool_result" {
			return
		}
	}
	t.Fatalf("missing read_tool_result tool def")
}
