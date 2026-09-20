# 008: Run candidate discovery from the GitHub web UI

Status: Implemented

## Problem

The `find-candidates` executable is implemented and covered by offline CI tests, but there is no manual workflow to run a live search and download its output. Candidate reports must remain available when sampling limits or failures cause exit 2.

## Proposed change

Add `.github/workflows/find-candidates.yml`, named **Find candidates**, with only a `workflow_dispatch` trigger and no custom inputs. The user opens **Actions → Find candidates → Run workflow**, selects `main`, starts the run, and downloads its report from the run page. The workflow must be present on the default branch before the manual launcher is available; running it requires repository write access.

Use one GitHub-hosted `ubuntu-24.04` job with a ten-minute timeout. Reuse the existing workflows' Go version, toolchain settings, and pinned checkout/setup/upload actions. Set `contents: read`, leave checkout credentials unpersisted, and provide `GH_TOKEN: ${{ github.token }}` only to the discovery step. Do not add personal tokens, supplied secrets, or self-hosted runners.

The job follows this sequence:

1. Check out this project and build `./cmd/find-candidates`.
2. Run the executable in a fresh output directory. Capture its process exit code instead of failing the step immediately. Keep `candidates.json` as written by the tool, plus `stdout.txt`, `stderr.txt`, and `exit-code.txt`.
3. Validate the JSON report and its agreement with the saved process exit code. Write a small job summary showing complete/incomplete status, candidate count, queries completed, files filtered, and errors or sampling limits. Show at most 20 issues, shorten long text to 512 characters, and HTML-escape report text. Direct the user to the artifact for full details. Describe matches as provisional candidates, not scanner findings or confirmed vulnerabilities.
4. Upload the output files as a **candidate-search** artifact with seven-day retention. Attempt upload after discovery even if discovery or summary validation failed. A normal exit 2 must not prevent upload.
5. Apply the saved exit code after summary and upload: 0 permits success, while 2 marks the job failed without discarding its artifact. Unexpected exit codes, missing/malformed reports, disagreement between the report and process result, and upload failures must not become successful runs.

A complete search can return candidates and exit 0. Exit 2 can retain useful candidates and may reflect the fixed sampling limits rather than an authentication failure. An infrastructure failure, cancellation, or failure before discovery creates output may leave no report; do not generate a replacement report claiming an empty successful search.

Keep the executable's four queries, regex filters, report schema, and limits unchanged: one page of 25 results per query, 20 unique workflow downloads, 256 KiB per blob, 1 MiB per metadata response, 15 seconds per request, and a two-minute discovery deadline. Blob downloads remain anonymous and workflow text is never executed. This change adds no scheduled searches, configurable search form, bulk scanning, or scanner-rule changes.

### Documentation and affected files

- `.github/workflows/find-candidates.yml`: manual launcher, summary, artifact upload, and final status.
- `README.md`: add the search launch steps near the existing GitHub scan instructions, with the artifact name and exit-2 explanation.
- `docs/SEARCHING.md`: replace the hypothetical workflow example with the actual launch/download procedure; retain CLI usage and search limitations.
- `docs/ARCHITECTURE.md`: document the launcher and its build → discovery → summary/artifact → final-status sequence.
- `spec/006-automated-candidate-search.md`: resolve the discovery-runner open question by linking this change, while preserving the earlier validation record.
- This spec and `spec/README.md`.

## Acceptance criteria

- A user with repository write access can start **Find candidates** from the GitHub web UI without entering a repository, query, or token.
- A normal completed discovery produces a downloadable **candidate-search** artifact containing `candidates.json`, stdout, stderr, and the actual exit code.
- When discovery exits 2, the report and any candidates it contains are uploaded before the workflow is marked failed.
- Missing/invalid output and report-upload failures are visible failures; incomplete discovery is never presented as a clean search.
- The summary distinguishes provisional candidates from scanner findings and explains errors or sampling limits.
- The architecture and usage documentation describe the implemented workflow in the same change.

## Validation plan

Review workflow syntax, action pins, permissions, token scope, output paths, report validation, and step conditions before execution. Check the capture/summary/upload/final-status ordering for exit 0, exit 2 with retained candidates, and missing or malformed output. Run whitespace checks; no local project-code execution is needed.

After a separately authorized commit and push makes the workflow available on `main`, run one bounded live search on a GitHub-hosted runner. Inspect the summary, download the artifact, and compare its saved process exit code with `candidates.json` and the final job status. Verify artifact preservation for exit 2 using the live result or a controlled synthetic report in a hosted validation run. Do not force API rate limits to test failures. Record the tested commit, run links, actual results, and any unexercised paths here; distinguish synthetic report validation from live discovery. The existing scanner CI continues to check the Go code.

## Open questions

None. This proposal uses no custom inputs and seven-day artifact retention, matching the existing manual scanner's retention.

## Implementation and validation results

Implemented `.github/workflows/find-candidates.yml` with the manual trigger, pinned actions, read-only permissions, step-scoped job token, fresh output directory, report validation and summary, seven-day artifact upload, and final exit-code handling. Updated the README, search guide, architecture walkthrough and flowchart, and spec 006's runner question. The Go executable, queries, filters, schema, and limits are unchanged.

Statically reviewed action pins against the existing workflows, token scope, output paths, report fields, and the conditions for exit 0, exit 2, missing/malformed output, and upload failure. Tracked and new-file whitespace checks passed. No project code or workflow scripts were executed locally.

Committed and pushed as `0c1bb07931dff1bcbe577b972de91cf292f2a143`. [Scanner CI](https://github.com/zorosz/workflow-hardener/actions/runs/35533849579) passed tests, vet, both executable builds, and all seven demo checks.

The [manual discovery run](https://github.com/zorosz/workflow-hardener/actions/runs/35534151199) on the same commit reached a GitHub rate limit on its first search request. It recorded zero completed queries, zero downloads, zero filtered files, zero candidates, and one `search_failed` issue. Summary validation and artifact upload succeeded before the final step applied exit 2 and marked the job failed. Downloaded and inspected artifact `10612402758`: it contains `candidates.json` (`complete: false`, `exit_code: 2`), matching `exit-code.txt`, the expected count message in `stdout.txt`, and empty `stderr.txt`. Its seven-day expiry is 2026-09-27. This verifies report preservation for a real exit-2 failure; it does not verify successful live discovery or preservation of nonempty candidates.

Live exit 0, nonempty candidates, malformed/missing reports, and upload failures remain unexercised. Those workflow paths received static review; the existing offline Go tests cover candidate retention and discovery errors. The first-request failure needs better rate-limit diagnostics, proposed separately in [spec 009](009-candidate-rate-limit-diagnostics.md).

References: [manual workflow requirements](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow), [step status conditions](https://docs.github.com/en/actions/reference/workflows-and-actions/expressions#status-check-functions), and [artifact upload options](https://github.com/actions/upload-artifact#usage).
