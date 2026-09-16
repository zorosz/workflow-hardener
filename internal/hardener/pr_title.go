package hardener

import "strings"

// AnalyzePRTitle finds direct PR-title expressions in explicit Bash run steps.
// Scripts and expressions are inspected as decoded text and never executed.
func AnalyzePRTitle(w Workflow) Analysis {
	result := Analysis{Findings: []Finding{}, Unsupported: []Issue{}}
	for _, job := range w.ReusableJobs {
		result.Unsupported = append(result.Unsupported, Issue{
			Kind: "coverage", Code: "reusable_workflow", JobID: job,
			Message: "Reusable workflow jobs are outside WH-R001 coverage.",
		})
	}
	if len(w.Steps) == 0 {
		result.Unsupported = append(result.Unsupported, Issue{
			Kind: "coverage", Code: "no_run_steps",
			Message: "No run steps are available for WH-R001 analysis.",
		})
	}
	for _, step := range w.Steps {
		index := step.Index
		if !step.ShellExplicit || step.Shell != "bash" {
			result.Unsupported = append(result.Unsupported, Issue{
				Kind: "coverage", Code: "unsupported_shell", JobID: step.JobID, StepIndex: &index,
				Message: "Run step does not explicitly declare shell: bash; shell inference is outside WH-R001 coverage.",
			})
			continue
		}
		evidence, supported := directTitleExpressions(step.Run)
		if !supported {
			result.Unsupported = append(result.Unsupported, Issue{
				Kind: "coverage", Code: "unsupported_expression", JobID: step.JobID, StepIndex: &index,
				Message: "Run script contains an expression outside WH-R001's exact direct title form; the entire step is unsupported.",
			})
			continue
		}
		result.AnalyzedSteps++
		if len(evidence) == 0 {
			continue
		}
		result.Findings = append(result.Findings, Finding{
			RuleID: RuleID, Path: w.Path, JobID: step.JobID, StepIndex: step.Index,
			Location: step.Location,
			Message:  "Pull request title expression is inserted directly into a Bash run script; review possible script injection.",
			Evidence: evidence,
		})
	}
	return result
}

// Every opener must begin one complete canonical expression. An unmodeled
// expression invalidates the entire step, including any earlier direct match.
func directTitleExpressions(script string) ([]string, bool) {
	const opener = "${{"
	const title = "github.event.pull_request.title"
	evidence := []string{}
	for offset := 0; offset < len(script); {
		relative := strings.Index(script[offset:], opener)
		if relative < 0 {
			break
		}
		start := offset + relative
		cursor := skipTitleWhitespace(script, start+len(opener))
		if !strings.HasPrefix(script[cursor:], title) {
			return nil, false
		}
		cursor = skipTitleWhitespace(script, cursor+len(title))
		if !strings.HasPrefix(script[cursor:], "}}") {
			return nil, false
		}
		offset = cursor + 2
		evidence = append(evidence, script[start:offset])
	}
	return evidence, true
}

func skipTitleWhitespace(script string, offset int) int {
	for offset < len(script) {
		switch script[offset] {
		case ' ', '\t', '\r', '\n':
			offset++
		default:
			return offset
		}
	}
	return offset
}
