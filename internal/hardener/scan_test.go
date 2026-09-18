package hardener

import (
	"reflect"
	"strings"
	"testing"
)

func TestScanLimitAndErrorPrecedence(t *testing.T) {
	in, dir := testInputs(t)
	put(t, dir, "title.workflow.txt", []byte(strings.Replace(simpleWorkflow, "echo literal", "echo ${{ github.event.pull_request.title }}", 1)))
	put(t, dir, "literal.workflow.txt", []byte(simpleWorkflow))
	put(t, dir, "unsupported.workflow.txt", []byte(strings.Replace(simpleWorkflow, "shell: bash", "shell: pwsh", 1)))
	for _, tc := range []struct {
		name        string
		other       string
		wantExit    int
		wantErrors  int
		unsupported int
	}{
		{"finding and no match", "literal.workflow.txt", 1, 0, 0},
		{"finding and missing input", "missing.workflow.txt", 2, 1, 0},
		{"finding and unsupported", "unsupported.workflow.txt", 2, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, names := range [][]string{{"title.workflow.txt", tc.other}, {tc.other, "title.workflow.txt"}} {
				report, err := Scan(in, names)
				if err != nil || report.ExitCode != tc.wantExit || report.Totals.Findings != 1 ||
					report.Totals.ErrorFiles != tc.wantErrors || report.Totals.UnsupportedFiles != tc.unsupported {
					t.Fatalf("lost finding or incomplete status: %+v %v", report, err)
				}
			}
		})
	}
	for _, names := range [][]string{
		nil,
		{"title.workflow.txt", "title.workflow.txt"},
		{"title.workflow.txt", "TITLE.workflow.txt"},
		make([]string, MaxFiles+1),
	} {
		if _, err := Scan(in, names); err == nil {
			t.Fatalf("invalid request accepted: %v", names)
		}
	}
}

func TestScanStableOrder(t *testing.T) {
	in, dir := testInputs(t)
	put(t, dir, "a.workflow.txt", []byte(simpleWorkflow))
	put(t, dir, "z.workflow.txt", []byte(simpleWorkflow))
	names := []string{"z.workflow.txt", "a.workflow.txt"}
	first, err := Scan(in, names)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Scan(in, []string{"a.workflow.txt", "z.workflow.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Files[0].Path != "a.workflow.txt" || !reflect.DeepEqual(first, second) || names[0] != "z.workflow.txt" {
		t.Fatal("scan order is unstable or mutated the caller's file list")
	}
}

func TestScanBothRulesPreserveFindingsWithIncompleteAnalysis(t *testing.T) {
	in, dir := testInputs(t)
	data := `jobs:
  inspect:
    steps:
      - shell: bash
        run: echo ${{ github.event.pull_request.title }} ${{ github.event.pull_request.body }}
      - shell: bash
        run: echo ${{ github.event.pull_request.body }} ${{ toJSON(github.workspace) }}
      - shell: bash
        run: echo literal
`
	put(t, dir, "mixed.workflow.txt", []byte(data))
	report, err := Scan(in, []string{"mixed.workflow.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if report.ExitCode != 2 || len(report.Files) != 1 ||
		report.Totals != (Totals{Files: 1, ParsedFiles: 1, RunSteps: 3, AnalyzedSteps: 2, UnsupportedFiles: 1, Findings: 2}) {
		t.Fatalf("incomplete analysis hid findings or double-counted steps: %+v", report)
	}
	f := report.Files[0]
	if f.Status != Unsupported || len(f.Findings) != 2 || len(f.Issues) != 1 ||
		f.Issues[0].Code != "unsupported_expression" || f.Issues[0].StepIndex == nil || *f.Issues[0].StepIndex != 1 {
		t.Fatalf("incorrect mixed result: %+v", f)
	}
	for i, ruleID := range []string{"WH-R001", "WH-R002"} {
		if f.Findings[i].RuleID != ruleID || f.Findings[i].StepIndex != 0 ||
			f.Findings[i].Location != (Location{Kind: "run_scalar_start", Line: 5, Column: 14}) {
			t.Fatalf("finding %d lost its attribution: %+v", i, f.Findings[i])
		}
	}
}

func TestResolvedShellEnvironmentValuesStayOutsideRun(t *testing.T) {
	for _, jobFields := range []string{
		"defaults: {run: {shell: sh}}",
		"runs-on: ubuntu-latest",
		"runs-on: ubuntu-latest\n    container: ubuntu",
	} {
		t.Run(jobFields, func(t *testing.T) {
			data := "jobs:\n  inspect:\n    " + jobFields + `
    steps:
      - env:
          PR_TITLE: ${{ github.event.pull_request.title }}
          PR_BODY: ${{ github.event.pull_request.body }}
        run: printf '%s\n' "$PR_TITLE" "$PR_BODY"
`
			in, dir := testInputs(t)
			put(t, dir, "environment.workflow.txt", []byte(data))
			report, err := Scan(in, []string{"environment.workflow.txt"})
			if err != nil || report.ExitCode != 0 || len(report.Files) != 1 || report.Files[0].Status != NoMatch ||
				report.Totals != (Totals{Files: 1, ParsedFiles: 1, RunSteps: 1, AnalyzedSteps: 1}) {
				t.Fatalf("environment values became findings or unsupported: %+v, %v", report, err)
			}
		})
	}
}
