package nanocyber

import (
	"strings"
	"testing"
	"time"
)

func TestOfflineCodeLeaseMatchesNanoCyberV1Boundary(t *testing.T) {
	now := time.Now().UTC()
	request := NewOfflineCodeLease(OfflineCodeInput{
		ParentRunID: "run-42/generation-3", SubagentID: "subagent-7", Role: "Test/Fix",
		EngagementID: "eng_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		AllowedTools: []string{"read_file", "write_file", "run_typed_job"},
		MaxTurns:     6, MaxToolCalls: 12, MaxJobs: 4, MaxOutputBytes: 4 * 1024 * 1024,
		EvidenceRequired: []string{"command_receipt", "test_result"}, TTL: 30 * time.Minute,
	}, now)

	if err := request.Validate(now); err != nil {
		t.Fatalf("valid offline lease rejected: %v", err)
	}
	if request.Caller != "themauler" || request.Network.Profile != NetworkOffline {
		t.Fatalf("unexpected lease identity: %#v", request)
	}
}

func TestLeaseValidationRejectsUnboundedOrExpandedAuthority(t *testing.T) {
	now := time.Now().UTC()
	request := NewOfflineCodeLease(OfflineCodeInput{
		ParentRunID: "run-42", SubagentID: "subagent-7", Role: "Test/Fix",
		EngagementID: "eng_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		AllowedTools: []string{"run_typed_job"}, MaxTurns: 6, MaxToolCalls: 12,
		MaxJobs: 4, MaxOutputBytes: 1024, EvidenceRequired: []string{"command_receipt"},
		TTL: 30 * time.Minute,
	}, now)
	request.Network.AllowedTargets = []Target{{Host: "example.test"}}
	if err := request.Validate(now); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("expected offline authority expansion to fail, got %v", err)
	}

	request = NewOfflineCodeLease(OfflineCodeInput{
		ParentRunID: "run-42", SubagentID: "subagent-7", Role: "Test/Fix",
		EngagementID: "eng_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		AllowedTools: []string{"run_typed_job"}, MaxTurns: 6, MaxToolCalls: 12,
		MaxJobs: 65, MaxOutputBytes: 1024, EvidenceRequired: []string{"command_receipt"},
		TTL: 30 * time.Minute,
	}, now)
	if err := request.Validate(now); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected oversized job budget to fail, got %v", err)
	}
}

func TestLeaseValidationRejectsUnknownEvidenceContract(t *testing.T) {
	now := time.Now().UTC()
	request := NewOfflineCodeLease(OfflineCodeInput{
		ParentRunID: "run-42", SubagentID: "subagent-7", Role: "Test/Fix",
		EngagementID: "eng_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		AllowedTools: []string{"run_typed_job"}, MaxTurns: 6, MaxToolCalls: 12,
		MaxJobs: 4, MaxOutputBytes: 1024, EvidenceRequired: []string{"model_said_passed"},
		TTL: 30 * time.Minute,
	}, now)
	if err := request.Validate(now); err == nil || !strings.Contains(err.Error(), "evidence kind") {
		t.Fatalf("expected unknown evidence kind to fail, got %v", err)
	}
}

func TestLeaseValidationRejectsMaulerEngagementIDAsNanoCyberAuthority(t *testing.T) {
	now := time.Now().UTC()
	request := NewOfflineCodeLease(OfflineCodeInput{
		ParentRunID: "run-42", SubagentID: "subagent-7", Role: "Test/Fix",
		EngagementID: "eng-deadbeef", AllowedTools: []string{"run_typed_job"},
		MaxTurns: 2, MaxToolCalls: 2, MaxJobs: 1, MaxOutputBytes: 1024,
		EvidenceRequired: []string{"command_receipt"}, TTL: 10 * time.Minute,
	}, now)
	if err := request.Validate(now); err == nil || !strings.Contains(err.Error(), "eng_ opaque ID") {
		t.Fatalf("expected cross-authority engagement ID rejection, got %v", err)
	}
}
