package app

import (
	"os"
	"strings"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestLoadContextQualityFixturesCoversRequiredTaskClasses(t *testing.T) {
	fixtures, err := loadContextQualityFixtures()
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 7 {
		t.Fatalf("fixture count=%d want 7", len(fixtures))
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		seen[fixture.ID] = true
		if len(fixture.Prompts) < 3 {
			t.Fatalf("fixture %q has %d paraphrases", fixture.ID, len(fixture.Prompts))
		}
	}
	for _, id := range []string{
		"public-cve-lookup",
		"go-backend-fix",
		"react-ui-splitter",
		"telegram-final-delivery",
		"bug-bounty-assessment",
		"openrouter-one-task-boost",
		"wsl-terminal-operation",
	} {
		if !seen[id] {
			t.Fatalf("missing context quality fixture %q", id)
		}
	}
}

func TestContextQualityFixturesPassFiveAgainstRepository(t *testing.T) {
	root := repositoryRootForContextQualityTest(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = root
	cfg.Memory.Enabled = false
	cfg.Skills.Enabled = false
	cfg.Agents.ModeOverride = "Auto"
	cfg.Tools.ActiveToolset = "run-lean"
	profiles := settings.DefaultProfiles()
	profile := activeProfile(&cfg, &profiles)
	app := &App{
		cfg:        &cfg,
		profiles:   &profiles,
		history:    agent.NewHistory(profile.CtxTokens),
		registry:   tools.New(),
		autoAgents: true,
	}
	app.registerAppTools()

	fixtures, err := loadContextQualityFixtures()
	if err != nil {
		t.Fatal(err)
	}
	report := app.runContextQualityFixtures(cfg.ActiveProfile, 5, fixtures)
	if !report.Pass || report.PassCount != report.Total || report.AttemptPassCount != report.AttemptTotal || !report.HostilePass {
		var failures []string
		for _, result := range report.Results {
			if !result.Pass {
				failures = append(failures, result.ID+": "+strings.Join(result.Failures, " | "))
			}
		}
		t.Fatalf("context quality report failed (%d/%d fixtures, %d/%d attempts):\n%s", report.PassCount, report.Total, report.AttemptPassCount, report.AttemptTotal, strings.Join(failures, "\n"))
	}
	if report.PassPower != "pass^5" || report.AttemptTotal != 7*3*5 {
		t.Fatalf("unexpected repeated-run accounting: %#v", report)
	}
}

func TestContextQualityUsesCanonicalEnvelopeDespiteOperatorOverrides(t *testing.T) {
	root := repositoryRootForContextQualityTest(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = root
	cfg.Tools.ActiveToolset = "unrestricted"
	cfg.Tools.EnabledTools["web_search"] = false
	cfg.Tools.EnabledTools["fetch_url"] = false
	cfg.Agents.ModeOverride = "Manual"
	profiles := settings.DefaultProfiles()
	profile := activeProfile(&cfg, &profiles)
	app := &App{
		cfg:        &cfg,
		profiles:   &profiles,
		history:    agent.NewHistory(profile.CtxTokens),
		registry:   tools.New(),
		autoAgents: false,
		autonomous: true,
	}
	app.registerAppTools()

	report := app.RunContextQualityEval(cfg.ActiveProfile, 5)
	if !report.Pass || report.PassCount != 7 || report.AttemptPassCount != 105 {
		var failures []string
		for _, result := range report.Results {
			if !result.Pass {
				failures = append(failures, result.ID+": "+strings.Join(result.Failures, " | "))
			}
		}
		t.Fatalf("operator overrides leaked into canonical context eval: %s", strings.Join(failures, "\n"))
	}
	if report.EvaluationEnvelope != contextQualityEvaluationEnvelope {
		t.Fatalf("evaluation envelope = %q", report.EvaluationEnvelope)
	}
}

func TestContextQualityHostileToolGuard(t *testing.T) {
	if !contextQualityHostileToolGuardPasses() {
		t.Fatal("hostile tool-output guard fixture failed")
	}
}

func TestContextQualityRepairAndUpdateParaphrasesRouteToWorkingAgents(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ModeOverride = "Auto"
	for prompt, want := range map[string]string{
		"Fix the failing Go app test around persisted task state, keeping the change scoped.":                                              "Fixer",
		"Implement updated Mauler OpenRouter one-task cloud boost defaults while keeping local InferenceBridge as the persistent default.": "Builder",
	} {
		if got := selectAgentMode(prompt, cfg).Name; got != want {
			t.Fatalf("mode for %q=%q want %q; shell-centric=%t", prompt, got, want, looksShellCentricTask(prompt))
		}
	}
}

func TestShellRoutingDoesNotTreatKeepingAsPing(t *testing.T) {
	for _, prompt := range []string{
		"Fix the failing Go app test while keeping the change scoped.",
		"Implement provider defaults while keeping local inference persistent.",
	} {
		if looksShellCentricTask(prompt) || isOperationalTargetTask(strings.ToLower(prompt)) {
			t.Fatalf("ordinary implementation prompt was misread as ping: %q", prompt)
		}
	}
	if !looksShellCentricTask("ping the authorised lab target once") {
		t.Fatal("whole-word ping should remain an operational task")
	}
}

func TestContextQualityDetectsPolicyAndToolMismatch(t *testing.T) {
	observation := contextQualityObservation{
		Packet: projectInstructionPacket{
			Policy:            "minimal_external_research",
			EffectiveClass:    "minimal",
			RouteID:           "wrong-route",
			EstimatedTokens:   700,
			PacketLimitTokens: 500,
		},
		Mode:      AgentMode{Name: "Researcher"},
		ToolNames: []string{"read", "write"},
	}
	fixture := contextQualityFixture{
		ExpectedPolicy:   "minimal_external_research",
		ExpectedClass:    "minimal",
		ExpectedRoutes:   []string{"external-public-research"},
		ExpectedModes:    []string{"Researcher"},
		RequiredTools:    []string{"web_search"},
		ForbiddenTools:   []string{"write"},
		MaxProjectTokens: 500,
	}
	failures, dimensions := contextQualityObservationFailures(observation, fixture)
	if len(failures) < 4 || dimensions.policy || dimensions.tools || dimensions.budget {
		t.Fatalf("mismatch was not classified: failures=%#v dimensions=%#v", failures, dimensions)
	}
}

func TestNormalizeContextQualityRepeats(t *testing.T) {
	for input, want := range map[int]int{-1: 5, 0: 5, 1: 1, 5: 5, 99: 10} {
		if got := normalizeContextQualityRepeats(input); got != want {
			t.Fatalf("repeats(%d)=%d want %d", input, got, want)
		}
	}
}

func repositoryRootForContextQualityTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := findProjectInstructionRoot(wd)
	if root == "" {
		t.Fatal("repository root was not found")
	}
	return root
}
