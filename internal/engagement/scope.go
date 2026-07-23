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
		decision.Allowed = true
		decision.Reason = "relative path inherits the locked project target"
		return decision
	}
	t, err := parseScopeTarget(target)
	if err != nil {
		decision.Reason = err.Error()
		return decision
	}
	for _, raw := range scope {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, network, cidrErr := net.ParseCIDR(strings.Trim(raw, "[]")); cidrErr == nil {
			if ip := net.ParseIP(t.host); ip != nil && network.Contains(ip) {
				decision.Allowed, decision.Matched = true, raw
				decision.Reason = "target IP is within the locked CIDR"
				return decision
			}
			continue
		}
		s, scopeErr := parseScopeTarget(raw)
		if scopeErr != nil || !strings.EqualFold(s.host, t.host) {
			continue
		}
		if s.port != "" && s.port != t.port {
			continue
		}
		if s.path != "" && s.path != "/" && !pathHasPrefix(t.path, s.path) {
			continue
		}
		decision.Allowed, decision.Matched = true, raw
		decision.Reason = "target matches locked host, port, and path constraints"
		return decision
	}
	decision.Reason = fmt.Sprintf("%q does not match any locked scope entry", target)
	return decision
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
