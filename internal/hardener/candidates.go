package hardener

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	candidatePageSize       = 25
	candidateDownloadLimit  = 20
	candidateSearchInterval = 6 * time.Second
	candidateDebugBodyLimit = 8 * 1024
	candidateDebugLimit     = 64 * 1024
)

// Candidate is a provisional text match, not a scanner finding.
type Candidate struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	BlobSHA    string `json:"blob_sha"`
	Filter     string `json:"filter"`
	PRField    string `json:"pr_field"`
	MatchLine  int    `json:"match_line"`
}

type CandidateQuery struct {
	Query      string `json:"query"`
	Completed  bool   `json:"completed"`
	TotalCount int    `json:"total_count"`
	Returned   int    `json:"returned"`
}

type CandidateIssue struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Query      string `json:"query,omitempty"`
	Repository string `json:"repository,omitempty"`
	Path       string `json:"path,omitempty"`
	BlobSHA    string `json:"blob_sha,omitempty"`
}

// Complete covers the fixed sample only, never all GitHub repositories.
type CandidateReport struct {
	Complete           bool             `json:"complete"`
	ExitCode           int              `json:"exit_code"`
	Queries            []CandidateQuery `json:"queries"`
	DownloadsAttempted int              `json:"downloads_attempted"`
	FilesFiltered      int              `json:"files_filtered"`
	Candidates         []Candidate      `json:"candidates"`
	Issues             []CandidateIssue `json:"issues"`
}

type candidateHit struct {
	Path       string `json:"path"`
	SHA        string `json:"sha"`
	Repository struct {
		FullName string `json:"full_name"`
		Private  *bool  `json:"private"`
	} `json:"repository"`
}

var candidateLayouts = []struct {
	name   string
	prefix string
}{
	{"one_line", `shell:[ \t]*bash[ \t]*\r?\n[ \t]+run:[^\r\n]*`},
	{"multiline", `shell:[ \t]*bash[ \t]*\r?\n[ \t]+run:[ \t]*[|>][-+]?[ \t]*\r?\n([^\n]*\n){0,7}[^\n]*`},
}

type candidateFilter struct {
	name, field string
	pattern     *regexp.Regexp
}

var candidateFilters = func() []candidateFilter {
	var filters []candidateFilter
	for _, layout := range candidateLayouts {
		for _, field := range []string{"title", "body"} {
			filters = append(filters, candidateFilter{layout.name, field,
				regexp.MustCompile(layout.prefix + `\$\{\{[ \t]*github\.event\.pull_request\.` + field + `[ \t]*\}\}`)})
		}
	}
	return filters
}()

func filterCandidates(hit candidateHit, data []byte) []Candidate {
	var candidates []Candidate
	for _, filter := range candidateFilters {
		if match := filter.pattern.FindIndex(data); match != nil {
			candidates = append(candidates, Candidate{
				Repository: hit.Repository.FullName, Path: hit.Path, BlobSHA: hit.SHA,
				Filter: filter.name, PRField: filter.field,
				MatchLine: 1 + strings.Count(string(data[:match[0]]), "\n"),
			})
		}
	}
	return candidates
}

func validCandidateHit(hit candidateHit) bool {
	name := hit.Repository.FullName
	if hit.Repository.Private == nil || *hit.Repository.Private || !repositoryPattern.MatchString(name) ||
		!gitSHAPattern.MatchString(hit.SHA) {
		return false
	}
	repo := strings.Split(name, "/")[1]
	path := strings.TrimPrefix(hit.Path, ".github/workflows/")
	return repo != "." && repo != ".." && path != hit.Path && !strings.Contains(path, "/") &&
		validPath(hit.Path) && (strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml"))
}

// FindCandidates searches with the supplied job token and downloads public blobs
// anonymously. Target bytes stay in memory and are never executed or YAML-parsed.
func FindCandidates(ctx context.Context, token string) CandidateReport {
	return findCandidates(ctx, token, newGitHubClient(), waitCandidateSearch, nil)
}

func waitCandidateSearch(ctx context.Context) error {
	timer := time.NewTimer(candidateSearchInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func findCandidates(ctx context.Context, token string, client *http.Client, pause func(context.Context) error, debug *candidateDebug) CandidateReport {
	report := CandidateReport{Candidates: []Candidate{}, Issues: []CandidateIssue{}}
	for _, field := range []string{"title", "body"} {
		for _, extension := range []string{"yml", "yaml"} {
			report.Queries = append(report.Queries, CandidateQuery{
				Query: `"github.event.pull_request.` + field + `" in:file path:.github/workflows extension:` + extension,
			})
		}
	}
	addIssue := func(code, message, query string, hit *candidateHit) {
		issue := CandidateIssue{Code: code, Message: message, Query: query}
		if hit != nil {
			issue.Repository, issue.Path, issue.BlobSHA = hit.Repository.FullName, hit.Path, hit.SHA
		}
		report.Issues = append(report.Issues, issue)
		report.ExitCode = 2
	}
	// Reject malformed headers without including token bytes in any diagnostic.
	if token == "" || len(token) > 4096 || strings.IndexFunc(token, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		addIssue("invalid_token", "GH_TOKEN must contain a nonempty GitHub Actions job token; no requests were made", "", nil)
		return report
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	seen := map[string]bool{}
	limitedDownloads := false
	for i := range report.Queries {
		query := &report.Queries[i]
		if i > 0 {
			if err := pause(ctx); err != nil {
				addIssue("search_canceled", "discovery was canceled or exceeded its time limit; remaining work skipped", query.Query, nil)
				return report
			}
		}
		params := url.Values{"q": {query.Query}, "per_page": {fmt.Sprint(candidatePageSize)}, "page": {"1"}}
		data, err := candidateGET(ctx, client, "/search/code?"+params.Encode(), "application/vnd.github+json", maxGitHubMetadataBytes, token, debug)
		if err != nil {
			addIssue("search_failed", err.Error()+"; remaining work skipped", query.Query, nil)
			return report
		}
		var page struct {
			TotalCount *int           `json:"total_count"`
			Incomplete *bool          `json:"incomplete_results"`
			Items      []candidateHit `json:"items"`
		}
		if json.Unmarshal(data, &page) != nil || page.TotalCount == nil || *page.TotalCount < 0 ||
			page.Incomplete == nil || page.Items == nil || len(page.Items) > candidatePageSize || *page.TotalCount < len(page.Items) {
			addIssue("invalid_search_response", "GitHub returned invalid search metadata; remaining work skipped", query.Query, nil)
			return report
		}
		query.Completed, query.TotalCount, query.Returned = true, *page.TotalCount, len(page.Items)
		if *page.Incomplete {
			addIssue("incomplete_search", "GitHub marked these search results incomplete", query.Query, nil)
		}
		if *page.TotalCount > len(page.Items) {
			addIssue("search_sample_limit", "only the first page of up to 25 results is inspected; additional results were omitted", query.Query, nil)
		}
		for _, hit := range page.Items {
			if !validCandidateHit(hit) {
				addIssue("invalid_search_hit", "search result is not a confirmed public repository with a direct portable workflow path and blob SHA; remaining work skipped", query.Query, nil)
				return report
			}
			key := strings.ToLower(hit.Repository.FullName) + "\n" + hit.Path + "\n" + hit.SHA
			if seen[key] {
				continue
			}
			seen[key] = true
			if report.DownloadsAttempted == candidateDownloadLimit {
				if !limitedDownloads {
					addIssue("download_sample_limit", "only the first 20 unique workflow results are downloaded; additional files were omitted", "", nil)
					limitedDownloads = true
				}
				continue
			}
			report.DownloadsAttempted++
			data, err := candidateGET(ctx, client, "/repos/"+hit.Repository.FullName+"/git/blobs/"+hit.SHA, "application/vnd.github.raw+json", MaxFileBytes, "", debug)
			if err != nil {
				addIssue("download_failed", err.Error()+"; remaining work skipped", query.Query, &hit)
				return report
			}
			report.FilesFiltered++
			report.Candidates = append(report.Candidates, filterCandidates(hit, data)...)
		}
	}
	report.Complete = len(report.Issues) == 0
	return report
}

func candidateGET(ctx context.Context, client *http.Client, path, accept string, limit int64, token string, debug *candidateDebug) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		return nil, errors.New("cannot construct GitHub request")
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "workflow-hardener")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	var record candidateDebugRecord
	logging := debug != nil && !debug.stopped
	if logging {
		started := time.Now()
		record.Event, record.Timestamp = "http_request", started.UTC().Format(time.RFC3339Nano)
		record.Endpoint, record.Query = "https://api.github.com"+req.URL.Path, req.URL.Query().Get("q")
		defer func() {
			record.ElapsedMS = time.Since(started).Milliseconds()
			debug.write(record)
		}()
	}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			record.Error = "canceled_or_deadline"
			return nil, errors.New("discovery was canceled or exceeded its time limit")
		}
		record.Error = "request_failed_or_timed_out"
		return nil, errors.New("GitHub request failed or timed out")
	}
	defer response.Body.Close()
	if logging {
		record.Status = response.StatusCode
		record.RequestID = candidateRequestID(response.Header)
		if dates := response.Header.Values("Date"); len(dates) == 1 && len(dates[0]) <= 64 {
			if date, err := http.ParseTime(dates[0]); err == nil && date.Year() >= 0 && date.Year() <= 9999 {
				record.ServerDate = date.UTC().Format(time.RFC3339)
			}
		}
		details, _ := candidateRateLimitDetails(response.Header)
		record.RateLimit = strings.Join(details, "; ")
	}
	if response.StatusCode != http.StatusOK {
		if logging {
			record.GitHubMessage, record.MessageUnavailable, record.MessageTruncated = debug.message(response.Body)
		}
		switch {
		case response.StatusCode == 429 || (response.StatusCode == 403 && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "")):
			return nil, candidateRateLimitError(response.StatusCode, response.Header)
		case response.StatusCode >= 300 && response.StatusCode < 400:
			return nil, errors.New("GitHub redirect refused")
		default:
			return nil, fmt.Errorf("GitHub returned HTTP %d", response.StatusCode)
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		record.Error = "response_read_failed"
		return nil, errors.New("cannot read GitHub response")
	}
	if int64(len(data)) > limit {
		record.Error = "response_too_large"
		return nil, fmt.Errorf("GitHub response exceeds %d bytes", limit)
	}
	return data, nil
}

func candidateRateLimitError(status int, header http.Header) error {
	fields, hasTiming := candidateRateLimitDetails(header)
	details := append([]string{fmt.Sprintf("GitHub API rate limit reached (HTTP %d)", status)}, fields...)
	if hasTiming {
		details = append(details, "respect the reported reset/retry time before rerunning")
	} else {
		details = append(details, "retry time unknown")
	}
	return errors.New(strings.Join(details, "; "))
}

func candidateRateLimitDetails(header http.Header) ([]string, bool) {
	var details []string
	if values := header.Values("X-RateLimit-Resource"); len(values) > 0 {
		resource := "unknown"
		if len(values) == 1 {
			switch values[0] {
			case "core", "search", "code_search":
				resource = values[0]
			}
		}
		details = append(details, "resource="+resource)
	}
	for _, field := range []struct{ header, label string }{
		{"X-RateLimit-Limit", "limit"}, {"X-RateLimit-Remaining", "remaining"},
	} {
		if value, ok := candidateRateLimitNumber(header, field.header, 1<<31-1); ok {
			details = append(details, fmt.Sprintf("%s=%d", field.label, value))
		}
	}
	// Bound reset timestamps to the end of year 9999 for RFC3339 output.
	reset, hasReset := candidateRateLimitNumber(header, "X-RateLimit-Reset", 253402300799)
	if hasReset {
		details = append(details, "reset="+time.Unix(reset, 0).UTC().Format(time.RFC3339))
	}
	retry, hasRetry := candidateRateLimitNumber(header, "Retry-After", 1<<31-1)
	if hasRetry {
		details = append(details, fmt.Sprintf("retry_after=%d seconds", retry))
	}
	return details, hasReset || hasRetry
}

// Accept one short decimal value; never echo raw or ambiguous header text.
func candidateRateLimitNumber(header http.Header, name string, max int64) (int64, bool) {
	values := header.Values(name)
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 12 {
		return 0, false
	}
	for _, digit := range values[0] {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseInt(values[0], 10, 64)
	return value, err == nil && value <= max
}

type candidateDebugRecord struct {
	Event              string `json:"event"`
	Timestamp          string `json:"timestamp"`
	Endpoint           string `json:"endpoint"`
	Query              string `json:"query,omitempty"`
	ElapsedMS          int64  `json:"elapsed_ms"`
	Status             int    `json:"status,omitempty"`
	Error              string `json:"error,omitempty"`
	RequestID          string `json:"request_id,omitempty"`
	ServerDate         string `json:"server_date,omitempty"`
	RateLimit          string `json:"rate_limit,omitempty"`
	GitHubMessage      string `json:"github_message,omitempty"`
	MessageUnavailable string `json:"message_unavailable,omitempty"`
	MessageTruncated   bool   `json:"message_truncated,omitempty"`
}

type candidateDebug struct {
	out             io.Writer
	token           string
	written         int
	stopped, failed bool
}

func (d *candidateDebug) redact(text string) string {
	if d.token != "" {
		return strings.ReplaceAll(text, d.token, "[REDACTED]")
	}
	return text
}

func (d *candidateDebug) message(body io.Reader) (string, string, bool) {
	data, err := io.ReadAll(io.LimitReader(body, candidateDebugBodyLimit+1))
	if err != nil {
		return "", "read_failed", false
	}
	if len(data) > candidateDebugBodyLimit {
		return "", "body_too_large", false
	}
	var payload struct {
		Message *string `json:"message"`
	}
	if !utf8.Valid(data) || json.Unmarshal(data, &payload) != nil {
		return "", "invalid_json", false
	}
	if payload.Message == nil || *payload.Message == "" {
		return "", "missing_message", false
	}
	// Decode and redact before truncation, including JSON-escaped token bytes.
	text := []rune(d.redact(*payload.Message))
	if len(text) > 1024 {
		return string(text[:1023]) + "…", "", true
	}
	return string(text), "", false
}

func candidateRequestID(header http.Header) string {
	values := header.Values("X-GitHub-Request-Id")
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 128 {
		return ""
	}
	for _, c := range values[0] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ':' || c == '-') {
			return ""
		}
	}
	return values[0]
}

func (d *candidateDebug) write(record candidateDebugRecord) {
	if d.stopped {
		return
	}
	for _, field := range []*string{&record.Event, &record.Timestamp, &record.Endpoint, &record.Query,
		&record.Error, &record.RequestID, &record.ServerDate, &record.RateLimit, &record.GitHubMessage, &record.MessageUnavailable} {
		*field = d.redact(*field)
	}
	data, _ := json.Marshal(record) // This struct contains only strings, integers, and a boolean.
	data = append(data, '\n')
	const truncated = "{\"event\":\"debug_truncated\"}\n"
	if d.written+len(data) > candidateDebugLimit-len(truncated) {
		data = []byte(truncated)
		d.stopped = true
	}
	n, err := d.out.Write(data)
	d.written += n
	if err != nil || n != len(data) {
		d.failed, d.stopped = true, true
	}
}

// RunCandidates writes a new candidates.json in the current directory. Keeping
// the token out of command-line arguments avoids exposing it in process listings.
func RunCandidates(args []string, token string, stdout, stderr io.Writer) int {
	return runCandidates(args, token, stdout, stderr, func(ctx context.Context, token string, debug *candidateDebug) CandidateReport {
		return findCandidates(ctx, token, newGitHubClient(), waitCandidateSearch, debug)
	})
}

func runCandidates(args []string, token string, stdout, stderr io.Writer, find func(context.Context, string, *candidateDebug) CandidateReport) int {
	const help = "usage: find-candidates [--debug]\nUses GH_TOKEN and writes a new candidates.json in the current directory.\n--debug writes bounded, redacted HTTP diagnostics to stderr."
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, help)
		return 0
	}
	debugEnabled := len(args) == 1 && args[0] == "--debug"
	if len(args) != 0 && !debugEnabled {
		fmt.Fprintln(stderr, help)
		return 2
	}
	file, err := os.OpenFile("candidates.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintln(stderr, "cannot create candidates.json; use a writable directory without an existing candidates.json")
		return 2
	}
	var debug *candidateDebug
	if debugEnabled {
		debug = &candidateDebug{out: stderr, token: token}
	}
	report := find(context.Background(), token, debug)
	if debug != nil && debug.failed {
		fmt.Fprintln(stdout, "Debug diagnostics could not be fully written; the discovery result is preserved.")
	}
	writeErr := writeJSON(file, report)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		fmt.Fprintln(stderr, "cannot write candidates.json; the file may be incomplete")
		return 2
	}
	fmt.Fprintf(stdout, "Saved candidates.json: %d candidates, %d files filtered, %d issues; complete=%t\n", len(report.Candidates), report.FilesFiltered, len(report.Issues), report.Complete)
	return report.ExitCode
}
