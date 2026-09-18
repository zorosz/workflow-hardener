package hardener

import (
	"strings"
	"testing"
)

func TestShellResolutionAndBothRules(t *testing.T) {
	const workflowSh = "defaults: {run: {shell: sh}}\n"
	const workflowBash = "defaults: {run: {shell: bash}}\n"
	cases := []struct {
		name, workflow, job, step, wantShell string
	}{
		{"step overrides defaults", workflowSh, "defaults: {run: {shell: pwsh}}, ", "shell: bash, ", "bash"},
		{"step sh without runner", "", "", "shell: sh, ", "sh"},
		{"job overrides workflow", workflowBash, "defaults: {run: {shell: sh}}, ", "", "sh"},
		{"workflow default", workflowBash, "", "", "bash"},
		{"workflow sh default", workflowSh, "", "", "sh"},
		{"job directory preserves workflow shell", workflowSh, "defaults: {run: {working-directory: scripts}}, ", "", "sh"},
		{"empty job defaults inherit", workflowBash, "defaults: {}, ", "", "bash"},
		{"empty job run defaults inherit", workflowBash, "defaults: {run: {}}, ", "", "bash"},
		{"default beats container", workflowBash, "runs-on: ubuntu-latest, container: ubuntu, ", "", "bash"},
		{"job default beats container", "", "runs-on: ubuntu-latest, container: ubuntu, defaults: {run: {shell: bash}}, ", "", "bash"},
		{"container string", "", "runs-on: ubuntu-latest, container: ubuntu, ", "", "sh"},
		{"container mapping", "", "runs-on: ubuntu-24.04, container: {image: 'ubuntu:24.04'}, ", "", "sh"},
		{"service is not a job container", "", "runs-on: ubuntu-24.04, services: {db: {image: postgres}}, ", "", defaultPOSIXShell},
		{"explicit shell on Windows", "", "runs-on: windows-latest, ", "shell: bash, ", "bash"},
		{"explicit shell with dynamic context", "", "runs-on: '${{ matrix.os }}', container: '${{ matrix.image }}', ", "shell: sh, ", "sh"},
		{"job shell on unknown runner", "", "runs-on: custom, defaults: {run: {shell: bash}}, ", "", "bash"},
		{"step overrides dynamic defaults", "defaults: {run: {shell: '${{ inputs.shell }}'}}\n", "", "shell: sh, ", "sh"},
		{"job overrides unsupported workflow default", "defaults: {run: {shell: python}}\n", "defaults: {run: {shell: bash}}, ", "", "bash"},
		{"missing context", "", "", "", ""},
		{"Windows default", "", "runs-on: windows-latest, ", "", ""},
		{"custom runner", "", "runs-on: ubuntu-custom, ", "", ""},
		{"unlisted runner", "", "runs-on: ubuntu-24.04-arm, ", "", ""},
		{"runner array", "", "runs-on: [ubuntu-latest], ", "", ""},
		{"runner group", "", "runs-on: {group: example, labels: ubuntu-latest}, ", "", ""},
		{"self hosted", "", "runs-on: [self-hosted, linux], ", "", ""},
		{"matrix runner", "", "runs-on: '${{ matrix.os }}', ", "", ""},
		{"null runner", "", "runs-on: null, ", "", ""},
		{"container without runner", "", "container: ubuntu, ", "", ""},
		{"container on macOS", "", "runs-on: macos-latest, container: ubuntu, ", "", ""},
		{"dynamic container", "", "runs-on: ubuntu-latest, container: '${{ matrix.container }}', ", "", ""},
		{"dynamic image", "", "runs-on: ubuntu-latest, container: {image: 'ubuntu:${{ matrix.version }}'}, ", "", ""},
		{"empty container", "", "runs-on: ubuntu-latest, container: '', ", "", ""},
		{"blank container image", "", "runs-on: ubuntu-latest, container: {image: '   '}, ", "", ""},
		{"null container", "", "runs-on: ubuntu-latest, container: null, ", "", ""},
		{"container array", "", "runs-on: ubuntu-latest, container: [ubuntu], ", "", ""},
		{"missing container image", "", "runs-on: ubuntu-latest, container: {}, ", "", ""},
		{"non-string container image", "", "runs-on: ubuntu-latest, container: {image: false}, ", "", ""},
		{"empty step blocks fallback", workflowBash, "runs-on: ubuntu-latest, ", "shell: '', ", ""},
		{"step PowerShell blocks fallback", workflowBash, "runs-on: ubuntu-latest, container: ubuntu, ", "shell: pwsh, ", ""},
		{"custom shell blocks fallback", workflowBash, "", "shell: 'bash {0}', ", ""},
		{"shell whitespace blocks fallback", workflowBash, "", "shell: 'bash ', ", ""},
		{"dynamic step blocks fallback", workflowBash, "", "shell: '${{ inputs.shell }}', ", ""},
		{"internal marker is not a shell keyword", workflowBash, "runs-on: ubuntu-latest, ", "shell: bash-or-sh, ", ""},
		{"empty job shell blocks fallback", workflowBash, "defaults: {run: {shell: ''}}, ", "", ""},
		{"job Python blocks fallback", workflowBash, "defaults: {run: {shell: python}}, ", "", ""},
		{"dynamic job shell blocks fallback", workflowBash, "defaults: {run: {shell: '${{ inputs.shell }}'}}, ", "", ""},
		{"workflow cmd blocks runner fallback", "defaults: {run: {shell: cmd}}\n", "runs-on: ubuntu-latest, ", "", ""},
		{"empty workflow shell blocks runner fallback", "defaults: {run: {shell: ''}}\n", "runs-on: ubuntu-latest, ", "", ""},
	}
	for _, runner := range []string{"ubuntu-latest", "ubuntu-22.04", "ubuntu-24.04", "ubuntu-26.04", "macos-latest", "macos-14", "macos-15", "macos-26"} {
		cases = append(cases, struct{ name, workflow, job, step, wantShell string }{
			name: runner, job: "runs-on: " + runner + ", ", wantShell: defaultPOSIXShell,
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const script = "echo ${{ github.event.pull_request.title }} ${{ github.event.pull_request.body }}"
			jobLine := "  inspect: {" + tc.job + "steps: [{" + tc.step + "run: '" + script + "'}]}"
			data := tc.workflow + "jobs:\n" + jobLine + "\n"
			w, err := ParseWorkflow("shell.workflow.txt", []byte(data))
			if err != nil {
				t.Fatal(err)
			}
			if len(w.Steps) != 1 || w.Steps[0].Shell != tc.wantShell || w.Steps[0].ShellExplicit != (tc.step != "") {
				t.Fatalf("unexpected shell selection: %+v", w.Steps)
			}
			a := AnalyzePRText(w)
			if tc.wantShell == "" {
				if a.AnalyzedSteps != 0 || len(a.Findings) != 0 || len(a.Unsupported) != 1 || a.Unsupported[0].Code != "unsupported_shell" {
					t.Fatalf("unknown shell became supported: %+v", a)
				}
				return
			}
			if a.AnalyzedSteps != 1 || len(a.Findings) != 2 || len(a.Unsupported) != 0 {
				t.Fatalf("resolved shell lost findings: %+v", a)
			}
			location := Location{Kind: "run_scalar_start", Line: strings.Count(tc.workflow, "\n") + 2, Column: strings.Index(jobLine, "run: '") + 6}
			for i, id := range []string{PRTitleRuleID, PRBodyRuleID} {
				f := a.Findings[i]
				if f.RuleID != id || f.Path != w.Path || f.JobID != "inspect" || f.StepIndex != 0 || f.Location != location {
					t.Fatalf("finding lost source attribution: %+v, want location %+v", f, location)
				}
			}
		})
	}
}

func TestMalformedShellDefaultsAreErrors(t *testing.T) {
	for _, defaults := range []string{"null", "[]", "bash", "{run: null}", "{run: []}", "{run: bash}", "{run: {shell: null}}", "{run: {shell: 3}}", "{run: {shell: []}}"} {
		for _, level := range []string{"workflow", "job"} {
			t.Run(level+"/"+defaults, func(t *testing.T) {
				// A valid step shell cannot hide structurally invalid defaults.
				job := "steps: [{shell: bash, run: literal}]"
				data := "defaults: " + defaults + "\njobs: {inspect: {" + job + "}}\n"
				if level == "job" {
					data = "jobs: {inspect: {defaults: " + defaults + ", " + job + "}}\n"
				}
				if _, err := ParseWorkflow("invalid.workflow.txt", []byte(data)); err == nil || !strings.Contains(err.Error(), level+" defaults") {
					t.Fatalf("invalid %s defaults accepted or misattributed: %v", level, err)
				}
			})
		}
	}
}

func TestShellDefaultsStayWithinTheirJob(t *testing.T) {
	data := `defaults: {run: {shell: sh}}
jobs:
  first:
    defaults: {run: {shell: pwsh}}
    steps:
      - run: echo literal
      - shell: bash
        run: echo literal
      - run: echo literal
  second:
    steps:
      - run: echo literal
`
	w, err := ParseWorkflow("jobs.workflow.txt", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Steps) != 4 {
		t.Fatalf("unexpected steps: %+v", w.Steps)
	}
	for i, want := range []string{"", "bash", "", "sh"} {
		if got := w.Steps[i].Shell; got != want {
			t.Fatalf("step %d inherited another step/job shell: %q, want %q", i, got, want)
		}
	}
}
