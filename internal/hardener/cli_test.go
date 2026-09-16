package hardener

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
