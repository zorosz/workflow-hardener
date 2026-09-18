package hardener

import "strings"

// AnalyzePRText finds direct PR-title and PR-body expressions in explicit Bash run steps.
// Scripts and expressions are inspected as decoded text and never executed.
func AnalyzePRText(w Workflow) Analysis {
	result := Analysis{Findings: []Finding{}, Unsupported: []Issue{}}
	for _, job := range w.ReusableJobs {
		result.Unsupported = append(result.Unsupported, Issue{
			Kind: "coverage", Code: "reusable_workflow", JobID: job,
			Message: "Reusable workflow jobs are outside WH-R001 and WH-R002 coverage.",
		})
	}
	if len(w.Steps) == 0 {
		result.Unsupported = append(result.Unsupported, Issue{
			Kind: "coverage", Code: "no_run_steps",
			Message: "No run steps are available for WH-R001 and WH-R002 analysis.",
		})
	}
	for _, step := range w.Steps {
		index := step.Index
		if !step.ShellExplicit || step.Shell != "bash" {
			result.Unsupported = append(result.Unsupported, Issue{
				Kind: "coverage", Code: "unsupported_shell", JobID: step.JobID, StepIndex: &index,
				Message: "Run step does not explicitly declare shell: bash; shell inference is outside WH-R001 and WH-R002 coverage.",
			})
			continue
		}
		title, body, supported := directPRTextExpressions(step.Run)
		if !supported {
			result.Unsupported = append(result.Unsupported, Issue{
				Kind: "coverage", Code: "unsupported_expression", JobID: step.JobID, StepIndex: &index,
				Message: "Run script contains an expression outside the exact direct PR-title and PR-body forms; the entire step is unsupported for both rules.",
			})
			continue
		}
		result.AnalyzedSteps++
		// Count each step once, then emit at most one finding per rule in rule-ID order.
		for _, match := range []struct {
			ruleID   string
			field    string
			evidence []string
		}{
			{PRTitleRuleID, "title", title},
			{PRBodyRuleID, "body", body},
		} {
			if len(match.evidence) == 0 {
				continue
			}
			result.Findings = append(result.Findings, Finding{
				RuleID: match.ruleID, Path: w.Path, JobID: step.JobID, StepIndex: step.Index,
				Location: step.Location,
				Message:  "Pull request " + match.field + " expression is inserted directly into a Bash run script; review possible script injection.",
				Evidence: match.evidence,
			})
		}
	}
	return result
}

// Every opener must begin one complete direct title or body expression. An
// unmodeled expression invalidates the entire step for both rules.
func directPRTextExpressions(script string) (title, body []string, supported bool) {
	const opener = "${{"
	for offset := 0; offset < len(script); {
		relative := strings.Index(script[offset:], opener)
		if relative < 0 {
			break
		}
		start := offset + relative
		content := start + len(opener)
		relativeEnd := strings.Index(script[content:], "}}")
		if relativeEnd < 0 {
			return nil, nil, false
		}
		end := content + relativeEnd
		offset = end + 2
		switch strings.Trim(script[content:end], " \t\r\n") {
		case "github.event.pull_request.title":
			title = append(title, script[start:offset])
		case "github.event.pull_request.body":
			body = append(body, script[start:offset])
		default:
			return nil, nil, false
		}
	}
	return title, body, true
}
