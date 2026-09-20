# 006: Automate candidate repository search

Status: Approved

## Problem

Finding scan candidates currently requires manual GitHub searches and inspection. A small discovery tool can collect likely matches into a file for later scanning.

## Proposed change

Add a small Go discovery executable using only the standard library (`net/http`, `regexp`, and `encoding/json`). Give it a thin entry point at `cmd/find-candidates/main.go`, with its logic and tests in the existing `internal/hardener` application package. It adds no dependencies and follows four steps:

1. Search GitHub's REST API for PR-title and PR-body references in `.github/workflows` files, using separate broad queries for each field and `.yml`/`.yaml` extension.
2. Download the matching public workflow blobs when their contents are needed. Deduplicate by repository, path, and blob SHA; keep downloaded text in memory.
3. Apply case-sensitive regex filters adapted from the one-line and multiline patterns in `docs/SEARCHING.md`. This checks complete file contents rather than relying on API snippets. Keep the filters small and separate so they are easy to inspect and adjust.
4. Write `candidates.json`, containing candidate records and any discovery errors or sampling limits.

Each candidate records the repository, workflow path, downloaded blob SHA, matched filter, PR field (`title` or `body`), and match line. Record one candidate per file and filter/field combination, retaining the first matching line. The line belongs to the regex match and is not the scanner's YAML source location.

Start with the documented regex layouts and their existing limitations, including the eight-line multiline window. Regex matches remain provisional: comments, examples, and matches across YAML boundaries can produce false positives; inherited shells and other layouts may be missed. The scanner will decide which candidates produce supported findings.

Use a small fixed sample initially: one page of 25 results for each of four queries, at most 20 unique workflow downloads, and 256 KiB per downloaded file. Report when these limits omit results. Keep API responses bounded to 1 MiB, requests to 15 seconds, and the run to two minutes. Pace search requests and stop on rate limits or API failures, preserving collected candidates and recording unfinished work. An incomplete run must not look like a successful empty search.

Use only constructed `api.github.com` GET endpoints, validate repository names, direct workflow paths, and blob SHAs, and refuse redirects. Accept public repositories only. Workflow contents are untrusted text and are never executed. Do not print credentials or raw API error bodies.

The Go scanner, its commands, and its two rules remain unchanged. This change does not add a search command, a configurable Actions form, or automatic scanning.

### Later bulk scanning

A separate change can read `candidates.json`, deduplicate repository names, call the existing `scan --repo` once per repository, and save each full JSON report and exit code. Exit 2 and any accompanying findings must be retained. The repository scan resolves its own current commit, which may differ from the earlier search blob; record both sources rather than assuming identical contents.

### Affected files

- `cmd/find-candidates/main.go`: thin entry point for the discovery executable.
- `internal/hardener/candidates.go` and `candidates_test.go`: discovery logic and focused tests.
- `docs/SEARCHING.md`: short usage, output, and regex-limitations documentation.
- `docs/ARCHITECTURE.md`: a short note locating the discovery executable and explaining its relationship to the scanner.
- `.github/workflows/ci.yml`: build the discovery executable; existing `go test ./...` and `go vet ./...` cover the new code and offline tests.
- `.github/workflows/check-code-search.yml`: a minimal authentication check on the `test/code-search-token` branch, using the built-in job token with `permissions: {}` and no checkout.
- This spec and `spec/README.md`.

## Acceptance criteria

- A bounded API search downloads relevant public workflow text, filters it, and writes a reusable candidate file.
- Duplicate search hits do not cause repeated downloads or duplicate candidate records.
- Output identifies the inspected blob and regex match, and makes errors and sampling limits visible.
- Regex filtering has examples that match and do not match, with its YAML limitations documented.
- Discovery does not execute target code or change scanner behavior.
- Discovery uses Go's standard library and the existing application package, with no Python requirement or new dependency.

## Validation plan

Review the Go code before execution. Add offline tests with fake API responses and inert workflow text for filtering, deduplication, source attribution, input bounds, and partial failures. Run tests, vet, and builds through approved GitHub-hosted CI; do not execute generated project code locally without separate authorization. Live authenticated search remains unverified until actually exercised. Commit and push require separate authorization.

First verify authentication with one bounded `GET /search/code` request for `addClass repo:jquery/jquery in:file`, requesting one result. Run the check on a GitHub-hosted runner when the dedicated test branch is pushed. Use no checkout, third-party actions, or supplied secrets; pass only the built-in job token through a step environment variable. Do not follow redirects or print the token or raw response. Require HTTP 200, `incomplete_results: false`, and a result attributed to the public `jquery/jquery` repository. An empty or incomplete response is inconclusive; API/transport failures do not establish working access. Report the result in the job summary. The check does not build the scanner, fetch target scripts, or implement discovery.

## Open questions

The built-in Actions token has demonstrated code-search access to an external public repository with `permissions: {}`. The final discovery runner remains to be settled; no personal token or supplied-secret fallback is included. The four candidate-discovery queries and their download/filtering pipeline have not yet been implemented or exercised.

## Implementation and validation results

The discovery tool is not implemented. Reviewed the branch authentication check's single API request, empty permissions, redirect behavior, time/size bounds, result validation, and output handling. Whitespace checks passed for the workflow and spec changes. A separate anonymous, read-only check confirmed that public `jquery/jquery` contains `addClass` in `src/attributes/classes.js`.

[Check code search access passed](https://github.com/zorosz/workflow-hardener/actions/runs/35500013168) on 2026-09-20 for commit `267d47365029d4a403d72b9f39842857e6abff45` on `test/code-search-token`. Its successful result requires HTTP 200, `incomplete_results: false`, and a public `jquery/jquery` match. This confirms external public code-search access with the built-in job token and `permissions: {}` for the tested query. No checkout, supplied secrets, or target-code execution were used. Scanner tests/builds were not run for this workflow-only check, and no generated project code was executed locally.

References: [REST code search](https://docs.github.com/en/rest/search/search#search-code), [API regex limitations](https://cli.github.com/manual/gh_search_code), and [the built-in Actions token](https://docs.github.com/en/actions/concepts/security/github_token).
