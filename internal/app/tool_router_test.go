package app

import (
	"testing"

	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestOpsToolRouterKeepsFirstTurnLeanEvenWhenUnrestricted(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	selected := selectToolsForTurn(cfg, "continue the HTB box and inspect the webshell output", 0, 0)

	for _, want := range []string{
		"todo_write",
		"read_tool_result", reasoningEffortToolName,
		"memory", "session_search", "progress",
		"read", "glob", "grep",
		"shell", "terminal_send", "terminal_read",
		"http_probe",
	} {
		if !selected[want] {
			t.Fatalf("Ops first-turn selection missing %s: %#v", want, selected)
		}
	}
	for _, notWant := range []string{
		"bash", "run_script", "read_many", "read_chunks", "read_pdf", "read_file",
		"web_search", "fetch_url",
		"browser_open", "browser_agent",
		"subagent_research", "subagent_explore", "subagent_testfix",
		"evidence_bundle", "start_listener", "todo_clear", "skills_list", "skill_view", "terminal_run",
	} {
		if selected[notWant] {
			t.Fatalf("Ops first-turn selection should not include %s: %#v", notWant, selected)
		}
	}
	if got, wantMax := len(selected), 18; got > wantMax {
		t.Fatalf("Ops first-turn selection too broad: got %d tools, want <= %d: %#v", got, wantMax, selected)
	}
}

func TestOpsToolRouterKeepsContinuationLean(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	selected := selectToolsForTurn(cfg, "continue the HTB box and inspect the webshell output", 1, 4)

	for _, want := range []string{
		"shell", "terminal_send", "terminal_read",
		"read", "glob", "grep",
		"memory", "session_search", "http_probe", "progress", "evidence_bundle",
	} {
		if !selected[want] {
			t.Fatalf("Ops continuation selection missing %s: %#v", want, selected)
		}
	}
	for _, notWant := range []string{
		"bash", "run_script", "read_many", "read_chunks", "read_pdf", "start_listener",
		"write_file", "edit_file", "write", "edit", "skills_list", "skill_view", "terminal_run",
		"subagent_explore", "subagent_testfix", "subagent_summarize",
		"browser_agent", "web_search", "fetch_url",
	} {
		if selected[notWant] {
			t.Fatalf("Ops continuation should not add %s without explicit intent: %#v", notWant, selected)
		}
	}
	if got, wantMax := len(selected), 19; got > wantMax {
		t.Fatalf("Ops continuation selection too broad: got %d tools, want <= %d: %#v", got, wantMax, selected)
	}
}

func TestExplicitOpsResearchAddsWebButNotBrowserAgent(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	selected := selectToolsForTurn(cfg, "research current CVE details for this HTB service and verify exploit path", 0, 0)

	for _, want := range []string{"web_search", "fetch_url", "task"} {
		if !selected[want] {
			t.Fatalf("explicit research should include %s: %#v", want, selected)
		}
	}
	if selected["browser"] || selected["browser_agent"] || selected["browser_click"] {
		t.Fatalf("browser automation should need explicit browser intent: %#v", selected)
	}
}

func TestPublicCVELookupRoutesWebToolsInsteadOfMasterSkill(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	prompt := "find and reachsearch this poc for me CVE-2026-50522 its exploited in the wild now"

	selected := selectToolsForTurn(cfg, prompt, 0, 0)
	for _, want := range []string{"web_search", "fetch_url"} {
		if !selected[want] {
			t.Fatalf("public CVE/PoC lookup should include %s: %#v", want, selected)
		}
	}
	if selected["skill"] {
		t.Fatalf("public CVE/PoC lookup should not offer the broad master skill: %#v", selected)
	}
	if !explicitWebResearchIntent(prompt) || looksShellCentricTask(prompt) {
		t.Fatalf("public CVE/PoC lookup was not separated from target-shell work")
	}

	registry := tools.New()
	defs, choice := toolDefsAndChoiceForTurn(registry, cfg, prompt, 0, 0)
	if choice != "required" {
		t.Fatalf("first lookup turn choice = %q, want required", choice)
	}
	if !toolCallAdvertised(defs, "web_search") || !toolCallAdvertised(defs, "fetch_url") || toolCallAdvertised(defs, "skill") {
		t.Fatalf("bad public lookup definitions: %s", toolProtocolToolNames(defs))
	}
}

func TestOpsToolRouterUsesPhaseSpecificToolsets(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	cases := []struct {
		name    string
		prompt  string
		want    []string
		notWant []string
		max     int
	}{
		{
			name:   "recon",
			prompt: "continue HTB recon with nmap service enumeration",
			want:   []string{"shell", "terminal_send", "terminal_read", "http_probe", "grep", "progress"},
			notWant: []string{
				"start_listener", "write", "edit", "web_search", "browser",
			},
			max: 15,
		},
		{
			name:    "webshell",
			prompt:  "use the working webshell URL to run id and inspect foothold",
			want:    []string{"shell", "terminal_send", "terminal_read", "http_probe", "read_tool_result", "progress"},
			notWant: []string{"web_search", "browser", "run_script", "write"},
			max:     17,
		},
		{
			name:    "reverse shell",
			prompt:  "start listener and catch a reverse shell callback with LHOST and LPORT",
			want:    []string{"start_listener", "terminal_read", "terminal_send", "shell", "progress"},
			notWant: []string{"http_probe", "web_search", "browser", "run_script", "write"},
			max:     17,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selected := selectToolsForTurn(cfg, tc.prompt, 0, 0)
			for _, want := range tc.want {
				if !selected[want] {
					t.Fatalf("missing %s for %s phase: %#v", want, tc.name, selected)
				}
			}
			for _, notWant := range tc.notWant {
				if selected[notWant] {
					t.Fatalf("%s phase should not include %s: %#v", tc.name, notWant, selected)
				}
			}
			if got := len(selected); got > tc.max {
				t.Fatalf("%s phase too broad: got %d want <= %d: %#v", tc.name, got, tc.max, selected)
			}
		})
	}
}

func TestOpsReportPhasePrefersFilesAndEvidence(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	selected := selectToolsForTurn(cfg, "create the HTB writeup report from current evidence", 0, 0)
	for _, want := range []string{"read", "grep", "write", "edit", "http_probe", "evidence_bundle", "memory", "progress"} {
		if !selected[want] {
			t.Fatalf("report phase missing %s: %#v", want, selected)
		}
	}
	for _, notWant := range []string{"terminal_send", "start_listener", "web_search", "browser"} {
		if selected[notWant] {
			t.Fatalf("report phase should not include %s without explicit intent: %#v", notWant, selected)
		}
	}
}

func TestAnswerOnlyFileReportUsesInspectionTools(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	for _, prompt := range []string{
		"Create a concise table of endpoints from the attached swagger file",
		"Generate a coverage report from the attached OpenAPI file and show me the totals",
		"Tell me what this config file contains and summarise it",
	} {
		selected := selectToolsForTurn(cfg, prompt, 0, 0)
		for _, want := range []string{"read", "glob", "grep"} {
			if !selected[want] {
				t.Fatalf("answer-only file route missing %s for %q: %#v", want, prompt, selected)
			}
		}
		for _, notWant := range []string{"write", "edit", "shell", "run_script", "terminal_send", "start_listener", "evidence_bundle"} {
			if selected[notWant] {
				t.Fatalf("answer-only file route advertised %s for %q: %#v", notWant, prompt, selected)
			}
		}
	}
}

func TestMixedReportAndSaveKeepsWriteTools(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	selected := selectToolsForTurn(cfg, "Generate a coverage report and save it to a file", 0, 0)
	for _, want := range []string{"read", "write", "edit"} {
		if !selected[want] {
			t.Fatalf("saved report route missing %s: %#v", want, selected)
		}
	}
}

func TestReportPhaseDoesNotAdvertiseTerminalSend(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	app := New()
	app.suppressEvents = true

	defs, _ := toolDefsAndChoiceForTurnWithState(app.registry, cfg, "finish the Connected writeup from current evidence", 0, 0, TerminalStateSnapshot{State: "ready"})
	if toolCallAdvertised(defs, "terminal_send") {
		t.Fatalf("report phase should not advertise terminal_send: %s", toolProtocolToolNames(defs))
	}
	if !toolCallAdvertised(defs, "http_probe") {
		t.Fatalf("report phase should keep http_probe for evidence checks: %s", toolProtocolToolNames(defs))
	}
}

func TestReportPhaseWithLiveTerminalAdvertisesExecutionTools(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	app := New()
	app.suppressEvents = true

	defs, _ := toolDefsAndChoiceForTurnWithState(app.registry, cfg, "finish the Connected writeup from current evidence", 0, 8, TerminalStateSnapshot{State: "connected"})
	for _, want := range []string{"shell", "http_probe", "terminal_send", "terminal_read"} {
		if !toolCallAdvertised(defs, want) {
			t.Fatalf("live terminal report route should advertise %s: %s", want, toolProtocolToolNames(defs))
		}
	}
	if toolCallAdvertised(defs, "start_listener") {
		t.Fatalf("connected terminal route must not advertise start_listener: %s", toolProtocolToolNames(defs))
	}
	if got := opsPhaseForTaskWithState("finish the Connected writeup from current evidence", TerminalStateSnapshot{State: "connected"}); got != "live_terminal" {
		t.Fatalf("phase = %q, want live_terminal", got)
	}
}

func TestOpsToolRouterUsesLiveTerminalState(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	connected := selectToolsForTurnWithState(cfg, "continue the HTB box with the current shell", 1, 8, TerminalStateSnapshot{State: "connected"})
	if !connected["terminal_send"] || !connected["terminal_read"] {
		t.Fatalf("connected session should keep terminal send/read: %#v", connected)
	}
	if connected["start_listener"] {
		t.Fatalf("connected session should not advertise start_listener: %#v", connected)
	}

	busy := selectToolsForTurnWithState(cfg, "continue the HTB box and inspect output", 1, 8, TerminalStateSnapshot{State: "running"})
	if !busy["terminal_read"] || !busy["terminal_send"] || !busy["shell"] || !busy["http_probe"] {
		t.Fatalf("running terminal should keep read/send plus one-shot routes: %#v", busy)
	}
	if busy["start_listener"] {
		t.Fatalf("running terminal should not advertise start_listener: %#v", busy)
	}

	reportConnected := selectToolsForTurnWithState(cfg, "finish the Connected writeup from current evidence", 0, 0, TerminalStateSnapshot{State: "connected"})
	for _, want := range []string{"shell", "http_probe", "terminal_send", "terminal_read"} {
		if !reportConnected[want] {
			t.Fatalf("connected terminal should keep %s even for report wording: %#v", want, reportConnected)
		}
	}
	if reportConnected["start_listener"] {
		t.Fatalf("connected report wording should not advertise start_listener: %#v", reportConnected)
	}
}

func TestOperationalHostsPromptKeepsShellAndHTTPProbe(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	selected := selectToolsForTurn(cfg, "the current box ip is 10.129.26.26 update all needed to call it in wsl and /etc/hosts then verify DNS and HTTP reachability", 0, 0)
	for _, want := range []string{"shell", "http_probe", "terminal_read", "terminal_send", "read", "grep", "todo_write"} {
		if !selected[want] {
			t.Fatalf("hosts/DNS operational prompt missing %s: %#v", want, selected)
		}
	}
}

func TestOpsToolDefsTrimBackToPhaseBudget(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	prompt := "continue the HTB box and inspect the webshell output"
	defs := []llm.ToolDef{}
	for _, name := range []string{
		"read", "glob", "grep", "write", "edit",
		"shell", "run_script", "terminal_send", "terminal_read", "start_listener",
		"http_probe", "evidence_bundle", "web_search", "fetch_url", "browser", "task",
		"memory", "session_search", "progress", "skill", "todo_write",
	} {
		defs = append(defs, llm.ToolDef{Type: "function", Function: llm.ToolFunctionDef{Name: name}})
	}
	got := trimOpsToolDefsIfNeeded(defs, cfg, prompt)
	names := map[string]bool{}
	for _, def := range got {
		names[def.Function.Name] = true
	}
	if len(got) > opsToolBudgetSoftCap {
		t.Fatalf("trimmed ops tools still over budget: %d > %d", len(got), opsToolBudgetSoftCap)
	}
	for _, want := range []string{"shell", "terminal_send", "terminal_read", "http_probe", "grep", "progress"} {
		if !names[want] {
			t.Fatalf("trimmed ops tools missing %s: %#v", want, names)
		}
	}
	for _, notWant := range []string{"browser", "web_search", "run_script", "bash", "read_pdf", "subagent_testfix", "terminal_run"} {
		if names[notWant] {
			t.Fatalf("trimmed ops tools should not include %s: %#v", notWant, names)
		}
	}
}

func TestOpsToolDefinitionsStayUnderBudgetByPhase(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	registry := tools.New()
	cases := []struct {
		name   string
		prompt string
		state  TerminalStateSnapshot
	}{
		{name: "recon", prompt: "continue HTB recon with nmap service enumeration"},
		{name: "webshell", prompt: "use the working webshell URL to run id and inspect foothold"},
		{name: "reverse shell", prompt: "start listener and catch a reverse shell callback with LHOST and LPORT"},
		{name: "connected terminal", prompt: "continue the HTB box with the current shell", state: TerminalStateSnapshot{State: "connected"}},
		{name: "report", prompt: "create the HTB writeup report from current evidence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defs, choice := toolDefsAndChoiceForTurnWithState(registry, cfg, tc.prompt, 0, 0, tc.state)
			if choice == "none" {
				t.Fatalf("%s unexpectedly disabled tools", tc.name)
			}
			if len(defs) > opsToolBudgetSoftCap {
				names := make([]string, 0, len(defs))
				for _, def := range defs {
					names = append(names, def.Function.Name)
				}
				t.Fatalf("%s routed %d tool defs, want <= %d: %#v", tc.name, len(defs), opsToolBudgetSoftCap, names)
			}
		})
	}
}
