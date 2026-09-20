package hardener

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const candidateTestToken = "synthetic-job-token"
const candidateTitle = "${{ github.event.pull_request.title }}"
const candidateBody = "${{ github.event.pull_request.body }}"
const candidateScript = "shell: bash\n  run: echo " + candidateTitle + "\n"

func candidateTestHit(path, sha string) candidateHit {
	hit := candidateHit{Path: path, SHA: sha}
	public := false
	hit.Repository.FullName, hit.Repository.Private = "owner/project", &public
	return hit
}

func TestCandidateFilters(t *testing.T) {
	for _, tc := range []struct {
		name, data, filter, field string
		line                      int
	}{
		{"one line", "# heading\n" + candidateScript, "one_line", "title", 2},
		{"body with tabs and CRLF", "\tshell:\tbash \r\n\trun: echo ${{\tgithub.event.pull_request.body\t}}", "one_line", "body", 1},
		{"literal block", "shell: bash\n  run: |\n    echo " + candidateBody, "multiline", "body", 1},
		{"folded block", "shell: bash\r\n  run: >-\r\n    echo " + candidateTitle, "multiline", "title", 1},
		{"keep chomping", "shell: bash\n  run: |+\n    echo " + candidateBody, "multiline", "body", 1},
		{"eighth line", "shell: bash\n  run: |\n" + strings.Repeat("    echo literal\n", 7) + "    echo " + candidateTitle, "multiline", "title", 1},
		{"ninth line", "shell: bash\n  run: |\n" + strings.Repeat("    echo literal\n", 8) + "    echo " + candidateTitle, "", "", 0},
		{"first matching step", "# first\n" + candidateScript + candidateScript, "one_line", "title", 2},
		{"env only", "shell: bash\n  env:\n    TITLE: " + candidateTitle + "\n  run: echo $TITLE", "", "", 0},
		{"inherited shell", "defaults:\n  run:\n    shell: bash\njobs:\n  test:\n    steps:\n      - run: echo " + candidateTitle, "", "", 0},
		{"sh", "shell: sh\n  run: echo " + candidateTitle, "", "", 0},
		{"quoted shell", "shell: 'bash'\n  run: echo " + candidateTitle, "", "", 0},
		{"reversed fields", "run: echo " + candidateTitle + "\n  shell: bash", "", "", 0},
		{"intervening comment", "shell: bash\n  # comment\n  run: echo " + candidateTitle, "", "", 0},
		{"fallback expression", "shell: bash\n  run: echo ${{ github.event.pull_request.title || '' }}", "", "", 0},
		{"case sensitive", "shell: bash\n  run: echo ${{ github.event.pull_request.Title }}", "", "", 0},
		{"comment false positive", "# shell: bash\n  run: echo " + candidateTitle, "one_line", "title", 1},
		{"cross step false positive", "shell: bash\n  run: |\n    echo literal\n- env:\n    BODY: " + candidateBody, "multiline", "body", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hit := candidateTestHit(".github/workflows/ci.yml", testBlob)
			got := filterCandidates(hit, []byte(tc.data))
			if tc.line == 0 {
				if len(got) != 0 {
					t.Fatalf("unexpected candidates: %+v", got)
				}
				return
			}
			want := Candidate{"owner/project", hit.Path, testBlob, tc.filter, tc.field, tc.line}
			if len(got) != 1 || got[0] != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
	data := "shell: bash\n  run: echo " + candidateTitle + candidateBody + candidateTitle + "\n" +
		"shell: bash\n  run: |\n    echo " + candidateTitle + candidateBody
	got := filterCandidates(candidateTestHit(".github/workflows/both.yaml", testOther), []byte(data))
	if len(got) != 4 || got[0].Filter != "one_line" || got[0].PRField != "title" || got[1].PRField != "body" ||
		got[2].Filter != "multiline" || got[2].PRField != "title" || got[3].PRField != "body" || got[2].MatchLine != 3 {
		t.Fatalf("repeated expressions or both fields/layouts grouped incorrectly: %+v", got)
	}
}

func candidatePage(t *testing.T, hits ...candidateHit) apiReply {
	t.Helper()
	if hits == nil {
		hits = []candidateHit{}
	}
	return jsonReply(t, map[string]any{"total_count": len(hits), "incomplete_results": false, "items": hits})
}

// No sockets, environment credentials, or real delays are used by discovery tests.
func fakeCandidateClient(t *testing.T, pages []apiReply, blobs map[string]apiReply) (*http.Client, *[]string) {
	t.Helper()
	requests := []string{}
	queries := []string{
		`"github.event.pull_request.title" in:file path:.github/workflows extension:yml`,
		`"github.event.pull_request.title" in:file path:.github/workflows extension:yaml`,
		`"github.event.pull_request.body" in:file path:.github/workflows extension:yml`,
		`"github.event.pull_request.body" in:file path:.github/workflows extension:yaml`,
	}
	searches := 0
	client := newGitHubClient()
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "api.github.com" || r.URL.User != nil || r.Header.Get("Cookie") != "" {
			t.Fatalf("unexpected request destination: %s %s", r.Method, r.URL)
		}
		if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > 2*time.Minute || client.Timeout != 15*time.Second {
			t.Fatal("discovery is missing its request or overall timeout")
		}
		requests = append(requests, r.URL.RequestURI())
		var reply apiReply
		accept := "application/vnd.github.raw+json"
		if r.URL.Path == "/search/code" {
			q := r.URL.Query()
			if searches >= len(pages) || searches >= len(queries) || q.Get("q") != queries[searches] ||
				q.Get("per_page") != "25" || q.Get("page") != "1" || len(q) != 3 || r.Header.Get("Authorization") != "Bearer "+candidateTestToken {
				t.Fatalf("unexpected search request: %s", r.URL)
			}
			reply = pages[searches]
			searches++
			accept = "application/vnd.github+json"
		} else {
			if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
				t.Fatal("blob request included credentials or query parameters")
			}
			var ok bool
			reply, ok = blobs[r.URL.Path]
			if !ok {
				t.Fatalf("unexpected blob request: %s", r.URL)
			}
		}
		if r.Header.Get("Accept") != accept || r.Header.Get("X-GitHub-Api-Version") == "" || r.Header.Get("User-Agent") == "" {
			t.Fatal("missing API headers")
		}
		return &http.Response{StatusCode: reply.status, Header: reply.header, Body: io.NopCloser(strings.NewReader(reply.body)), Request: r}, nil
	})
	return client, &requests
}

func noCandidatePause(context.Context) error { return nil }

func TestCandidateDiscoveryDeduplicationAndAttribution(t *testing.T) {
	a := candidateTestHit(".github/workflows/a.yml", testBlob)
	alias := a
	alias.Repository.FullName = "OWNER/PROJECT"
	b := candidateTestHit(".github/workflows/b.yaml", testBlob)
	changed := candidateTestHit(a.Path, testOther)
	client, requests := fakeCandidateClient(t, []apiReply{
		candidatePage(t, a, a), candidatePage(t, b), candidatePage(t, alias, changed), candidatePage(t),
	}, map[string]apiReply{
		testPrefix + "/git/blobs/" + testBlob:  {status: 200, body: "# heading\n" + candidateScript},
		testPrefix + "/git/blobs/" + testOther: {status: 200, body: strings.ReplaceAll(candidateScript, candidateTitle, candidateBody)},
	})
	pauses := 0
	report := findCandidates(context.Background(), candidateTestToken, client, func(context.Context) error { pauses++; return nil }, nil)
	if !report.Complete || report.ExitCode != 0 || len(report.Issues) != 0 || report.DownloadsAttempted != 3 || report.FilesFiltered != 3 ||
		len(report.Candidates) != 3 || len(*requests) != 7 || pauses != 3 {
		t.Fatalf("unexpected discovery result: %+v, requests=%v, pauses=%d", report, *requests, pauses)
	}
	want := []Candidate{
		{"owner/project", a.Path, testBlob, "one_line", "title", 2},
		{"owner/project", b.Path, testBlob, "one_line", "title", 2},
		{"owner/project", a.Path, testOther, "one_line", "body", 1},
	}
	if !reflect.DeepEqual(report.Candidates, want) {
		t.Fatalf("lost source attribution: %+v", report.Candidates)
	}
	for _, query := range report.Queries {
		if !query.Completed {
			t.Fatalf("completed query marked pending: %+v", query)
		}
	}
}

func TestCandidateDiscoveryEmptyAndNoMatch(t *testing.T) {
	for _, withFile := range []bool{false, true} {
		first := candidatePage(t)
		if withFile {
			first = candidatePage(t, candidateTestHit(".github/workflows/ci.yml", testBlob))
		}
		client, _ := fakeCandidateClient(t, []apiReply{first, candidatePage(t), candidatePage(t), candidatePage(t)}, map[string]apiReply{
			testPrefix + "/git/blobs/" + testBlob: {status: 200, body: "shell: bash\n  run: echo literal"},
		})
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
		if !report.Complete || report.ExitCode != 0 || len(report.Candidates) != 0 || report.Candidates == nil || report.Issues == nil {
			t.Fatalf("empty result should complete with JSON arrays: %+v", report)
		}
	}
}

func TestCandidateDiscoveryRejectsUntrustedHits(t *testing.T) {
	for _, change := range []func(*candidateHit){
		func(h *candidateHit) { h.Repository.Private = nil },
		func(h *candidateHit) { *h.Repository.Private = true },
		func(h *candidateHit) { h.Repository.FullName = "owner/.." },
		func(h *candidateHit) { h.Repository.FullName = "owner/repo?query=1" },
		func(h *candidateHit) { h.Repository.FullName = "https://example.com/owner/repo" },
		func(h *candidateHit) { h.SHA = "../escape" },
		func(h *candidateHit) { h.Path = ".github/workflows/nested/ci.yml" },
		func(h *candidateHit) { h.Path = "elsewhere/.github/workflows/ci.yml" },
		func(h *candidateHit) { h.Path = ".github/workflows/../ci.yml" },
		func(h *candidateHit) { h.Path = ".github/workflows/NUL.yml" },
		func(h *candidateHit) { h.Path = ".github/workflows/x\\ci.yml" },
		func(h *candidateHit) { h.Path = ".github/workflows/ci.yml\n" },
		func(h *candidateHit) { h.Path = ".github/workflows/ci.txt" },
	} {
		hit := candidateTestHit(".github/workflows/ci.yml", testBlob)
		change(&hit)
		client, requests := fakeCandidateClient(t, []apiReply{candidatePage(t, hit)}, nil)
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
		if report.Complete || report.ExitCode != 2 || report.DownloadsAttempted != 0 || len(*requests) != 1 || len(report.Issues) != 1 || report.Issues[0].Code != "invalid_search_hit" {
			t.Fatalf("invalid result accepted: %+v", report)
		}
	}
}

func TestCandidateDiscoveryReportsSamplingLimits(t *testing.T) {
	for _, count := range []int{20, 21} {
		var hits []candidateHit
		for i := 0; i < count; i++ {
			hits = append(hits, candidateTestHit(fmt.Sprintf(".github/workflows/%d.yml", i), testBlob))
		}
		client, requests := fakeCandidateClient(t, []apiReply{candidatePage(t, hits...), candidatePage(t), candidatePage(t), candidatePage(t)}, map[string]apiReply{
			testPrefix + "/git/blobs/" + testBlob: {status: 200, body: candidateScript},
		})
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
		if report.DownloadsAttempted != 20 || report.FilesFiltered != 20 || len(*requests) != 24 || len(report.Candidates) != 20 ||
			report.Complete != (count == 20) || (count == 21 && (report.ExitCode != 2 || len(report.Issues) != 1 || report.Issues[0].Code != "download_sample_limit")) {
			t.Fatalf("download limit hidden or exceeded: %+v", report)
		}
	}
	hit := candidateTestHit(".github/workflows/ci.yml", testBlob)
	for _, tc := range []struct {
		total      int
		incomplete bool
		code       string
	}{
		{26, false, "search_sample_limit"}, {1, true, "incomplete_search"},
	} {
		page := jsonReply(t, map[string]any{"total_count": tc.total, "incomplete_results": tc.incomplete, "items": []candidateHit{hit}})
		client, _ := fakeCandidateClient(t, []apiReply{page, candidatePage(t), candidatePage(t), candidatePage(t)}, map[string]apiReply{
			testPrefix + "/git/blobs/" + testBlob: {status: 200, body: candidateScript},
		})
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
		if report.Complete || report.ExitCode != 2 || len(report.Candidates) != 1 || len(report.Issues) != 1 || report.Issues[0].Code != tc.code {
			t.Fatalf("partial search lost its candidates or limit: %+v", report)
		}
	}
}

func TestCandidateDiscoverySearchFailures(t *testing.T) {
	hits := make([]candidateHit, 26)
	for i := range hits {
		hits[i] = candidateTestHit(".github/workflows/ci.yml", testBlob)
	}
	for _, tc := range []struct {
		name    string
		reply   apiReply
		message string
	}{
		{"rate limited", apiReply{status: 429, body: candidateTestToken}, "rate limit"},
		{"secondary rate limit", apiReply{status: 403, header: http.Header{"Retry-After": {"60"}}}, "rate limit"},
		{"unauthorized", apiReply{status: 401, body: candidateTestToken, header: http.Header{"Retry-After": {"60"}}}, "GitHub returned HTTP 401"},
		{"forbidden without rate limit signal", apiReply{status: 403, header: http.Header{"X-Ratelimit-Remaining": {"10"}}}, "GitHub returned HTTP 403"},
		{"server error with retry delay", apiReply{status: 503, header: http.Header{"Retry-After": {"60"}}}, "GitHub returned HTTP 503"},
		{"redirect", apiReply{status: 302, header: http.Header{"Location": {"https://example.com/steal-token"}}}, "redirect"},
		{"invalid JSON", apiReply{status: 200, body: "{" + candidateTestToken}, "invalid search metadata"},
		{"missing fields", apiReply{status: 200, body: `{}`}, "invalid search metadata"},
		{"missing completeness marker", apiReply{status: 200, body: `{"total_count":0,"items":[]}`}, "invalid search metadata"},
		{"null items", apiReply{status: 200, body: `{"total_count":0,"incomplete_results":false,"items":null}`}, "invalid search metadata"},
		{"negative total", apiReply{status: 200, body: `{"total_count":-1,"incomplete_results":false,"items":[]}`}, "invalid search metadata"},
		{"too many results", candidatePage(t, hits...), "invalid search metadata"},
		{"inconsistent count", jsonReply(t, map[string]any{"total_count": 0, "incomplete_results": false, "items": hits[:1]}), "invalid search metadata"},
		{"oversized", apiReply{status: 200, body: strings.Repeat(" ", maxGitHubMetadataBytes+1)}, "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, requests := fakeCandidateClient(t, []apiReply{tc.reply}, nil)
			report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
			encoded, _ := json.Marshal(report)
			if report.Complete || report.ExitCode != 2 || len(*requests) != 1 || len(report.Issues) != 1 ||
				!strings.Contains(report.Issues[0].Message, tc.message) || strings.Contains(string(encoded), candidateTestToken) || report.Queries[0].Completed {
				t.Fatalf("search failure hidden or leaked data: %+v", report)
			}
		})
	}
}

func TestCandidateDiscoveryRateLimitDiagnostics(t *testing.T) {
	const unknown = "; retry time unknown"
	const advice = "; respect the reported reset/retry time before rerunning"
	type rateLimitCase struct {
		name   string
		status int
		header http.Header
		want   string
	}
	cases := []rateLimitCase{
		{"exhausted allowance", 403, http.Header{
			"X-Ratelimit-Resource": {"code_search"}, "X-Ratelimit-Limit": {"10"},
			"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1700000000"},
		}, "; resource=code_search; limit=10; remaining=0; reset=2023-11-14T22:13:20Z" + advice},
		{"retry delay", 403, http.Header{"Retry-After": {"60"}}, "; retry_after=60 seconds" + advice},
		{"combined headers", 429, http.Header{
			"X-Ratelimit-Resource": {"search"}, "X-Ratelimit-Limit": {"00010"},
			"X-Ratelimit-Remaining": {"00007"}, "X-Ratelimit-Reset": {"1700000000"}, "Retry-After": {"0060"},
		}, "; resource=search; limit=10; remaining=7; reset=2023-11-14T22:13:20Z; retry_after=60 seconds" + advice},
		{"no headers", 429, nil, unknown},
		{"zero bounds", 429, http.Header{
			"X-Ratelimit-Resource": {"core"}, "X-Ratelimit-Limit": {"0"},
			"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"0"}, "Retry-After": {"0"},
		}, "; resource=core; limit=0; remaining=0; reset=1970-01-01T00:00:00Z; retry_after=0 seconds" + advice},
		{"maximum bounds", 429, http.Header{
			"X-Ratelimit-Limit": {"2147483647"}, "X-Ratelimit-Remaining": {"2147483647"},
			"X-Ratelimit-Reset": {"253402300799"}, "Retry-After": {"2147483647"},
		}, "; limit=2147483647; remaining=2147483647; reset=9999-12-31T23:59:59Z; retry_after=2147483647 seconds" + advice},
		{"outside bounds", 429, http.Header{
			"X-Ratelimit-Limit": {"2147483648"}, "X-Ratelimit-Remaining": {"2147483648"},
			"X-Ratelimit-Reset": {"253402300800"}, "Retry-After": {"2147483648"},
		}, unknown},
		{"unknown resource", 429, http.Header{"X-Ratelimit-Resource": {candidateTestToken}}, "; resource=unknown" + unknown},
		{"oversized resource", 429, http.Header{"X-Ratelimit-Resource": {strings.Repeat("x", 4096)}}, "; resource=unknown" + unknown},
		{"duplicate resource", 429, http.Header{"X-Ratelimit-Resource": {"core", candidateTestToken}}, "; resource=unknown" + unknown},
		{"valid reset with invalid delay", 429, http.Header{
			"X-Ratelimit-Reset": {"1700000000"}, "Retry-After": {candidateTestToken},
		}, "; reset=2023-11-14T22:13:20Z" + advice},
		{"invalid reset with valid delay", 429, http.Header{
			"X-Ratelimit-Reset": {candidateTestToken}, "Retry-After": {"60"},
		}, "; retry_after=60 seconds" + advice},
		{"malformed delay on forbidden response", 403, http.Header{"Retry-After": {candidateTestToken}}, unknown},
	}
	for _, values := range [][]string{
		{""}, {"-1"}, {"+1"}, {"1.5"}, {"1e3"}, {"0x10"}, {" 60 "}, {"60,120"},
		{"\u0666\u0660"}, {"60\r\n"}, {candidateTestToken}, {strings.Repeat("9", 4096)},
		{"0000000000000"}, {"9223372036854775808"}, {"60", "120"},
	} {
		header := http.Header{}
		for _, name := range []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "Retry-After"} {
			header[http.CanonicalHeaderKey(name)] = values
		}
		cases = append(cases, rateLimitCase{fmt.Sprintf("invalid numeric headers %d", len(cases)), 429, header, unknown})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, requests := fakeCandidateClient(t, []apiReply{{status: tc.status, header: tc.header, body: candidateTestToken}}, nil)
			report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
			if report.Complete || report.ExitCode != 2 || len(*requests) != 1 || len(report.Issues) != 1 ||
				len(report.Candidates) != 0 || report.DownloadsAttempted != 0 || report.FilesFiltered != 0 || len(report.Queries) != 4 {
				t.Fatalf("rate limit did not stop discovery: %+v; requests=%v", report, *requests)
			}
			for _, query := range report.Queries {
				if query.Completed {
					t.Fatalf("failed or skipped query marked complete: %+v", query)
				}
			}
			want := fmt.Sprintf("GitHub API rate limit reached (HTTP %d)", tc.status) + tc.want + "; remaining work skipped"
			if report.Issues[0].Code != "search_failed" || report.Issues[0].Message != want || report.Issues[0].Query != report.Queries[0].Query || len(want) > 512 {
				t.Fatalf("unexpected rate limit issue: %+v; want %q", report.Issues[0], want)
			}
			encoded, err := json.Marshal(report)
			if err != nil || strings.Contains(string(encoded), candidateTestToken) {
				t.Fatal("report encoding failed or leaked synthetic sensitive text")
			}
		})
	}
}

func TestCandidateDiscoveryRetainsPartialResults(t *testing.T) {
	a := candidateTestHit(".github/workflows/a.yml", testBlob)
	b := candidateTestHit(".github/workflows/b.yml", testOther)
	rateLimit := apiReply{status: 403, header: http.Header{
		"X-Ratelimit-Resource": {"core"}, "X-Ratelimit-Remaining": {"0"}, "Retry-After": {"60"},
	}}
	for _, failure := range []apiReply{{status: 500}, rateLimit,
		{status: 200, body: strings.Repeat("x", MaxFileBytes+1)}, {status: 302, header: http.Header{"Location": {"https://example.com/blob"}}}} {
		client, requests := fakeCandidateClient(t, []apiReply{candidatePage(t, a, b, candidateTestHit(".github/workflows/c.yml", testCommit))}, map[string]apiReply{
			testPrefix + "/git/blobs/" + testBlob:  {status: 200, body: candidateScript},
			testPrefix + "/git/blobs/" + testOther: failure,
		})
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
		if report.Complete || report.ExitCode != 2 || len(report.Candidates) != 1 || report.FilesFiltered != 1 || report.DownloadsAttempted != 2 || len(*requests) != 3 ||
			len(report.Issues) != 1 || report.Issues[0].Code != "download_failed" || report.Issues[0].Path != b.Path || report.Issues[0].BlobSHA != b.SHA {
			t.Fatalf("partial failure hidden or downloads continued: %+v", report)
		}
		if failure.status == 403 && !strings.Contains(report.Issues[0].Message, "resource=core; remaining=0; retry_after=60 seconds") {
			t.Fatalf("blob rate limit diagnostics missing: %+v", report.Issues[0])
		}
	}
	for _, failure := range []apiReply{{status: 503}, rateLimit} {
		client, requests := fakeCandidateClient(t, []apiReply{candidatePage(t, a), failure}, map[string]apiReply{
			testPrefix + "/git/blobs/" + testBlob: {status: 200, body: candidateScript},
		})
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
		if report.Complete || report.ExitCode != 2 || len(report.Candidates) != 1 || len(*requests) != 3 ||
			report.DownloadsAttempted != 1 || report.FilesFiltered != 1 || len(report.Issues) != 1 ||
			report.Issues[0].Code != "search_failed" || report.Issues[0].Query != report.Queries[1].Query ||
			report.Queries[1].Completed || report.Queries[2].Completed || report.Queries[3].Completed || !report.Queries[0].Completed {
			t.Fatalf("later search failure lost earlier candidates or continued requests: %+v", report)
		}
		if failure.status == 403 && !strings.Contains(report.Issues[0].Message, "resource=core; remaining=0; retry_after=60 seconds") {
			t.Fatalf("later search rate limit diagnostics missing: %+v", report.Issues[0])
		}
	}
}

func TestCandidateDiscoveryExactByteLimits(t *testing.T) {
	hit := candidateTestHit(".github/workflows/ci.yml", testBlob)
	page := candidatePage(t, hit)
	page.body += strings.Repeat(" ", maxGitHubMetadataBytes-len(page.body))
	client, _ := fakeCandidateClient(t, []apiReply{page, candidatePage(t), candidatePage(t), candidatePage(t)}, map[string]apiReply{
		testPrefix + "/git/blobs/" + testBlob: {status: 200, body: candidateScript + strings.Repeat(" ", MaxFileBytes-len(candidateScript))},
	})
	report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
	if !report.Complete || report.ExitCode != 0 || len(report.Candidates) != 1 {
		t.Fatalf("exact byte limit rejected: %+v", report)
	}
}

func TestCandidateDiscoveryCancellationAndTransportFailures(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if canceled {
			cancel()
		}
		client := newGitHubClient()
		client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New(candidateTestToken) })
		report := findCandidates(ctx, candidateTestToken, client, noCandidatePause, nil)
		cancel()
		if report.Complete || report.ExitCode != 2 || len(report.Issues) != 1 || strings.Contains(report.Issues[0].Message, candidateTestToken) {
			t.Fatalf("network error not handled: %+v", report)
		}
	}
	client, requests := fakeCandidateClient(t, []apiReply{candidatePage(t)}, nil)
	report := findCandidates(context.Background(), candidateTestToken, client, func(context.Context) error { return context.DeadlineExceeded }, nil)
	if report.ExitCode != 2 || len(*requests) != 1 || report.Issues[0].Code != "search_canceled" {
		t.Fatalf("cancellation during pacing ignored: %+v", report)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitCandidateSearch(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pacing ignored context: %v", err)
	}
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: brokenBody{}, Request: r}, nil
	})
	report = findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, nil)
	if report.ExitCode != 2 || !strings.Contains(report.Issues[0].Message, "cannot read") {
		t.Fatalf("broken response body ignored: %+v", report)
	}
}

func TestCandidateDiscoveryInvalidTokenMakesNoRequests(t *testing.T) {
	client, requests := fakeCandidateClient(t, nil, nil)
	for _, token := range []string{"", "bad\r\nheader", "bad token", strings.Repeat("x", 4097)} {
		report := findCandidates(context.Background(), token, client, noCandidatePause, nil)
		if report.ExitCode != 2 || report.Complete || len(*requests) != 0 || report.Issues[0].Code != "invalid_token" {
			t.Fatalf("invalid token accepted: %+v", report)
		}
	}
}

func TestCandidateCLI(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if code := RunCandidates(nil, "", io.Discard, io.Discard); code != 2 {
			t.Fatalf("missing token exit %d", code)
		}
		data, err := os.ReadFile("candidates.json")
		if err != nil {
			t.Fatal(err)
		}
		var report CandidateReport
		if err := json.Unmarshal(data, &report); err != nil || report.Complete || report.ExitCode != 2 ||
			len(report.Queries) != 4 || len(report.Issues) != 1 || report.Issues[0].Code != "invalid_token" {
			t.Fatalf("missing token did not produce an incomplete JSON report: %s (%v)", data, err)
		}
	})
	for _, code := range []int{0, 2} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			t.Chdir(t.TempDir())
			var stdout, stderr bytes.Buffer
			want := CandidateReport{Complete: code == 0, ExitCode: code, Candidates: []Candidate{{Repository: "owner/project"}}}
			find := func(_ context.Context, token string, debug *candidateDebug) CandidateReport {
				if token != candidateTestToken || debug != nil {
					t.Fatal("job token not passed to discovery")
				}
				return want
			}
			if got := runCandidates(nil, candidateTestToken, &stdout, &stderr, find); got != code {
				t.Fatalf("exit %d, want %d", got, code)
			}
			data, err := os.ReadFile("candidates.json")
			if err != nil {
				t.Fatal(err)
			}
			var report CandidateReport
			if err := json.Unmarshal(data, &report); err != nil || !reflect.DeepEqual(report, want) {
				t.Fatalf("report lost results: %s (%v)", data, err)
			}
			find = func(context.Context, string, *candidateDebug) CandidateReport {
				t.Fatal("existing file triggered discovery")
				return CandidateReport{}
			}
			if got := runCandidates(nil, candidateTestToken, &stdout, &stderr, find); got != 2 {
				t.Fatalf("existing file accepted: %d", got)
			}
			after, err := os.ReadFile("candidates.json")
			if err != nil || !bytes.Equal(data, after) {
				t.Fatal("existing report was changed")
			}
		})
	}
	t.Run("arguments", func(t *testing.T) {
		t.Chdir(t.TempDir())
		for _, args := range [][]string{{"--help"}, {"-h"}, {"--repo", "owner/project"}, {"extra"}, {"--debug", "--debug"}, {"--debug=true"}} {
			find := func(context.Context, string, *candidateDebug) CandidateReport {
				t.Fatal("arguments triggered discovery")
				return CandidateReport{}
			}
			want := 2
			if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
				want = 0
			}
			var help bytes.Buffer
			if got := runCandidates(args, "", &help, io.Discard, find); got != want {
				t.Fatalf("arguments %v: exit %d", args, got)
			}
			if want == 0 && !strings.Contains(help.String(), "--debug") {
				t.Fatal("help does not describe debug mode")
			}
		}
		if _, err := os.Stat("candidates.json"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("arguments created an output file")
		}
	})
}

func TestCandidateDebugCLI(t *testing.T) {
	message := "Temporary limit: " + candidateTestToken + "\n::error::text, not a command\r\n"
	reply := jsonReply(t, map[string]string{"message": message, "unrelated": "private-body-field"})
	reply.status = 429
	reply.body = strings.ReplaceAll(reply.body, "synthetic", `\u0073ynthetic`)
	reply.header = http.Header{
		"X-Github-Request-Id": {"ABcd:1234-5678"}, "Date": {"Sun, 20 Sep 2026 20:42:14 GMT"},
		"X-Ratelimit-Resource": {"code_search"}, "X-Ratelimit-Limit": {"10"}, "X-Ratelimit-Remaining": {"10"},
		"X-Ratelimit-Reset": {"1790023394"}, "Retry-After": {"4"}, "Set-Cookie": {"private-cookie"},
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			t.Chdir(t.TempDir())
			var stdout, stderr bytes.Buffer
			var args []string
			if enabled {
				args = []string{"--debug"}
			}
			client, requests := fakeCandidateClient(t, []apiReply{reply}, nil)
			find := func(ctx context.Context, token string, debug *candidateDebug) CandidateReport {
				if (debug != nil) != enabled {
					t.Fatal("debug option not passed to discovery")
				}
				return findCandidates(ctx, token, client, noCandidatePause, debug)
			}
			if code := runCandidates(args, candidateTestToken, &stdout, &stderr, find); code != 2 || len(*requests) != 1 {
				t.Fatalf("debug changed exit or request count: %d, %v", code, *requests)
			}
			data, err := os.ReadFile("candidates.json")
			var report CandidateReport
			if err != nil || json.Unmarshal(data, &report) != nil || report.ExitCode != 2 || report.Complete ||
				len(report.Issues) != 1 || report.Issues[0].Code != "search_failed" || report.Queries[0].Completed {
				t.Fatalf("debug changed the failure report: %s (%v)", data, err)
			}
			if !enabled {
				if stderr.Len() != 0 {
					t.Fatal("debug logging enabled by default")
				}
				return
			}
			var record candidateDebugRecord
			if json.Unmarshal(stderr.Bytes(), &record) != nil || strings.Count(stderr.String(), "\n") != 1 {
				t.Fatal("response text injected a log line or invalid JSON")
			}
			if record.Event != "http_request" || record.Status != 429 || record.ElapsedMS < 0 ||
				record.Endpoint != "https://api.github.com/search/code" || record.Query != report.Queries[0].Query ||
				record.RequestID != "ABcd:1234-5678" || record.ServerDate != "2026-09-20T20:42:14Z" ||
				record.GitHubMessage != strings.ReplaceAll(message, candidateTestToken, "[REDACTED]") ||
				record.MessageUnavailable != "" || record.MessageTruncated ||
				!strings.Contains(record.RateLimit, "resource=code_search; limit=10; remaining=10;") ||
				!strings.Contains(record.RateLimit, "retry_after=4 seconds") {
				t.Fatalf("missing or incorrect debug metadata: %+v", record)
			}
			if _, err := time.Parse(time.RFC3339Nano, record.Timestamp); err != nil || !strings.HasSuffix(record.Timestamp, "Z") {
				t.Fatal("debug timestamp is not UTC")
			}
			for _, secret := range []string{candidateTestToken, "private-cookie", "private-body-field", "Authorization", "Set-Cookie"} {
				if strings.Contains(stderr.String(), secret) {
					t.Fatal("diagnostic leaked sensitive or unselected data")
				}
			}
		})
	}
}

func TestCandidateDebugPreservesDiscovery(t *testing.T) {
	a := candidateTestHit(".github/workflows/a.yml", testBlob)
	b := candidateTestHit(".github/workflows/b.yml", testOther)
	for _, blobFailure := range []bool{false, true} {
		var baseline CandidateReport
		var baselineRequests []string
		for _, enabled := range []bool{false, true} {
			second := apiReply{status: 200, body: "workflow-content-must-not-be-logged"}
			if blobFailure {
				second = apiReply{status: 403, body: `{"message":"` + candidateTestToken + `"}`, header: http.Header{
					"X-Ratelimit-Remaining": {"0"}, "X-Github-Request-Id": {candidateTestToken},
				}}
			}
			client, requests := fakeCandidateClient(t, []apiReply{candidatePage(t, a, b), candidatePage(t), candidatePage(t), candidatePage(t)}, map[string]apiReply{
				testPrefix + "/git/blobs/" + testBlob:  {status: 200, body: candidateScript},
				testPrefix + "/git/blobs/" + testOther: second,
			})
			var output bytes.Buffer
			var debug *candidateDebug
			if enabled {
				debug = &candidateDebug{out: &output, token: candidateTestToken}
			}
			report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, debug)
			if !enabled {
				baseline, baselineRequests = report, *requests
				continue
			}
			if !reflect.DeepEqual(report, baseline) || !reflect.DeepEqual(*requests, baselineRequests) || len(report.Candidates) != 1 {
				t.Fatalf("debug changed requests or results: %+v", report)
			}
			if strings.Contains(output.String(), candidateTestToken) || strings.Contains(output.String(), candidateScript) ||
				strings.Contains(output.String(), "workflow-content-must-not-be-logged") {
				t.Fatal("debug exposed a token or successful response body")
			}
			lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
			if len(lines) != len(*requests) {
				t.Fatal("missing per-request diagnostic records")
			}
			for _, line := range lines {
				var record candidateDebugRecord
				if json.Unmarshal(line, &record) != nil || (record.Status == 200 && record.GitHubMessage != "") {
					t.Fatal("invalid record or successful body logged")
				}
				if record.Status == 403 && (record.GitHubMessage != "[REDACTED]" || record.RequestID != "[REDACTED]") {
					t.Fatal("active token was not redacted on the anonymous blob path")
				}
			}
		}
	}
}

type candidateTrackedBody struct {
	io.Reader
	read   int
	closed bool
}

func (b *candidateTrackedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}

func (b *candidateTrackedBody) Close() error { b.closed = true; return nil }

func TestCandidateDebugErrorBodyBounds(t *testing.T) {
	for _, tc := range []struct {
		name, body, unavailable string
	}{
		{"malformed", "{" + candidateTestToken, "invalid_json"},
		{"missing", `{}`, "missing_message"},
		{"empty message", `{"message":""}`, "missing_message"},
		{"wrong type", `{"message":123}`, "invalid_json"},
		{"invalid UTF8", "{\"message\":\"\xff\"}", "invalid_json"},
		{"oversized", strings.Repeat("x", candidateDebugBodyLimit+100), "body_too_large"},
		{"exact limit", `{"message":"ok"}` + strings.Repeat(" ", candidateDebugBodyLimit-len(`{"message":"ok"}`)), ""},
	} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", tc.name, enabled), func(t *testing.T) {
				body := &candidateTrackedBody{Reader: strings.NewReader(tc.body)}
				client := newGitHubClient()
				client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 429, Body: body, Request: r}, nil
				})
				var output bytes.Buffer
				var debug *candidateDebug
				if enabled {
					debug = &candidateDebug{out: &output, token: candidateTestToken}
				}
				report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause, debug)
				if report.ExitCode != 2 || report.Complete || len(report.Issues) != 1 ||
					!strings.Contains(report.Issues[0].Message, "HTTP 429") || !body.closed || body.read > candidateDebugBodyLimit+1 {
					t.Fatal("debug replaced the HTTP failure, leaked the body, or exceeded its read limit")
				}
				if !enabled {
					if body.read != 0 || output.Len() != 0 {
						t.Fatal("disabled mode consumed the error response")
					}
					return
				}
				var record candidateDebugRecord
				if json.Unmarshal(output.Bytes(), &record) != nil || record.MessageUnavailable != tc.unavailable ||
					(tc.unavailable != "" && record.GitHubMessage != "") || (tc.unavailable == "" && record.GitHubMessage != "ok") {
					t.Fatalf("incorrect unavailable-message diagnostic: %+v", record)
				}
			})
		}
	}
}

func TestCandidateDebugRedactsBeforeTruncation(t *testing.T) {
	d := candidateDebug{token: candidateTestToken}
	for _, message := range []string{strings.Repeat("x", 1020) + candidateTestToken + " tail", strings.Repeat("界", 1025)} {
		data, _ := json.Marshal(map[string]string{"message": message})
		text, unavailable, truncated := d.message(bytes.NewReader(data))
		if unavailable != "" || !truncated || utf8.RuneCountInString(text) != 1024 || !utf8.ValidString(text) ||
			strings.Contains(text, "synt") || !strings.HasSuffix(text, "…") {
			t.Fatal("message truncation exposed a token prefix or broke its character bound")
		}
	}
}

func TestCandidateDebugRequestIDs(t *testing.T) {
	for _, values := range [][]string{nil, {""}, {"ok:123-ABC"}, {strings.Repeat("a", 128)},
		{strings.Repeat("a", 129)}, {"a_b"}, {"a\nb"}, {"界"}, {"a", "b"}} {
		want := ""
		if len(values) == 1 && (values[0] == "ok:123-ABC" || values[0] == strings.Repeat("a", 128)) {
			want = values[0]
		}
		if got := candidateRequestID(http.Header{"X-Github-Request-Id": values}); got != want {
			t.Fatal("unsafe or ambiguous request ID accepted")
		}
	}
}

func TestCandidateDebugOutputBudget(t *testing.T) {
	var output bytes.Buffer
	d := candidateDebug{out: &output}
	for i := 0; i < 100; i++ {
		d.write(candidateDebugRecord{Event: "http_request", GitHubMessage: strings.Repeat("x", 1024)})
	}
	if output.Len() > candidateDebugLimit || !d.stopped || !strings.HasSuffix(output.String(), "{\"event\":\"debug_truncated\"}\n") {
		t.Fatal("debug budget exceeded or truncation hidden")
	}
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'}) {
		if !json.Valid(line) {
			t.Fatal("output limit split a JSON record")
		}
	}
	before := output.Len()
	d.write(candidateDebugRecord{Event: "after_limit"})
	if output.Len() != before {
		t.Fatal("logging continued after truncation")
	}
}

func TestCandidateDebugReadAndTransportFailures(t *testing.T) {
	for _, transportFailure := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			ctx, cancel := context.WithCancel(context.Background())
			client := newGitHubClient()
			body := &candidateTrackedBody{Reader: io.MultiReader(strings.NewReader(`{"message":"`+candidateTestToken), brokenBody{})}
			client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if canceled {
					cancel()
				}
				if transportFailure {
					return nil, errors.New("private transport details: " + candidateTestToken)
				}
				return &http.Response{StatusCode: 429, Body: body, Request: r, Header: http.Header{
					"Date": {candidateTestToken}, "X-Github-Request-Id": {"invalid\n::error::"},
				}}, nil
			})
			var output bytes.Buffer
			debug := &candidateDebug{out: &output, token: candidateTestToken}
			report := findCandidates(ctx, candidateTestToken, client, noCandidatePause, debug)
			cancel()
			var record candidateDebugRecord
			if report.ExitCode != 2 || report.Complete || len(report.Issues) != 1 || json.Unmarshal(output.Bytes(), &record) != nil ||
				strings.Contains(output.String(), candidateTestToken) || strings.Contains(output.String(), "private transport") ||
				record.RequestID != "" || record.ServerDate != "" {
				t.Fatal("debug hid the failure or exposed invalid metadata/raw errors")
			}
			if transportFailure {
				want := "request_failed_or_timed_out"
				if canceled {
					want = "canceled_or_deadline"
				}
				if record.Error != want || record.Status != 0 {
					t.Fatalf("wrong transport category: %+v", record)
				}
			} else if record.Status != 429 || record.MessageUnavailable != "read_failed" || record.GitHubMessage != "" ||
				!strings.Contains(report.Issues[0].Message, "HTTP 429") || !body.closed {
				t.Fatal("debug body-read failure replaced the HTTP error or left its body open")
			}
		}
	}
}

type candidateShortWriter struct{}

func (candidateShortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestCandidateDebugWriterFailurePreservesReport(t *testing.T) {
	for _, code := range []int{0, 2} {
		for _, writer := range []io.Writer{failingWriter{}, candidateShortWriter{}} {
			t.Run(fmt.Sprintf("%d/%T", code, writer), func(t *testing.T) {
				t.Chdir(t.TempDir())
				var stdout bytes.Buffer
				want := CandidateReport{Complete: code == 0, ExitCode: code, Candidates: []Candidate{{Repository: "owner/project"}}}
				find := func(_ context.Context, _ string, debug *candidateDebug) CandidateReport {
					debug.write(candidateDebugRecord{Event: "http_request", Status: 429})
					if !debug.failed || !debug.stopped {
						t.Fatal("debug output continued after a failed or short write")
					}
					written := debug.written
					debug.write(candidateDebugRecord{Event: "after_failure"})
					if debug.written != written {
						t.Fatal("failed debug writer was retried")
					}
					return want
				}
				if got := runCandidates([]string{"--debug"}, candidateTestToken, &stdout, writer, find); got != code ||
					!strings.Contains(stdout.String(), "Debug diagnostics could not be fully written") {
					t.Fatal("debug writer failure changed the result or went unreported")
				}
				data, err := os.ReadFile("candidates.json")
				var report CandidateReport
				if err != nil || json.Unmarshal(data, &report) != nil || !reflect.DeepEqual(report, want) {
					t.Fatal("debug writer failure lost the report")
				}
			})
		}
	}
}
