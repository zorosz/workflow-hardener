package hardener

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const (
	testCommit = "1111111111111111111111111111111111111111"
	testRoot   = "2222222222222222222222222222222222222222"
	testGitHub = "3333333333333333333333333333333333333333"
	testFlows  = "4444444444444444444444444444444444444444"
	testBlob   = "5555555555555555555555555555555555555555"
	testOther  = "6666666666666666666666666666666666666666"
	testPrefix = "/repos/owner/project"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type apiReply struct {
	status int
	body   string
	header http.Header
}

func jsonReply(t *testing.T, value any) apiReply {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return apiReply{status: 200, body: string(data)}
}

func treeReply(t *testing.T, entries ...gitTreeEntry) apiReply {
	t.Helper()
	if entries == nil {
		entries = []gitTreeEntry{}
	}
	return jsonReply(t, map[string]any{"tree": entries, "truncated": false})
}

func blobEntry(name, sha, data string) gitTreeEntry {
	size := int64(len(data))
	return gitTreeEntry{Path: name, SHA: sha, Size: &size, Type: "blob", Mode: "100644"}
}

func fakeSnapshot(t *testing.T, entries ...gitTreeEntry) map[string]apiReply {
	t.Helper()
	return map[string]apiReply{
		testPrefix:                                jsonReply(t, map[string]any{"private": false, "default_branch": "release/main"}),
		testPrefix + "/commits/release%2Fmain":    {status: 200, body: testCommit},
		testPrefix + "/git/commits/" + testCommit: jsonReply(t, map[string]any{"tree": map[string]string{"sha": testRoot}}),
		testPrefix + "/git/trees/" + testRoot:     treeReply(t, gitTreeEntry{Path: ".github", Type: "tree", Mode: "040000", SHA: testGitHub}),
		testPrefix + "/git/trees/" + testGitHub:   treeReply(t, gitTreeEntry{Path: "workflows", Type: "tree", Mode: "040000", SHA: testFlows}),
		testPrefix + "/git/trees/" + testFlows:    treeReply(t, entries...),
	}
}

// This transport returns HTTP responses without opening network connections.
func fakeGitHub(t *testing.T, replies map[string]apiReply) (*http.Client, *[]string) {
	t.Helper()
	requests := []string{}
	client := newGitHubClient()
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "api.github.com" ||
			r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatalf("unexpected request destination or credentials: %s %s", r.Method, r.URL)
		}
		path := r.URL.EscapedPath()
		requests = append(requests, path)
		wantAccept := "application/vnd.github+json"
		if strings.Contains(path, "/commits/release") {
			wantAccept = "application/vnd.github.sha"
		}
		if strings.Contains(path, "/git/blobs/") {
			wantAccept = "application/vnd.github.raw+json"
		}
		if r.Header.Get("Accept") != wantAccept || r.Header.Get("X-GitHub-Api-Version") == "" || r.Header.Get("User-Agent") == "" {
			t.Fatalf("incorrect request headers for %s", path)
		}
		reply, ok := replies[path]
		if !ok {
			t.Fatalf("unexpected request: %s", path)
		}
		return &http.Response{StatusCode: reply.status, Header: reply.header, Body: io.NopCloser(strings.NewReader(reply.body)), Request: r}, nil
	})
	return client, &requests
}

func TestRepositoryNames(t *testing.T) {
	for _, input := range []string{"owner/project", "owner/project.git", "https://github.com/owner/project", "https://github.com/owner/project.git/", " owner/project "} {
		got, err := parseRepository(input)
		if err != nil || got != "owner/project" {
			t.Fatalf("parse %q: %q %v", input, got, err)
		}
	}
	for _, input := range []string{
		"", "owner", "owner/..", "owner/.", "../repo", "owner/repo/extra", "owner/repo//", "owner/repo?x=1",
		"http://github.com/owner/repo", "https://example.com/owner/repo", "https://github.com.example.com/owner/repo",
		"https://user:password@github.com/owner/repo", "https://github.com:443/owner/repo", "https://github.com/owner/repo?",
		"https://github.com/owner/repo#", "https://github.com/owner/repo#main", "https://github.com/owner/%72epo",
		"https://github.com/owner/repo/tree/main", "owner/repo\nvalue", "owner/repo$(command)", strings.Repeat("a", 257),
	} {
		if _, err := parseRepository(input); err == nil {
			t.Errorf("accepted repository %q", input)
		}
	}
}

func TestRepositoryScanSnapshotAndLocations(t *testing.T) {
	data := "jobs:\n  inspect:\n    steps:\n      - shell: bash\n        run: echo ${{ github.event.pull_request.title }} ${{ github.event.pull_request.body }}\n"
	entries := []gitTreeEntry{blobEntry("z.yaml", testOther, simpleWorkflow), blobEntry("a.yml", testBlob, data), {Path: "notes.txt", Mode: "120000", Type: "blob", SHA: testOther}}
	replies := fakeSnapshot(t, entries...)
	replies[testPrefix+"/git/blobs/"+testBlob] = apiReply{status: 200, body: data}
	replies[testPrefix+"/git/blobs/"+testOther] = apiReply{status: 200, body: simpleWorkflow}
	client, requests := fakeGitHub(t, replies)
	report := scanRepository(context.Background(), "https://github.com/owner/project", client)
	if report.ExitCode != 1 || len(report.Issues) != 0 || len(report.Files) != 2 ||
		report.Source == nil || *report.Source != (RepositorySource{Repository: "owner/project", Commit: testCommit}) ||
		report.Totals != (Totals{Files: 2, ParsedFiles: 2, RunSteps: 2, AnalyzedSteps: 2, Findings: 2}) {
		t.Fatalf("unexpected snapshot report: %+v", report)
	}
	if report.Files[0].Path != ".github/workflows/a.yml" || report.Files[1].Path != ".github/workflows/z.yaml" {
		t.Fatalf("files are not ordered: %+v", report.Files)
	}
	for i, id := range []string{PRTitleRuleID, PRBodyRuleID} {
		f := report.Files[0].Findings[i]
		if f.RuleID != id || f.Path != ".github/workflows/a.yml" || f.JobID != "inspect" || f.StepIndex != 0 ||
			f.Location != (Location{Kind: "run_scalar_start", Line: 5, Column: 14}) {
			t.Fatalf("finding lost attribution: %+v", f)
		}
	}
	if len(*requests) != 8 {
		t.Fatalf("unexpected requests: %v", *requests)
	}
}

func TestRepositoryDiscoveryFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		reply apiReply
		code  string
	}{
		{"not public", testPrefix, apiReply{status: 404}, "repository_unavailable"},
		{"rate limit", testPrefix, apiReply{status: 403, header: http.Header{"X-Ratelimit-Remaining": {"0"}}}, "repository_unavailable"},
		{"private metadata", testPrefix, apiReply{status: 200, body: `{"private":true,"default_branch":"main"}`}, "invalid_repository_metadata"},
		{"missing metadata", testPrefix, apiReply{status: 200, body: `{}`}, "invalid_repository_metadata"},
		{"bad JSON", testPrefix, apiReply{status: 200, body: `{`}, "repository_unavailable"},
		{"oversized metadata", testPrefix, apiReply{status: 200, body: strings.Repeat(" ", maxGitHubMetadataBytes+1)}, "repository_unavailable"},
		{"invalid commit", testPrefix + "/commits/release%2Fmain", apiReply{status: 200, body: "../../other"}, "invalid_commit"},
		{"missing commit", testPrefix + "/commits/release%2Fmain", apiReply{status: 409}, "commit_unavailable"},
		{"invalid tree SHA", testPrefix + "/git/commits/" + testCommit, apiReply{status: 200, body: `{"tree":{"sha":"bad"}}`}, "discovery_failed"},
		{"truncated tree", testPrefix + "/git/trees/" + testFlows, apiReply{status: 200, body: `{"truncated":true,"tree":[]}`}, "discovery_failed"},
		{"missing truncation marker", testPrefix + "/git/trees/" + testFlows, apiReply{status: 200, body: `{"tree":[]}`}, "discovery_failed"},
		{"missing tree", testPrefix + "/git/trees/" + testFlows, apiReply{status: 200, body: `{"truncated":false}`}, "discovery_failed"},
		{"no directory", testPrefix + "/git/trees/" + testRoot, treeReply(t), "no_workflows"},
		{"empty workflow directory", testPrefix + "/git/trees/" + testFlows, treeReply(t), "no_workflows"},
		{"linked parent", testPrefix + "/git/trees/" + testRoot, treeReply(t, gitTreeEntry{Path: ".github", Type: "blob", Mode: "120000", SHA: testGitHub}), "unsupported_workflow_directory"},
		{"linked directory", testPrefix + "/git/trees/" + testGitHub, treeReply(t, gitTreeEntry{Path: "workflows", Type: "blob", Mode: "120000", SHA: testFlows}), "unsupported_workflow_directory"},
		{"unsafe filename", testPrefix + "/git/trees/" + testFlows, treeReply(t, blobEntry("NUL.yml", testBlob, simpleWorkflow)), "invalid_workflow_path"},
		{"nested path", testPrefix + "/git/trees/" + testFlows, treeReply(t, blobEntry("../escape.yml", testBlob, simpleWorkflow)), "discovery_failed"},
		{"duplicate entry", testPrefix + "/git/trees/" + testFlows, treeReply(t, blobEntry("a.yml", testBlob, simpleWorkflow), blobEntry("a.yml", testBlob, simpleWorkflow)), "discovery_failed"},
		{"case collision", testPrefix + "/git/trees/" + testFlows, treeReply(t, blobEntry("a.yml", testBlob, simpleWorkflow), blobEntry("A.yml", testBlob, simpleWorkflow)), "invalid_workflow_list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replies := fakeSnapshot(t)
			replies[tc.path] = tc.reply
			client, _ := fakeGitHub(t, replies)
			report := scanRepository(context.Background(), "owner/project", client)
			if report.ExitCode != 2 || len(report.Files) != 0 || len(report.Issues) != 1 || report.Issues[0].Code != tc.code {
				t.Fatalf("discovery failure became clean or lost its issue: %+v", report)
			}
		})
	}
}

func TestRepositoryFileCountLimit(t *testing.T) {
	entries := []gitTreeEntry{}
	for i := 0; i < MaxFiles; i++ {
		entries = append(entries, blobEntry(fmt.Sprintf("file-%02d.yml", i), testBlob, simpleWorkflow))
	}
	for _, count := range []int{MaxFiles, MaxFiles + 1} {
		if count > MaxFiles {
			entries = append(entries, blobEntry("extra.yml", testBlob, simpleWorkflow))
		}
		replies := fakeSnapshot(t, entries...)
		replies[testPrefix+"/git/blobs/"+testBlob] = apiReply{status: 200, body: simpleWorkflow}
		client, requests := fakeGitHub(t, replies)
		report := scanRepository(context.Background(), "owner/project", client)
		if count == MaxFiles {
			if report.ExitCode != 0 || len(report.Files) != MaxFiles {
				t.Fatalf("exact file limit failed: %+v", report)
			}
		} else if report.ExitCode != 2 || len(report.Files) != 0 || len(report.Issues) != 1 || len(*requests) != 6 {
			t.Fatalf("over-limit list was scanned or silently truncated: %+v", report)
		}
	}
}

func TestRepositoryPartialFindingsAndFileFailures(t *testing.T) {
	match := strings.Replace(simpleWorkflow, "echo literal", "echo ${{ github.event.pull_request.body }}", 1)
	for _, tc := range []struct {
		name       string
		entry      gitTreeEntry
		reply      apiReply
		wantStatus Status
	}{
		{"symlink", gitTreeEntry{Path: "z.yml", Type: "blob", Mode: "120000", SHA: testOther}, apiReply{}, Error},
		{"submodule", gitTreeEntry{Path: "z.yml", Type: "commit", Mode: "160000", SHA: testOther}, apiReply{}, Error},
		{"missing size", gitTreeEntry{Path: "z.yml", Type: "blob", Mode: "100644", SHA: testOther}, apiReply{}, Error},
		{"invalid SHA", blobEntry("z.yml", "../bad", simpleWorkflow), apiReply{}, Error},
		{"oversized declared file", blobEntry("z.yml", testOther, strings.Repeat("a", MaxFileBytes+1)), apiReply{}, Error},
		{"oversized response", blobEntry("z.yml", testOther, simpleWorkflow), apiReply{status: 200, body: strings.Repeat("a", MaxFileBytes+1)}, Error},
		{"size mismatch", blobEntry("z.yml", testOther, simpleWorkflow), apiReply{status: 200, body: "short"}, Error},
		{"server failure", blobEntry("z.yml", testOther, simpleWorkflow), apiReply{status: 500}, Error},
		{"bad YAML", blobEntry("z.yml", testOther, "jobs: ["), apiReply{status: 200, body: "jobs: ["}, Error},
		{"unsupported shell", blobEntry("z.yml", testOther, "jobs: {job: {steps: [{run: literal}]}}"), apiReply{status: 200, body: "jobs: {job: {steps: [{run: literal}]}}"}, Unsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replies := fakeSnapshot(t, blobEntry("a.yml", testBlob, match), tc.entry)
			replies[testPrefix+"/git/blobs/"+testBlob] = apiReply{status: 200, body: match}
			if tc.reply.status != 0 {
				replies[testPrefix+"/git/blobs/"+testOther] = tc.reply
			}
			client, _ := fakeGitHub(t, replies)
			report := scanRepository(context.Background(), "owner/project", client)
			if report.ExitCode != 2 || len(report.Files) != 2 || report.Totals.Findings != 1 ||
				report.Files[0].Status != Match || report.Files[1].Status != tc.wantStatus || len(report.Files[1].Issues) == 0 {
				t.Fatalf("partial result lost findings or failure: %+v", report)
			}
		})
	}
}

func TestRepositoryStopsAfterRateLimit(t *testing.T) {
	replies := fakeSnapshot(t, blobEntry("a.yml", testBlob, simpleWorkflow), blobEntry("b.yml", testOther, simpleWorkflow))
	replies[testPrefix+"/git/blobs/"+testBlob] = apiReply{status: 429}
	client, requests := fakeGitHub(t, replies)
	report := scanRepository(context.Background(), "owner/project", client)
	if report.ExitCode != 2 || report.Totals.ErrorFiles != 2 || len(*requests) != 7 ||
		!strings.Contains(report.Files[0].Issues[0].Message, "rate limit") || !strings.Contains(report.Files[1].Issues[0].Message, "skipped") {
		t.Fatalf("rate limit was retried or hidden: %+v; requests=%v", report, *requests)
	}
}

func TestRepositoryExactByteLimitAndEmptyWorkflow(t *testing.T) {
	for _, data := range []string{simpleWorkflow + "#" + strings.Repeat("x", MaxFileBytes-len(simpleWorkflow)-1), ""} {
		replies := fakeSnapshot(t, blobEntry("ci.yml", testBlob, data))
		replies[testPrefix+"/git/blobs/"+testBlob] = apiReply{status: 200, body: data}
		client, _ := fakeGitHub(t, replies)
		report := scanRepository(context.Background(), "owner/project", client)
		if len(data) == MaxFileBytes {
			if report.ExitCode != 0 || report.Totals.AnalyzedSteps != 1 {
				t.Fatalf("exact byte limit refused: %+v", report)
			}
		} else if report.ExitCode != 2 || report.Totals.ErrorFiles != 1 {
			t.Fatalf("empty workflow became a clean scan: %+v", report)
		}
	}
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenBody) Close() error             { return nil }

func TestRepositoryBrokenDownload(t *testing.T) {
	client := newGitHubClient()
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: brokenBody{}, Request: r}, nil
	})
	report := scanRepository(context.Background(), "owner/project", client)
	if report.ExitCode != 2 || len(report.Issues) != 1 || !strings.Contains(report.Issues[0].Message, "cannot read") {
		t.Fatalf("broken response became a clean scan: %+v", report)
	}
}

func TestRepositoryRefusesRedirects(t *testing.T) {
	replies := fakeSnapshot(t)
	replies[testPrefix] = apiReply{status: 302, header: http.Header{"Location": {"https://example.com/private"}}}
	client, requests := fakeGitHub(t, replies)
	report := scanRepository(context.Background(), "owner/project", client)
	if report.ExitCode != 2 || len(*requests) != 1 || !strings.Contains(report.Issues[0].Message, "redirect") {
		t.Fatalf("redirect was followed or hidden: %+v", report)
	}
}

func TestRepositoryCancellationAndNetworkFailure(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if canceled {
			cancel()
		}
		client := newGitHubClient()
		client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Context().Err() != nil {
				return nil, r.Context().Err()
			}
			return nil, errors.New("simulated network failure")
		})
		report := scanRepository(ctx, "owner/project", client)
		cancel()
		if report.ExitCode != 2 || len(report.Issues) != 1 || len(report.Files) != 0 {
			t.Fatalf("request failure hidden: %+v", report)
		}
	}
}

func TestRepositoryCLIUsesRemoteReport(t *testing.T) {
	replies := fakeSnapshot(t, blobEntry("ci.yml", testBlob, simpleWorkflow))
	replies[testPrefix+"/git/blobs/"+testBlob] = apiReply{status: 200, body: simpleWorkflow}
	client, _ := fakeGitHub(t, replies)
	fetch := func(ctx context.Context, name string) ScanReport { return scanRepository(ctx, name, client) }
	var stdout, stderr bytes.Buffer
	if code := run([]string{"scan", "--repo", "owner/project"}, &stdout, &stderr, fetch); code != 0 {
		t.Fatalf("exit %d: %s %s", code, stdout.String(), stderr.String())
	}
	var report ScanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Source == nil || report.Source.Commit != testCommit || report.Totals.Files != 1 || !reflect.DeepEqual(report.RuleIDs, []string{PRTitleRuleID, PRBodyRuleID}) {
		t.Fatalf("remote CLI report incomplete: %+v", report)
	}
	if code := run([]string{"scan", "--repo", "owner/project"}, failingWriter{}, io.Discard, fetch); code != 2 {
		t.Fatalf("output failure exit %d", code)
	}
}
