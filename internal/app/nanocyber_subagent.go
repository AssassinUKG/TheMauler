package app

import (
	"encoding/base64"
	"fmt"
	"time"

	"mauler/internal/nanocyber"
)

type nanoCyberAdmissionPlan struct {
	Request nanocyber.LeaseRequest
	Digest  string
}

// buildNanoCyberAdmissionPlan binds a future lease request to the exact
// authoritative parent run generation/epoch and one subagent attempt. It does
// not perform admission: live use remains gated on engagement binding,
// typed-job execution, receipt retrieval, and terminal-status reconciliation.
func buildNanoCyberAdmissionPlan(run *TaskRun, spec subagentSpec, subagentID, nanoCyberEngagementID string, timeout, maxToolCalls int, now time.Time) (nanoCyberAdmissionPlan, error) {
	if run == nil || run.ID == "" || run.Generation == 0 {
		return nanoCyberAdmissionPlan{}, fmt.Errorf("NanoCyber admission requires an authoritative parent run generation")
	}
	if spec.EnvironmentProfile != "offline-code" {
		return nanoCyberAdmissionPlan{}, fmt.Errorf("unsupported NanoCyber subagent environment %q", spec.EnvironmentProfile)
	}
	if subagentID == "" {
		return nanoCyberAdmissionPlan{}, fmt.Errorf("NanoCyber admission requires a subagent attempt ID")
	}
	if nanoCyberEngagementID == "" {
		return nanoCyberAdmissionPlan{}, fmt.Errorf("NanoCyber admission requires an explicit NanoCyber engagement authority")
	}
	if timeout < 10 {
		timeout = 10
	}
	if maxToolCalls < 1 {
		maxToolCalls = 1
	}
	ttl := time.Duration(timeout)*time.Second + 5*time.Minute
	if ttl < 10*time.Minute {
		ttl = 10 * time.Minute
	}
	if ttl > 2*time.Hour {
		ttl = 2 * time.Hour
	}
	maxJobs := maxToolCalls
	if maxJobs > 4 {
		maxJobs = 4
	}
	request := nanocyber.NewOfflineCodeLease(nanocyber.OfflineCodeInput{
		ParentRunID:      nanoCyberParentRunIdentity(*run),
		SubagentID:       nanoCyberAttemptIdentity(subagentID),
		Role:             string(spec.Kind),
		EngagementID:     nanoCyberEngagementID,
		AllowedTools:     []string{"read_file", "write_file", "run_typed_job"},
		MaxTurns:         uint32(spec.MaxTurns),
		MaxToolCalls:     uint32(maxToolCalls),
		MaxJobs:          uint32(maxJobs),
		MaxOutputBytes:   4 * 1024 * 1024,
		EvidenceRequired: []string{"command_receipt", "test_result", "verification_result", "environment_manifest"},
		TTL:              ttl,
	}, now.UTC())
	if err := request.Validate(now.UTC()); err != nil {
		return nanoCyberAdmissionPlan{}, err
	}
	_, digest, err := nanocyber.EncodeLeaseRequest(request)
	if err != nil {
		return nanoCyberAdmissionPlan{}, err
	}
	return nanoCyberAdmissionPlan{Request: request, Digest: digest}, nil
}

func nanoCyberParentRunIdentity(run TaskRun) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(run.ID))
	return fmt.Sprintf("mauler/%s/generation-%d/epoch-%d", encoded, run.Generation, run.ConversationEpoch)
}

func nanoCyberAttemptIdentity(subagentID string) string {
	return "mauler-" + base64.RawURLEncoding.EncodeToString([]byte(subagentID))
}
