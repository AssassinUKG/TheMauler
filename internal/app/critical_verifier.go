package app

import (
	"encoding/json"
	"regexp"
	"strings"

	"mauler/internal/llm"
)

var (
	verifierWebshellURLRe = regexp.MustCompile(`(?i)https?://[^\s"'<>]+(?:shell|cmd|webshell|\.php)[^\s"'<>]*`)
	verifierUIDRe         = regexp.MustCompile(`(?i)\buid=(\d+)\(([^)]+)\)`)
	verifierRootHintRe    = regexp.MustCompile(`(?i)(?:\buid=0\(root\)|\broot\.txt\b|\bwhoami\s*[:=]?\s*root\b)`)
	verifierDNSMovedRe    = regexp.MustCompile(`(?i)\b(?:different IP|resolv(?:es|ing) to|/etc/hosts|connection refused|failed to connect|exit code 7|no route to host|host unreachable)\b`)
	httpRedirectStatusRe  = regexp.MustCompile(`(?i)\bHTTP/[0-9.]+\s+30[1278]\b`)
	httpRedirectBodyRe    = regexp.MustCompile(`(?i)\b(?:301|302|307|308)\b|\bMoved Permanently\b|\bFound\b|\bTemporary Redirect\b|\bPermanent Redirect\b`)
	httpLocationRe        = regexp.MustCompile(`(?i)\bLocation:\s*(https?://[^\s"'<>]+)`)
	httpHrefRe            = regexp.MustCompile(`(?i)href=["'](https?://[^"']+)["']`)
	ipv4TokenRe           = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
)

func appendCriticalVerifierHint(tc llm.ToolCallDef, result string) string {
	return appendCriticalVerifierHintWithTarget(tc, result, "")
}

func appendCriticalVerifierHintWithTarget(tc llm.ToolCallDef, result, confirmedTarget string) string {
	hint := criticalVerifierHintWithTarget(tc, result, confirmedTarget)
	if hint == "" || strings.Contains(result, "[verifier_required:") {
		return result
	}
	return strings.TrimRight(result, "\r\n") + "\n" + hint
}

func criticalVerifierHint(tc llm.ToolCallDef, result string) string {
	return criticalVerifierHintWithTarget(tc, result, "")
}

func criticalVerifierHintWithTarget(tc llm.ToolCallDef, result, confirmedTarget string) string {
	lowerTool := strings.ToLower(strings.TrimSpace(tc.Function.Name))
	if lowerTool != "shell" && lowerTool != "terminal_send" && lowerTool != "terminal_read" && lowerTool != "http_probe" && lowerTool != "write" && lowerTool != "edit" && lowerTool != "write_file" && lowerTool != "edit_file" {
		return ""
	}
	lowerResult := strings.ToLower(result)
	command := strings.ToLower(probeTargetTextFromToolArgs(tc.Function.Arguments))
	if hint := offTargetIPHint(command, confirmedTarget, result); hint != "" && (lowerTool == "shell" || lowerTool == "http_probe" || lowerTool == "terminal_send") {
		return hint
	}
	if hint := httpRedirectHint(result); hint != "" && (lowerTool == "shell" || lowerTool == "http_probe" || lowerTool == "terminal_send") {
		return hint
	}
	if strings.Contains(lowerResult, "command_failed: exit=") {
		return "[verifier_required:command_exit] The command exited non-zero. Identify the failure class (network/DNS, path/URL, auth, payload, or command syntax) from the output before retrying or claiming success/failure - do not report the step as done."
	}
	if verifierRootHintRe.MatchString(result) {
		return "[verifier_required:root] Before reporting root/success, verify with `id; hostname; pwd` and check the expected flag/evidence path once."
	}
	if strings.Contains(lowerResult, "connection received") || strings.Contains(lowerResult, "connected live session") || strings.Contains(lowerResult, "state=connected") {
		return "[verifier_required:session] Before treating the live session as usable, verify through terminal_send/terminal_read with `id; hostname; pwd`."
	}
	if strings.Contains(lowerResult, "state: listening") || strings.Contains(lowerResult, "listener started") || strings.Contains(lowerResult, "listening on") {
		return "[verifier_required:listener] Before firing a callback, verify the listener is still active with terminal_read, then trigger through a separate shell/webshell path."
	}
	if verifierUIDRe.MatchString(result) {
		return "[verifier_required:shell_user] Before treating shell/user context as settled, verify `id` plus `pwd`/`hostname` or record the exact webshell/session used."
	}
	if verifierWebshellURLRe.MatchString(result) && !strings.Contains(lowerResult, "uid=") {
		return "[verifier_required:webshell] Verify the webshell with a tiny command such as `id` and `pwd` before building the next step on it."
	}
	if verifierDNSMovedRe.MatchString(result) || strings.Contains(command, "/etc/hosts") {
		return "[verifier_required:target_route] Before declaring target reboot/VPN/DNS failure, verify hostname resolution, `/etc/hosts`, TCP reachability, and one service probe."
	}
	if strings.Contains(lowerResult, "modified") || strings.Contains(lowerResult, "wrote ") || strings.Contains(lowerResult, "saved ") {
		if path := pathFromVerifierArgs(tc.Function.Arguments); path != "" {
			return "[verifier_required:file] Verify the changed file exists and contains the intended update: " + path
		}
	}
	return ""
}

func probeTargetTextFromToolArgs(raw json.RawMessage) string {
	var args map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return shellCommandFromToolArgs(raw)
	}
	for _, key := range []string{"command", "url", "target", "host"} {
		if text, ok := args[key].(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return shellCommandFromToolArgs(raw)
}

func offTargetIPHint(text, confirmedTarget, result string) string {
	if strings.Contains(result, "[hint:target]") {
		return ""
	}
	confirmed := firstIPv4(confirmedTarget)
	if confirmed == "" {
		return ""
	}
	for _, candidate := range ipv4TokenRe.FindAllString(text, -1) {
		if candidate == confirmed || !validIPv4(candidate) {
			continue
		}
		return "[hint:target] Confirmed target is " + confirmed + ". This command probes " + candidate + ". Confirm " + candidate + " is intentional; do not chase stale IPs from old runs or cached notes."
	}
	return ""
}

func firstIPv4(text string) string {
	for _, candidate := range ipv4TokenRe.FindAllString(text, -1) {
		if validIPv4(candidate) {
			return candidate
		}
	}
	return ""
}

func validIPv4(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 3 {
			return false
		}
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
			n = n*10 + int(r-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}

func httpRedirectHint(result string) string {
	if strings.Contains(result, "[hint:redirect]") {
		return ""
	}
	if !httpRedirectStatusRe.MatchString(result) && !httpRedirectBodyRe.MatchString(result) {
		return ""
	}
	target := ""
	if match := httpLocationRe.FindStringSubmatch(result); len(match) == 2 {
		target = strings.TrimSpace(match[1])
	} else if match := httpHrefRe.FindStringSubmatch(result); len(match) == 2 {
		target = strings.TrimSpace(match[1])
	}
	if target != "" {
		return "[hint:redirect] Response is an HTTP redirect to " + target + ". curl did not follow it - re-run with `curl -L` or request " + target + " directly. Do NOT re-run the same non-following curl with a different `| head -N`; the body will be identical."
	}
	return "[hint:redirect] Response is an HTTP 3xx redirect. curl did not follow it - re-run with `curl -L` or request the Location target directly. Do NOT re-run the same non-following curl with a different `| head -N`; the body will be identical."
}

func pathFromVerifierArgs(raw json.RawMessage) string {
	var args map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return ""
	}
	for _, key := range []string{"path", "file", "target"} {
		if text, ok := args[key].(string); ok && strings.TrimSpace(text) != "" {
			return truncateRunes(strings.TrimSpace(text), 180)
		}
	}
	return ""
}

func buildPendingVerifierPrompt(msgs []llm.Message) string {
	seen := map[string]bool{}
	var kinds []string
	for _, msg := range msgs {
		text := messageText(msg)
		for _, match := range regexp.MustCompile(`\[verifier_required:([a-z_]+)\]`).FindAllStringSubmatch(text, -1) {
			if len(match) != 2 || seen[match[1]] {
				continue
			}
			seen[match[1]] = true
			kinds = append(kinds, match[1])
		}
	}
	if len(kinds) == 0 {
		return ""
	}
	return "Proper verifier required before any final answer or success/failure claim. Do not summarize as complete yet. Your next action must be an evidence-backed verifier step for: " + strings.Join(kinds, ", ") + ". Use existing access/session where possible and capture command plus result evidence. Required standards: webshell -> prove the exact URL with id, pwd, and hostname; listener -> terminal_read the listener state before triggering and then verify the callback session; session/shell_user/root -> verify id, hostname, pwd, and the expected evidence/flag path where relevant; target_route -> verify hosts/DNS, TCP reachability, and one service probe before claiming reboot/VPN/DNS failure; file -> read or grep the changed file and confirm intended content. If verification fails, update the facts/blocker instead of claiming success."
}
