package checkpacks

import (
	"fmt"
	"sort"
	"strings"

	"mauler/internal/engagement"
)

type TargetContext struct {
	TargetKind    string   `json:"target_kind,omitempty"`
	Protocols     []string `json:"protocols,omitempty"`
	EndpointKinds []string `json:"endpoint_kinds,omitempty"`
	Technologies  []string `json:"technologies,omitempty"`
	Features      []string `json:"features,omitempty"`
	Authenticated bool     `json:"authenticated,omitempty"`
}

type SelectionOptions struct {
	IncludeDeprecated   bool
	IncludeExperimental bool
}

type Selection struct {
	Applicable []engagement.CheckDefinition `json:"applicable"`
	Excluded   []ExcludedCheck              `json:"excluded"`
}

type ExcludedCheck struct {
	ID      string   `json:"id"`
	Reasons []string `json:"reasons"`
}

func Select(checklist engagement.ChecklistDefinition, target TargetContext, options SelectionOptions) Selection {
	result := Selection{}
	for _, check := range checklist.Items {
		applicable, reasons := Evaluate(check, target, options)
		if applicable {
			result.Applicable = append(result.Applicable, check)
			continue
		}
		result.Excluded = append(result.Excluded, ExcludedCheck{ID: check.ID, Reasons: reasons})
	}
	return result
}

func Evaluate(check engagement.CheckDefinition, target TargetContext, options SelectionOptions) (bool, []string) {
	reasons := []string{}
	if check.Deprecated && !options.IncludeDeprecated {
		reasons = append(reasons, "deprecated")
	}
	if strings.EqualFold(strings.TrimSpace(check.Maturity), "experimental") && !options.IncludeExperimental {
		reasons = append(reasons, "experimental")
	}
	applicability := check.AppliesWhen
	if !matchesOne(target.TargetKind, applicability.TargetKinds) {
		reasons = append(reasons, fmt.Sprintf("target kind %q is not applicable", target.TargetKind))
	}
	if !setsOverlap(target.Protocols, applicability.Protocols) {
		reasons = append(reasons, "no applicable protocol")
	}
	if !setsOverlap(target.EndpointKinds, applicability.EndpointKinds) {
		reasons = append(reasons, "no applicable endpoint kind")
	}
	if !setsOverlap(target.Technologies, applicability.Technologies) {
		reasons = append(reasons, "no applicable technology")
	}
	if !setsOverlap(target.Features, applicability.AnyFeatures) {
		reasons = append(reasons, "none of the applicable features were discovered")
	}
	if missing := missingValues(target.Features, applicability.RequiresFeatures); len(missing) > 0 {
		reasons = append(reasons, "missing required features: "+strings.Join(missing, ", "))
	}
	switch strings.ToLower(strings.TrimSpace(applicability.Authentication)) {
	case "authenticated":
		if !target.Authenticated {
			reasons = append(reasons, "requires an authenticated context")
		}
	case "anonymous":
		if target.Authenticated {
			reasons = append(reasons, "requires an anonymous context")
		}
	}
	return len(reasons) == 0, reasons
}

func matchesOne(value string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == strings.ToLower(strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func setsOverlap(actual, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	set := normalisedSet(actual)
	for _, candidate := range allowed {
		if set[strings.ToLower(strings.TrimSpace(candidate))] {
			return true
		}
	}
	return false
}

func missingValues(actual, required []string) []string {
	set := normalisedSet(actual)
	missing := []string{}
	for _, value := range required {
		if !set[strings.ToLower(strings.TrimSpace(value))] {
			missing = append(missing, value)
		}
	}
	sort.Strings(missing)
	return missing
}

func normalisedSet(values []string) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		if key := strings.ToLower(strings.TrimSpace(value)); key != "" {
			set[key] = true
		}
	}
	return set
}
