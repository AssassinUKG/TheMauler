package settings

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const maxLabScopeTargets = 512

// NormaliseLabScope migrates the legacy scalar target/hostname fields into the
// structured scope list. It is intentionally tolerant during settings load:
// invalid historical values remain visible as invalid rows but never become
// authoritative locked scope.
func NormaliseLabScope(target, hostname string, entries []LabScopeTarget) (string, string, []LabScopeTarget) {
	if len(entries) == 0 {
		for _, value := range SplitLabScopeValues(target) {
			entries = append(entries, LabScopeTarget{Value: value})
		}
		hostname = strings.TrimSpace(hostname)
		if hostname != "" && !strings.EqualFold(hostname, "boxname.htb") {
			entries = append(entries, LabScopeTarget{Value: hostname})
		}
	}

	out := make([]LabScopeTarget, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		values := SplitLabScopeValues(entry.Value)
		for _, value := range values {
			next := entry
			next.Value = strings.TrimSpace(value)
			if strings.HasPrefix(next.Value, "!") {
				next.Excluded = true
				next.Value = strings.TrimSpace(strings.TrimPrefix(next.Value, "!"))
			}
			next.Kind = DetectLabScopeKind(next.Value)
			next.Environment = normaliseLabScopeEnvironment(next.Environment)
			next.Label = truncateScopeField(strings.TrimSpace(next.Label), 120)
			next.Notes = truncateScopeField(strings.TrimSpace(next.Notes), 500)
			if next.Value == "" || strings.EqualFold(next.Value, "boxname.htb") {
				continue
			}
			key := fmt.Sprintf("%t:%s", next.Excluded, strings.ToLower(next.Value))
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, next)
			if len(out) == maxLabScopeTargets {
				break
			}
		}
		if len(out) == maxLabScopeTargets {
			break
		}
	}

	primary, primaryHostname := "", ""
	for _, entry := range out {
		if entry.Excluded || entry.Kind == "invalid" {
			continue
		}
		if primary == "" {
			primary = entry.Value
		}
		if primaryHostname == "" && entry.Kind == "hostname" {
			primaryHostname = entry.Value
		}
	}
	return primary, primaryHostname, out
}

// SplitLabScopeValues is shared by migrations and paste/import behavior. It
// accepts commas, semicolons, and newlines without splitting URL paths or IPv6.
func SplitLabScopeValues(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// DetectLabScopeKind validates and classifies one rule value. Exact ports and
// URL path prefixes remain part of the value and are enforced by engagement
// scope matching.
func DetectLabScopeKind(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "!"))
	if value == "" || strings.ContainsAny(value, "\t\r\n,;") {
		return "invalid"
	}
	if _, _, err := net.ParseCIDR(strings.Trim(value, "[]")); err == nil {
		return "cidr"
	}
	if net.ParseIP(strings.Trim(value, "[]")) != nil {
		return "ip"
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Hostname() != "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https":
			return "url"
		default:
			return "invalid"
		}
	}
	parsed, err := url.Parse("//" + value)
	if err != nil || parsed.Hostname() == "" || strings.ContainsAny(parsed.Hostname(), " /\\") {
		return "invalid"
	}
	if ip := net.ParseIP(strings.Trim(parsed.Hostname(), "[]")); ip != nil {
		return "ip"
	}
	if validScopeHostname(parsed.Hostname()) {
		return "hostname"
	}
	return "invalid"
}

func validScopeHostname(value string) bool {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".")
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
				return false
			}
		}
	}
	return true
}

func normaliseLabScopeEnvironment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "external", "internal":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "auto"
	}
}

func truncateScopeField(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// LabScopeValues returns the exact locked-scope representation. Deny entries
// are prefixed with ! and are evaluated before allow entries in engagement.
func LabScopeValues(lab LabContext) []string {
	_, _, entries := NormaliseLabScope(lab.Target, lab.Hostname, lab.ScopeTargets)
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind == "invalid" {
			continue
		}
		value := entry.Value
		if entry.Excluded {
			value = "!" + value
		}
		out = append(out, value)
	}
	return out
}

func HasAllowedLabScope(lab LabContext) bool {
	for _, value := range LabScopeValues(lab) {
		if !strings.HasPrefix(value, "!") {
			return true
		}
	}
	return false
}

func ValidateLabScopeTargets(entries []LabScopeTarget) error {
	if len(entries) > maxLabScopeTargets {
		return fmt.Errorf("authorised scope has %d entries; maximum is %d", len(entries), maxLabScopeTargets)
	}
	for i, entry := range entries {
		if strings.TrimSpace(entry.Value) == "" {
			return fmt.Errorf("authorised scope row %d is empty", i+1)
		}
		if DetectLabScopeKind(entry.Value) == "invalid" {
			return fmt.Errorf("authorised scope row %d has an invalid IP, CIDR, hostname, host:port, or HTTP(S) URL: %q", i+1, entry.Value)
		}
		if len([]rune(entry.Label)) > 120 || len([]rune(entry.Notes)) > 500 {
			return fmt.Errorf("authorised scope row %d label or notes exceed the supported length", i+1)
		}
	}
	return nil
}

// ReplacePrimaryLabScopeTarget keeps older target-only UI surfaces useful. A
// comma/newline list replaces the allowed scope while retaining explicit deny
// rules; a single value replaces only the current primary allowed entry.
func ReplacePrimaryLabScopeTarget(entries []LabScopeTarget, value string) []LabScopeTarget {
	_, _, entries = NormaliseLabScope("", "", entries)
	values := SplitLabScopeValues(value)
	if len(values) > 1 {
		out := make([]LabScopeTarget, 0, len(values)+len(entries))
		for _, item := range values {
			out = append(out, LabScopeTarget{Value: item, Kind: DetectLabScopeKind(item), Environment: "auto"})
		}
		for _, entry := range entries {
			if entry.Excluded {
				out = append(out, entry)
			}
		}
		return out
	}
	value = strings.TrimSpace(value)
	for i := range entries {
		if !entries[i].Excluded {
			if value == "" {
				return append(entries[:i], entries[i+1:]...)
			}
			entries[i].Value = value
			entries[i].Kind = DetectLabScopeKind(value)
			return entries
		}
	}
	if value != "" {
		entries = append([]LabScopeTarget{{Value: value, Kind: DetectLabScopeKind(value), Environment: "auto"}}, entries...)
	}
	return entries
}
