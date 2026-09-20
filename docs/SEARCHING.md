# Find repositories to scan

Use these queries in [GitHub's web code search](https://github.com/search?type=code), then confirm candidates with Workflow Hardener. Code search requires signing in and can include private repositories you can access; select a **public repository** for this scanner. GitHub searches indexed content on the default branch and does not guarantee exhaustive results. See [code-search access and limitations](https://docs.github.com/en/search-github/github-code-search/about-github-code-search).

Copy each query as one line. GitHub supports `content:`, regular expressions, and Boolean operators; see the [search syntax reference](https://docs.github.com/en/search-github/github-code-search/understanding-github-code-search-syntax). These queries filter candidate files; they do not parse YAML or confirm scanner findings.

## Automated candidate discovery

The separate Go executable at [`cmd/find-candidates`](../cmd/find-candidates/main.go) automates a small sample of the search, download, and regex-filtering procedure. It writes `candidates.json` for later review or bulk scanning. It does not run the scanner or change its two rules.

Build with `go build -o bin/find-candidates ./cmd/find-candidates` (use `bin/find-candidates.exe` on Windows). The executable takes no arguments other than `--help`/`-h`. It reads `GH_TOKEN` from its environment and creates `candidates.json` in the current directory, refusing to overwrite an existing file. Use an empty output directory for each run.

The intended authentication source is the built-in GitHub Actions job token. Its external public code-search access with `permissions: {}` was confirmed by the [authentication check](https://github.com/zorosz/workflow-hardener/actions/runs/35500013168). No personal token or supplied secret is needed. In a trusted GitHub-hosted job, after checkout and Go setup, the build and discovery steps would be:

```yaml
- name: Build candidate discovery
  run: go build -o bin/find-candidates ./cmd/find-candidates
- name: Find candidates
  shell: bash
  env:
    GH_TOKEN: ${{ github.token }}
  run: |
    mkdir -p out/candidates
    cd out/candidates
    ../../bin/find-candidates
```

This is a usage example, not a new runnable workflow in this change. A manual discovery runner, artifact upload, and bulk scanning remain separate work. CI builds this executable and runs its offline tests without credentials or live searches. Keep checkout credentials unpersisted and action versions pinned, as in the existing CI. Checkout requires `contents: read`; discovery itself needs no additional token permissions. Only the discovery step needs `GH_TOKEN`.

The [REST API uses legacy search syntax](https://docs.github.com/en/search-github/searching-on-github/searching-code), so the executable makes four broad queries in this order:

```text
"github.event.pull_request.title" in:file path:.github/workflows extension:yml
"github.event.pull_request.title" in:file path:.github/workflows extension:yaml
"github.event.pull_request.body" in:file path:.github/workflows extension:yml
"github.event.pull_request.body" in:file path:.github/workflows extension:yaml
```

For each query it requests page 1 with 25 results. Search calls are separated by at least six seconds. It validates public repository metadata, portable paths directly under `.github/workflows`, and blob SHAs, then downloads up to 20 unique repository/path/SHA combinations. Repository names are deduplicated without regard to case; paths and SHAs remain exact. Both PR fields and both layouts below are checked in every downloaded file, regardless of which query found it.

Requests go only to constructed `api.github.com` GET endpoints and never follow redirects or response-provided URLs. Search requests use the job token; blob downloads are anonymous. Workflow text stays in memory and is never executed. Metadata is limited to 1 MiB, blobs to 256 KiB, each request to 15 seconds, and the discovery run to two minutes. API, transport, invalid metadata, or download failures stop further requests while retaining earlier candidates. Rate-limit errors include no raw response body or credential. The anonymous blob requests share GitHub's IP-based rate limit with other callers.

The report contains:

| Field | Meaning |
|---|---|
| `candidates` | One record per repository/path/SHA, filter, and PR field. Each has `repository`, `path`, `blob_sha`, `filter` (`one_line` or `multiline`), `pr_field` (`title` or `body`), and `match_line`. |
| `match_line` | One-based line where the first regex match begins, at `shell:`. This is not the scanner's YAML `run` location or necessarily the expression's line. Repeated expressions or matching steps do not create extra records for that file/filter/field. |
| `queries` | The four fixed queries, whether each returned valid metadata (`completed`), its reported `total_count`, and the number of items `returned`. An unfinished query retains `completed: false`. |
| `downloads_attempted`, `files_filtered` | Number of unique blob fetches attempted and number successfully filtered. |
| `issues` | Errors, incomplete API results, and omissions from the page/download limits, with the query or validated file source where applicable. |
| `complete`, `exit_code` | `true`/0 only when the fixed queries and returned files finish without errors or reported omissions; otherwise `false`/2. Candidates remain available on exit 2. |

Exit 0 can contain candidates: discovery does not confirm scanner findings and never uses exit 1. Even a complete discovery report is not an exhaustive search of GitHub. If output cannot be written, the process returns 2 and reports the failure on stderr; there may be no valid JSON file.

The filters use the one-line and eight-line multiline layouts below, including their false positives and omissions. They do not apply the web query's fork/archive operators; REST search has its own indexing restrictions. `candidates.json` stores blob identities, not a repository commit. A later `scan --repo` resolves the current default-branch commit, which can differ from the indexed blob. Keep that scan's JSON and exit code, including exit 2, alongside the original candidate record.

## Broad candidate search

```text
path:.github/workflows/ (content:"github.event.pull_request.title" OR content:"github.event.pull_request.body") content:"shell: bash"
```

This finds files whose paths contain `.github/workflows/`, with a PR-title or PR-body reference and the text `shell: bash`. The terms can occur anywhere in the same file: a PR reference might be in `env`, while the Bash shell belongs to another step.

## One-line run scripts

This narrower query targets an unquoted `shell: bash` immediately followed by a `run:` line containing an exact direct PR-title or PR-body expression:

```text
path:/^\.github\/workflows\/[^\/]+\.ya?ml$/ content:/(?-i)shell:[ \t]*bash[ \t]*\r?\n[ \t]+run:[^\r\n]*\$\{\{[ \t]*github\.event\.pull_request\.(title|body)[ \t]*\}\}/ NOT is:fork NOT is:archived
```

Example layout it targets:

```yaml
- name: Print the title
  shell: bash
  run: echo "${{ github.event.pull_request.title }}"
```

The anchored path restricts results to `.yml` and `.yaml` files directly inside the repository's `.github/workflows` directory. `(?-i)` makes the content pattern case-sensitive. `NOT is:fork` and `NOT is:archived` exclude forks and archived repositories.

## Multiline run scripts

This version targets `shell: bash` immediately followed by `run: |` or `run: >`. It looks for the direct PR expression in the first eight lines after the `run:` header:

```text
path:/^\.github\/workflows\/[^\/]+\.ya?ml$/ content:/(?-i)shell:[ \t]*bash[ \t]*\r?\n[ \t]+run:[ \t]*[|>][-+]?[ \t]*\r?\n([^\n]*\n){0,7}[^\n]*\$\{\{[ \t]*github\.event\.pull_request\.(title|body)[ \t]*\}\}/ NOT is:fork NOT is:archived
```

Example layout it targets:

```yaml
- name: Print the body
  shell: bash
  run: |
    echo "Starting"
    printf '%s\n' "${{ github.event.pull_request.body }}"
```

The `{0,7}` portion allows up to seven preceding lines before the matching line. The path, case, fork, and archive restrictions are the same as in the one-line query.

## What the queries miss

The narrower queries depend on a particular layout. They miss inherited shells, `sh`, quoted shell values, intervening fields or comments between `shell` and `run`, and steps that put `shell` after `run`. The multiline query also misses expressions beyond its eight-line window. Both narrower queries omit fallback expressions such as `${{ github.event.pull_request.body || '' }}`, even though the scanner supports them.

The patterns do not enforce YAML indentation or step boundaries. The multiline pattern can cross into another step or field, and text in comments or embedded examples can also match. Treat every result as a candidate for inspection. No positive-match rate has been measured, and a search match does not establish exploitability.

## Turn a result into a scan

1. Open a result and check that its repository is public.
2. Copy the repository's `OWNER/REPO` or `https://github.com/OWNER/REPO`. A file URL containing `/blob/` or a branch URL containing `/tree/` is not accepted by the scanner.
3. Follow [Run entirely from GitHub](../README.md#run-entirely-from-github) and put that value in the **repository** field. For local use, see [Scan a public repository](../README.md#scan-a-public-repository).
4. Review both **Findings** and **Coverage and errors** in the summary. Download the **repository-scan** artifact for the full report.

The scanner checks the repository's current default-branch snapshot, so its content may differ from an indexed search result. Exit **1** means a finding was reported with complete supported analysis. Exit **2** means analysis was incomplete or an error occurred; findings from supported steps remain available. A positive finding can therefore coexist with exit 2 because another step or workflow is unsupported. Both exit codes mark the manual GitHub run failed.
