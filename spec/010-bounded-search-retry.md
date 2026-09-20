# 010: Retry a briefly throttled candidate search once

Status: Approved

## Problem

Candidate discovery stops immediately on every rate-limit response, even when GitHub requests a short pause. The [manual run with diagnostics](https://github.com/zorosz/workflow-hardener/actions/runs/35536367731) received HTTP 429 on its first query with `resource=code_search`, `limit=10`, `remaining=10`, and `retry_after=4 seconds`. No query completed and no files were downloaded. These headers are consistent with temporary throttling while the reported allowance remains available; they do not establish GitHub's exact internal cause or guarantee that a retry will succeed.

## Proposed change

Allow at most **one additional authenticated search request per discovery run**, retrying the same query after a short, explicitly requested delay. This is one shared retry allowance across the four queries, not one retry per query. At most five search requests may be sent in total.

Retry only a rate-limit response already recognized by candidate discovery when it contains a valid numeric `Retry-After` header. Reuse the bounded header validation from spec 009. Wait at least the greater of the existing six-second search interval and the reported retry delay plus one second. If a valid `X-RateLimit-Remaining` is zero, also respect a valid reset timestamp plus one second; if that reset timestamp is missing or invalid, stop instead. A future reset timestamp does not require waiting for the full window when a positive allowance remains.

The calculated wait must be at most 30 seconds and leave at least one full 15-second request timeout within the existing two-minute discovery deadline. If either condition is not met, report why no retry was made and return exit 2 with the original rate-limit details. Make the wait cancellable and close the first response before waiting. Missing/malformed timing, further rate limits after the shared retry is used, and all other API/transport/metadata errors stop discovery. Anonymous blob downloads are not retried.

If the retry succeeds, process its result normally and continue discovery. A recovered response does not become a permanent error: exit 0 is possible only if all sampled work ultimately completes without omissions or other errors. If recovery fails or the wait is canceled, preserve earlier candidates, report the final failure, and return exit 2. Never mark an unfinished query completed.

Add a top-level integer `search_retries` to `candidates.json`, always 0 or 1, counting additional search requests actually sent. Show and validate this count in the existing Actions summary so recovery is visible without treating it as an unresolved issue. Waiting without sending another request leaves the counter at zero. Preserve artifact upload before the final exit code is applied.

Update rate-limit guidance and diagnostic wording to distinguish the reset of an exhausted allowance from a short `Retry-After` delay with allowance remaining. Keep the four queries, filters, page/download limits, response bounds, authentication, permissions, and two-minute deadline. Add no supplied secrets, extra rate-limit API requests, schedules, scanner rules, or automatic blob retries.

Affected files: `internal/hardener/candidates.go`, `internal/hardener/candidates_test.go`, `.github/workflows/find-candidates.yml`, `README.md`, `docs/SEARCHING.md`, `docs/ARCHITECTURE.md`, this spec, and the spec index.

## Acceptance criteria

- A first-query HTTP 429 with `remaining=10`, `Retry-After: 4`, and a later reset timestamp waits at least six seconds, retries that query once, and continues if it receives valid results. The report and summary show `search_retries: 1`.
- At most one extra search request is sent across the whole discovery run. A second rate-limit response stops work with exit 2 and retains prior candidates.
- An exhausted allowance respects both the retry delay and reset time. Missing/invalid required headers, a wait over 30 seconds, insufficient remaining deadline, or cancellation cannot cause an early retry or a clean result.
- Authentication, server, transport, metadata, and blob-download failures are not retried. Recovered searches still report sampling omissions and other unresolved issues as exit 2.
- The summary accepts only a retry count of 0 or 1, displays it, and continues preserving reports on exit 2. Documentation describes the bounded behavior and makes no promise of recovery.

## Validation plan

Use fake HTTP responses with injected time and waiting so tests make no live requests or real delays. Cover the observed 429 followed by success, retry exhaustion across queries, partial-result retention, positive versus zero allowance, reset/delay combinations, exact wait/deadline boundaries, missing/malformed/duplicate/oversized headers, cancellation, and errors that must not retry. Assert query identity, request counts, retry counters, and exit codes. Retain the existing credential and destination checks.

Review generated code, workflow summary validation, and documentation before execution; run formatting and whitespace checks. Run tests, vet, and builds in the approved GitHub-hosted CI after separately authorized commit/push. A later manual discovery run can check live recovery and summary/artifact output; record whether GitHub actually exercised the retry path. Do not deliberately exhaust API limits or run project code locally.

References: [GitHub rate-limit recovery](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#exceeding-the-rate-limit), [handling rate-limit errors](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api#handle-rate-limit-errors-appropriately).

## Open questions

None. The proposal uses one retry per run, a maximum 30-second wait, and only explicit valid retry hints.

## Implementation and validation results

Not implemented.
