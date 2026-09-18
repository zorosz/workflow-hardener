# Find repositories to scan

Use these queries in [GitHub's web code search](https://github.com/search?type=code), then confirm candidates with Workflow Hardener. Code search requires signing in and can include private repositories you can access; select a **public repository** for this scanner. GitHub searches indexed content on the default branch and does not guarantee exhaustive results. See [code-search access and limitations](https://docs.github.com/en/search-github/github-code-search/about-github-code-search).

Copy each query as one line. GitHub supports `content:`, regular expressions, and Boolean operators; see the [search syntax reference](https://docs.github.com/en/search-github/github-code-search/understanding-github-code-search-syntax). These queries filter candidate files; they do not parse YAML or confirm scanner findings.

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
