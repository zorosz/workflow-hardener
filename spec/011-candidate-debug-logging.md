# 011: Add optional candidate-discovery debug logging

Status: Implemented

## Problem

Candidate discovery reports selected rate-limit headers but discards GitHub's error response body. HTTP 429 with an unused reported allowance and a short retry delay does not explain the precise rejection. Capturing GitHub's own error message and request identifier can improve diagnosis before changing retry behavior, although GitHub may still return only a generic explanation.

## Proposed change

Add `find-candidates --debug`, disabled by default, and a **Debug logging** boolean input on **Actions -> Find candidates -> Run workflow**. Pass the input through an environment variable and choose the literal CLI flag in the shell. Keep credentials scoped to the existing discovery step. This change can be implemented independently of the approved retry proposal in spec 010; it adds no retries or additional API requests.

When enabled, write bounded JSON-lines diagnostic records to stderr for candidate HTTP requests. Record the UTC timestamp, constructed endpoint and fixed search query where applicable, elapsed milliseconds, HTTP status or a sanitized transport failure category, and selected response metadata. Metadata includes GitHub's request ID, a parsed server date, and the validated rate-limit values already supported by spec 009. Accept a request ID only as a single value of at most 128 ASCII letters, digits, colons, or hyphens; omit invalid values.

For non-success responses, read at most 8 KiB plus one byte to detect overflow, under the existing request timeout and overall deadline. Decode only the top-level JSON `message` string. Redact the active job token before shortening the decoded message to at most 1,024 characters, then JSON-encode it so embedded newlines and control characters remain text. Omit all other body fields. Missing, malformed, oversized, or unreadable bodies produce a fixed diagnostic explaining that the message is unavailable, without copying raw bytes or replacing the original API failure. Do not log request headers, cookies, environment dumps, successful response bodies, workflow contents, or raw transport errors. Bound total debug output to 64 KiB and indicate truncation within that budget.

Use the existing `stderr.txt` in the **candidate-search** artifact. When debug mode is enabled, also show its bounded diagnostic output in the discovery step log; point to it from the summary. Preserve summary/artifact handling before applying exit 2. Disabled mode retains current behavior and does not read error bodies for debugging. Debug collection must not create a clean result from failed or incomplete discovery, or replace an existing operational/API error with a diagnostic error.

Update CLI help, the README launch instructions, `docs/SEARCHING.md`, and `docs/ARCHITECTURE.md` to explain the checkbox/flag, output location, limits, and what the logs can establish. Use Go's standard library and keep the existing application package. The query/filter scope, report schema, authentication, request limits, and scanner remain unchanged.

Affected files: `internal/hardener/candidates.go`, `internal/hardener/candidates_test.go`, `.github/workflows/find-candidates.yml`, `README.md`, `docs/SEARCHING.md`, `docs/ARCHITECTURE.md`, this spec, and the spec index. Adjust `cmd/find-candidates/main.go` only if option/writer plumbing requires it.

## Acceptance criteria

- Debug logging is off by default and can be enabled through the Go CLI or the Actions checkbox.
- A synthetic HTTP 429 containing a JSON error message produces diagnostics with that message, a valid request ID, status, timing, and rate-limit details in stderr and the existing artifact/log output.
- Tokens, request headers, successful response bodies, and workflow contents are absent. Sensitive text is redacted before truncation; response text cannot inject extra log lines or Actions commands.
- Missing or malformed diagnostic data remains visibly unavailable; output and body-read limits are enforced. Debug failures preserve the original failure and exit 2.
- Request counts, destinations, authentication, candidate findings, and completeness semantics remain unchanged. No retry is introduced by this change.
- Documentation clearly distinguishes observed GitHub error messages from an inferred internal cause.

## Validation plan

Add focused fake-HTTP and CLI tests for enabled/disabled output, the observed 429 shape with a synthetic JSON message, safe request IDs, token redaction including escaped JSON and truncation boundaries, control characters, malformed/oversized/read-failed bodies, bounded output, diagnostic-writer failures, and preservation of normal reports and request counts. Review the workflow input handling and JSON-lines display before execution. Run formatting and whitespace checks, then tests/vet/builds in approved GitHub-hosted CI after separately authorized commit/push. A later manual run with debug enabled can capture GitHub's current explanation; do not deliberately exhaust rate limits or execute project code locally.

References: [GitHub REST troubleshooting](https://docs.github.com/en/rest/using-the-rest-api/troubleshooting-the-rest-api#rate-limit-errors), [Go HTTP debugging helpers](https://pkg.go.dev/net/http/httputil).

## Open questions

None. The proposal uses a CLI flag and an Actions checkbox, with diagnostics retained in the existing artifact.

## Implementation and validation results

Implemented the optional `--debug` flag and the default-off Actions checkbox. Candidate HTTP requests emit JSON-lines diagnostics with timestamps, constructed destinations, elapsed time, status or fixed error categories, and selected validated metadata. Error-body messages are bounded, decoded, redacted before truncation, and kept out of the normal candidate report. Diagnostic read failures preserve the original API error; output failures stop logging and produce a fixed stdout notice without changing the discovery result.

The existing `stderr.txt` artifact retains diagnostics. The workflow displays it only when debug is enabled, temporarily disabling Actions command processing with a fresh kernel-generated UUID and restoring processing afterward. If a marker cannot be read, diagnostics remain in the artifact without being displayed. The summary points to both locations. Updated CLI help, README instructions, the search guide, and the architecture walkthrough. The report schema and discovery request/retry behavior are unchanged; spec 010 remains unimplemented.

Added fake-HTTP and CLI cases for the observed 429 shape, enabled/disabled behavior, unchanged reports and request counts, successful-body omission, token redaction on search and anonymous blob paths, JSON escapes and truncation boundaries, invalid request IDs, error-body/output bounds, cancellation, and diagnostic read/write failures.

Reviewed code, test expectations, workflow input handling, command-processing protection, and documentation. `gofmt` and its follow-up formatting check passed, as did tracked/new-spec whitespace checks. No project code or workflow scripts were executed locally. Tests, vet, builds, hosted CI, and the live debug run remain unrun for this change; hosted validation requires separately authorized commit/push. The checkbox is not available on GitHub until the workflow change is pushed.
