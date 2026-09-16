package hardener

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const simpleWorkflow = "jobs:\n  inspect:\n    steps:\n      - shell: bash\n        run: echo literal\n"

func TestParserPreservesStructureAndLocations(t *testing.T) {
	data := "name: '${{ github.event.pull_request.title }}'\n" +
		"jobs:\n  z:\n    steps:\n      - uses: local/action\n      - shell: bash\n        run: |\n          echo first\n          echo second\n  a:\n    steps:\n      - run: echo other\n"
	w, err := ParseWorkflow("sample.workflow.txt", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Steps) != 2 || w.OutsideSteps != 1 {
		t.Fatalf("unexpected shape: %+v", w)
	}
	if w.Steps[0].JobID != "a" || w.Steps[0].ShellExplicit {
		t.Fatalf("wrong first job: %+v", w.Steps[0])
	}
	step := w.Steps[1]
	if step.JobID != "z" || step.Index != 1 || step.Shell != "bash" || !step.ShellExplicit {
		t.Fatalf("wrong attribution: %+v", step)
	}
	if step.Run != "echo first\necho second\n" {
		t.Fatalf("run scalar changed: %q", step.Run)
	}
	if step.Location != (Location{Kind: "run_scalar_start", Line: 7, Column: 14}) {
		t.Fatalf("wrong location: %+v", step.Location)
	}
}

func TestParserRefusesInvalidInputs(t *testing.T) {
	cases := map[string]string{
		"empty":                   "",
		"malformed":               "jobs: [",
		"duplicate root":          "jobs: {}\njobs: {}\n",
		"duplicate nested":        "jobs:\n  x:\n    steps:\n      - run: echo first\n        run: echo second\n",
		"multiple documents":      simpleWorkflow + "---\n" + simpleWorkflow,
		"trailing empty document": simpleWorkflow + "---\n",
		"alias":                   "base: &value echo text\njobs:\n  x:\n    steps:\n      - run: *value\n",
		"merge":                   "base: &base {a: b}\njobs:\n  x:\n    <<: *base\n    steps: []\n",
		"unknown tag":             "extra: !custom text\n" + simpleWorkflow,
		"root sequence":           "- jobs\n",
		"missing jobs":            "name: title\n",
		"empty jobs":              "jobs: {}\n",
		"bad jobs":                "jobs: []\n",
		"bad job":                 "jobs: {x: []}\n",
		"missing steps":           "jobs: {x: {runs-on: ubuntu-latest}}\n",
		"bad steps":               "jobs: {x: {steps: {run: echo}}}\n",
		"bad step":                "jobs: {x: {steps: [text]}}\n",
		"missing run":             "jobs: {x: {steps: [{name: incomplete}]}}\n",
		"run type":                "jobs: {x: {steps: [{run: [echo]}]}}\n",
		"shell type":              "jobs: {x: {steps: [{run: echo, shell: 3}]}}\n",
		"both run and uses":       "jobs: {x: {steps: [{run: echo, uses: local/action}]}}\n",
		"bad reusable":            "jobs: {x: {uses: 3}}\n",
		"reusable and steps":      "jobs: {x: {uses: local/workflow, steps: []}}\n",
		"nonstring key":           "jobs: {true: {steps: []}}\n",
		"oversize":                strings.Repeat(" ", MaxFileBytes+1),
		"excessive nesting":       "extra: " + strings.Repeat("[", 70) + "0" + strings.Repeat("]", 70) + "\n" + simpleWorkflow,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseWorkflow("case.workflow.txt", []byte(data)); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestParserRetainsUnmodeledContexts(t *testing.T) {
	for _, data := range []string{
		"jobs: {x: {uses: local/workflow}}",
		"jobs: {x: {steps: []}}",
		"jobs: {x: {steps: [{uses: local/action}]}}",
		"jobs: {x: {steps: [{shell: pwsh, run: text}]}}",
	} {
		if _, err := ParseWorkflow("case.workflow.txt", []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFixtureStructure(t *testing.T) {
	paths := []string{"risky-title", "env-title", "whitespace-title", "outside-run-title", "pwsh-title", "default-shell-title", "comment-title", "wrapped-title", "mixed-support-title"}
	for _, name := range paths {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name+".workflow.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseWorkflow(name+".workflow.txt", data); err != nil {
				t.Fatal(err)
			}
		})
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "invalid-yaml.workflow.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseWorkflow("invalid-yaml.workflow.txt", data); err == nil {
		t.Fatal("malformed fixture accepted")
	}
}
