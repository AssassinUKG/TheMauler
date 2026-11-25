package app

import (
	"strings"
	"testing"
	"time"
)

func TestNanoCyberAdmissionPlanBindsRunGenerationAndAttempt(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	run := &TaskRun{ID: "task-2026-10-29T10-00-00Z-1", Generation: 42, ConversationEpoch: 7}
	spec := mustSubagentSpec(t, "subagent_testfix")
	plan, err := buildNanoCyberAdmissionPlan(run, spec, "subagent-99", "eng_01ARZ3NDEKTSV4RRFFQ69G5FAV", 240, 12, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Request.ParentRunID, "/generation-42/epoch-7") || plan.Request.SubagentID == "subagent-99" {
		t.Fatalf("request identities are not attempt-bound: %#v", plan.Request)
	}
	if len(plan.Digest) != 64 || plan.Request.Network.Profile != "offline" || plan.Request.EnvironmentProfile != "offline-code" {
		t.Fatalf("unexpected plan: %#v", plan)
	}

	run.Generation++
	next, err := buildNanoCyberAdmissionPlan(run, spec, "subagent-99", "eng_01ARZ3NDEKTSV4RRFFQ69G5FAV", 240, 12, now)
	if err != nil {
		t.Fatal(err)
	}
	if next.Digest == plan.Digest {
		t.Fatal("generation change did not change the sealed request digest")
	}
}

func TestNanoCyberAdmissionPlanRejectsMissingAuthorityAndCrossRepoID(t *testing.T) {
	now := time.Now().UTC()
	spec := mustSubagentSpec(t, "subagent_testfix")
	if _, err := buildNanoCyberAdmissionPlan(&TaskRun{ID: "run", Generation: 0}, spec, "attempt", "eng_01ARZ3NDEKTSV4RRFFQ69G5FAV", 30, 2, now); err == nil || !strings.Contains(err.Error(), "authoritative") {
		t.Fatalf("missing generation accepted: %v", err)
	}
	if _, err := buildNanoCyberAdmissionPlan(&TaskRun{ID: "run", Generation: 1}, spec, "attempt", "", 30, 2, now); err == nil || !strings.Contains(err.Error(), "engagement authority") {
		t.Fatalf("missing NanoCyber engagement authority accepted: %v", err)
	}
	if _, err := buildNanoCyberAdmissionPlan(&TaskRun{ID: "run", Generation: 1}, spec, "attempt", "eng-deadbeef", 30, 2, now); err == nil || !strings.Contains(err.Error(), "eng_ opaque ID") {
		t.Fatalf("Mauler engagement ID accepted as NanoCyber authority: %v", err)
	}
}
