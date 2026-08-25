package engagement

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ScopeDecision is the deterministic result used by both engagement state
// transitions and structured network tools.
type ScopeDecision struct {
	Allowed bool   `json:"allowed"`
	Target  string `json:"target"`
	Matched string `json:"matched,omitempty"`
	Reason  string `json:"reason"`
}

// CheckTargetScope accepts exact hosts/IPs, CIDRs, optional ports, and URL path
// prefixes. Relative paths inherit the already-authorised project target.
func CheckTargetScope(scope []string, target string) ScopeDecision {
	target = strings.TrimSpace(target)
	decision := ScopeDecision{Target: target}
	if target == "" {
		decision.Reason = "target is empty"
		return decision
	}
	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") {
		return checkRelativeTargetScope(scope, target)
	}
	t, err := parseScopeTarget(target)
	if err != nil {
		decision.Reason = err.Error()
		return decision
	}
	// Explicit exclusions always win, regardless of their order in persisted
	// scope. This permits a broad authorised CIDR with narrow operator-owned
	// carve-outs without allowing the model to retry around them.
	for _, raw := range scope {
		raw = strings.TrimSpace(raw)
		if !strings.HasPrefix(raw, "!") {
			continue
		}
		rule := strings.TrimSpace(strings.TrimPrefix(raw, "!"))
		if scopeTargetMatches(rule, t) {
			decision.Matched = raw
			decision.Reason = fmt.Sprintf("%q matches an explicit locked-scope exclusion", target)
			return decision
		}
	}
	for _, raw := range scope {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "!") {
			continue
		}
		if scopeTargetMatches(raw, t) {
			decision.Allowed, decision.Matched = true, raw
			if _, _, cidrErr := net.ParseCIDR(strings.Trim(raw, "[]")); cidrErr == nil {
				decision.Reason = "target IP is within the locked CIDR"
			} else {
				decision.Reason = "target matches locked host, port, and path constraints"
			}
			return decision
		}
	}
	decision.Reason = fmt.Sprintf("%q does not match any locked scope entry", target)
	return decision
}

func checkRelativeTargetScope(scope []string, target string) ScopeDecision {
	decision := ScopeDecision{Target: target}
	if len(scope) == 0 {
		// Legacy/imported endpoint-only grids may have no base authority. Current
		// project creation never produces this state, but retaining relative
		// endpoint editing keeps those records usable without authorising any
		// absolute network target.
		decision.Allowed = true
		decision.Reason = "relative endpoint retained for a legacy grid with no absolute scope"
		return decision
	}
	var primaryRaw string
	var primary parsedScopeTarget
	for _, raw := range scope {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "!") {
			continue
		}
		if _, _, err := net.ParseCIDR(strings.Trim(raw, "[]")); err == nil {
			decision.Reason = "relative target is ambiguous because the primary locked scope entry is a CIDR; use an absolute in-scope target"
			return decision
		}
		parsed, err := parseScopeTarget(raw)
		if err != nil {
			continue
		}
		primaryRaw, primary = raw, parsed
		break
	}
	if primaryRaw == "" {
		decision.Reason = "relative target has no concrete primary locked-scope host"
		return decision
	}
	relative := parsedScopeTarget{host: primary.host, port: primary.port, path: target}
	for _, raw := range scope {
		raw = strings.TrimSpace(raw)
		if !strings.HasPrefix(raw, "!") {
			continue
		}
		if scopeTargetMatches(strings.TrimSpace(strings.TrimPrefix(raw, "!")), relative) {
			decision.Matched = raw
			decision.Reason = fmt.Sprintf("%q inherits the primary target but matches an explicit locked-scope exclusion", target)
			return decision
		}
	}
	if !scopeTargetMatches(primaryRaw, relative) {
		decision.Reason = fmt.Sprintf("%q falls outside the primary locked URL path prefix", target)
		return decision
	}
	decision.Allowed, decision.Matched = true, primaryRaw
	decision.Reason = "relative path inherits the first allowed primary project target"
	return decision
}

func scopeTargetMatches(raw string, target parsedScopeTarget) bool {
	if _, network, cidrErr := net.ParseCIDR(strings.Trim(raw, "[]")); cidrErr == nil {
		if ip := net.ParseIP(target.host); ip != nil {
			return network.Contains(ip)
		}
		return false
	}
	scopeTarget, err := parseScopeTarget(raw)
	if err != nil || !strings.EqualFold(scopeTarget.host, target.host) {
		return false
	}
	if scopeTarget.port != "" && scopeTarget.port != target.port {
		return false
	}
	return scopeTarget.path == "" || scopeTarget.path == "/" || pathHasPrefix(target.path, scopeTarget.path)
}

type parsedScopeTarget struct{ host, port, path string }

func parseScopeTarget(raw string) (parsedScopeTarget, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		parsed, err = url.Parse("//" + raw)
	}
	if err != nil || parsed.Hostname() == "" {
		return parsedScopeTarget{}, fmt.Errorf("target %q is not a valid host, IP, CIDR, or URL", raw)
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return parsedScopeTarget{host: strings.Trim(parsed.Hostname(), "[]"), port: parsed.Port(), path: path}, nil
}

func pathHasPrefix(target, prefix string) bool {
	target = strings.TrimSuffix(target, "/") + "/"
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	return strings.HasPrefix(target, prefix)
}
