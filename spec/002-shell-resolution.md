# 002: Resolve Bash and sh shells for the existing PR rules

Status: Implemented

## Problem

The current detector rejects every run step without step-level `shell: bash`. Shell selection is shared by both PR rules, so expand that shared support while retaining `WH-R001` for titles and `WH-R002` for bodies.

The [example workflow](https://github.com/BKaperick/Bryan-Kaperick.me/blob/0e39b831042020ea373e5f4527c1b8165df38e26/.github/workflows/main.yml) uses `container: ubuntu`. Its omitted shells default to `sh`. The explicit Bash step containing the PR-body expression is already covered. Expected results below are based on static inspection of this pinned source.

## Proposed change

Support direct title and body interpolation in Bash and `sh` run scripts, including shells selected through static defaults. Resolve each step's shell in this order, using the first applicable setting:

| Priority | Setting | Supported case |
|---|---|---|
| 1 | Step `shell` | Exact literal `bash` or `sh` |
| 2 | Job `defaults.run.shell` | Exact literal `bash` or `sh` |
| 3 | Workflow `defaults.run.shell` | Exact literal `bash` or `sh` |
| 4 | Static job container on a recognized Ubuntu runner | Default `sh` |
| 5 | Recognized Ubuntu or macOS runner without a job container | Default Bash with possible `sh` fallback; both are within scope |

For runner inference, initially recognize only scalar `runs-on` values `ubuntu-latest`, `ubuntu-22.04`, `ubuntu-24.04`, `ubuntu-26.04`, `macos-latest`, `macos-14`, `macos-15`, and `macos-26`. Use an explicit allowlist, not a prefix match. Container inference accepts a nonempty literal image string or a mapping with a nonempty literal `image`. Do not download or inspect images.

A selected empty, dynamic, custom, or unsupported shell must not fall through to a lower-priority default. Malformed `defaults`/`run` mappings or non-string shell values remain parsing errors. Job defaults that specify only `working-directory` must still inherit a workflow shell. An explicit supported shell can be analyzed without inferring the runner or container.

Unknown runners, other runner labels, runner arrays/groups, matrix expressions, and dynamic container selection remain unsupported when needed to infer the shell. PowerShell, cmd, Python, and custom shell commands remain unsupported. Missing information must not imply Bash.

These precedence and platform rules follow [GitHub's shell and defaults documentation](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idstepsshell). The container default is documented separately in [Running jobs in a container](https://docs.github.com/en/actions/how-tos/write-workflows/choose-where-workflows-run/run-jobs-in-a-container).

Preserve both rule IDs, JSON field names, finding grouping, run-value source locations, input limits, and exit-code precedence. Update the report scope and messages to describe the expanded shell support. Keep one application package and the existing dependencies.

### Remaining expression limitation

Expression handling stays unchanged: any expression outside the exact direct title/body forms makes that entire step unsupported. No expressions, conditions, scripts, or target dependencies are evaluated or executed.

For the pinned target, static inspection predicts that the five `unsupported_shell` cases would become three analyzed steps and two `unsupported_expression` cases. The clone and push steps contain expressions such as `${{ github.head_ref || github.ref_name }}`. The expected total is seven run steps, five analyzed steps, one `WH-R002` finding, and two unsupported steps: exit 2 still applies. Resolving shells does not promise exit 1 for this repository. Broader expression support needs a separate proposal.

### Affected files

- `internal/hardener/workflow.go`, `model.go`, and `pr_text.go`: retain shell-selection context, resolve supported shells, and update scope and messages; a small helper may stay in this package.
- Tests beside those files, scanner/repository tests, and `cmd/hardener/main_test.go`: cover the new behavior in local and repository modes.
- `testdata/`: add small synthetic examples stored as inert `.workflow.txt` files.
- `.github/workflows/ci.yml`: change the inherited-shell demo to expect a finding and add the existing PowerShell fixture as an unsupported demo.
- `README.md`, `docs/ARCHITECTURE.md`, and the opening scope sentence in `AGENTS.md`: describe the expanded coverage and remaining limitations.

## Acceptance criteria

- Both rules detect their direct expressions in each supported shell-selection case, retaining source attribution and one finding per matching rule per step.
- Step settings override job settings, and job settings override workflow settings. A selected unsupported shell produces an explicit issue.
- A literal static-container example with no step shell and a direct PR-body expression reports `WH-R002` with exit 1 when all analysis is supported.
- The existing inherited-Bash title fixture changes from unsupported/exit 2 to `WH-R001`/exit 1.
- Environment-variable examples remain free of direct-interpolation findings.
- Unsupported shells, unresolved context, malformed input, and unknown expressions remain visible; incomplete analysis still returns 2 and retains findings from supported steps.
- A synthetic example shaped like the reported target produces the totals described above, without copying or executing its operational scripts.

## Validation plan

- Review all generated changes before execution and run `git diff --check`.
- Add focused parser and detector cases for precedence, inherited working-directory-only defaults, explicit `sh`, platform defaults, container forms, dynamic/unknown inputs, malformed defaults, and blocked fallback.
- Add scanner and compiled CLI cases for supported inheritance, container PR-body interpolation, and findings retained alongside unsupported expressions. Use simulated HTTP responses for repository-mode checks.
- Run tests, vet, build, and the updated demos in the approved GitHub-hosted Scanner CI after separately authorized commit/push. Preserve pinned actions, read-only permissions, and unpersisted checkout credentials.
- No project code or target scripts will be executed locally. Report unrun checks explicitly.

## Open questions

None. This proposal intentionally covers shell resolution; expression support remains a separate change.

## Implementation and validation results

- Added shared shell resolution in `internal/hardener/shell.go` and connected it to parsing and both existing rules. Unsupported selections block fallback; malformed defaults remain errors.
- Added coverage for precedence, the eight runner labels, container defaults, unknown contexts, and expression boundaries in all supported shells. Added local/repository parity checks and four synthetic fixtures, bringing compiled CLI checks to eighteen examples.
- Updated the inherited-shell demo to expect `WH-R001` and added the PowerShell unsupported example, for seven CI demos. Updated the README, walkthrough, and project scope.
- Reviewed the code and test changes. `gofmt -l` reported no formatting differences, and `git diff --check` passed.
- [Scanner CI passed](https://github.com/zorosz/workflow-hardener/actions/runs/35325203894) for implementation commit `324e09d`: tests (including all eighteen compiled CLI examples), vet, build, and all seven demos. No project code or target scripts were executed locally.
