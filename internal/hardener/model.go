package hardener

const (
	PRTitleRuleID = "WH-R001"
	PRBodyRuleID  = "WH-R002"
	MaxFiles      = 50
	MaxFileBytes  = 256 * 1024
	MaxTextBytes  = 512
	ScanScope     = "Explicit step-level shell: bash; direct github.event.pull_request.title (WH-R001) and github.event.pull_request.body (WH-R002) expressions with surrounding ASCII spaces, tabs, CRs, or LFs only. Any other or incomplete expression, missing/other shell, reusable-workflow job, or no run steps is unsupported. Conditions, shell interpretation, and data flow are not evaluated."
)

type Status string

const (
	Match       Status = "match"
	NoMatch     Status = "no_match"
	Unsupported Status = "unsupported"
	Error       Status = "error"
)

type Location struct {
	Kind   string `json:"kind"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Step struct {
	JobID         string
	Index         int
	Run           string
	Shell         string
	ShellExplicit bool
	Location      Location
}

type Workflow struct {
	Path         string
	Steps        []Step
	ReusableJobs []string
	OutsideSteps int
}

type Finding struct {
	RuleID    string   `json:"rule_id"`
	Path      string   `json:"path"`
	JobID     string   `json:"job_id"`
	StepIndex int      `json:"step_index"`
	Location  Location `json:"location"`
	Message   string   `json:"message"`
	Evidence  []string `json:"evidence"`
}

type Issue struct {
	Kind      string `json:"kind"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	JobID     string `json:"job_id,omitempty"`
	StepIndex *int   `json:"step_index,omitempty"`
}

type Analysis struct {
	Findings      []Finding
	Unsupported   []Issue
	AnalyzedSteps int
}

type FileResult struct {
	Path          string    `json:"path"`
	Status        Status    `json:"status"`
	Parsed        bool      `json:"parsed"`
	RunSteps      int       `json:"run_steps"`
	AnalyzedSteps int       `json:"analyzed_steps"`
	OutsideSteps  int       `json:"outside_steps"`
	Findings      []Finding `json:"findings"`
	Issues        []Issue   `json:"issues"`
}

type Totals struct {
	Files            int `json:"files"`
	ParsedFiles      int `json:"parsed_files"`
	RunSteps         int `json:"run_steps"`
	AnalyzedSteps    int `json:"analyzed_steps"`
	OutsideSteps     int `json:"outside_steps"`
	UnsupportedFiles int `json:"unsupported_files"`
	ErrorFiles       int `json:"error_files"`
	Findings         int `json:"findings"`
}

func (t *Totals) add(f FileResult) {
	t.Files++
	if f.Parsed {
		t.ParsedFiles++
	}
	t.RunSteps += f.RunSteps
	t.AnalyzedSteps += f.AnalyzedSteps
	t.OutsideSteps += f.OutsideSteps
	t.Findings += len(f.Findings)
	if f.Status == Unsupported {
		t.UnsupportedFiles++
	}
	if f.Status == Error {
		t.ErrorFiles++
	}
}

func issue(kind, code, message string) Issue {
	return Issue{Kind: kind, Code: code, Message: bounded(message, MaxTextBytes)}
}
