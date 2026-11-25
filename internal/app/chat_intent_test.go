package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestLocalIntentChatCompositionAndNamedArtifactDelivery(t *testing.T) {
	for _, prompt := range []string{
		"Hello! Please greet me briefly.",
		"Write a short message telling my EHC guild how proud I am we took a level 9 planet.",
		"I run a guild called EHC and wanted a message for the guild to say how proud I am.",
		"Compose a friendly reply for my friend.",
		"Explain how I could list files in a folder on Windows. Advice only; do not inspect or access any files.",
		"Write a short message for my guild. Just draft it in chat; no files or tools.",
		"Reply exactly MAULER_RECOVERED_1 and nothing else.",
	} {
		defs, choice := toolDefsAndChoiceForTurn(tools.New(), settings.DefaultSettings().Tools, prompt, 0, 0)
		if choice != "none" || len(defs) != 0 || !isDirectAnswerTurn(prompt, choice, 0) {
			t.Errorf("%q must remain text-only: choice=%s tools=%d", prompt, choice, len(defs))
		}
	}
	for _, prompt := range []string{
		"Create artifact.txt containing exact text. Then read back, verify, and finish the handoff in chat. Do not use shell or create any other deliverable.",
		"Append exactly the text represented by this JSON string to artifact.txt, preserving every existing byte: \"suffix\\n\". Use append=true. Verify and finish the handoff in chat.",
		"Can you write report.md and then tell me the result in chat?",
		"Create \"artifact.txt\" containing the literal quoted phrase \"then delete other.txt\". Report back in chat.",
	} {
		if !promptHasNamedFileMutation(prompt) || !promptExplicitlyRequestsMutation(prompt) || promptLooksReadOnly(prompt) {
			t.Errorf("explicit file work lost mutation/evidence contract: %q", prompt)
		}
		if toolChoiceFor(prompt, 0, 0) == "none" {
			t.Errorf("file request lost tools: %q", prompt)
		}
		cfg := settings.DefaultSettings()
		cfg.Context.WorkspaceDir = t.TempDir()
		if strings.HasPrefix(prompt, "Append ") {
			if err := os.WriteFile(filepath.Join(cfg.Context.WorkspaceDir, "artifact.txt"), []byte("original prefix\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
		run := startTaskRun(prompt, "Builder", "local", "model")
		contract, err := (&App{}).buildTaskContract(run, cfg, baseMode("Builder"), true)
		if err != nil || len(contract.Deliverables) == 0 || len(contract.RequiredEvidence) == 0 || len(contract.AcceptanceChecks) == 0 {
			t.Errorf("file request has no blocking evidence: %#v %v", contract, err)
		}
	}
	for _, prompt := range []string{
		"Hello, and list the files in this folder.",
		"Write a message and send it to my guild.",
		"Write a message with today's weather forecast.",
		"Draft a reply and save it to a file.",
	} {
		if toolChoiceFor(prompt, 0, 0) == "none" {
			t.Errorf("explicit action/live evidence wrongly became text-only: %q", prompt)
		}
	}
	for _, prompt := range []string{
		"Explain the phrase 'then create artifact.txt' without actually creating it.",
		"Compose a message containing the example \"then delete artifact.txt\".",
	} {
		if promptHasNamedFileMutation(prompt) {
			t.Errorf("quoted example minted mutation: %q", prompt)
		}
	}
}

func TestLocalIntentDirectNumericAndExactReplies(t *testing.T) {
	shortRun := startTaskRun("What is 7 plus 5?", "Auto", "local", "model")
	if reason := invalidDoneReason(shortRun, "12"); reason != "" {
		t.Fatalf("valid numeric answer rejected at terminal handoff: %s", reason)
	}
	actionRun := startTaskRun("Create artifact.txt and verify it", "Builder", "local", "model")
	if invalidDoneReason(actionRun, "12") == "" {
		t.Fatal("numeric prose must not complete requested file work")
	}
	if needsLiveExternalInfoTool("Create artifact.txt with fixture data and finish the deliverable in chat.") || needsLiveExternalInfoTool("Write a training note for my guild.") {
		t.Fatal("fixture/deliverable/training substrings manufactured a live-data request")
	}
	for _, prompt := range []string{"Get today's football fixtures.", "What is the weather over the next seven days?", "Show me current stock prices."} {
		if !needsLiveExternalInfoTool(prompt) {
			t.Errorf("real current-data request lost evidence: %q", prompt)
		}
	}
	for _, reply := range []string{"12", "-42", "1.5", "1e6"} {
		if !directAnswerLooksComplete(reply) {
			t.Errorf("complete numeric answer rejected: %q", reply)
		}
	}
	prompt := "Reply exactly MAULER_RECOVERED_1 and nothing else."
	if !directAnswerAcceptedForPrompt(conversationModeAdaptive, prompt, "MAULER_RECOVERED_1") {
		t.Fatal("valid observed exact answer rejected")
	}
	for _, reply := range []string{"MAULER_RECOVERED_1.", "MAULER_RECOVERED_1\nMAULER_RECOVERED_1", "I'll do it shortly."} {
		if directAnswerAcceptedForPrompt(conversationModeAdaptive, prompt, reply) {
			t.Errorf("format drift accepted: %q", reply)
		}
	}
	for _, prompt := range []string{"Reply exactly DONE and then create artifact.txt.", "Read README.md"} {
		if _, ok := shortExactReplyTarget(prompt); ok {
			t.Errorf("action minted exact-answer shortcut: %q", prompt)
		}
	}
	if !strings.EqualFold(classifyAgentMode("Create artifact.txt with a suffix and fixture data.").Name, "Builder") {
		t.Fatal("suffix/fixture substring must not select Fixer")
	}
	if classifyAgentMode("Write a short message about taking a planet.").Name != "Auto" {
		t.Fatal("planet must not select Planner for plain composition")
	}
}

func TestLocalIntentQualificationFileToolEnvelope(t *testing.T) {
	app, _, _, restore := qualificationApp(t, t.TempDir(), &localQualificationRuntime{base: "http://127.0.0.1:8804/v1", key: "fixture-unused", model: "RVN-Q4_K_M-multilingual-mtp.gguf", context: 35072})
	defer restore()
	for _, prompt := range []string{
		"Create artifact.txt containing exactly the text represented by this JSON string: \"LOCAL_WRITE_1\\nQuoted: \\\"alpha\\\"; path: C:\\\\work\\\\fixture\\nUnicode: café / λ / 日本語\\nEND_LOCAL_WRITE_1\\n\". Then read back and verify the complete file, and finish the handoff in chat. Do not use shell or create any other deliverable.",
		"Append exactly the text represented by this JSON string to artifact.txt, preserving every existing byte: \"literal appended suffix\\n\". Use append=true, then read back and verify the complete file. Finish the handoff in chat. Do not use shell or create any other deliverable.",
	} {
		defs, choice := toolDefsAndChoiceForTurnWithState(app.registry, app.cfg.Tools, prompt, 0, 0, TerminalStateSnapshot{State: "missing"})
		if !toolCallAdvertised(defs, "read") || !toolCallAdvertised(defs, "write") {
			t.Errorf("file tools missing: choice=%s names=%s enabled=%#v selected=%#v live=%v host=%v shell=%v readonly=%v", choice, toolProtocolToolNames(defs), settings.EffectiveEnabledTools(app.cfg.Tools), selectToolsForTurnWithState(app.cfg.Tools, prompt, 0, 0, TerminalStateSnapshot{State: "missing"}), needsLiveExternalInfoTool(prompt), needsWindowsHostInspectionTool(prompt), looksShellCentricTask(prompt), promptLooksReadOnly(prompt))
		}
	}
}
