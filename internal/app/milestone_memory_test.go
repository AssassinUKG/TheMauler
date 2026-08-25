package app

import (
	"strings"
	"testing"
)

func TestBuildRunMilestoneMemorySummarizesReconAndResume(t *testing.T) {
	run := startTaskRun("HTB run: get user/root on 10.129.13.198 and update the writeup", "Builder", "profile", "model")
	run.addTool("shell", `{"command":"nmap -sV -sC -oA scans/target 10.129.13.198"}`, `Starting Nmap
22/tcp open  ssh     OpenSSH 7.4
80/tcp open  http    Apache httpd 2.4.6
443/tcp open ssl/http Apache httpd 2.4.6
Nmap done`, "success", 1000)
	run.addTool("web_search", `{"query":"FreePBX 16 privilege escalation CVE"}`, `results`, "success", 100)
	run.stop("search_budget_exhausted", "web_search budget exhausted")
	run.finish("stopped", "blocked while researching exploit leads")

	mem := buildRunMilestoneMemory(&run)
	if mem == nil {
		t.Fatal("expected milestone memory")
	}
	if mem.Title != "Run memory: 10.129.13.198" {
		t.Fatalf("title = %q", mem.Title)
	}
	for _, want := range []string{
		"Recon found open ports/services",
		"22/tcp open",
		"80/tcp open",
		"Web research query",
		"Stop: search_budget_exhausted",
		"Next: Resume with expanded exploit research budget",
	} {
		if !strings.Contains(mem.Content, want) {
			t.Fatalf("memory content missing %q:\n%s", want, mem.Content)
		}
	}
	if !containsString(mem.Tags, "target-10-129-13-198") || !containsString(mem.Tags, "htb") || !containsString(mem.Tags, "docs") {
		t.Fatalf("unexpected tags: %#v", mem.Tags)
	}
}

func TestMilestoneMemoryRedactsSecretsAndFlags(t *testing.T) {
	secretFlag := "abcdef1234567890abcdef1234567890"
	got := sanitizeMilestoneMemory("user.txt: " + secretFlag + "\npassword=SuperSecret123\napi_key: deadbeef")

	if strings.Contains(got, secretFlag) || strings.Contains(got, "SuperSecret123") || strings.Contains(got, "deadbeef") {
		t.Fatalf("secret values leaked after redaction:\n%s", got)
	}
	for _, want := range []string{"user.txt=[redacted]", "password=[redacted]", "api_key=[redacted]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted output missing %q:\n%s", want, got)
		}
	}
}

func TestBuildRunMilestoneMemoryDoesNotTreatProductVersionAsTarget(t *testing.T) {
	run := startTaskRun("carry on", "Ops", "profile", "model")
	run.addTool("shell", `{"command":"curl -s http://connected.htb/admin/config.php | head -n 20"}`, `<title>FreePBX Administration</title><link href="assets/css/app.css?load_version=16.0.40.7">`, "done", 100)
	run.stop("repeated_tool_failure", "blocked")
	run.finish("stopped", "blocked")

	mem := buildRunMilestoneMemory(&run)
	if mem == nil {
		t.Fatal("expected run memory")
	}
	if strings.Contains(mem.Title, "16.0.40.7") || strings.Contains(mem.Content, "Target: 16.0.40.7") {
		t.Fatalf("product version was treated as target: %#v", mem)
	}
	if !strings.Contains(mem.Content, "FreePBX version: 16.0.40.7") {
		t.Fatalf("product version milestone missing: %s", mem.Content)
	}
}

func TestBuildRunMilestoneMemorySkipsEmptyRuns(t *testing.T) {
	run := startTaskRun("ordinary prompt with no durable result", "Auto", "profile", "model")

	if mem := buildRunMilestoneMemory(&run); mem != nil {
		t.Fatalf("expected no memory for empty running task, got %#v", mem)
	}
}

func TestBuildRunMilestoneMemorySkipsUserStoppedWithoutMilestones(t *testing.T) {
	run := startTaskRun("Can you access the attached Swagger file?", "Auto", "profile", "model")
	run.stop("user_stopped", "The user stopped the current run.")
	run.finish("stopped", "Run stopped")

	if mem := buildRunMilestoneMemory(&run); mem != nil {
		t.Fatalf("expected no memory for a user-stopped run without durable milestones, got %#v", mem)
	}
}

func TestAutoInjectionRejectsOldLowSignalStoppedRunMemory(t *testing.T) {
	entry := MemoryEntry{
		ID: "runmem-old", Title: "Run memory", Source: "previous_run", Kind: "fact",
		Content: "Prompt: access swagger.json\nStatus: stopped\nStop: user_stopped\nNext: The user stopped the current run.",
	}
	if autoInjectMemoryAllowed(entry, "ops", []string{"swagger"}, "read swagger.json") {
		t.Fatal("old stopped-run narration must not be auto-injected")
	}
	entry.Pinned = true
	if !autoInjectMemoryAllowed(entry, "ops", []string{"swagger"}, "read swagger.json") {
		t.Fatal("an explicitly pinned memory must remain eligible")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
