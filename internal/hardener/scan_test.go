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
