package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestRepositoryContextDocumentsStayCompactAndCanonical(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate context_docs_test.go")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))

	read := func(relative string) []byte {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		if len(content) == 0 {
			t.Fatalf("%s is empty", relative)
		}
		return content
	}

	agents := read("AGENTS.md")
	mauler := read("MAULER.md")
	archive := read("docs/archive/agents-handoff-snapshot-2026-07-21.md")
	if len(agents) > 14*1024 {
		t.Fatalf("live AGENTS.md regrew beyond the guarded 14 KiB core: %d bytes", len(agents))
	}
	if len(mauler) > 2*1024 {
		t.Fatalf("MAULER.md must remain a compatibility shim: %d bytes", len(mauler))
	}
	if len(archive) < 60*1024 || len(archive) < len(agents)*4 {
		t.Fatalf("content-preserving AGENTS archive appears incomplete: %d bytes", len(archive))
	}
	for _, want := range []string{
		"Original title: `TheMauler - Agent Handoff Document`",
		"## What is Already Working",
		"## What to Build Next",
	} {
		if !strings.Contains(string(archive), want) {
			t.Fatalf("AGENTS archive missing %q", want)
		}
	}

	agentsText := string(agents)
	for _, want := range []string{
		"InferenceBridge",
		"OpenRouter",
		"never recommend Q6_K",
		"Context packets are bounded and task-aware",
		"Explorer, Inspector, bottom work area, and Terminal/AI Commands",
		"Telegram side chat is separate from desktop Chat",
		"Assistant prose is never completion evidence",
		"docs/context/non-negotiables.md",
	} {
		if !strings.Contains(agentsText, want) {
			t.Fatalf("compact AGENTS.md missing canonical rule %q", want)
		}
	}

	maulerText := string(mauler)
	for _, want := range []string{"`AGENTS.md` is the canonical", "docs/context/README.md"} {
		if !strings.Contains(maulerText, want) {
			t.Fatalf("MAULER.md shim missing %q", want)
		}
	}
	for _, stale := range []string{"LM Studio", "vLLM", "SGLang", "Q6_K"} {
		if strings.Contains(maulerText, stale) {
			t.Fatalf("MAULER.md must not duplicate provider/model policy; found %q", stale)
		}
	}

	domainDocs := []string{
		"current-state.md",
		"architecture.md",
		"non-negotiables.md",
		"roadmap.md",
		"verification.md",
		"feature-catalog.md",
		"troubleshooting.md",
	}
	for _, name := range domainDocs {
		read("docs/context/" + name)
		if !strings.Contains(agentsText, "docs/context/"+name) {
			t.Fatalf("AGENTS.md does not route to %s", name)
		}
	}

	manifestBytes := read("docs/context/manifest.json")
	var manifest struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("parse context manifest: %v", err)
	}
	if manifest.Version != 1 || manifest.Status != "active" {
		t.Fatalf("manifest must be the validated active v1 M3 contract: %#v", manifest)
	}
	selection := selectProjectContextManifest(root, "relevant_workspace", "update the OpenRouter provider in this repository")
	if selection.Status != contextManifestStatusActive || selection.RouteID != "provider-and-models" {
		t.Fatalf("live repository manifest is not executable: %#v", selection)
	}
	if len(selection.Documents) == 0 || len(selection.Documents) > contextManifestMaxDocuments {
		t.Fatalf("live repository manifest selected an invalid document count: %d", len(selection.Documents))
	}
}

func TestRepositoryInstructionPacketDoesNotAutoInjectArchivedHandoff(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate context_docs_test.go")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPromptForTask(settings.ContextConfig{
		ProjectDocMaxBytes:          128 * 1024,
		ProjectDocFallbackFilenames: []string{"MAULER.md", "AGENTS.md"},
	}, "implement the next context milestone in this repository")
	if !strings.Contains(prompt, "## Non-negotiable behaviour") {
		t.Fatalf("workspace packet missing compact canonical instructions:\n%s", prompt)
	}
	for _, archivedOnly := range []string{
		"Archived TheMauler agent handoff snapshot",
		"## What is Already Working",
		"### NEXT: Workbench cockpit UI cleanup",
	} {
		if strings.Contains(prompt, archivedOnly) {
			t.Fatalf("workspace packet auto-injected archived handoff content %q", archivedOnly)
		}
	}
}

func TestManifestContextRoutingIsBoundedDeterministicAndProvenanced(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "# Compact core\nCORE_RULE_SENTINEL\n")
	providerDoc := "# Provider guide\n" + strings.Repeat("general transport notes\n", 160) +
		"\n## OpenRouter context policy\nTARGET_PROVIDER_RULE: cloud use resets to local after one task.\n" +
		strings.Repeat("provider implementation detail\n", 80) +
		"\n## Historical appendix\n" + strings.Repeat("old detail\n", 200) + "UNRELATED_END_SENTINEL\n"
	mustWrite(t, filepath.Join(root, "docs", "context", "provider.md"), providerDoc)
	mustWrite(t, filepath.Join(root, "docs", "context", "manifest.json"), `{
  "version": 1,
  "status": "active",
  "default_packet": "relevant_workspace",
  "packets": {
    "minimal_external_research": {"max_project_tokens": 500, "documents": []},
    "relevant_workspace": {"max_project_tokens": 1200, "documents": ["../../AGENTS.md"]}
  },
  "routes": [
    {
      "id": "provider-and-models",
      "priority": 20,
      "signals": ["provider", "OpenRouter", "model"],
      "packet": "relevant_workspace",
      "documents": ["../../AGENTS.md", "provider.md"]
    },
    {
      "id": "workspace-implementation",
      "priority": 1,
      "signals": ["fix", "repository", "Mauler"],
      "packet": "relevant_workspace",
      "documents": ["../../AGENTS.md"]
    }
  ]
}`)

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cfg := settings.ContextConfig{ProjectDocMaxBytes: 128 * 1024}
	task := "fix the OpenRouter provider context handling in this Mauler repository"
	first := buildProjectInstructionPacket(cfg, task)
	if first.ManifestStatus != contextManifestStatusActive || first.RouteID != "provider-and-models" {
		t.Fatalf("unexpected manifest selection: %#v", first)
	}
	if len(first.Provenance) != 2 || len(first.Sources) != 2 {
		t.Fatalf("selection must contain at most the two routed documents: %#v", first.Provenance)
	}
	if first.PromptBytes > 1200*4 || first.EstimatedTokens != estimateContextTokens(first.PromptBytes) {
		t.Fatalf("manifest packet exceeded its declared budget: bytes=%d tokens=%d", first.PromptBytes, first.EstimatedTokens)
	}
	for _, want := range []string{"CORE_RULE_SENTINEL", "TARGET_PROVIDER_RULE", "Selected route: provider-and-models"} {
		if !strings.Contains(first.Prompt, want) {
			t.Fatalf("manifest packet missing %q:\n%s", want, first.Prompt)
		}
	}
	if strings.Contains(first.Prompt, "UNRELATED_END_SENTINEL") {
		t.Fatalf("heading-aware packet included an unrelated appendix:\n%s", first.Prompt)
	}
	providerHash := sha256Hex([]byte(providerDoc))
	targetLine := strings.Count(strings.Split(providerDoc, "TARGET_PROVIDER_RULE")[0], "\n") + 1
	foundTargetRange := false
	for _, source := range first.Provenance {
		if len(source.SHA256) != 64 || source.SourceBytes <= 0 || source.PromptBytes <= 0 || source.EstimatedTokens <= 0 {
			t.Fatalf("incomplete source provenance: %#v", source)
		}
		if source.SHA256 == providerHash {
			for _, excerpt := range source.ExcerptRanges {
				if excerpt.StartLine <= targetLine && targetLine <= excerpt.EndLine {
					foundTargetRange = true
				}
			}
		}
	}
	if !foundTargetRange {
		t.Fatalf("provider provenance did not include target line %d: %#v", targetLine, first.Provenance)
	}
	firstEvidence := first.ledgerDetail()
	for i := 0; i < 5; i++ {
		next := buildProjectInstructionPacket(cfg, task)
		if next.Prompt != first.Prompt || next.ledgerDetail() != firstEvidence {
			t.Fatalf("manifest selection changed on identical run %d", i+1)
		}
	}
}

func TestActiveManifestKeepsUnrelatedPublicResearchMinimal(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "# Compact core\nPRIVATE_REPOSITORY_RULE\n")
	mustWrite(t, filepath.Join(root, "docs", "context", "manifest.json"), `{
  "version": 1,
  "status": "active",
  "default_packet": "relevant_workspace",
  "packets": {
    "minimal_external_research": {"max_project_tokens": 500, "documents": []},
    "relevant_workspace": {"max_project_tokens": 1200, "documents": ["../../AGENTS.md"]}
  },
  "routes": [
    {
      "id": "external-public-research",
      "priority": 100,
      "signals": ["CVE-", "PoC", "in the wild"],
      "packet": "minimal_external_research",
      "documents": []
    }
  ]
}`)

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	packet := buildProjectInstructionPacket(settings.ContextConfig{}, "research CVE-2026-50522 PoC in the wild")
	if packet.Policy != "minimal_external_research" || packet.ManifestStatus != contextManifestStatusActive || packet.RouteID != "external-public-research" {
		t.Fatalf("unexpected public-research route: %#v", packet)
	}
	if len(packet.Sources) != 0 || len(packet.Provenance) != 0 || packet.SourceBytes != 0 {
		t.Fatalf("public research injected project documents: %#v", packet)
	}
	if strings.Contains(packet.Prompt, "PRIVATE_REPOSITORY_RULE") || packet.EstimatedTokens > 500 {
		t.Fatalf("public research packet was not minimal:\n%s", packet.Prompt)
	}
}

func TestInvalidManifestFallsBackToCompactCoreWithoutAuthorityExpansion(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "# Compact core\nSAFE_CORE_SENTINEL\n")
	mustWrite(t, filepath.Join(root, "docs", "context", "manifest.json"), `{
  "version": 1,
  "status": "active",
  "default_packet": "relevant_workspace",
  "tools": ["shell"],
  "packets": {
    "minimal_external_research": {"max_project_tokens": 500, "documents": []},
    "relevant_workspace": {"max_project_tokens": 1200, "documents": ["../../AGENTS.md"]}
  },
  "routes": []
}`)

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	packet := buildProjectInstructionPacket(settings.ContextConfig{}, "implement a safe repository change")
	if packet.ManifestStatus != contextManifestStatusInvalid || packet.RouteID != "fallback-compact-core" {
		t.Fatalf("invalid manifest did not use deterministic core fallback: %#v", packet)
	}
	if !strings.Contains(packet.FallbackReason, "unknown field") || !strings.Contains(packet.Prompt, "SAFE_CORE_SENTINEL") {
		t.Fatalf("fallback reason/core missing: %#v\n%s", packet, packet.Prompt)
	}
	if len(packet.Provenance) != 1 || !strings.EqualFold(filepath.Base(packet.Provenance[0].Path), "AGENTS.md") {
		t.Fatalf("fallback selected anything beyond compact core: %#v", packet.Provenance)
	}
}

func TestManifestRejectsPathEscapeAndTooManyDocuments(t *testing.T) {
	tests := []struct {
		name      string
		documents string
		want      string
	}{
		{name: "path escape", documents: `["../../../outside.md"]`, want: "escapes the workspace root"},
		{name: "more than three", documents: `["../../AGENTS.md", "one.md", "two.md", "three.md"]`, want: "more than 3 documents"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
			mustWrite(t, filepath.Join(root, "AGENTS.md"), "# Compact core\nSAFE_ONLY\n")
			for _, name := range []string{"one.md", "two.md", "three.md"} {
				mustWrite(t, filepath.Join(root, "docs", "context", name), "# doc\ncontent\n")
			}
			manifest := `{
  "version": 1,
  "status": "active",
  "default_packet": "relevant_workspace",
  "packets": {
    "minimal_external_research": {"max_project_tokens": 500, "documents": []},
    "relevant_workspace": {"max_project_tokens": 1200, "documents": ` + tc.documents + `}
  },
  "routes": []
}`
			mustWrite(t, filepath.Join(root, "docs", "context", "manifest.json"), manifest)
			old, _ := os.Getwd()
			defer os.Chdir(old)
			if err := os.Chdir(root); err != nil {
				t.Fatal(err)
			}
			packet := buildProjectInstructionPacket(settings.ContextConfig{}, "implement repository work")
			if packet.ManifestStatus != contextManifestStatusInvalid || !strings.Contains(packet.FallbackReason, tc.want) {
				t.Fatalf("unsafe manifest was not rejected: %#v", packet)
			}
			if !strings.Contains(packet.Prompt, "SAFE_ONLY") || len(packet.Provenance) != 1 {
				t.Fatalf("unsafe manifest did not fall back to compact core: %#v", packet.Provenance)
			}
		})
	}
}

func TestBuildProjectInstructionsPromptLayersDocs(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "frontend", "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "MAULER.md"), "root mauler")
	mustWrite(t, filepath.Join(root, "frontend", "AGENTS.override.md"), "frontend override")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		ProjectDocMaxBytes:          4096,
		ProjectDocFallbackFilenames: []string{"MAULER.md", "AGENTS.md"},
	})
	if !strings.Contains(prompt, "root mauler") || !strings.Contains(prompt, "frontend override") {
		t.Fatalf("expected layered docs, got:\n%s", prompt)
	}
	if strings.Index(prompt, "root mauler") > strings.Index(prompt, "frontend override") {
		t.Fatalf("root instructions should appear before nested overrides:\n%s", prompt)
	}
}

func TestBuildProjectInstructionsPromptCompilesLargeDocsToBoundedPacket(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	large := "# Start\n" + strings.Repeat("head material\n", 900) + "\n## Important Middle Section\n" + strings.Repeat("middle material\n", 900) + "\n# Final Rules\nTAIL_SENTINEL"
	mustWrite(t, filepath.Join(root, "AGENTS.md"), large)

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		ProjectDocMaxBytes:          len(large),
		ProjectDocFallbackFilenames: []string{"AGENTS.md"},
	})
	if len(prompt) > projectInstructionPromptMaxBytes+2048 {
		t.Fatalf("compiled project prompt is unbounded: %d bytes", len(prompt))
	}
	for _, want := range []string{"# Start", "Heading index:", "## Important Middle Section", "# Final Rules", "TAIL_SENTINEL", "use targeted read"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("compiled project prompt missing %q", want)
		}
	}
	if !strings.Contains(prompt, "(truncated)") || !strings.Contains(prompt, "middle omitted") {
		t.Fatalf("compiled project prompt should label its excerpt:\n%s", prompt)
	}
}

func TestProjectInstructionsUseMinimalPacketForUnrelatedCVEResearch(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "# Project rules\nPRIVATE_PROJECT_SENTINEL\n")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cfg := settings.ContextConfig{
		ProjectDocMaxBytes:          65536,
		ProjectDocFallbackFilenames: []string{"AGENTS.md"},
	}
	prompt := buildProjectInstructionsPromptForTask(cfg, "find and research the PoC for CVE-2026-50522 exploited in the wild")
	if strings.Contains(prompt, "PRIVATE_PROJECT_SENTINEL") {
		t.Fatalf("unrelated public research should not inject repository handoff content:\n%s", prompt)
	}
	for _, want := range []string{"minimal external research", "not copied into the prompt", "AGENTS.md"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("minimal packet missing %q:\n%s", want, prompt)
		}
	}
	packet := buildProjectInstructionPacket(cfg, "find and research the PoC for CVE-2026-50522 exploited in the wild")
	if packet.Policy != "minimal_external_research" || packet.PromptBytes >= packet.SourceBytes+2048 {
		t.Fatalf("unexpected minimal packet: %#v", packet)
	}
}

func TestProjectInstructionsRemainAvailableForWorkspaceImplementation(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "# Project rules\nPRIVATE_PROJECT_SENTINEL\n")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cfg := settings.ContextConfig{
		ProjectDocMaxBytes:          65536,
		ProjectDocFallbackFilenames: []string{"AGENTS.md"},
	}
	prompt := buildProjectInstructionsPromptForTask(cfg, "research the latest API and update this repository implementation")
	if !strings.Contains(prompt, "PRIVATE_PROJECT_SENTINEL") {
		t.Fatalf("workspace implementation should retain relevant project instructions:\n%s", prompt)
	}
	if policy := projectInstructionPolicyForTask("research the latest API and update this repository implementation"); policy != "relevant_workspace" {
		t.Fatalf("workspace research policy = %q", policy)
	}
}

func TestBuildProjectInstructionsPromptDoesNotAutoLoadMasterSkillsAlongsideProjectDoc(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "MAULER.md"), "root mauler")
	mustWrite(t, filepath.Join(root, "master_skills.md"), "master skill instructions")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		ProjectDocMaxBytes: 4096,
	})
	if !strings.Contains(prompt, "root mauler") {
		t.Fatalf("expected project doc to be loaded, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "master skill instructions") {
		t.Fatalf("master skills should not be auto-loaded as project docs:\n%s", prompt)
	}
}

func TestBuildProjectInstructionsPromptDoesNotAutoLoadSingularMasterSkillFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "master_skill.md"), "singular master skill instructions")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		ProjectDocMaxBytes: 4096,
	})
	if strings.Contains(prompt, "singular master skill instructions") {
		t.Fatalf("master_skill.md should not be auto-loaded, got:\n%s", prompt)
	}
}

func TestBuildProjectInstructionsPromptDoesNotAutoLoadMasterSkillsDirectory(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "MAULER.md"), "root mauler")
	mustWrite(t, filepath.Join(root, "master_skills", "01-build.md"), "build skill")
	mustWrite(t, filepath.Join(root, "master_skills", "nested", "02-review.md"), "review skill")
	mustWrite(t, filepath.Join(root, "master_skills", "ignored.txt"), "not markdown")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		ProjectDocMaxBytes: 4096,
	})
	if !strings.Contains(prompt, "root mauler") {
		t.Fatalf("expected project doc to be loaded, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "build skill") || strings.Contains(prompt, "review skill") || strings.Contains(prompt, "not markdown") {
		t.Fatalf("master_skills directory should not be auto-loaded, got:\n%s", prompt)
	}
}

func TestProjectInstructionFilenamesFiltersLegacyMasterSkillFallbacks(t *testing.T) {
	names := projectInstructionFilenames([]string{"MAULER.md", "master_skill.md", "master_skills", "AGENTS.md"})
	joined := strings.Join(names, " ")
	if strings.Contains(joined, "master_skill") || strings.Contains(joined, "master_skills") {
		t.Fatalf("legacy master skill names should be filtered: %#v", names)
	}
}

func TestBuildProjectInstructionsPromptHonorsExplicitPath(t *testing.T) {
	root := t.TempDir()
	explicit := filepath.Join(root, "custom.md")
	mustWrite(t, explicit, strings.Repeat("x", 100))
	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		MAULERMDPath:       explicit,
		ProjectDocMaxBytes: 20,
	})
	if !strings.Contains(prompt, "(truncated)") {
		t.Fatalf("expected explicit doc to be truncated, got:\n%s", prompt)
	}
	if strings.Count(prompt, "x") > 25 {
		t.Fatalf("expected cap to apply, got:\n%s", prompt)
	}
}

func TestExplicitInstructionDirectoryPrioritizesMasterSkill(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "WIRING_DIAGRAM.md"), "wiring first alphabetically")
	mustWrite(t, filepath.Join(root, "SKILL.md"), "local skill")
	mustWrite(t, filepath.Join(root, "master_skill.md"), "navigator master brain")
	mustWrite(t, filepath.Join(root, "maps", "htb_methodology.md"), "htb map")

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		MAULERMDPath:       root,
		ProjectDocMaxBytes: 4096,
	})
	if !strings.Contains(prompt, "navigator master brain") {
		t.Fatalf("expected master_skill.md to load from explicit directory, got:\n%s", prompt)
	}
	if strings.Index(prompt, "navigator master brain") > strings.Index(prompt, "local skill") {
		t.Fatalf("master_skill.md should be loaded before adjacent SKILL.md:\n%s", prompt)
	}
	if strings.Index(prompt, "navigator master brain") > strings.Index(prompt, "wiring first alphabetically") {
		t.Fatalf("master_skill.md should be loaded before alphabetically earlier docs:\n%s", prompt)
	}
	if !strings.Contains(prompt, "read at most 1-3 targeted follow-up files") {
		t.Fatalf("expected anti-crawl guidance in explicit framework prompt:\n%s", prompt)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
