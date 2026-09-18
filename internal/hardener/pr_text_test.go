package hardener

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func prTextTestWorkflow(script, shell string, explicit bool) Workflow {
	return Workflow{
		Path: "sample.workflow.txt",
		Steps: []Step{{
			JobID: "inspect", Index: 2, Run: script, Shell: shell, ShellExplicit: explicit,
			Location: Location{Kind: "run_scalar_start", Line: 8, Column: 14},
		}},
	}
}

// Table-driven cases define the supported expression syntax.
func TestPRTextExpressionBoundary(t *testing.T) {
	for _, rule := range []struct{ field, ruleID string }{
		{"title", PRTitleRuleID}, {"body", PRBodyRuleID},
	} {
		t.Run(rule.field, func(t *testing.T) {
			for _, shell := range []string{"bash", "sh", defaultPOSIXShell} {
				t.Run(shell, func(t *testing.T) {
					testPRTextExpressionBoundary(t, rule.field, rule.ruleID, shell)
				})
			}
		})
	}
}

func testPRTextExpressionBoundary(t *testing.T, field, ruleID, shell string) {
	property := "github.event.pull_request." + field
	direct := "${{ " + property + " }}"
	compact := "${{" + property + "}}"
	multiline := "${{\t\r\n" + property + "\n\t}}"
	for _, tc := range []struct {
		name      string
		script    string
		supported bool
		evidence  []string
	}{
		{"literal", "echo literal", true, nil},
		{"empty script", "", true, nil},
		{"property as literal text", "echo " + property, true, nil},
		{"compact", compact, true, []string{compact}},
		{"surrounding whitespace", multiline, true, []string{multiline}},
		{"quoted", "echo \"" + direct + "\"", true, []string{direct}},
		{"comment", "# " + direct, true, []string{direct}},
		{"backslash before opener", "\\" + direct, true, []string{direct}},
		{"repeated", compact + "\necho " + direct, true, []string{compact, direct}},
		{"trailing literal brace", direct + "}", true, []string{direct}},
		{"bracket notation", "${{ github['event']['pull_request']['" + field + "'] }}", false, nil},
		{"function", "${{ toJSON(" + property + ") }}", false, nil},
		{"other field", "${{ github.workspace }}", false, nil},
		{"other field after match", direct + "\n${{ github.workspace }}", false, nil},
		{"other field before match", "${{ github.workspace }}\n" + direct, false, nil},
		{"expression literal containing direct form", "${{ '" + direct + "' }}", false, nil},
		{"nested opener", "${{ " + direct + " }}", false, nil},
		{"unterminated after match", direct + " ${{", false, nil},
		{"missing closer", "${{ " + property + " }", false, nil},
		{"empty expression", "${{ }}", false, nil},
		{"wrong property case", "${{ github.event.pull_request." + strings.ToUpper(field[:1]) + field[1:] + " }}", false, nil},
		{"property whitespace", "${{ github.event. pull_request." + field + " }}", false, nil},
		{"longer property", "${{ " + property + "_extra }}", false, nil},
		{"suffix expression", "${{ " + property + " || 'fallback' }}", false, nil},
		{"nonbreaking space", "${{\u00a0" + property + " }}", false, nil},
		{"vertical tab", "${{\v" + property + " }}", false, nil},
		{"form feed suffix", "${{ " + property + "\f}}", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := prTextTestWorkflow(tc.script, shell, shell != defaultPOSIXShell)
			a := AnalyzePRText(w)
			if !tc.supported {
				if len(a.Findings) != 0 || a.AnalyzedSteps != 0 || len(a.Unsupported) != 1 ||
					a.Unsupported[0].Code != "unsupported_expression" {
					t.Fatalf("unmodeled expression was accepted: %+v", a)
				}
				return
			}
			if len(a.Unsupported) != 0 || a.AnalyzedSteps != 1 {
				t.Fatalf("supported step refused: %+v", a)
			}
			if len(tc.evidence) == 0 {
				if len(a.Findings) != 0 {
					t.Fatalf("literal produced finding: %+v", a)
				}
				return
			}
			if len(a.Findings) != 1 {
				t.Fatalf("expected one step finding: %+v", a)
			}
			f := a.Findings[0]
			if f.RuleID != ruleID || f.Path != w.Path || f.JobID != "inspect" || f.StepIndex != 2 ||
				f.Location != w.Steps[0].Location || !reflect.DeepEqual(f.Evidence, tc.evidence) {
				t.Fatalf("finding attribution or occurrence order changed: %+v", f)
			}
		})
	}
}

func TestPRTextRequiresSupportedShell(t *testing.T) {
	for _, tc := range []struct {
		shell    string
		explicit bool
	}{
		{"", false}, {"", true}, {"Bash", true},
		{"bash ", true}, {" bash", true}, {"bash {0}", true},
		{"pwsh", true}, {"sh {0}", true}, {"python", true},
	} {
		t.Run(fmt.Sprintf("%q-explicit-%v", tc.shell, tc.explicit), func(t *testing.T) {
			for _, script := range []string{"literal", "${{ github.event.pull_request.title }}", "${{ github.event.pull_request.body }}"} {
				w := prTextTestWorkflow(script, tc.shell, tc.explicit)
				a := AnalyzePRText(w)
				if len(a.Findings) != 0 || a.AnalyzedSteps != 0 || len(a.Unsupported) != 1 ||
					a.Unsupported[0].Code != "unsupported_shell" || *a.Unsupported[0].StepIndex != 2 {
					t.Fatalf("unsupported shell was accepted: %+v", a)
				}
			}
		})
	}
}

func TestPRTextGroupsFindingsByStepAndRule(t *testing.T) {
	title := "${{ github.event.pull_request.title }}"
	body := "${{ github.event.pull_request.body }}"
	compactBody := "${{github.event.pull_request.body}}"
	w := prTextTestWorkflow(body+"\n"+title+"\n"+compactBody+"\n"+title, "bash", true)
	w.Steps = append(w.Steps, Step{
		JobID: "second", Index: 0, Run: body, Shell: "bash", ShellExplicit: true,
		Location: Location{Kind: "run_scalar_start", Line: 20, Column: 14},
	})
	a := AnalyzePRText(w)
	if len(a.Unsupported) != 0 || a.AnalyzedSteps != 2 || len(a.Findings) != 3 {
		t.Fatalf("steps or findings counted incorrectly: %+v", a)
	}
	for i, want := range []struct {
		ruleID   string
		step     Step
		evidence []string
	}{
		{PRTitleRuleID, w.Steps[0], []string{title, title}},
		{PRBodyRuleID, w.Steps[0], []string{body, compactBody}},
		{PRBodyRuleID, w.Steps[1], []string{body}},
	} {
		f := a.Findings[i]
		if f.RuleID != want.ruleID || f.Path != w.Path || f.JobID != want.step.JobID ||
			f.StepIndex != want.step.Index || f.Location != want.step.Location || !reflect.DeepEqual(f.Evidence, want.evidence) {
			t.Fatalf("finding %d lost its rule, step, or evidence order: %+v", i, f)
		}
	}
	if a.Findings[1].Message != "Pull request body expression is inserted directly into a Bash/sh run script; review possible script injection." {
		t.Fatalf("body finding has the wrong explanation: %q", a.Findings[1].Message)
	}
}

func TestPRTextUnknownExpressionInvalidatesBothRules(t *testing.T) {
	title := "${{ github.event.pull_request.title }}"
	body := "${{ github.event.pull_request.body }}"
	unknown := "${{ github.workspace }}"
	for _, script := range []string{
		unknown + title + body,
		title + unknown + body,
		title + body + unknown,
		body + title + "${{",
		title + body + "${{ toJSON(github.event.pull_request.body) }}",
	} {
		a := AnalyzePRText(prTextTestWorkflow(script, "bash", true))
		if len(a.Findings) != 0 || a.AnalyzedSteps != 0 || len(a.Unsupported) != 1 ||
			a.Unsupported[0].Code != "unsupported_expression" {
			t.Fatalf("mixed step was partially accepted: %+v", a)
		}
	}
}

func TestPRTextReusableAndZeroRunCoverage(t *testing.T) {
	for _, data := range []string{
		"jobs: {inspect: {steps: []}}",
		"jobs: {inspect: {steps: [{uses: local/action}]}}",
		"jobs: {reuse: {uses: local/workflow}}",
	} {
		w, err := ParseWorkflow("empty.workflow.txt", []byte(data))
		if err != nil {
			t.Fatal(err)
		}
		a := AnalyzePRText(w)
		if len(a.Findings) != 0 || a.AnalyzedSteps != 0 || len(a.Unsupported) == 0 {
			t.Fatalf("zero-run workflow became complete: %+v", a)
		}
	}
	in, dir := testInputs(t)
	data := "jobs:\n  reuse:\n    uses: local/workflow\n  inspect:\n    steps:\n      - shell: bash\n        run: echo ${{ github.event.pull_request.title }}\n"
	put(t, dir, "mixed.workflow.txt", []byte(data))
	report, err := Scan(in, []string{"mixed.workflow.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if report.ExitCode != 2 || report.Files[0].Status != Unsupported || report.Totals.Findings != 1 ||
		report.Totals.AnalyzedSteps != 1 || report.Files[0].Issues[0].Code != "reusable_workflow" {
		t.Fatalf("reusable job hid coverage or finding: %+v", report)
	}
}

func TestPRTextDecodedScalarsAndConditions(t *testing.T) {
	for _, scalar := range []string{
		"|\n          echo ${{\n          github.event.pull_request.title\n          }}\n",
		">\n          echo ${{\n          github.event.pull_request.title\n          }}\n",
		"\"echo \\u0024{{\\tgithub.event.pull_request.title\\n}}\"\n",
	} {
		for _, field := range []string{"title", "body"} {
			scalar := strings.ReplaceAll(scalar, ".title", "."+field)
			data := "jobs:\n  inspect:\n    if: false\n    steps:\n      - if: false\n        shell: bash\n        run: " + scalar
			w, err := ParseWorkflow("decoded.workflow.txt", []byte(data))
			if err != nil {
				t.Fatal(err)
			}
			a := AnalyzePRText(w)
			if len(a.Findings) != 1 || a.AnalyzedSteps != 1 || len(a.Unsupported) != 0 ||
				a.Findings[0].Location != (Location{Kind: "run_scalar_start", Line: 7, Column: 14}) {
				t.Fatalf("decoded script or conditional scope changed: %+v", a)
			}
		}
	}
}

func TestPRTextEvidenceIsBoundedInScanReports(t *testing.T) {
	in, dir := testInputs(t)
	for _, field := range []string{"title", "body"} {
		expression := "${{" + strings.Repeat(" ", MaxTextBytes+100) + "github.event.pull_request." + field + "}}"
		data := "jobs:\n  inspect:\n    steps:\n      - shell: bash\n        run: echo " + expression + "\n"
		put(t, dir, "long.workflow.txt", []byte(data))
		report, err := Scan(in, []string{"long.workflow.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if report.ExitCode != 1 || report.Totals.Findings != 1 {
			t.Fatalf("long canonical expression refused: %+v", report)
		}
		evidence := report.Files[0].Findings[0].Evidence
		if len(evidence) != 1 || len(evidence[0]) > MaxTextBytes+3 || !strings.HasSuffix(evidence[0], "…") {
			t.Fatalf("evidence is not bounded: %q", evidence)
		}
	}
}
