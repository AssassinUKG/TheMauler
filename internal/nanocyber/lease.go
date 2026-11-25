// Package nanocyber defines TheMauler's client-side view of NanoCyber's
// versioned subagent environment lease contract. It deliberately contains no
// Docker or host-shell implementation: NanoCyber remains the authority for
// admission, execution, evidence, cancellation, and cleanup.
package nanocyber

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type NetworkProfile string

const (
	NetworkOffline      NetworkProfile = "offline"
	NetworkDependencies NetworkProfile = "dependencies"
	NetworkScopedTarget NetworkProfile = "scoped_target"
)

type Target struct {
	Scheme     string   `json:"scheme,omitempty"`
	Host       string   `json:"host"`
	Ports      []uint16 `json:"ports,omitempty"`
	PathPrefix string   `json:"path_prefix,omitempty"`
}

type NetworkPolicy struct {
	Profile           NetworkProfile `json:"profile"`
	ScopeRevision     uint64         `json:"scope_revision,omitempty"`
	AllowedTargets    []Target       `json:"allowed_targets,omitempty"`
	DependencySources []string       `json:"dependency_sources,omitempty"`
}

type Budget struct {
	MaxTurns       uint32 `json:"max_turns"`
	MaxToolCalls   uint32 `json:"max_tool_calls"`
	MaxJobs        uint32 `json:"max_jobs"`
	MaxOutputBytes uint64 `json:"max_output_bytes"`
}

type LeaseRequest struct {
	SchemaVersion      uint32        `json:"schema_version"`
	Caller             string        `json:"caller"`
	ParentRunID        string        `json:"parent_run_id"`
	SubagentID         string        `json:"subagent_id"`
	Role               string        `json:"role"`
	EngagementID       string        `json:"engagement_id,omitempty"`
	EnvironmentProfile string        `json:"environment_profile"`
	Network            NetworkPolicy `json:"network"`
	AllowedTools       []string      `json:"allowed_tools"`
	Budget             Budget        `json:"budget"`
	EvidenceRequired   []string      `json:"evidence_required"`
	ExpiresAt          time.Time     `json:"expires_at"`
}

type OfflineCodeInput struct {
	ParentRunID      string
	SubagentID       string
	Role             string
	EngagementID     string
	AllowedTools     []string
	MaxTurns         uint32
	MaxToolCalls     uint32
	MaxJobs          uint32
	MaxOutputBytes   uint64
	EvidenceRequired []string
	TTL              time.Duration
}

func NewOfflineCodeLease(input OfflineCodeInput, now time.Time) LeaseRequest {
	return LeaseRequest{
		SchemaVersion:      1,
		Caller:             "themauler",
		ParentRunID:        input.ParentRunID,
		SubagentID:         input.SubagentID,
		Role:               input.Role,
		EngagementID:       input.EngagementID,
		EnvironmentProfile: "offline-code",
		Network:            NetworkPolicy{Profile: NetworkOffline},
		AllowedTools:       append([]string(nil), input.AllowedTools...),
		Budget: Budget{
			MaxTurns: input.MaxTurns, MaxToolCalls: input.MaxToolCalls,
			MaxJobs: input.MaxJobs, MaxOutputBytes: input.MaxOutputBytes,
		},
		EvidenceRequired: append([]string(nil), input.EvidenceRequired...),
		ExpiresAt:        now.Add(input.TTL),
	}
}

var (
	labelPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
	toolPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	nanoIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}_[0-9A-HJKMNP-TV-Z]{26}$`)
)

func (request LeaseRequest) Validate(now time.Time) error {
	if request.SchemaVersion != 1 || request.Caller != "themauler" {
		return fmt.Errorf("unsupported NanoCyber lease contract")
	}
	if !labelPattern.MatchString(request.ParentRunID) || !labelPattern.MatchString(request.SubagentID) {
		return fmt.Errorf("parent run and subagent IDs must be bounded opaque labels")
	}
	if strings.TrimSpace(request.Role) == "" || len(request.Role) > 128 || strings.ContainsAny(request.Role, "\r\n\x00") {
		return fmt.Errorf("subagent role must be a single bounded line")
	}
	if request.EnvironmentProfile != "offline-code" && request.EnvironmentProfile != "scoped-poc" && request.EnvironmentProfile != "vm-windows" {
		return fmt.Errorf("unsupported NanoCyber environment profile %q", request.EnvironmentProfile)
	}
	if err := request.Network.Validate(); err != nil {
		return err
	}
	if request.Network.Profile == NetworkScopedTarget && strings.TrimSpace(request.EngagementID) == "" {
		return fmt.Errorf("scoped target leases require an engagement ID")
	}
	if request.EngagementID != "" && (!nanoIDPattern.MatchString(request.EngagementID) || !strings.HasPrefix(request.EngagementID, "eng_")) {
		return fmt.Errorf("NanoCyber engagement ID must be a valid eng_ opaque ID")
	}
	if len(request.AllowedTools) == 0 || len(request.AllowedTools) > 64 {
		return fmt.Errorf("NanoCyber lease requires between 1 and 64 tools")
	}
	seenTools := make(map[string]struct{}, len(request.AllowedTools))
	for _, tool := range request.AllowedTools {
		if !toolPattern.MatchString(tool) {
			return fmt.Errorf("invalid NanoCyber tool name %q", tool)
		}
		key := strings.ToLower(tool)
		if _, exists := seenTools[key]; exists {
			return fmt.Errorf("duplicate NanoCyber tool name %q", tool)
		}
		seenTools[key] = struct{}{}
	}
	if request.Budget.MaxTurns == 0 || request.Budget.MaxTurns > 64 ||
		request.Budget.MaxToolCalls == 0 || request.Budget.MaxToolCalls > 256 ||
		request.Budget.MaxJobs == 0 || request.Budget.MaxJobs > 64 ||
		request.Budget.MaxOutputBytes == 0 || request.Budget.MaxOutputBytes > 64*1024*1024 {
		return fmt.Errorf("NanoCyber lease budget exceeds the v1 admission bounds")
	}
	if len(request.EvidenceRequired) == 0 || len(request.EvidenceRequired) > 16 {
		return fmt.Errorf("NanoCyber lease requires between 1 and 16 evidence kinds")
	}
	seenEvidence := make(map[string]struct{}, len(request.EvidenceRequired))
	for _, evidence := range request.EvidenceRequired {
		if !knownEvidence(evidence) {
			return fmt.Errorf("unknown NanoCyber evidence kind %q", evidence)
		}
		if _, exists := seenEvidence[evidence]; exists {
			return fmt.Errorf("duplicate NanoCyber evidence kind %q", evidence)
		}
		seenEvidence[evidence] = struct{}{}
	}
	if now.IsZero() || !request.ExpiresAt.After(now) || request.ExpiresAt.After(now.Add(7*24*time.Hour)) {
		return fmt.Errorf("NanoCyber lease expiry is outside the v1 admission window")
	}
	return nil
}

func knownEvidence(value string) bool {
	switch value {
	case "command_receipt", "artifact_export_receipt", "output", "http_capture", "screenshot",
		"generated_file", "test_result", "service_log", "research_source", "image_derivative",
		"environment_manifest", "verification_result", "operator_note":
		return true
	default:
		return false
	}
}

func (network NetworkPolicy) Validate() error {
	switch network.Profile {
	case NetworkOffline:
		if len(network.AllowedTargets) != 0 || len(network.DependencySources) != 0 || network.ScopeRevision != 0 {
			return fmt.Errorf("offline NanoCyber lease cannot contain network allowances")
		}
	case NetworkDependencies:
		if len(network.DependencySources) == 0 || len(network.AllowedTargets) != 0 || network.ScopeRevision != 0 {
			return fmt.Errorf("dependency NanoCyber lease requires declared sources only")
		}
	case NetworkScopedTarget:
		if network.ScopeRevision == 0 || len(network.AllowedTargets) == 0 || len(network.DependencySources) != 0 {
			return fmt.Errorf("scoped NanoCyber lease requires a scope revision and targets only")
		}
	default:
		return fmt.Errorf("unknown NanoCyber network profile %q", network.Profile)
	}
	return nil
}
