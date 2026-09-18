package hardener

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestCLIExitCodes(t *testing.T) {
	_, dir := testInputs(t)
	put(t, dir, "literal.workflow.txt", []byte(simpleWorkflow))
	put(t, dir, "title.workflow.txt", []byte(strings.Replace(simpleWorkflow, "echo literal", "echo ${{ github.event.pull_request.title }}", 1)))
	put(t, dir, "unsupported.workflow.txt", []byte(strings.Replace(simpleWorkflow, "shell: bash", "shell: pwsh", 1)))
	put(t, dir, "invalid.workflow.txt", []byte("jobs: ["))
	for _, tc := range []struct {
		name   string
		args   []string
		want   int
		status Status
	}{
		{"help", []string{"--help"}, 0, ""},
		{"scan help", []string{"scan", "--help"}, 0, ""},
		{"missing command", nil, 2, ""},
		{"unknown command", []string{"other"}, 2, ""},
		{"removed command", []string{"evaluate"}, 2, ""},
		{"missing file", []string{"scan", "--root", dir}, 2, ""},
		{"extra argument", []string{"scan", "--root", dir, "extra"}, 2, ""},
		{"bad flag", []string{"scan", "--invalid"}, 2, ""},
		{"scan no match", []string{"scan", "--root", dir, "--file", "literal.workflow.txt"}, 0, NoMatch},
		{"scan match", []string{"scan", "--root", dir, "--file", "title.workflow.txt"}, 1, Match},
		{"scan unsupported", []string{"scan", "--root", dir, "--file", "unsupported.workflow.txt"}, 2, Unsupported},
		{"scan malformed", []string{"scan", "--root", dir, "--file", "invalid.workflow.txt"}, 2, Error},
		{"scan missing input", []string{"scan", "--root", dir, "--file", "missing.workflow.txt"}, 2, Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := Run(tc.args, &stdout, &stderr); got != tc.want {
				t.Fatalf("exit %d, want %d; stdout=%s stderr=%s", got, tc.want, stdout.String(), stderr.String())
			}
			if tc.status != "" {
				var report ScanReport
				if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				if report.ExitCode != tc.want || len(report.Files) != 1 || report.Files[0].Status != tc.status {
					t.Fatalf("unexpected report: %+v", report)
				}
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("controlled write failure") }

func TestCLIOutputFailure(t *testing.T) {
	_, dir := testInputs(t)
	put(t, dir, "literal.workflow.txt", []byte(simpleWorkflow))
	if got := Run([]string{"scan", "--root", dir, "--file", "literal.workflow.txt"}, failingWriter{}, io.Discard); got != 2 {
		t.Fatalf("stdout failure exit %d", got)
	}
}

func TestCLIReportRuleIDs(t *testing.T) {
	_, dir := testInputs(t)
	put(t, dir, "body.workflow.txt", []byte(strings.Replace(simpleWorkflow, "echo literal", "echo ${{ github.event.pull_request.body }}", 1)))
	var stdout, stderr bytes.Buffer
	if got := Run([]string{"scan", "--root", dir, "--file", "body.workflow.txt"}, &stdout, &stderr); got != 1 {
		t.Fatalf("body scan exit %d; stderr=%s", got, stderr.String())
	}
	var report map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if _, exists := report["rule_id"]; exists {
		t.Fatal("report still claims to cover a single rule")
	}
	var ruleIDs []string
	if err := json.Unmarshal(report["rule_ids"], &ruleIDs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ruleIDs, []string{"WH-R001", "WH-R002"}) {
		t.Fatalf("incorrect report rule IDs: %v", ruleIDs)
	}
}

func TestCLIRejectsMixedRepositoryAndLocalInputs(t *testing.T) {
	for _, args := range [][]string{
		{"scan", "--repo", "owner/repo", "--root", "."},
		{"scan", "--repo", "owner/repo", "--file", "ci.yml"},
		{"scan", "--repo", "", "--file", "ci.yml"},
	} {
		var stdout, stderr bytes.Buffer
		fetch := func(context.Context, string) ScanReport {
			t.Fatal("conflicting arguments triggered a network scan")
			return ScanReport{}
		}
		if code := run(args, &stdout, &stderr, fetch); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "cannot be combined") {
			t.Fatalf("conflicting inputs accepted: exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
	}
}

func TestCLIInvalidRepositoryReturnsJSON(t *testing.T) {
	for _, repository := range []string{"", "https://example.com/owner/repo", "owner/repo/tree/main"} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"scan", "--repo", repository}, &stdout, &stderr); code != 2 {
			t.Fatalf("invalid repository exit %d", code)
		}
		var report ScanReport
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.ExitCode != 2 || len(report.Issues) != 1 || report.Issues[0].Code != "invalid_repository" || report.Source != nil {
			t.Fatalf("invalid repository was not reported: %+v", report)
		}
	}
}
