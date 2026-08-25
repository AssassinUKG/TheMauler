package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"mauler/internal/engagement"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

const maxEngagementToolEndpoints = 24

type engagementClaimantKey struct{}

type engagementClaimantContext struct {
	ID    string
	Alias string
}

func withEngagementClaimant(ctx context.Context, id, alias string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, engagementClaimantKey{}, engagementClaimantContext{ID: strings.TrimSpace(id), Alias: strings.TrimSpace(alias)})
}

func engagementClaimantFromContext(ctx context.Context) (engagement.Claimant, error) {
	if ctx == nil {
		return engagement.Claimant{}, fmt.Errorf("engagement claimant context is missing")
	}
	value, _ := ctx.Value(engagementClaimantKey{}).(engagementClaimantContext)
	if strings.TrimSpace(value.ID) == "" {
		return engagement.Claimant{}, fmt.Errorf("engagement claimant context is missing")
	}
	return engagement.Claimant{ID: value.ID, Alias: value.Alias}, nil
}

type engagementTool struct{ app *App }

func (t *engagementTool) Name() string { return "engagement" }

func (t *engagementTool) Description() string {
	return "Only for an explicitly requested project Engagement Grid workflow; it is not general task completion or ordinary file analysis. Read and update the active Grid. Use available to find distinct parallel checks, or now/status for the single deterministic cursor, then claim -> test -> add_evidence -> observe -> finish. get_notes/set_notes share the compact project risk model with the operator. Evidence stores only hashed RunLedger/file provenance, never duplicated bodies. Vulnerable checks cannot finish without raw evidence; findings require reproducible raw evidence and report-ready screenshot/waiver gates. add_endpoint is locked to the operator-configured target scope."
}

func (t *engagementTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "action": {"type": "string", "enum": ["list", "create", "now", "available", "status", "get_notes", "set_notes", "claim", "observe", "finish", "advance", "add_endpoint", "group_endpoint", "add_evidence", "list_evidence", "upsert_finding", "confirm_finding", "list_findings"]},
    "engagement_id": {"type": "string"},
    "name": {"type": "string"},
    "workflow_id": {"type": "string"},
    "scope": {"type": "array", "items": {"type": "string"}},
    "kind": {"type": "string", "enum": ["step", "global_check", "endpoint_check"]},
    "phase_id": {"type": "string"},
    "endpoint_id": {"type": "string"},
    "work_id": {"type": "string"},
    "lease_seconds": {"type": "integer", "minimum": 15, "maximum": 300},
    "limit": {"type": "integer", "minimum": 1, "maximum": 12},
    "status": {"type": "string", "enum": ["done", "skipped", "failed", "passed", "warning", "vulnerable", "not_applicable"]},
    "observation": {"type": "string"},
    "notes": {"type": "string", "maxLength": 24000},
    "expected_revision": {"type": "integer", "minimum": 1},
    "method": {"type": "string"},
    "url": {"type": "string"},
    "feature_group": {"type": "string"}
    ,"source_kind": {"type": "string", "enum": ["ledger_event", "artifact", "screenshot", "http_capture", "external_file"]}
    ,"ledger_event_id": {"type": "string"}
    ,"path": {"type": "string"}
    ,"description": {"type": "string"}
    ,"finding_id": {"type": "string"}
    ,"title": {"type": "string"}
    ,"severity": {"type": "string", "enum": ["info", "low", "medium", "high", "critical"]}
    ,"impact": {"type": "string"}
    ,"recommendation": {"type": "string"}
    ,"confidence": {"type": "string"}
    ,"reproduction": {"type": "string"}
    ,"evidence_ids": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["action"]
}`)
}

func (t *engagementTool) Destructive() bool { return false }

func (t *engagementTool) Metadata() tools.ToolMetadata {
	return tools.ToolMetadata{
		AccessClass: "memory", LatencyClass: "instant", OutputClass: "normal",
		SideEffects: []string{"engagement_state"}, PreferredNext: []string{"shell", "http_probe", "evidence_bundle"}, UnrestrictedReady: true,
	}
}

type engagementToolArgs struct {
	Action           string   `json:"action"`
	EngagementID     string   `json:"engagement_id"`
	Name             string   `json:"name"`
	WorkflowID       string   `json:"workflow_id"`
	Scope            []string `json:"scope"`
	Kind             string   `json:"kind"`
	PhaseID          string   `json:"phase_id"`
	EndpointID       string   `json:"endpoint_id"`
	WorkID           string   `json:"work_id"`
	LeaseSeconds     int      `json:"lease_seconds"`
	Limit            int      `json:"limit"`
	Status           string   `json:"status"`
	Observation      string   `json:"observation"`
	Notes            string   `json:"notes"`
	ExpectedRevision uint64   `json:"expected_revision"`
	Method           string   `json:"method"`
	URL              string   `json:"url"`
	FeatureGroup     string   `json:"feature_group"`
	SourceKind       string   `json:"source_kind"`
	LedgerEventID    string   `json:"ledger_event_id"`
	Path             string   `json:"path"`
	Description      string   `json:"description"`
	FindingID        string   `json:"finding_id"`
	Title            string   `json:"title"`
	Severity         string   `json:"severity"`
	Impact           string   `json:"impact"`
	Recommendation   string   `json:"recommendation"`
	Confidence       string   `json:"confidence"`
	Reproduction     string   `json:"reproduction"`
	EvidenceIDs      []string `json:"evidence_ids"`
}

func (t *engagementTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	if t == nil || t.app == nil || t.app.engagements == nil {
		return "", fmt.Errorf("engagement service is unavailable")
	}
	var args engagementToolArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("engagement: bad params: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	switch action {
	case "list":
		return t.runList(ctx)
	case "create":
		return t.runCreate(ctx, args)
	case "now", "status":
		record, err := t.resolveRecord(ctx, args.EngagementID)
		if err != nil {
			return "", err
		}
		return t.statusResult(ctx, action, record)
	case "available":
		return t.runAvailable(ctx, args)
	case "get_notes":
		return t.runGetNotes(ctx, args)
	case "set_notes":
		return t.runSetNotes(ctx, args)
	case "claim":
		return t.runClaim(ctx, args)
	case "observe":
		return t.runObserve(ctx, args)
	case "finish":
		return t.runFinish(ctx, args)
	case "advance":
		return t.runAdvance(ctx, args)
	case "add_endpoint":
		return t.runAddEndpoint(ctx, args)
	case "group_endpoint":
		return t.runGroupEndpoint(ctx, args)
	case "add_evidence":
		return t.runAddEvidence(ctx, args)
	case "list_evidence":
		return t.runListEvidence(ctx, args)
	case "upsert_finding":
		return t.runUpsertFinding(ctx, args)
	case "confirm_finding":
		return t.runConfirmFinding(ctx, args)
	case "list_findings":
		return t.runListFindings(ctx, args)
	default:
		return "", fmt.Errorf("engagement: unsupported action %q", args.Action)
	}
}

func (t *engagementTool) runGetNotes(ctx context.Context, args engagementToolArgs) (string, error) {
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	return marshalEngagementToolResult("get_notes", map[string]any{
		"engagement_id": record.State.ID,
		"notes":         record.State.Notes,
		"revision":      record.State.NotesRevision,
		"updated_at":    record.State.NotesUpdatedAt,
	})
}

func (t *engagementTool) runSetNotes(ctx context.Context, args engagementToolArgs) (string, error) {
	if args.ExpectedRevision == 0 {
		return "", fmt.Errorf("engagement: set_notes requires expected_revision from get_notes or status")
	}
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	revision, err := t.app.engagements.SetNotes(ctx, record.State.ID, args.Notes, args.ExpectedRevision, time.Now())
	if err != nil {
		return "", err
	}
	return marshalEngagementToolResult("set_notes", map[string]any{
		"engagement_id": record.State.ID,
		"revision":      revision,
		"characters":    len([]rune(args.Notes)),
	})
}

func (t *engagementTool) runAvailable(ctx context.Context, args engagementToolArgs) (string, error) {
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	claimantID := ""
	if claimant, claimantErr := engagementClaimantFromContext(ctx); claimantErr == nil {
		claimantID = claimant.ID
	}
	queue, err := t.app.engagements.Available(ctx, record.State.ID, claimantID, args.Limit, time.Now())
	if err != nil {
		if errors.Is(err, engagement.ErrPhaseBlocked) {
			return marshalEngagementToolResult("available", compactEngagementAvailable(record.State.ID, queue))
		}
		return "", err
	}
	return marshalEngagementToolResult("available", compactEngagementAvailable(record.State.ID, queue))
}

func (t *engagementTool) runAddEvidence(ctx context.Context, args engagementToolArgs) (string, error) {
	claimant, err := engagementClaimantFromContext(ctx)
	if err != nil {
		return "", err
	}
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	ref, err := engagementWorkRef(args)
	if err != nil {
		return "", err
	}
	created, err := t.app.engagements.AddEvidence(ctx, engagement.EvidenceInput{
		EngagementID: record.State.ID, Work: ref, Claimant: claimant, SourceKind: args.SourceKind,
		LedgerEventID: args.LedgerEventID, Path: args.Path, Description: args.Description,
	}, time.Now())
	if err != nil {
		return "", err
	}
	return marshalEngagementToolResult("add_evidence", map[string]any{"engagement_id": record.State.ID, "evidence": compactEngagementEvidence(created)})
}

func (t *engagementTool) runListEvidence(ctx context.Context, args engagementToolArgs) (string, error) {
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	items := make([]map[string]any, 0, minInt(len(record.State.EvidenceOrder), 40))
	for _, id := range record.State.EvidenceOrder {
		if evidence := record.State.Evidence[id]; evidence != nil {
			items = append(items, compactEngagementEvidence(*evidence))
		}
		if len(items) >= 40 {
			break
		}
	}
	return marshalEngagementToolResult("list_evidence", map[string]any{"engagement_id": record.State.ID, "evidence": items, "truncated": len(record.State.EvidenceOrder) > len(items)})
}

func (t *engagementTool) runUpsertFinding(ctx context.Context, args engagementToolArgs) (string, error) {
	claimant, err := engagementClaimantFromContext(ctx)
	if err != nil {
		return "", err
	}
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	ref, err := engagementWorkRef(args)
	if err != nil {
		return "", err
	}
	updated, err := t.app.engagements.UpsertFinding(ctx, engagement.FindingInput{
		ID: args.FindingID, EngagementID: record.State.ID, Work: ref, Claimant: claimant,
		Title: args.Title, Severity: args.Severity, Description: args.Description, Impact: args.Impact,
		Recommendation: args.Recommendation, Confidence: args.Confidence, Reproduction: args.Reproduction,
		EvidenceIDs: args.EvidenceIDs, ExpectedRevision: args.ExpectedRevision,
	}, time.Now())
	if err != nil {
		return "", err
	}
	return marshalEngagementToolResult("upsert_finding", map[string]any{"engagement_id": record.State.ID, "finding": compactEngagementFinding(updated)})
}

func (t *engagementTool) runConfirmFinding(ctx context.Context, args engagementToolArgs) (string, error) {
	claimant, err := engagementClaimantFromContext(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(args.FindingID) == "" || args.ExpectedRevision == 0 {
		return "", fmt.Errorf("engagement: confirm_finding requires finding_id and expected_revision")
	}
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	confirmed, err := t.app.engagements.ConfirmFinding(ctx, record.State.ID, args.FindingID, claimant.ID, args.ExpectedRevision, "", false, time.Now())
	if err != nil {
		return "", err
	}
	return marshalEngagementToolResult("confirm_finding", map[string]any{"engagement_id": record.State.ID, "finding": compactEngagementFinding(confirmed)})
}

func (t *engagementTool) runListFindings(ctx context.Context, args engagementToolArgs) (string, error) {
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	items := make([]map[string]any, 0, minInt(len(record.State.FindingOrder), 40))
	for _, id := range record.State.FindingOrder {
		if finding := record.State.Findings[id]; finding != nil {
			items = append(items, compactEngagementFinding(*finding))
		}
		if len(items) >= 40 {
			break
		}
	}
	return marshalEngagementToolResult("list_findings", map[string]any{"engagement_id": record.State.ID, "findings": items, "truncated": len(record.State.FindingOrder) > len(items)})
}

func (t *engagementTool) runList(ctx context.Context) (string, error) {
	workspace := workspaceScope()
	if workspace == "" {
		return "", fmt.Errorf("engagement: current workspace is unavailable")
	}
	items, err := t.app.engagements.List(ctx, workspace)
	if err != nil {
		return "", err
	}
	if len(items) > 20 {
		items = items[:20]
	}
	return marshalEngagementToolResult("list", map[string]any{"workspace": workspace, "engagements": items})
}

func (t *engagementTool) runCreate(ctx context.Context, args engagementToolArgs) (string, error) {
	workspace := workspaceScope()
	if workspace == "" {
		return "", fmt.Errorf("engagement: current workspace is unavailable")
	}
	lab := t.app.engagementLabContext()
	scope, err := lockedAuthoritativeScope(lab, args.Scope)
	if err != nil {
		return "", fmt.Errorf("engagement: create: %w", err)
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		name = strings.TrimSpace(lab.Name)
	}
	if name == "" {
		name = filepath.Base(filepath.Clean(workspace))
	}
	workflowID := strings.TrimSpace(args.WorkflowID)
	if workflowID == "" {
		workflowID = "webapp-simple"
	}
	record, err := t.app.engagements.Create(ctx, engagement.CreateInput{
		Name: name, Workspace: workspace, WorkflowID: workflowID, Scope: scope, ScopeLocked: true,
	}, time.Now())
	if err != nil {
		return "", err
	}
	return t.statusResult(ctx, "create", record)
}

func (a *App) engagementLabContext() settings.LabContext {
	if a == nil {
		return settings.LabContext{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		return settings.LabContext{}
	}
	return a.cfg.Context.Lab
}

func (t *engagementTool) runClaim(ctx context.Context, args engagementToolArgs) (string, error) {
	claimant, err := engagementClaimantFromContext(ctx)
	if err != nil {
		return "", err
	}
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	ref, err := engagementWorkRef(args)
	if err != nil {
		return "", err
	}
	lease := time.Duration(args.LeaseSeconds) * time.Second
	if args.LeaseSeconds == 0 {
		lease = engagement.DefaultClaimLease
	}
	work, err := t.app.engagements.Claim(ctx, record.State.ID, ref, claimant, lease, time.Now())
	if err != nil {
		return "", err
	}
	return marshalEngagementToolResult("claim", map[string]any{"engagement_id": record.State.ID, "work": compactEngagementWork(work)})
}

func (t *engagementTool) runObserve(ctx context.Context, args engagementToolArgs) (string, error) {
	claimant, err := engagementClaimantFromContext(ctx)
	if err != nil {
		return "", err
	}
	if args.ExpectedRevision == 0 {
		return "", fmt.Errorf("engagement: observe requires expected_revision from the claim result")
	}
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	ref, err := engagementWorkRef(args)
	if err != nil {
		return "", err
	}
	work, err := t.app.engagements.RecordResult(ctx, engagement.ObservationInput{
		EngagementID: record.State.ID, Ref: ref, ClaimantID: claimant.ID,
		Status: args.Status, Observation: args.Observation, ExpectedRevision: args.ExpectedRevision,
	}, time.Now())
	if err != nil {
		return "", err
	}
	return marshalEngagementToolResult("observe", map[string]any{"engagement_id": record.State.ID, "work": compactEngagementWork(work)})
}

func (t *engagementTool) runFinish(ctx context.Context, args engagementToolArgs) (string, error) {
	claimant, err := engagementClaimantFromContext(ctx)
	if err != nil {
		return "", err
	}
	if args.ExpectedRevision == 0 {
		return "", fmt.Errorf("engagement: finish requires expected_revision from the observe result")
	}
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	ref, err := engagementWorkRef(args)
	if err != nil {
		return "", err
	}
	finished, err := t.app.engagements.Finish(ctx, record.State.ID, ref, claimant.ID, args.ExpectedRevision, time.Now())
	if err != nil {
		return "", err
	}
	refreshed, err := t.app.engagements.Get(ctx, record.State.ID)
	if err != nil {
		return "", err
	}
	next, blocker := t.currentNext(ctx, refreshed)
	return marshalEngagementToolResult("finish", map[string]any{
		"engagement_id": record.State.ID, "work": compactEngagementWork(finished.Work),
		"run_again": finished.RunAgain, "next_run": finished.NextRun, "next": next, "blocker": blocker,
	})
}

func (t *engagementTool) runAdvance(ctx context.Context, args engagementToolArgs) (string, error) {
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	phase, err := t.app.engagements.Advance(ctx, record.State.ID, time.Now())
	if err != nil {
		return "", err
	}
	refreshed, err := t.app.engagements.Get(ctx, record.State.ID)
	if err != nil {
		return "", err
	}
	next, blocker := t.currentNext(ctx, refreshed)
	return marshalEngagementToolResult("advance", map[string]any{
		"engagement_id": record.State.ID, "phase_id": phase, "next": next, "blocker": blocker,
	})
}

func (t *engagementTool) runAddEndpoint(ctx context.Context, args engagementToolArgs) (string, error) {
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	endpoint, err := t.app.engagements.AddEndpoint(ctx, record.State.ID, engagement.EndpointInput{
		ID: args.EndpointID, Method: args.Method, URL: args.URL, Name: args.Name, FeatureGroup: args.FeatureGroup,
	}, time.Now())
	if err != nil {
		return "", err
	}
	return marshalEngagementToolResult("add_endpoint", map[string]any{"engagement_id": record.State.ID, "endpoint": compactEngagementEndpoint(endpoint)})
}

func (t *engagementTool) runGroupEndpoint(ctx context.Context, args engagementToolArgs) (string, error) {
	if strings.TrimSpace(args.EndpointID) == "" || strings.TrimSpace(args.FeatureGroup) == "" {
		return "", fmt.Errorf("engagement: group_endpoint requires endpoint_id and feature_group")
	}
	record, err := t.resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", err
	}
	endpoint, err := t.app.engagements.SetEndpointGroup(ctx, record.State.ID, args.EndpointID, args.FeatureGroup, time.Now())
	if err != nil {
		return "", err
	}
	return marshalEngagementToolResult("group_endpoint", map[string]any{"engagement_id": record.State.ID, "endpoint": compactEngagementEndpoint(endpoint)})
}

func (t *engagementTool) resolveRecord(ctx context.Context, id string) (engagement.Record, error) {
	workspace := workspaceScope()
	if workspace == "" {
		return engagement.Record{}, fmt.Errorf("engagement: current workspace is unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		items, err := t.app.engagements.List(ctx, workspace)
		if err != nil {
			return engagement.Record{}, err
		}
		if len(items) == 0 {
			return engagement.Record{}, fmt.Errorf("engagement: no engagement exists for the current workspace; create one first")
		}
		id = items[0].ID
	}
	record, err := t.app.engagements.Get(ctx, id)
	if err != nil {
		return engagement.Record{}, err
	}
	if !sameFilesystemPath(record.Workspace, workspace) {
		return engagement.Record{}, fmt.Errorf("engagement %q belongs to another workspace", id)
	}
	return record, nil
}

func (t *engagementTool) statusResult(ctx context.Context, action string, record engagement.Record) (string, error) {
	next, blocker := t.currentNext(ctx, record)
	refreshed, err := t.app.engagements.Get(ctx, record.State.ID)
	if err == nil {
		record = refreshed
	}
	return marshalEngagementToolResult(action, compactEngagementStatus(record, next, blocker))
}

func (t *engagementTool) currentNext(ctx context.Context, record engagement.Record) (any, string) {
	next, err := t.app.engagements.Now(ctx, record.State.ID, time.Now())
	if err != nil {
		if errors.Is(err, engagement.ErrPhaseBlocked) {
			return nil, err.Error()
		}
		return nil, "state error: " + err.Error()
	}
	return compactEngagementNext(next), ""
}

func engagementWorkRef(args engagementToolArgs) (engagement.WorkRef, error) {
	ref := engagement.WorkRef{
		Kind: strings.ToLower(strings.TrimSpace(args.Kind)), PhaseID: strings.TrimSpace(args.PhaseID),
		EndpointID: strings.TrimSpace(args.EndpointID), ID: strings.TrimSpace(args.WorkID),
	}
	if ref.ID == "" {
		return engagement.WorkRef{}, fmt.Errorf("engagement: work_id is required")
	}
	switch ref.Kind {
	case engagement.WorkStep:
		if ref.PhaseID == "" {
			return engagement.WorkRef{}, fmt.Errorf("engagement: phase_id is required for step work")
		}
	case engagement.WorkGlobalCheck:
	case engagement.WorkEndpointCheck:
		if ref.EndpointID == "" {
			return engagement.WorkRef{}, fmt.Errorf("engagement: endpoint_id is required for endpoint_check work")
		}
	default:
		return engagement.WorkRef{}, fmt.Errorf("engagement: kind must be step, global_check, or endpoint_check")
	}
	return ref, nil
}

func lockedAuthoritativeScope(lab settings.LabContext, requested []string) ([]string, error) {
	authoritative := settings.LabScopeValues(lab)
	if !settings.HasAllowedLabScope(lab) {
		return nil, fmt.Errorf("configure the project target IP/URL or hostname before creating an engagement")
	}
	if len(requested) == 0 {
		return authoritative, nil
	}
	exact := map[string]bool{}
	for _, value := range authoritative {
		exact[strings.ToLower(strings.TrimSpace(value))] = true
	}
	result := []string{}
	seen := map[string]bool{}
	for _, value := range requested {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("requested scope contains an empty entry")
		}
		key := strings.ToLower(value)
		allowed := exact[key]
		if !allowed && !strings.HasPrefix(value, "!") {
			allowed = engagement.CheckTargetScope(authoritative, value).Allowed
		}
		if !allowed {
			return nil, fmt.Errorf("requested scope %q does not match the operator-configured target/hostname", value)
		}
		if !seen[strings.ToLower(value)] {
			seen[strings.ToLower(value)] = true
			result = append(result, value)
		}
	}
	return result, nil
}

func scopeAuthorityKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	parsed, err = url.Parse("//" + value)
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(strings.TrimSuffix(value, "/"), "[]")
}

func marshalEngagementToolResult(action string, payload any) (string, error) {
	data, err := json.Marshal(map[string]any{"action": action, "result": payload})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func compactEngagementStatus(record engagement.Record, next any, blocker string) map[string]any {
	state := record.State
	endpoints := make([]map[string]any, 0, minInt(len(state.EndpointOrder), maxEngagementToolEndpoints))
	for _, id := range state.EndpointOrder {
		if len(endpoints) >= maxEngagementToolEndpoints {
			break
		}
		if endpoint := state.Endpoints[id]; endpoint != nil {
			endpoints = append(endpoints, compactEngagementEndpoint(*endpoint))
		}
	}
	stepsDone, globalDone, endpointDone := finishedEngagementCounts(state)
	return map[string]any{
		"engagement_id": state.ID, "name": state.Name, "workspace": record.Workspace,
		"workflow":  map[string]any{"id": record.Workflow.ID, "version": record.Workflow.Version, "digest": record.Workflow.Digest},
		"checklist": map[string]any{"id": record.Checklist.ID, "version": record.Checklist.Version, "digest": record.Checklist.Digest},
		"phase_id":  state.CurrentPhase, "revision": state.Revision, "notes_revision": state.NotesRevision, "scope": state.Scope, "scope_locked": state.ScopeLocked,
		"progress": map[string]any{
			"steps":           fmt.Sprintf("%d/%d", stepsDone, len(state.Steps)),
			"global_checks":   fmt.Sprintf("%d/%d", globalDone, len(state.GlobalChecks)),
			"endpoint_checks": fmt.Sprintf("%d/%d", endpointDone, len(state.EndpointChecks)),
		},
		"evidence_count": len(state.EvidenceOrder), "finding_count": len(state.FindingOrder),
		"endpoints": endpoints, "endpoints_truncated": len(state.EndpointOrder) > len(endpoints),
		"next": next, "blocker": blocker,
	}
}

func compactEngagementEvidence(evidence engagement.Evidence) map[string]any {
	return map[string]any{
		"id": evidence.ID, "work": evidence.Work, "source_kind": evidence.SourceKind,
		"ledger_event_id": evidence.LedgerEventID, "path": evidence.Path, "sha256": evidence.SHA256,
		"size": evidence.Size, "agent_composed": evidence.AgentComposed, "description": evidence.Description,
		"run": evidence.Run, "created_at": evidence.CreatedAt,
	}
}

func compactEngagementFinding(finding engagement.Finding) map[string]any {
	return map[string]any{
		"id": finding.ID, "work": finding.Work, "title": finding.Title, "severity": finding.Severity,
		"state": finding.State, "description": finding.Description, "reproduction": finding.Reproduction,
		"evidence_ids": finding.EvidenceIDs, "operator_waiver": finding.OperatorWaiver,
		"revision": finding.Revision, "updated_at": finding.UpdatedAt,
	}
}

func compactEngagementNext(next engagement.NextAction) map[string]any {
	result := map[string]any{
		"action": next.Action, "phase_id": next.PhaseID, "phase_name": next.PhaseName,
		"phase_complete": next.PhaseComplete, "workflow_done": next.WorkflowDone,
	}
	if next.Work != nil {
		result["work"] = compactEngagementWork(*next.Work)
	}
	return result
}

func compactEngagementAvailable(engagementID string, available engagement.AvailableWork) map[string]any {
	items := make([]map[string]any, 0, len(available.Items))
	for _, work := range available.Items {
		items = append(items, compactEngagementWork(work))
	}
	return map[string]any{
		"engagement_id": engagementID, "phase_id": available.PhaseID, "phase_name": available.PhaseName,
		"parallel": available.Parallel, "items": items, "blocker": available.Blocker,
	}
}

func compactEngagementWork(work engagement.WorkState) map[string]any {
	result := map[string]any{
		"ref": work.Ref, "title": work.Title, "status": work.Status, "revision": work.Revision,
		"runs_completed": work.RunsCompleted, "runs": work.Runs, "finished": work.Finished,
	}
	if work.Claim != nil {
		result["claim"] = map[string]any{
			"claimant_id": work.Claim.Claimant.ID, "alias": work.Claim.Claimant.Alias, "lease_until": work.Claim.LeaseUntil,
		}
	}
	return result
}

func compactEngagementEndpoint(endpoint engagement.Endpoint) map[string]any {
	return map[string]any{
		"id": endpoint.ID, "method": endpoint.Method, "url": endpoint.URL,
		"name": endpoint.Name, "feature_group": endpoint.FeatureGroup,
	}
}

func finishedEngagementCounts(state *engagement.State) (steps, global, endpoint int) {
	for _, work := range state.Steps {
		if work != nil && work.Finished {
			steps++
		}
	}
	for _, work := range state.GlobalChecks {
		if work != nil && work.Finished {
			global++
		}
	}
	for _, work := range state.EndpointChecks {
		if work != nil && work.Finished {
			endpoint++
		}
	}
	return steps, global, endpoint
}
