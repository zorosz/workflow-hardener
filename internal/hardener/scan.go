package hardener

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// workflowReader supplies bounded bytes from local files or a GitHub snapshot.
type workflowReader interface {
	Read(name string, limit int64) ([]byte, error)
}

// scanFile follows the input through reading, parsing, and detection.
func scanFile(in workflowReader, name string) FileResult {
	result := FileResult{Path: name, Status: Error, Findings: []Finding{}, Issues: []Issue{}}
	data, err := in.Read(name, MaxFileBytes)
	if err != nil {
		result.Issues = append(result.Issues, issue("input", "read_failed", err.Error()))
		return result
	}
	w, err := ParseWorkflow(name, data)
	if err != nil {
		result.Issues = append(result.Issues, issue("parse", "invalid_workflow", err.Error()))
		return result
	}
	result.Parsed = true
	result.RunSteps = len(w.Steps)
	result.OutsideSteps = w.OutsideSteps
	analysis := AnalyzePRText(w)
	result.AnalyzedSteps = analysis.AnalyzedSteps
	for _, f := range analysis.Findings {
		f.Message = bounded(f.Message, MaxTextBytes)
		for i := range f.Evidence {
			f.Evidence[i] = bounded(f.Evidence[i], MaxTextBytes)
		}
		result.Findings = append(result.Findings, f)
	}
	for _, p := range analysis.Unsupported {
		p.Message = bounded(p.Message, MaxTextBytes)
		result.Issues = append(result.Issues, p)
	}
	switch {
	case len(result.Issues) > 0:
		result.Status = Unsupported
	case len(result.Findings) > 0:
		result.Status = Match
	default:
		result.Status = NoMatch
	}
	return result
}

type ScanReport struct {
	RuleIDs  []string          `json:"rule_ids"`
	Scope    string            `json:"scope"`
	ExitCode int               `json:"exit_code"`
	Totals   Totals            `json:"totals"`
	Files    []FileResult      `json:"files"`
	Source   *RepositorySource `json:"source,omitempty"`
	Issues   []Issue           `json:"issues,omitempty"`
}

func newScanReport() ScanReport {
	return ScanReport{RuleIDs: []string{PRTitleRuleID, PRBodyRuleID}, Scope: ScanScope, Files: []FileResult{}}
}

// Scan processes explicit files in a stable order. Incomplete analysis takes
// precedence over a finding in the exit code, while all findings are retained.
func Scan(in workflowReader, names []string) (ScanReport, error) {
	report := newScanReport()
	if len(names) == 0 || len(names) > MaxFiles {
		return report, fmt.Errorf("scan requires 1-%d explicit files", MaxFiles)
	}
	seen := map[string]bool{}
	ordered := append([]string(nil), names...)
	sort.Strings(ordered)
	for _, name := range ordered {
		key := strings.ToLower(name)
		if seen[key] {
			return report, errors.New("scan files must be distinct")
		}
		seen[key] = true
	}
	for _, name := range ordered {
		f := scanFile(in, name)
		report.Files = append(report.Files, f)
		report.Totals.add(f)
		switch f.Status {
		case Error, Unsupported:
			report.ExitCode = 2
		case Match:
			if report.ExitCode == 0 {
				report.ExitCode = 1
			}
		}
	}
	return report, nil
}
