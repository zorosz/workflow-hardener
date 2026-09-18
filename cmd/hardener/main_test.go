package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/zorosz/workflow-hardener/internal/hardener"
)

// Exercise real process exit codes against synthetic workflow examples.
// These tests execute the project CLI, never the workflow scripts.
func TestCompiledCLI(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "hardener")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, "./cmd/hardener")
	build.Dir = root
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOFLAGS=")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	run := func(t *testing.T, want int, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var stdout, stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		got := 0
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("run CLI: %v", err)
			}
			got = exitErr.ExitCode()
		}
		if got != want {
			t.Fatalf("exit %d, want %d; stdout=%s stderr=%s", got, want, stdout.String(), stderr.String())
		}
		return stdout.Bytes()
	}

	for _, tc := range []struct {
		name    string
		file    string
		want    int
		status  hardener.Status
		ruleIDs []string
	}{
		{"direct match", "risky-title", 1, hardener.Match, []string{"WH-R001"}},
		{"environment variable", "env-title", 0, hardener.NoMatch, nil},
		{"whitespace and repeats", "whitespace-title", 1, hardener.Match, []string{"WH-R001"}},
		{"outside run", "outside-run-title", 0, hardener.NoMatch, nil},
		{"unsupported shell", "pwsh-title", 2, hardener.Unsupported, nil},
		{"shell default", "default-shell-title", 2, hardener.Unsupported, nil},
		{"script comment", "comment-title", 1, hardener.Match, []string{"WH-R001"}},
		{"wrapped expression", "wrapped-title", 2, hardener.Unsupported, nil},
		{"partial finding", "mixed-support-title", 2, hardener.Unsupported, []string{"WH-R001"}},
		{"parser failure", "invalid-yaml", 2, hardener.Error, nil},
		{"direct body", "risky-body", 1, hardener.Match, []string{"WH-R002"}},
		{"body environment variable", "env-body", 0, hardener.NoMatch, nil},
		{"title and body in one step", "mixed-title-body", 1, hardener.Match, []string{"WH-R001", "WH-R002"}},
		{"partial body finding", "mixed-support-body", 2, hardener.Unsupported, []string{"WH-R002"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := run(t, tc.want, "scan", "--root", root, "--file", "testdata/"+tc.file+".workflow.txt")
			var report hardener.ScanReport
			if err := json.Unmarshal(output, &report); err != nil {
				t.Fatal(err)
			}
			if report.ExitCode != tc.want || len(report.Files) != 1 ||
				report.Files[0].Status != tc.status || len(report.Files[0].Findings) != len(tc.ruleIDs) ||
				report.Totals.Findings != len(tc.ruleIDs) || !reflect.DeepEqual(report.RuleIDs, []string{"WH-R001", "WH-R002"}) {
				t.Fatalf("unexpected compiled result: %+v", report)
			}
			for i, ruleID := range tc.ruleIDs {
				if report.Files[0].Findings[i].RuleID != ruleID {
					t.Fatalf("finding %d has wrong rule: %+v", i, report.Files[0].Findings[i])
				}
			}
			if tc.file == "mixed-title-body" && (report.Totals.AnalyzedSteps != 1 ||
				len(report.Files[0].Findings[1].Evidence) != 2) {
				t.Fatalf("mixed step or repeated body counted incorrectly: %+v", report)
			}
		})
	}
	t.Run("error precedence preserves finding", func(t *testing.T) {
		output := run(t, 2, "scan", "--root", root,
			"--file", "testdata/risky-title.workflow.txt", "--file", "testdata/invalid-yaml.workflow.txt")
		var report hardener.ScanReport
		if err := json.Unmarshal(output, &report); err != nil {
			t.Fatal(err)
		}
		if report.Totals.Findings != 1 || report.Totals.ErrorFiles != 1 || report.ExitCode != 2 {
			t.Fatalf("incomplete scan lost its finding: %+v", report)
		}
	})
}
