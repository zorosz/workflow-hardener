# 009: Make candidate-search rate limits actionable

Status: Implemented

## Problem

Candidate discovery stops on a rate-limit response with only `GitHub API rate limit reached; retry later`. The report loses the HTTP status, affected rate-limit resource, remaining allowance, and any reset or retry time. A failure on the first query therefore cannot explain when another run is appropriate or which limit was reached.

The [first manual run](https://github.com/zorosz/workflow-hardener/actions/runs/35534151199) returned exit 2 before completing any query or downloading files. Its report and saved process code were preserved correctly. The available diagnostics do not establish the precise rate-limit cause. This is separate from sampling omissions and from the previously successful authentication probe.

## Proposed change

Improve rate-limit messages in candidate discovery's existing HTTP helper. Include the HTTP status and validated values from `X-RateLimit-Resource`, `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`, and `Retry-After` when available. Render reset times in UTC and retry delays in seconds. These details remain in the existing issue `message`, so the JSON schema and Actions summary need no new fields.

Parse header values with bounded lengths and numeric ranges; emit canonical values only. Allow known resource names (`core`, `search`, `code_search`); describe other names as unknown. Ignore missing or malformed values without echoing their raw text. Keep a generic rate-limit message when no useful details are available. Do not infer a primary or secondary limit from an HTTP status alone or claim that waiting guarantees success. Never include request headers, credentials, or raw response bodies.

Continue stopping after the failure, preserving candidates and exit 2. Keep all four queries, pacing, request/download limits, deadlines, authentication, and scanner behavior unchanged. Automatic retries, extra rate-limit API calls, additional token types, and permission changes are outside this change.

Update `docs/SEARCHING.md` with the diagnostic fields and guidance to respect GitHub's reported reset/retry time before rerunning. If neither time is available, explain that the precise retry time is unknown and GitHub recommends waiting at least one minute for secondary limits. Update the README's launch guidance and `docs/ARCHITECTURE.md` alongside the implementation.

Affected files: `internal/hardener/candidates.go`, `internal/hardener/candidates_test.go`, `README.md`, `docs/SEARCHING.md`, `docs/ARCHITECTURE.md`, this spec, and the spec index.

## Acceptance criteria

- A rate-limit response with valid headers produces an actionable message in `candidates.json` and the existing Actions summary, including its HTTP status and available resource/count/time details.
- Missing, invalid, oversized, or unexpected header values do not leak raw text, cause a panic, or conceal the original failure.
- A first-query failure leaves all queries unfinished and makes no downloads; a later failure retains earlier candidates. Both return exit 2 without retrying.
- Authentication failures and other HTTP errors remain distinguishable from recognized rate-limit responses.
- Documentation describes the new diagnostics and their limits without promising that a rerun will succeed.

## Validation plan

Add focused fake-HTTP tests for exhausted allowance, retry delays, combined headers, missing/malformed/oversized values, unexpected resource names, and synthetic sensitive text in headers and bodies. Check that request counts, retained candidates, and exit codes remain unchanged. Review code and whitespace before execution; run tests through the approved GitHub-hosted CI after separately authorized commit/push. Do not execute project code locally or deliberately exhaust GitHub's API limits. A later live run may confirm recovery, but a successful search cannot be guaranteed by this diagnostics change.

References: [GitHub REST rate limits and response headers](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api), [code-search request limits](https://docs.github.com/en/rest/search/search#rate-limit).

## Open questions

None. This change improves diagnosis and manual retry guidance; it does not introduce automatic retries.

## Implementation and validation results

Implemented `candidateRateLimitError` and bounded numeric-header parsing in the existing candidate HTTP helper. Recognized rate-limit failures now report their HTTP status, known resource or `unknown`, valid counts, UTC reset timestamp, and retry delay when supplied. Missing usable timing is explicit. These remain ordinary issue messages, displayed by the existing Actions summary without a schema or workflow change.

Numeric headers accept exactly one value containing 1–12 ASCII digits. Counts and delays are bounded to 2,147,483,647; reset timestamps are bounded to 253,402,300,799 (the end of year 9999). Malformed, duplicate, and oversized numeric values are omitted. The existing rate-limit classification, stop behavior, request bounds, authentication, and exit codes are unchanged.

Added fake-HTTP cases covering exhausted allowance, retry delays, combined and missing headers, canonical formatting, numeric boundaries, malformed/oversized/duplicate values, unknown resources, and synthetic sensitive text. Extended partial-result tests for later search and blob rate limits and other HTTP errors. Updated the README, search guide, and architecture walkthrough.

Reviewed the code, test expectations, report/summary compatibility, and documentation. `gofmt` completed and its follow-up formatting check was clean; tracked and new-spec whitespace checks passed. No project code was executed locally.

Committed and pushed as `ed5f1b955909104a590c6e8fd13329a28255d405`. [Scanner CI](https://github.com/zorosz/workflow-hardener/actions/runs/35535309639) passed tests, vet, both executable builds, and all seven demo checks on that commit. The new diagnostics were exercised through fake HTTP responses in the test suite.

The [live discovery run](https://github.com/zorosz/workflow-hardener/actions/runs/35536367731) at `c940b8d2a90d3c7b7eae8e92c29b617fe6245b10` exercised the new diagnostics against GitHub. Its first search returned HTTP 429 with `resource=code_search`, `limit=10`, `remaining=10`, reset `2026-09-20T20:43:14Z`, and a four-second retry delay. All four queries remained unfinished and no files were downloaded. Summary validation and artifact upload succeeded before the final step applied exit 2. Downloaded artifact `10613221999` and verified those fields in `candidates.json`, a matching `exit-code.txt`, the expected stdout summary, and empty stderr. This validates live rate-limit reporting and artifact preservation; successful live discovery remains unverified. A bounded retry is proposed separately in [spec 010](010-bounded-search-retry.md).
