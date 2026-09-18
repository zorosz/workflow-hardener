package hardener

import (
	"reflect"
	"strings"
	"testing"
)

func TestNonPRExpressions(t *testing.T) {
	expressions := []string{
		"${{ github.head_ref || github.ref_name }}",
		"${{ secrets.access_token }}",
		"${{ steps.build.outputs.artifact_name }}",
		"${{ needs.build.outputs.result }}",
		"${{ '' }}",
		"${{ '''' }}",
		"${{ 'It''s || }} ${{ github.event.pull_request.body }}' }}",
		"${{ 'github.event.pull_request.title' || 'github.event.pull_request.body' }}",
		"${{ 'literal' || 'fallback' }}",
		"${{ 'Unicode: \u00e9\u00a0\v\f' }}",
		"${{ 'backslash\\' }}", // Backslash does not escape the closing apostrophe.
		"${{\tgithub.head_ref\r\n||\tgithub.ref_name\n}}",
	}
	for _, root := range []string{"github", "env", "vars", "secrets", "inputs", "steps", "needs", "matrix", "runner", "job", "strategy"} {
		expressions = append(expressions, "${{ "+root+"._Property-1.value2 || '' }}")
	}
	for _, expression := range expressions {
		t.Run(expression, func(t *testing.T) {
			a := AnalyzePRText(prTextTestWorkflow(expression, "sh", true))
			if a.AnalyzedSteps != 1 || len(a.Unsupported) != 0 || len(a.Findings) != 0 {
				t.Fatalf("supported non-PR expression mishandled: %+v", a)
			}
		})
	}
}

func TestUnsupportedExpressionSyntax(t *testing.T) {
	for _, content := range []string{
		"", " ", "github", "jobs.example", "unknown.value", "Github.sha",
		"github.", "github..sha", "github. sha", "github .sha",
		"github.1value", "github.-value", "github.\u00e9", "github.sha/value",
		"github['sha']", "github.event.*.body", "(github.sha)", "toJSON(github.sha)",
		"0", "true", "false", "null", "github.sha || false",
		"!github.sha", "github.sha == 'value'", "github.sha && env.value",
		"github.sha | env.value", "github.sha ||| env.value", "github.sha || || env.value",
		"|| github.sha", "github.sha ||", "github.sha 'text'", "'' env.value",
		"\"text\"", "'unclosed", "'''", "'backslash\\'not escaped'", "'ok' junk",
		"github.sha\u00a0", "github.sha\v", "github.sha ||\fenv.value",
		"${{ github.sha }}", "github.sha || ${{ env.value }}",
		"github.event.Pull_Request.title", "github.event.pull_request.BODY",
	} {
		t.Run(content, func(t *testing.T) {
			a := AnalyzePRText(prTextTestWorkflow("${{ "+content+" }}", "bash", true))
			if a.AnalyzedSteps != 0 || len(a.Findings) != 0 || len(a.Unsupported) != 1 || a.Unsupported[0].Code != "unsupported_expression" {
				t.Fatalf("unsupported syntax became complete: %+v", a)
			}
		})
	}
}

func TestFallbackEvidenceAndRuleOrder(t *testing.T) {
	// Inspect every alternative, even after a truthy literal; do not evaluate it.
	expression := "${{ 'literal' || github.event.pull_request.body || github.event.pull_request.title || github.event.pull_request.body }}"
	w := prTextTestWorkflow(expression+"\n"+expression, "bash", true)
	a := AnalyzePRText(w)
	if a.AnalyzedSteps != 1 || len(a.Unsupported) != 0 || len(a.Findings) != 2 {
		t.Fatalf("mixed fallback incorrectly grouped: %+v", a)
	}
	for i, id := range []string{PRTitleRuleID, PRBodyRuleID} {
		f := a.Findings[i]
		if f.RuleID != id || f.StepIndex != 2 || f.Location != w.Steps[0].Location ||
			!reflect.DeepEqual(f.Evidence, []string{expression, expression}) {
			t.Fatalf("fallback attribution or evidence changed: %+v", f)
		}
	}
}

func TestLongFallbackChain(t *testing.T) {
	expression := "${{ " + strings.Repeat("env.value || ", 16000) + "github.event.pull_request.body }}"
	if len(expression) >= MaxFileBytes {
		t.Fatal("test expression exceeds the input limit")
	}
	a := AnalyzePRText(prTextTestWorkflow(expression, "bash", true))
	if a.AnalyzedSteps != 1 || len(a.Unsupported) != 0 || len(a.Findings) != 1 ||
		a.Findings[0].RuleID != PRBodyRuleID || !reflect.DeepEqual(a.Findings[0].Evidence, []string{expression}) {
		t.Fatal("long fallback chain was truncated or lost its final PR reference")
	}
	// A long valid prefix must not hide an incomplete final alternative.
	a = AnalyzePRText(prTextTestWorkflow(strings.TrimSuffix(expression, "}}")+" || }}", "bash", true))
	if a.AnalyzedSteps != 0 || len(a.Findings) != 0 || len(a.Unsupported) != 1 {
		t.Fatal("long malformed fallback became a complete scan")
	}
}
