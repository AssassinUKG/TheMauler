package app

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type networkAssessmentEvidence struct {
	OpenPorts       []string
	ScanScope       string
	Artifacts       []string
	Authenticated   bool
	HasScanEvidence bool
}

var (
	onlyOpenPortClaimRE = regexp.MustCompile(`(?i)\bonly\s+(?:tcp\s+)?port\s+([0-9]+)(?:/tcp)?\s+(?:was\s+|is\s+)?open\b`)
	topPortsInputRE     = regexp.MustCompile(`(?i)--top-ports(?:=|\s+)([0-9]+)`)
	artifactLineRE      = regexp.MustCompile(`(?im)^(?:artifact:|#\s*)([^\r\n]+\.(?:txt|nmap|gnmap|xml))\s*$`)
)

func collectNetworkAssessmentEvidence(run TaskRun) networkAssessmentEvidence {
	portSet := map[string]bool{}
	artifactSet := map[string]bool{}
	topPorts := 0
	footerTotal := 0
	requestedFull := false
	conflictingScope := false
	authenticated := false
	for _, tool := range run.Tools {
		combined := tool.Input + "\n" + tool.Result
		lower := strings.ToLower(combined)
		if strings.Contains(lower, "nmap") || nmapOpenPortLineRe.MatchString(tool.Result) {
			for _, match := range nmapOpenPortLineRe.FindAllStringSubmatch(tool.Result, -1) {
				if len(match) == 3 {
					portSet[match[1]+"/"+strings.ToLower(match[2])] = true
				}
			}
			if match := topPortsInputRE.FindStringSubmatch(tool.Input); len(match) == 2 {
				n, _ := strconv.Atoi(match[1])
				if n > topPorts {
					topPorts = n
				}
			}
			if nmapCommandRequestsFullTCP(tool.Input) {
				requestedFull = true
			}
			if strings.Contains(tool.Input, "--top-ports") && nmapCommandRequestsFullTCP(tool.Input) {
				conflictingScope = true
			}
			closed := 0
			if match := nmapClosedCountRe.FindStringSubmatch(tool.Result); len(match) == 2 {
				closed, _ = strconv.Atoi(match[1])
			}
			if total := closed + len(nmapOpenPortLineRe.FindAllStringSubmatch(tool.Result, -1)); total > footerTotal {
				footerTotal = total
			}
		}
		for _, match := range artifactLineRE.FindAllStringSubmatch(tool.Result, -1) {
			if len(match) == 2 {
				artifactSet[strings.TrimSpace(match[1])] = true
			}
		}
		if strings.EqualFold(strings.TrimSpace(tool.Status), "done") && hasAny(lower,
			"[terminal_read state=connected]", "authenticated session verified", "login successful", "uid=0(root)", "uid=") {
			authenticated = true
		}
	}

	ports := make([]string, 0, len(portSet))
	for port := range portSet {
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool {
		left, _ := strconv.Atoi(strings.SplitN(ports[i], "/", 2)[0])
		right, _ := strconv.Atoi(strings.SplitN(ports[j], "/", 2)[0])
		if left == right {
			return ports[i] < ports[j]
		}
		return left < right
	})
	artifacts := make([]string, 0, len(artifactSet))
	for artifact := range artifactSet {
		artifacts = append(artifacts, artifact)
	}
	sort.Strings(artifacts)

	scope := ""
	switch {
	case conflictingScope:
		scope = "conflicting Nmap port selectors; full-scan coverage was not verified"
	case requestedFull && footerTotal >= 65535:
		scope = "verified full TCP scan (1-65535)"
	case requestedFull && footerTotal > 0:
		scope = fmt.Sprintf("full TCP scan requested, but the captured footer accounts for only %d ports", footerTotal)
	case topPorts > 0:
		scope = fmt.Sprintf("bounded top-%d port scan (not full TCP coverage)", topPorts)
	case footerTotal > 0 && footerTotal < 65535:
		scope = fmt.Sprintf("bounded scan accounting for %d ports (not full TCP coverage)", footerTotal)
	}
	return networkAssessmentEvidence{
		OpenPorts: ports, ScanScope: scope, Artifacts: artifacts, Authenticated: authenticated,
		HasScanEvidence: len(ports) > 0 || scope != "",
	}
}

func networkEvidenceSummaryConflict(run TaskRun, summary string) string {
	evidence := collectNetworkAssessmentEvidence(run)
	if !evidence.HasScanEvidence || strings.TrimSpace(summary) == "" {
		return ""
	}
	lower := strings.ToLower(summary)
	if len(evidence.OpenPorts) > 1 {
		if match := onlyOpenPortClaimRE.FindStringSubmatch(summary); len(match) == 2 {
			return fmt.Sprintf("final report claimed only port %s was open, but saved scan evidence records %s open", match[1], strings.Join(evidence.OpenPorts, ", "))
		}
	}
	if networkEvidenceContains(evidence.OpenPorts, "443/tcp") && hasAny(lower, "no https", "https was closed", "https is closed", "443/tcp closed", "port 443 was closed", "port 443 is closed") {
		return "final report denied HTTPS/443, but saved scan evidence records 443/tcp open"
	}
	if evidence.ScanScope != "" && !strings.HasPrefix(evidence.ScanScope, "verified full") && hasAny(lower, "full tcp scan", "all 65535 ports", "full 1-65535", "every tcp port") {
		return "final report described full TCP coverage, but saved scan evidence says: " + evidence.ScanScope
	}
	if !evidence.Authenticated && hasAny(lower, "default credentials worked", "login successful", "logged in successfully", "authenticated successfully", "router was compromised", "exploit succeeded") {
		return "final report claimed authenticated access or exploitation without matching verified session evidence"
	}
	return ""
}

func buildNetworkAssessmentEvidenceHandoff(run TaskRun, conflict string) string {
	evidence := collectNetworkAssessmentEvidence(run)
	var sb strings.Builder
	sb.WriteString("The assessment stopped before a fully verified final report.\n\nConfirmed from saved tool evidence:\n")
	if len(evidence.OpenPorts) > 0 {
		sb.WriteString("- Open services observed by Nmap: " + strings.Join(evidence.OpenPorts, ", ") + ".\n")
	}
	if evidence.ScanScope != "" {
		sb.WriteString("- Scan scope: " + evidence.ScanScope + ".\n")
	}
	for _, artifact := range evidence.Artifacts {
		sb.WriteString("- Saved artifact: " + artifact + "\n")
	}
	if !evidence.Authenticated {
		sb.WriteString("\nNot verified:\n- No authenticated session or successful exploitation was established by the recorded evidence. Generic HTTP 200/error pages must not be treated as successful endpoints.\n")
	}
	if strings.TrimSpace(conflict) != "" {
		sb.WriteString("\nWhy Mauler replaced the generated report:\n- " + strings.TrimSpace(conflict) + ".\n")
	}
	if run.StopReason != "" {
		sb.WriteString("\nRun status: stopped (" + strings.ReplaceAll(run.StopReason, "_", " ") + ").")
	}
	return strings.TrimSpace(sb.String())
}

func buildRecoveryEvidenceDigest(run TaskRun, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 7000
	}
	var sb strings.Builder
	if evidence := collectNetworkAssessmentEvidence(run); evidence.HasScanEvidence {
		sb.WriteString("Controller-derived network evidence (authoritative over assistant prose):\n")
		if len(evidence.OpenPorts) > 0 {
			sb.WriteString("- observed open ports: " + strings.Join(evidence.OpenPorts, ", ") + "\n")
		}
		if evidence.ScanScope != "" {
			sb.WriteString("- scan scope: " + evidence.ScanScope + "\n")
		}
		if !evidence.Authenticated {
			sb.WriteString("- authenticated access/exploitation: not verified\n")
		}
	}
	count := 0
	for i := len(run.Tools) - 1; i >= 0 && count < 8 && sb.Len() < maxChars; i-- {
		tool := run.Tools[i]
		if strings.TrimSpace(tool.Result) == "" {
			continue
		}
		fmt.Fprintf(&sb, "\n- %s [%s]\n", firstNonEmpty(tool.Name, "tool"), firstNonEmpty(tool.Status, "unknown"))
		if strings.TrimSpace(tool.Input) != "" {
			fmt.Fprintf(&sb, "input: %s\n", truncateRunes(redactSensitiveToolResult(tool.Input), 450))
		}
		fmt.Fprintf(&sb, "result: %s\n", truncateRunes(tool.Result, 1100))
		count++
	}
	return truncateRunes(strings.TrimSpace(sb.String()), maxChars)
}

func networkEvidenceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
