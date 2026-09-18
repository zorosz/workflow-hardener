package hardener

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxGitHubMetadataBytes = 1024 * 1024

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)
var gitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// RepositorySource identifies the immutable snapshot used for a remote scan.
type RepositorySource struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit,omitempty"`
}

func parseRepository(input string) (string, error) {
	invalid := errors.New("repository must be OWNER/REPO or an HTTPS github.com repository URL without credentials, query, or fragment")
	if len(input) > 256 {
		return "", invalid
	}
	name := strings.TrimSpace(input)
	if strings.Contains(name, "://") {
		u, err := url.Parse(name)
		if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") ||
			u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(name, "#") || u.RawPath != "" {
			return "", invalid
		}
		name = strings.TrimPrefix(u.Path, "/")
	}
	name = strings.TrimSuffix(name, "/")
	name = strings.TrimSuffix(name, ".git")
	if !repositoryPattern.MatchString(name) {
		return "", invalid
	}
	parts := strings.Split(name, "/")
	if parts[1] == "." || parts[1] == ".." {
		return "", invalid
	}
	return name, nil
}

func newGitHubClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		// All requests use constructed api.github.com URLs. Renamed repositories
		// must be supplied by their current name; response URLs are never followed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// ScanRepository fetches only public workflow blobs, without credentials, Git,
// filesystem writes, or execution of anything from the target repository.
func ScanRepository(ctx context.Context, repository string) ScanReport {
	return scanRepository(ctx, repository, newGitHubClient())
}

func scanRepository(ctx context.Context, repository string, client *http.Client) ScanReport {
	report := newScanReport()
	fail := func(code string, err error) ScanReport {
		report.ExitCode = 2
		report.Issues = append(report.Issues, issue("repository", code, err.Error()))
		return report
	}
	name, err := parseRepository(repository)
	if err != nil {
		return fail("invalid_repository", err)
	}
	report.Source = &RepositorySource{Repository: name}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	in := &githubInputs{ctx: ctx, client: client, prefix: "/repos/" + name, entries: map[string]gitTreeEntry{}}
	var meta struct {
		Private       *bool  `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := in.getJSON(in.prefix, &meta); err != nil {
		return fail("repository_unavailable", err)
	}
	if meta.Private == nil || *meta.Private || meta.DefaultBranch == "" || len(meta.DefaultBranch) > 1024 {
		return fail("invalid_repository_metadata", errors.New("a public repository with a default branch is required"))
	}
	sha, err := in.get(in.prefix+"/commits/"+url.PathEscape(meta.DefaultBranch), "application/vnd.github.sha", 128)
	if err != nil {
		return fail("commit_unavailable", err)
	}
	commit := strings.TrimSpace(string(sha))
	if !gitSHAPattern.MatchString(commit) {
		return fail("invalid_commit", errors.New("GitHub did not return a valid commit SHA"))
	}
	report.Source.Commit = commit
	var commitObject struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := in.getJSON(in.prefix+"/git/commits/"+commit, &commitObject); err != nil {
		return fail("commit_unavailable", err)
	}
	tree, err := in.tree(commitObject.Tree.SHA)
	if err != nil {
		return fail("discovery_failed", err)
	}
	// Walk just the two directories. The Contents API can dereference symlinks;
	// Git trees expose their modes so neither directory links nor file links pass.
	for _, directory := range []string{".github", "workflows"} {
		var child *gitTreeEntry
		for i := range tree {
			if tree[i].Path == directory {
				child = &tree[i]
				break
			}
		}
		if child == nil {
			return fail("no_workflows", errors.New("no .github/workflows directory was found at this commit"))
		}
		if child.Type != "tree" || child.Mode != "040000" {
			return fail("unsupported_workflow_directory", errors.New(".github/workflows and its parent must be ordinary directories"))
		}
		tree, err = in.tree(child.SHA)
		if err != nil {
			return fail("discovery_failed", err)
		}
	}
	names := []string{}
	for _, entry := range tree {
		if !strings.HasSuffix(entry.Path, ".yml") && !strings.HasSuffix(entry.Path, ".yaml") {
			continue
		}
		path := ".github/workflows/" + entry.Path
		if !validPath(path) {
			return fail("invalid_workflow_path", errors.New("workflow filename is not a supported portable path"))
		}
		in.entries[path] = entry
		names = append(names, path)
	}
	if len(names) == 0 {
		return fail("no_workflows", errors.New("no .yml or .yaml workflow files were found at this commit"))
	}
	scanned, err := Scan(in, names)
	if err != nil {
		return fail("invalid_workflow_list", err)
	}
	scanned.Source = report.Source
	return scanned
}

type gitTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size *int64 `json:"size"`
}

type githubInputs struct {
	ctx     context.Context
	client  *http.Client
	prefix  string
	entries map[string]gitTreeEntry
	stopped error
}

func (in *githubInputs) tree(sha string) ([]gitTreeEntry, error) {
	if !gitSHAPattern.MatchString(sha) {
		return nil, errors.New("invalid Git tree SHA")
	}
	var tree struct {
		Entries   []gitTreeEntry `json:"tree"`
		Truncated *bool          `json:"truncated"`
	}
	if err := in.getJSON(in.prefix+"/git/trees/"+sha, &tree); err != nil {
		return nil, err
	}
	if tree.Truncated == nil || *tree.Truncated || tree.Entries == nil {
		return nil, errors.New("GitHub returned an incomplete Git tree")
	}
	seen := map[string]bool{}
	for _, entry := range tree.Entries {
		if entry.Path == "" || strings.Contains(entry.Path, "/") || seen[entry.Path] {
			return nil, errors.New("GitHub returned an invalid or duplicate tree entry")
		}
		seen[entry.Path] = true
	}
	return tree.Entries, nil
}

func (in *githubInputs) Read(name string, limit int64) ([]byte, error) {
	entry, exists := in.entries[name]
	if !exists || !validPath(name) {
		return nil, errors.New("workflow is not in the discovered snapshot")
	}
	if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") {
		return nil, errors.New("workflow must be an ordinary file; links, submodules, and directories are not accepted")
	}
	if !gitSHAPattern.MatchString(entry.SHA) || entry.Size == nil || *entry.Size < 0 {
		return nil, errors.New("workflow blob metadata is incomplete")
	}
	if *entry.Size > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	if in.stopped != nil {
		return nil, in.stopped
	}
	data, err := in.get(in.prefix+"/git/blobs/"+entry.SHA, "application/vnd.github.raw+json", limit)
	if err != nil {
		// Stop fetching after a network/API failure, including rate limits.
		// Scan still emits explicit error results for all remaining files.
		in.stopped = errors.New("download skipped after an earlier GitHub request failed")
		return nil, err
	}
	if int64(len(data)) != *entry.Size {
		return nil, errors.New("downloaded workflow size differs from Git tree metadata")
	}
	return data, nil
}

func (in *githubInputs) getJSON(path string, target any) error {
	data, err := in.get(path, "application/vnd.github+json", maxGitHubMetadataBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("GitHub returned invalid JSON metadata")
	}
	return nil
}

func (in *githubInputs) get(path, accept string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(in.ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		return nil, errors.New("cannot construct GitHub request")
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "workflow-hardener")
	response, err := in.client.Do(req)
	if err != nil {
		if in.ctx.Err() != nil {
			return nil, errors.New("repository scan was canceled or exceeded its time limit")
		}
		return nil, errors.New("GitHub request failed or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch {
		case response.StatusCode == 429 || (response.StatusCode == 403 && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "")):
			return nil, errors.New("GitHub API rate limit reached; retry later (anonymous requests share an IP-based limit)")
		case response.StatusCode >= 300 && response.StatusCode < 400:
			return nil, errors.New("GitHub redirect refused; use the repository's current canonical name")
		case response.StatusCode == 404:
			return nil, errors.New("GitHub resource not found or not publicly accessible")
		default:
			return nil, fmt.Errorf("GitHub returned HTTP %d", response.StatusCode)
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, errors.New("cannot read GitHub response")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("GitHub response exceeds %d bytes", limit)
	}
	return data, nil
}
