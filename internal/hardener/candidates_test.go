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
	report := findCandidates(context.Background(), candidateTestToken, client, func(context.Context) error { pauses++; return nil })
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
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause)
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
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause)
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
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause)
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
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause)
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
		{"unauthorized", apiReply{status: 401, body: candidateTestToken}, "HTTP 401"},
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
			report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause)
			encoded, _ := json.Marshal(report)
			if report.Complete || report.ExitCode != 2 || len(*requests) != 1 || len(report.Issues) != 1 ||
				!strings.Contains(report.Issues[0].Message, tc.message) || strings.Contains(string(encoded), candidateTestToken) || report.Queries[0].Completed {
				t.Fatalf("search failure hidden or leaked data: %+v", report)
			}
		})
	}
}

func TestCandidateDiscoveryRetainsPartialResults(t *testing.T) {
	a := candidateTestHit(".github/workflows/a.yml", testBlob)
	b := candidateTestHit(".github/workflows/b.yml", testOther)
	for _, failure := range []apiReply{{status: 500}, {status: 403, header: http.Header{"X-Ratelimit-Remaining": {"0"}}},
		{status: 200, body: strings.Repeat("x", MaxFileBytes+1)}, {status: 302, header: http.Header{"Location": {"https://example.com/blob"}}}} {
		client, requests := fakeCandidateClient(t, []apiReply{candidatePage(t, a, b, candidateTestHit(".github/workflows/c.yml", testCommit))}, map[string]apiReply{
			testPrefix + "/git/blobs/" + testBlob:  {status: 200, body: candidateScript},
			testPrefix + "/git/blobs/" + testOther: failure,
		})
		report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause)
		if report.Complete || report.ExitCode != 2 || len(report.Candidates) != 1 || report.FilesFiltered != 1 || report.DownloadsAttempted != 2 || len(*requests) != 3 ||
			len(report.Issues) != 1 || report.Issues[0].Code != "download_failed" || report.Issues[0].Path != b.Path || report.Issues[0].BlobSHA != b.SHA {
			t.Fatalf("partial failure hidden or downloads continued: %+v", report)
		}
	}
	client, _ := fakeCandidateClient(t, []apiReply{candidatePage(t, a), {status: 503}}, map[string]apiReply{
		testPrefix + "/git/blobs/" + testBlob: {status: 200, body: candidateScript},
	})
	report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause)
	if report.ExitCode != 2 || len(report.Candidates) != 1 || report.Queries[1].Completed || !report.Queries[0].Completed {
		t.Fatalf("later search failure lost earlier candidates: %+v", report)
	}
}

func TestCandidateDiscoveryExactByteLimits(t *testing.T) {
	hit := candidateTestHit(".github/workflows/ci.yml", testBlob)
	page := candidatePage(t, hit)
	page.body += strings.Repeat(" ", maxGitHubMetadataBytes-len(page.body))
	client, _ := fakeCandidateClient(t, []apiReply{page, candidatePage(t), candidatePage(t), candidatePage(t)}, map[string]apiReply{
		testPrefix + "/git/blobs/" + testBlob: {status: 200, body: candidateScript + strings.Repeat(" ", MaxFileBytes-len(candidateScript))},
	})
	report := findCandidates(context.Background(), candidateTestToken, client, noCandidatePause)
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
		report := findCandidates(ctx, candidateTestToken, client, noCandidatePause)
		cancel()
		if report.Complete || report.ExitCode != 2 || len(report.Issues) != 1 || strings.Contains(report.Issues[0].Message, candidateTestToken) {
			t.Fatalf("network error not handled: %+v", report)
		}
	}
	client, requests := fakeCandidateClient(t, []apiReply{candidatePage(t)}, nil)
	report := findCandidates(context.Background(), candidateTestToken, client, func(context.Context) error { return context.DeadlineExceeded })
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
	report = findCandidates(context.Background(), candidateTestToken, client, noCandidatePause)
	if report.ExitCode != 2 || !strings.Contains(report.Issues[0].Message, "cannot read") {
		t.Fatalf("broken response body ignored: %+v", report)
	}
}

func TestCandidateDiscoveryInvalidTokenMakesNoRequests(t *testing.T) {
	client, requests := fakeCandidateClient(t, nil, nil)
	for _, token := range []string{"", "bad\r\nheader", "bad token", strings.Repeat("x", 4097)} {
		report := findCandidates(context.Background(), token, client, noCandidatePause)
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
			find := func(_ context.Context, token string) CandidateReport {
				if token != candidateTestToken {
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
			find = func(context.Context, string) CandidateReport {
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
		for _, args := range [][]string{{"--help"}, {"-h"}, {"--repo", "owner/project"}, {"extra"}} {
			find := func(context.Context, string) CandidateReport {
				t.Fatal("arguments triggered discovery")
				return CandidateReport{}
			}
			want := 2
			if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
				want = 0
			}
			if got := runCandidates(args, "", io.Discard, io.Discard, find); got != want {
				t.Fatalf("arguments %v: exit %d", args, got)
			}
		}
		if _, err := os.Stat("candidates.json"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("arguments created an output file")
		}
	})
}
