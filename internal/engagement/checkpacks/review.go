package checkpacks

import (
	"fmt"
	"strings"

	"mauler/internal/engagement"
)

type QualityIssue struct {
	CheckID string `json:"check_id,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type QualityReport struct {
	Score  int            `json:"score"`
	Ready  bool           `json:"ready"`
	Issues []QualityIssue `json:"issues,omitempty"`
}

// ReviewChecklist performs the stricter editorial gate used before a
// candidate pack can be installed. Parse/Validate remains backward compatible
// with Shiftgrid JSON; this review intentionally reports its missing modern
// evidence and mapping metadata until it has been curated.
func ReviewChecklist(checklist engagement.ChecklistDefinition, registry *Registry) QualityReport {
	report := QualityReport{}
	if err := checklist.Validate(); err != nil {
		report.Issues = append(report.Issues, QualityIssue{Field: "definition", Message: err.Error()})
		return report
	}
	if checklist.SchemaVersion != engagement.CurrentDefinitionSchemaVersion {
		report.Issues = append(report.Issues, QualityIssue{Field: "schema_version", Message: "candidate pack must use the current definition schema"})
	}
	if strings.EqualFold(strings.TrimSpace(checklist.Source.Trust), engagement.PackTrustLocal) {
		// Local packs are operator-owned and do not claim an allow-listed upstream
		// identity. They still pass the complete structural/editorial review below.
	} else if registry == nil {
		report.Issues = append(report.Issues, QualityIssue{Field: "source", Message: "an allow-list registry is required"})
	} else if err := registry.ValidateProvenance(checklist.Source); err != nil {
		report.Issues = append(report.Issues, QualityIssue{Field: "source", Message: err.Error()})
	}
	if len(checklist.Items) == 0 {
		return report
	}
	total := 0
	for _, check := range checklist.Items {
		score, issues := reviewCheck(check)
		total += score
		report.Issues = append(report.Issues, issues...)
	}
	report.Score = total / len(checklist.Items)
	report.Ready = report.Score >= MinimumPublishScore && len(report.Issues) == 0
	return report
}

func reviewCheck(check engagement.CheckDefinition) (int, []QualityIssue) {
	score := 0
	issues := []QualityIssue{}
	require := func(ok bool, points int, field, message string) {
		if ok {
			score += points
			return
		}
		issues = append(issues, QualityIssue{CheckID: check.ID, Field: field, Message: message})
	}
	require(len(check.Mappings) > 0, 15, "mappings", "at least one authoritative framework mapping is required")
	require(len(check.Procedure) > 0, 20, "procedure", "a reproducible procedure is required")
	require(len(check.ExpectedSignals) > 0, 15, "expected_signals", "positive expected signals are required")
	require(len(check.NegativeSignals) > 0, 10, "negative_signals", "negative or fixed-case signals are required")
	require(len(check.FalsePositiveNotes) > 0, 10, "false_positive_notes", "false-positive controls are required")
	require(strings.TrimSpace(check.Safety) != "", 10, "safety", "an explicit safety class is required")
	require(check.Evidence.Minimum > 0 && len(check.Evidence.RequiredKinds) > 0, 15, "evidence", "minimum evidence kinds and count are required")
	require(strings.TrimSpace(check.Maturity) != "", 5, "maturity", "an explicit maturity is required")
	return score, issues
}

func (r QualityReport) Error() error {
	if r.Ready {
		return nil
	}
	return fmt.Errorf("checklist quality score %d is below %d or has %d unresolved issues", r.Score, MinimumPublishScore, len(r.Issues))
}
