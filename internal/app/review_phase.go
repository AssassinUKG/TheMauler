package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mauler/internal/settings"
)

type reviewPhaseDecision struct {
	Proceed        bool
	InjectedPrompt string
	StopReason     string
	StopDetail     string
	SummaryNote    string
}

func (a *App) runReviewPhase(ctx context.Context, run *TaskRun, profile settings.Profile, cfg *settings.Settings, mode AgentMode, autonomous bool, reviewCyclesUsed *int, budgetExhausted bool) reviewPhaseDecision {
	if run == nil || cfg == nil || reviewCyclesUsed == nil {
		return reviewPhaseDecision{Proceed: true}
	}
	rl := cfg.Agents.ReviewLoop
	if !rl.Enabled || (rl.OnlyAutonomous && !autonomous) || budgetExhausted || !runIsGateable(*run, mode) {
		return reviewPhaseDecision{Proceed: true}
	}

	var verdicts []VerifyVerdict
	if rl.VerifyGate {
		verdicts = append(verdicts, a.runVerifyGate(ctx, run, cfg)...)
	}
	if len(blockingReviewVerdicts(verdicts)) == 0 && rl.CompletionRails {
		verdicts = append(verdicts, runCompletionRails(run, cfg)...)
	}
	if len(blockingReviewVerdicts(verdicts)) == 0 && rl.ReviewerPass {
		verdicts = append(verdicts, a.runReviewerPass(ctx, run, profile, cfg))
	}
	recordReviewGateEvent(run, *reviewCyclesUsed, verdicts)
	blocking := blockingReviewVerdicts(verdicts)
	if len(blocking) == 0 {
		return reviewPhaseDecision{Proceed: true}
	}
	if inconclusive := inconclusiveReviewVerdicts(blocking); len(inconclusive) > 0 {
		detail := reviewBlockingSummary(inconclusive)
		return reviewPhaseDecision{
			Proceed:     true,
			StopReason:  "review_incomplete",
			StopDetail:  fmt.Sprintf("Completion verification could not complete: %s", detail),
			SummaryNote: fmt.Sprintf("Completion remains unverified because a gate was inconclusive: %s", detail),
		}
	}

	maxCycles := rl.MaxReviewCycles
	if maxCycles < 0 {
		maxCycles = 0
	}
	if *reviewCyclesUsed >= maxCycles {
		detail := reviewBlockingSummary(blocking)
		return reviewPhaseDecision{
			Proceed:     true,
			StopReason:  "review_incomplete",
			StopDetail:  fmt.Sprintf("Review loop reached its cap with unresolved blocking gate failures: %s", detail),
			SummaryNote: fmt.Sprintf("Completed with unresolved review gates: %s", detail),
		}
	}

	*reviewCyclesUsed++
	return reviewPhaseDecision{
		Proceed:        false,
		InjectedPrompt: buildReviewGateFailurePrompt(blocking, *reviewCyclesUsed, maxCycles),
	}
}

func inconclusiveReviewVerdicts(verdicts []VerifyVerdict) []VerifyVerdict {
	var out []VerifyVerdict
	for _, verdict := range verdicts {
		if strings.EqualFold(strings.TrimSpace(verdict.Status), "inconclusive") {
			out = append(out, verdict)
		}
	}
	return out
}

func recordReviewGateEvent(run *TaskRun, cycle int, verdicts []VerifyVerdict) {
	if run == nil || len(verdicts) == 0 {
		return
	}
	data, err := json.MarshalIndent(verdicts, "", "  ")
	if err != nil {
		run.addEvent("review_gate", fmt.Sprintf("Review gate cycle %d", cycle), fmt.Sprintf("%#v", verdicts))
		return
	}
	run.addEvent("review_gate", fmt.Sprintf("Review gate cycle %d", cycle), string(data))
}

func blockingReviewVerdicts(verdicts []VerifyVerdict) []VerifyVerdict {
	var out []VerifyVerdict
	for _, verdict := range verdicts {
		status := strings.ToLower(strings.TrimSpace(verdict.Status))
		if verdict.Blocking && status != "" && status != "pass" && status != "skip" {
			out = append(out, verdict)
		}
	}
	return out
}

func buildReviewGateFailurePrompt(blocking []VerifyVerdict, cycle, maxCycles int) string {
	var sb strings.Builder
	sb.WriteString("[review_gate:failed] The task is not complete. Blocking review gate(s) failed.\n")
	fmt.Fprintf(&sb, "state: blocked\nreview_cycle: %d/%d\nnext_tool: edit\n", cycle, maxCycles)
	for _, verdict := range blocking {
		summary := strings.TrimSpace(verdict.Summary)
		if summary == "" {
			summary = verdict.Gate + " failed"
		}
		fmt.Fprintf(&sb, "fix: %s\n", summary)
		for _, improvement := range verdict.Improvements {
			if strings.TrimSpace(improvement) != "" {
				fmt.Fprintf(&sb, "fix: %s\n", strings.TrimSpace(improvement))
			}
		}
		if evidence := strings.TrimSpace(verdict.Evidence); evidence != "" {
			if len(evidence) > 1200 {
				evidence = evidence[len(evidence)-1200:]
			}
			fmt.Fprintf(&sb, "evidence: %s\n", evidence)
		}
	}
	sb.WriteString("do_not_repeat: declaring done before blocking review gates pass.\n")
	return sb.String()
}

func reviewBlockingSummary(blocking []VerifyVerdict) string {
	parts := make([]string, 0, len(blocking))
	for _, verdict := range blocking {
		if summary := strings.TrimSpace(verdict.Summary); summary != "" {
			parts = append(parts, summary)
			continue
		}
		parts = append(parts, strings.TrimSpace(verdict.Gate)+" failed")
	}
	return strings.Join(parts, "; ")
}
