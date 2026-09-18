package hardener

import "strings"

// AnalyzePRText finds direct PR-title and PR-body expressions in resolved Bash/sh run steps.
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
		if step.Shell != "bash" && step.Shell != "sh" && step.Shell != defaultPOSIXShell {
			result.Unsupported = append(result.Unsupported, Issue{
				Kind: "coverage", Code: "unsupported_shell", JobID: step.JobID, StepIndex: &index,
				Message: "Run step shell is unsupported or cannot be resolved to Bash/sh from static shell settings, runner, and container information.",
			})
			continue
		}
		title, body, supported := directPRTextExpressions(step.Run)
		if !supported {
			result.Unsupported = append(result.Unsupported, Issue{
				Kind: "coverage", Code: "unsupported_expression", JobID: step.JobID, StepIndex: &index,
				Message: "Run script contains an expression outside supported context references, single-quoted strings, and || alternatives, or an incomplete expression; the entire step is unsupported for both rules.",
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
				Message:  "Pull request " + match.field + " expression is inserted directly into a Bash/sh run script; review possible script injection.",
				Evidence: match.evidence,
			})
		}
	}
	return result
}

// Every opener must begin a complete supported expression. An unmodeled
// expression invalidates the entire step for both rules, including earlier hits.
func directPRTextExpressions(script string) (title, body []string, supported bool) {
	const opener = "${{"
	for offset := 0; offset < len(script); {
		relative := strings.Index(script[offset:], opener)
		if relative < 0 {
			break
		}
		start := offset + relative
		parser := expressionParser{text: script, pos: start + len(opener)}
		hasTitle, hasBody, ok := parser.parse()
		if !ok {
			return nil, nil, false
		}
		offset = parser.pos
		if hasTitle {
			title = append(title, script[start:offset])
		}
		if hasBody {
			body = append(body, script[start:offset])
		}
	}
	return title, body, true
}
