# Workflow Hardener

A small Go command-line tool that checks GitHub Actions workflows for direct pull-request title and body interpolation in Bash scripts. It reports findings with file locations and makes unsupported cases visible. Workflow files are read as data; their scripts are never executed.

## The problem

GitHub Actions substitutes expressions before passing a `run` script to the shell. A pull-request title is user-controlled, so inserting it directly into a script can introduce shell commands. [GitHub's script injection guidance](https://docs.github.com/en/actions/concepts/security/script-injections) explains this risk.

This tool flags the direct expression in an explicitly Bash step:

```yaml
- shell: bash
  run: printf '%s\n' "${{ github.event.pull_request.title }}"
```

Passing the title through an environment variable avoids direct insertion into this script:

```yaml
- shell: bash
  env:
    PR_TITLE: ${{ github.event.pull_request.title }}
  run: printf '%s\n' "$PR_TITLE"
```

The body rule flags the same direct insertion of `${{ github.event.pull_request.body }}`. Pass that value through an environment variable such as `PR_BODY` and use `"$PR_BODY"` in the script.

This follows GitHub's [intermediate environment variable guidance](https://docs.github.com/en/actions/reference/security/secure-use#use-an-intermediate-environment-variable). The scanner checks these two patterns; it does not establish that a workflow is secure.

## How it works

```mermaid
flowchart TD
    FILE[Local workflow file] --> READ[Read bounded input]
    REPO[Public GitHub repository] --> FETCH[Resolve commit and fetch workflow blobs]
    FETCH --> READ
    READ --> YAML[Parse jobs and steps]
    YAML --> RULE[Inspect Bash run scripts]
    RULE --> RESULT[JSON findings and coverage]
```

There is one command, `scan`, and two rules:

| Rule ID | Direct expression in an explicit Bash run step |
|---|---|
| `WH-R001` | `${{ github.event.pull_request.title }}` |
| `WH-R002` | `${{ github.event.pull_request.body }}` |

The only application dependency is `go.yaml.in/yaml/v3`, which preserves YAML source locations. AI assisted development; the executable does not use a model. Local scans are offline; repository scans fetch public data from GitHub's API using Go's standard HTTP client.

The JSON report lists both rules in `"rule_ids": ["WH-R001", "WH-R002"]`. This replaces the previous report-level `rule_id` field. Each finding still has its own `rule_id`. A matching step produces one finding per matching rule; repeated occurrences appear in that finding's `evidence` array. A step containing both expressions produces two findings and counts as one analyzed step.

## Demo

On GitHub, open **Actions → Scanner CI → Run workflow**. The workflow runs the tests, vets and builds the program, then displays these six examples in the run summary:

| Example | Expected result | Exit |
|---|---|---|
| [Direct title](testdata/risky-title.workflow.txt) | `match`, one finding | 1 |
| [Environment variable](testdata/env-title.workflow.txt) | `no_match` | 0 |
| [Inherited shell](testdata/default-shell-title.workflow.txt) | `unsupported` | 2 |
| [Direct body](testdata/risky-body.workflow.txt) | `match`, one finding | 1 |
| [Body environment variable](testdata/env-body.workflow.txt) | `no_match` | 0 |
| [Title and repeated body in one step](testdata/mixed-title-body.workflow.txt) | `match`, two findings | 1 |

The inherited-shell example intentionally demonstrates a limitation. A successful demo checks all six examples, including exit 2. Full JSON reports are printed in the demo step's log.

With Go 1.27 or later, build and scan from the repository root. On Windows:

```text
go build -o bin/hardener.exe ./cmd/hardener
./bin/hardener.exe scan --file testdata/risky-title.workflow.txt
./bin/hardener.exe scan --file testdata/risky-body.workflow.txt
```

On Linux or macOS, use `-o bin/hardener` and run `./bin/hardener`. Repeat `--file` to scan more than one file. Use `--root DIR` to read paths beneath another directory; the default root is the current directory. The program writes JSON to standard output and usage or operational messages to standard error.

## Scan a public repository

After building the scanner, supply an HTTPS GitHub repository URL or `OWNER/REPO`:

```powershell
.\bin\hardener.exe scan --repo https://github.com/OWNER/REPO
.\bin\hardener.exe scan --repo OWNER/REPO
```

Use `--repo` by itself; it cannot be combined with `--root` or `--file`. A trailing `.git` or slash is accepted. Branch-specific URLs, credentials, query strings, and fragments are rejected.

The scanner resolves the default branch to one commit, discovers `.yml` and `.yaml` entries directly in `.github/workflows`, and downloads ordinary workflow files at that snapshot. It reads Git tree modes to reject links and submodules. It does not clone the repository, write target files to disk, install target dependencies, or run target scripts. Nested directories and other file extensions are outside discovery.

Repository reports include `source.repository` and `source.commit`. Discovery failures appear in a top-level `issues` array; download or analysis failures appear in the affected file's `issues`. Missing workflows, incomplete directory listings, limits, and download failures return exit 2. Findings from files already analyzed are retained. After a failed download, remaining downloads are skipped and reported as errors.

Requests are anonymous and restricted to `api.github.com`; the scanner does not read tokens or follow redirects. For a renamed repository, use its current name. GitHub limits anonymous REST requests to 60 per hour per originating IP address, shared with other callers. A successful scan makes six discovery requests plus one per workflow file. Rate-limit failures are reported explicitly. See [GitHub's rate-limit documentation](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api).

The existing 50-file and 256-KiB-per-file limits apply. Each metadata response is limited to 1 MiB, each request to 15 seconds, and the overall network scan to two minutes. Oversized responses and timeouts remain errors.

## Run entirely from GitHub

Once [the manual workflow](.github/workflows/scan-repository.yml) is on the default branch:

1. Open **Actions → Scan public repository → Run workflow** in this repository.
2. Enter a public repository URL or `OWNER/REPO` in the **repository** field.
3. Click **Run workflow**, then open the run summary for results.
4. Download the **repository-scan** artifact for `report.json`, `stderr.txt`, and `exit-code.txt`. Artifacts are retained for seven days.

GitHub requires write access to this repository to dispatch the workflow. The workflow builds this scanner on a GitHub-hosted runner and passes the form input through the `TARGET_REPOSITORY` environment variable into the quoted `--repo` argument. Checkout credentials are not persisted, action versions are pinned, and permissions are read-only. No supplied secrets are used to fetch the target.

The summary and artifact steps run before the scanner's exit code is applied. A run with findings (exit 1) or incomplete analysis/errors (exit 2) is marked failed while its report remains available. Read the summary to distinguish those outcomes. The summary shows up to 100 findings and 100 file issues, shortening long text; the artifact contains the full scanner report. A build or infrastructure failure before scanning may have no report.

The CLI takes the repository through `--repo`; it does not automatically read an environment variable. You can pass one explicitly in PowerShell too:

```powershell
$env:TARGET_REPOSITORY = 'https://github.com/OWNER/REPO'
.\bin\hardener.exe scan --repo "$env:TARGET_REPOSITORY"
```

## Scope and limitations

- Only explicit step-level `shell: bash` is supported. Shell defaults, other shells, and reusable workflows are reported as unsupported.
- The rules recognize `${{ github.event.pull_request.title }}` and `${{ github.event.pull_request.body }}` with optional surrounding spaces, tabs, or line breaks. Both can appear in the same step. Other or incomplete expressions make the entire step unsupported for both rules, with no findings from that step.
- Expressions in `env`, step names, and other fields are outside the rules. Action `uses` steps are counted but their implementations are not inspected.
- The rules inspect decoded script text, including shell comments. They do not evaluate conditions, shell behavior, exploitability, or data flow.
- Findings identify the start of the YAML `run` value, using one-based lines and columns and a zero-based step index.
- Input is limited to 50 files, each at most 256 KiB. Local filenames must be explicitly supplied; repository mode discovers workflow filenames. Invalid YAML, duplicate keys, multiple documents, aliases, merge keys, links, and local paths outside the input root are rejected.

Exit **0** means supported analysis completed without a finding. Exit **1** means a finding was reported. Exit **2** means an error or incomplete analysis; findings from supported steps remain in the report. A workflow with no run steps is also unsupported.

## Code

- [`cmd/hardener/`](cmd/hardener/) contains the executable entry point and compiled CLI tests.
- [`internal/hardener/`](internal/hardener/) contains input handling, parsing, detection, reporting, and unit tests.
- [`testdata/`](testdata/) contains small synthetic workflow examples stored as inert `.workflow.txt` files.
- [The code walkthrough](docs/ARCHITECTURE.md) follows one input through the functions and explains the Go concepts involved.

Tests live beside their code in `_test.go` files. CI runs `go test ./...`, `go vet ./...`, and `go build`; the integration test checks actual process exit codes and rule IDs against all fourteen examples.

Repository tests simulate GitHub HTTP responses without live network requests. They cover commit pinning, source locations, both rules, filename validation, redirects, rate limits, byte and file limits, partial failures, cancellation, and CLI reporting.
