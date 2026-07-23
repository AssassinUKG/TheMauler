package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mauler/internal/channelbus"
	"mauler/internal/engagement"
	"mauler/internal/llm"
	"mauler/internal/settings"
	appstore "mauler/internal/store"
	"mauler/internal/tools"
)

func TestEngagementToolRunsClaimObserveFinishLoopWithLockedProjectScope(t *testing.T) {
	app, cleanup := newEngagementToolTestApp(t)
	defer cleanup()
	ctx := withEngagementClaimant(context.Background(), "run-123", "Auto")

	created := runEngagementCall(t, app, ctx, map[string]any{
		"action": "create", "name": "Connected", "scope": []string{"https://connected.htb/login"},
	})
	createResult := resultObject(t, created)
	engagementID := stringValue(t, createResult, "engagement_id")
	if engagementID == "" || createResult["scope_locked"] != true {
		t.Fatalf("create result = %#v", createResult)
	}
	initialNotes := resultObject(t, runEngagementCall(t, app, ctx, map[string]any{"action": "get_notes", "engagement_id": engagementID}))
	if uintValue(t, initialNotes, "revision") != 1 {
		t.Fatalf("initial notes = %#v", initialNotes)
	}
	updatedNotes := resultObject(t, runEngagementCall(t, app, ctx, map[string]any{
		"action": "set_notes", "engagement_id": engagementID, "expected_revision": 1,
		"notes": "# Risk model\n\nThe authenticated profile flow is the main trust boundary.",
	}))
	if uintValue(t, updatedNotes, "revision") != 2 {
		t.Fatalf("updated notes = %#v", updatedNotes)
	}
	if _, err := runEngagementCallRaw(app, ctx, map[string]any{
		"action": "set_notes", "engagement_id": engagementID, "expected_revision": 1, "notes": "stale",
	}); !errors.Is(err, engagement.ErrRevisionConflict) {
		t.Fatalf("stale tool notes update = %v", err)
	}
	next := objectValue(t, createResult, "next")
	work := objectValue(t, next, "work")
	ref := objectValue(t, work, "ref")
	if stringValue(t, ref, "id") != "verify_reachable" {
		t.Fatalf("first work = %#v", work)
	}
	now := runEngagementCall(t, app, ctx, map[string]any{"action": "now", "engagement_id": engagementID})
	if got := stringValue(t, objectValue(t, objectValue(t, resultObject(t, now), "next"), "work"), "title"); got == "" {
		t.Fatalf("now result did not expose current work: %#v", now)
	}
	available := resultObject(t, runEngagementCall(t, app, ctx, map[string]any{"action": "available", "engagement_id": engagementID, "limit": 4}))
	availableItems, ok := available["items"].([]any)
	if !ok || len(availableItems) != 1 || available["parallel"] != false {
		t.Fatalf("initial available queue = %#v", available)
	}

	claimed := runEngagementCall(t, app, ctx, map[string]any{
		"action": "claim", "engagement_id": engagementID,
		"kind": ref["kind"], "phase_id": ref["phase_id"], "work_id": ref["id"],
	})
	claimedWork := objectValue(t, resultObject(t, claimed), "work")
	claimRevision := uintValue(t, claimedWork, "revision")
	claim := objectValue(t, claimedWork, "claim")
	if stringValue(t, claim, "claimant_id") != "run-123" {
		t.Fatalf("claim = %#v", claim)
	}
	if err := os.WriteFile("reachability.txt", []byte("HTTP/1.1 200 OK\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidenceEnvelope := runEngagementCall(t, app, ctx, map[string]any{
		"action": "add_evidence", "engagement_id": engagementID,
		"kind": ref["kind"], "phase_id": ref["phase_id"], "work_id": ref["id"],
		"source_kind": "artifact", "path": "reachability.txt", "description": "Reachability response captured on disk.",
	})
	evidence := objectValue(t, resultObject(t, evidenceEnvelope), "evidence")
	evidenceID := stringValue(t, evidence, "id")
	if evidenceID == "" || evidence["agent_composed"] != true {
		t.Fatalf("unlinked model path must remain agent-composed: %#v", evidence)
	}
	findingEnvelope := runEngagementCall(t, app, ctx, map[string]any{
		"action": "upsert_finding", "engagement_id": engagementID,
		"kind": ref["kind"], "phase_id": ref["phase_id"], "work_id": ref["id"],
		"title": "Reachability note", "severity": "low", "reproduction": "Request the configured base URL.",
		"evidence_ids": []string{evidenceID},
	})
	finding := objectValue(t, resultObject(t, findingEnvelope), "finding")
	if _, err := runEngagementCallRaw(app, ctx, map[string]any{
		"action": "confirm_finding", "engagement_id": engagementID,
		"finding_id": finding["id"], "expected_revision": finding["revision"],
	}); err == nil || !strings.Contains(err.Error(), "non-agent-composed evidence") {
		t.Fatalf("confirm with agent-composed evidence = %v", err)
	}
	listedEvidence := runEngagementCall(t, app, ctx, map[string]any{"action": "list_evidence", "engagement_id": engagementID})
	if items, ok := resultObject(t, listedEvidence)["evidence"].([]any); !ok || len(items) != 1 {
		t.Fatalf("listed evidence = %#v", listedEvidence)
	}

	observed := runEngagementCall(t, app, ctx, map[string]any{
		"action": "observe", "engagement_id": engagementID,
		"kind": ref["kind"], "phase_id": ref["phase_id"], "work_id": ref["id"],
		"status": "done", "observation": "The configured target returned an HTTPS response.",
		"expected_revision": claimRevision,
	})
	observedWork := objectValue(t, resultObject(t, observed), "work")
	observedRevision := uintValue(t, observedWork, "revision")

	finished := runEngagementCall(t, app, ctx, map[string]any{
		"action": "finish", "engagement_id": engagementID,
		"kind": ref["kind"], "phase_id": ref["phase_id"], "work_id": ref["id"],
		"expected_revision": observedRevision,
	})
	finishResult := resultObject(t, finished)
	finishNext := objectValue(t, finishResult, "next")
	finishNextWork := objectValue(t, finishNext, "work")
	finishNextRef := objectValue(t, finishNextWork, "ref")
	if stringValue(t, finishNextRef, "id") != "quick_port_scan" {
		t.Fatalf("next after finish = %#v", finishNext)
	}

	packet := app.buildEngagementPromptPacket()
	for _, want := range []string{"Active Engagement Grid packet", engagementID, "quick_port_scan", "locked=true", "authenticated profile flow"} {
		if !strings.Contains(packet, want) {
			t.Fatalf("prompt packet missing %q:\n%s", want, packet)
		}
	}
	if strings.Contains(packet, "configured target returned") {
		t.Fatalf("prompt packet leaked observation body:\n%s", packet)
	}
	if len([]rune(packet)) > maxEngagementPromptRunes {
		t.Fatalf("prompt packet has %d runes, max %d", len([]rune(packet)), maxEngagementPromptRunes)
	}
}

func TestEngagementToolRejectsModelChosenOutOfScopeTarget(t *testing.T) {
	app, cleanup := newEngagementToolTestApp(t)
	defer cleanup()
	_, err := runEngagementCallRaw(app, context.Background(), map[string]any{
		"action": "create", "name": "Bad scope", "scope": []string{"evil.example"},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match the operator-configured target") {
		t.Fatalf("out-of-scope create = %v", err)
	}
}

func TestStructuredHTTPProbeScopeFailsClosedForActiveEngagement(t *testing.T) {
	app, cleanup := newEngagementToolTestApp(t)
	defer cleanup()
	if _, err := app.CreateEngagement("Connected", "webapp-simple", nil); err != nil {
		t.Fatal(err)
	}
	if err := app.enforceEngagementScope(context.Background(), "http_probe", "http://connected.htb/admin"); err != nil {
		t.Fatalf("configured hostname rejected: %v", err)
	}
	if err := app.enforceEngagementScope(context.Background(), "http_probe", "http://10.129.26.26:8080/"); err != nil {
		t.Fatalf("configured IP rejected: %v", err)
	}
	if err := app.enforceEngagementScope(context.Background(), "http_probe", "https://evil.example/"); err == nil || !strings.Contains(err.Error(), "locked engagement scope") {
		t.Fatalf("out-of-scope HTTP probe = %v", err)
	}
}

func TestEngagementBindingsUseCurrentProjectAndPersistEndpointEdits(t *testing.T) {
	app, cleanup := newEngagementToolTestApp(t)
	defer cleanup()

	record, err := app.CreateEngagement("", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Name != "Connected" || !record.State.ScopeLocked {
		t.Fatalf("created binding record = %#v", record.State)
	}
	if strings.Join(record.State.Scope, ",") != "10.129.26.26,connected.htb" {
		t.Fatalf("scope = %#v", record.State.Scope)
	}

	summaries, err := app.ListEngagements()
	if err != nil || len(summaries) != 1 || summaries[0].ID != record.State.ID {
		t.Fatalf("summaries = %#v, err = %v", summaries, err)
	}
	next, err := app.GetEngagementNext(record.State.ID)
	if err != nil || next.Work == nil || next.Work.Ref.ID != "verify_reachable" {
		t.Fatalf("next = %#v, err = %v", next, err)
	}
	available, err := app.GetEngagementAvailable(record.State.ID, 8)
	if err != nil || len(available.Items) != 1 || available.Items[0].Ref != next.Work.Ref {
		t.Fatalf("available = %#v, err = %v", available, err)
	}

	endpoint, err := app.AddEngagementEndpoint(record.State.ID, engagement.EndpointInput{Method: "GET", URL: "/login"})
	if err != nil || endpoint.ID == "" {
		t.Fatalf("endpoint = %#v, err = %v", endpoint, err)
	}
	grouped, err := app.SetEngagementEndpointGroup(record.State.ID, endpoint.ID, "authentication")
	if err != nil || grouped.FeatureGroup != "authentication" {
		t.Fatalf("grouped endpoint = %#v, err = %v", grouped, err)
	}
	loaded, err := app.GetEngagement(record.State.ID)
	if err != nil || loaded.State.Endpoints[endpoint.ID].FeatureGroup != "authentication" {
		t.Fatalf("loaded endpoint = %#v, err = %v", loaded.State.Endpoints[endpoint.ID], err)
	}

	if err := app.DeleteEngagement(record.State.ID); err != nil {
		t.Fatal(err)
	}
	summaries, err = app.ListEngagements()
	if err != nil || len(summaries) != 0 {
		t.Fatalf("summaries after delete = %#v, err = %v", summaries, err)
	}
}

func TestEngagementToolRegistrationAndRoutingAreCompact(t *testing.T) {
	app, cleanup := newEngagementToolTestApp(t)
	defer cleanup()
	tool, ok := app.registry.Get("engagement")
	if !ok {
		t.Fatal("app registry missing engagement tool")
	}
	if tool.Destructive() {
		t.Fatal("engagement state tool should not request host-write confirmation")
	}
	cfg := settings.DefaultSettings().Tools
	selected := selectToolsForTurn(cfg, "continue the HTB engagement checklist", 0, 0)
	if !selected["engagement"] || !settings.EffectiveEnabledTools(cfg)["engagement"] {
		t.Fatalf("engagement routing selected=%#v enabled=%#v", selected, settings.EffectiveEnabledTools(cfg))
	}
	defs, _ := toolDefsAndChoiceForTurn(app.registry, cfg, "continue the HTB engagement checklist", 0, 0)
	if !toolCallAdvertised(defs, "engagement") {
		t.Fatalf("engagement definition missing: %s", toolProtocolToolNames(defs))
	}
}

func TestSubagentEngagementAssignmentIsFocusedAndWorkspaceScoped(t *testing.T) {
	app, cleanup := newEngagementToolTestApp(t)
	defer cleanup()
	created, err := app.CreateEngagement("Delegated target", "webapp-simple", nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := app.engagements.Now(context.Background(), created.State.ID, time.Now())
	if err != nil || next.Work == nil {
		t.Fatalf("next=%#v err=%v", next, err)
	}
	args := subagentArgs{
		EngagementID: created.State.ID,
		WorkKind:     next.Work.Ref.Kind,
		PhaseID:      next.Work.Ref.PhaseID,
		EndpointID:   next.Work.Ref.EndpointID,
		WorkID:       next.Work.Ref.ID,
	}
	packet, assigned, err := app.subagentEngagementAssignment(
		withEngagementClaimant(context.Background(), "parent-run", "Auto"), args,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !assigned || !strings.Contains(packet, "authoritative and exclusive") ||
		!strings.Contains(packet, "engagement_id="+created.State.ID) ||
		!strings.Contains(packet, "work_id="+next.Work.Ref.ID) ||
		!strings.Contains(packet, "operate only this assigned work item") {
		t.Fatalf("assignment packet = %q", packet)
	}
	if _, _, err := app.subagentEngagementAssignment(context.Background(), subagentArgs{EngagementID: created.State.ID}); err == nil {
		t.Fatal("partial assignment should be rejected")
	}
	if _, _, err := app.subagentEngagementAssignment(context.Background(), subagentArgs{
		EngagementID: created.State.ID, WorkKind: engagement.WorkStep,
		PhaseID: next.Work.Ref.PhaseID, WorkID: "quick_port_scan",
	}); err == nil || !strings.Contains(err.Error(), "not currently available") {
		t.Fatalf("future sequential assignment = %v", err)
	}
}

func TestChannelRunClaimantIsStableDistinctAndSanitised(t *testing.T) {
	env := channelbus.Envelope{ID: "Msg 42", Source: "Telegram", SessionID: "Chat/One", UserID: "123", Username: "Alice"}
	id, alias, origin := channelRunClaimant(env)
	if id != "channel:telegram:chatone:msg42" || alias != "Alice" || origin != "telegram" {
		t.Fatalf("claimant = %q %q %q", id, alias, origin)
	}
	idAgain, _, _ := channelRunClaimant(env)
	if idAgain != id {
		t.Fatalf("stable envelope produced %q then %q", id, idAgain)
	}
	env.ID = "Msg 43"
	idNext, _, _ := channelRunClaimant(env)
	if idNext == id {
		t.Fatalf("distinct messages share claimant id %q", id)
	}
}

func TestWorkspaceClaimHeartbeatAndOperatorRelease(t *testing.T) {
	app, cleanup := newEngagementToolTestApp(t)
	defer cleanup()
	created, err := app.CreateEngagement("Heartbeat target", "webapp-simple", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	next, err := app.engagements.Now(context.Background(), created.State.ID, now)
	if err != nil || next.Work == nil {
		t.Fatalf("next=%#v err=%v", next, err)
	}
	claimed, err := app.engagements.Claim(context.Background(), created.State.ID, next.Work.Ref, engagement.Claimant{ID: "channel:telegram:chat:msg", Alias: "Alice"}, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	pulses, err := app.heartbeatWorkspaceClaims(context.Background(), "channel:telegram:chat:msg", 2*time.Minute, now.Add(30*time.Second))
	if err != nil || len(pulses) != 1 {
		t.Fatalf("pulses=%#v err=%v", pulses, err)
	}
	if pulses[0].Work.Revision != claimed.Revision || pulses[0].Work.Claim == nil {
		t.Fatalf("heartbeat pulse = %#v", pulses[0])
	}
	released, err := app.ReleaseEngagementClaim(created.State.ID, "channel:telegram:chat:msg")
	if err != nil {
		t.Fatal(err)
	}
	if released.Claim != nil || released.Status != engagement.StatusPending {
		t.Fatalf("released work = %#v", released)
	}
}

func TestNativeEngagementAgentEvalCoversLifecycleEvidenceScopeAndPortability(t *testing.T) {
	result := runNativeEngagementEval()
	if !result.Pass || !result.RuntimePass || !result.ArtifactPass || result.Status != "done" {
		t.Fatalf("native engagement eval = %#v", result)
	}
	if result.VerifierVersion != engagementEvalVerifierVersion || result.ToolCalls < 30 || result.ArtifactHash == "" {
		t.Fatalf("native engagement eval metadata = %#v", result)
	}
}

func newEngagementToolTestApp(t *testing.T) (*App, func()) {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	db, err := appstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		_ = os.Chdir(oldWD)
		t.Fatal(err)
	}
	catalog, err := engagement.LoadEmbeddedCatalog()
	if err != nil {
		_ = db.Close()
		_ = os.Chdir(oldWD)
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(workspace)
	cfg.Context.Lab = settings.LabContext{Name: "Connected", Target: "10.129.26.26", Hostname: "connected.htb"}
	app := &App{
		cfg: &cfg, registry: tools.New(), db: db,
		engagements: engagement.NewService(db, catalog, nil), suppressEvents: true,
	}
	app.registerAppTools()
	cleanup := func() {
		_ = db.Close()
		_ = os.Chdir(oldWD)
	}
	return app, cleanup
}

func runEngagementCall(t *testing.T, app *App, ctx context.Context, args map[string]any) map[string]any {
	t.Helper()
	result, err := runEngagementCallRaw(app, ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("decode engagement result %q: %v", result, err)
	}
	return decoded
}

func runEngagementCallRaw(app *App, ctx context.Context, args map[string]any) (string, error) {
	raw, _ := json.Marshal(args)
	return app.registry.Run(ctx, llm.ToolCallDef{Function: llm.FunctionCall{Name: "engagement", Arguments: raw}})
}

func resultObject(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	return objectValue(t, envelope, "result")
}

func objectValue(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is not an object in %#v", key, object)
	}
	return value
}

func stringValue(t *testing.T, object map[string]any, key string) string {
	t.Helper()
	value, ok := object[key].(string)
	if !ok {
		t.Fatalf("%q is not a string in %#v", key, object)
	}
	return value
}

func uintValue(t *testing.T, object map[string]any, key string) uint64 {
	t.Helper()
	value, ok := object[key].(float64)
	if !ok || value < 0 {
		t.Fatalf("%q is not a non-negative number in %#v", key, object)
	}
	return uint64(value)
}
