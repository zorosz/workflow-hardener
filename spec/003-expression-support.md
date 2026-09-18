# 003: Support context references and fallback expressions

Status: Implemented

## Problem

The detector currently accepts only the exact PR-title and PR-body expressions. Any other expression makes the entire run step unsupported, even when its syntax is a simple context reference. Shell resolution does not remove this limitation, so a scan can still return exit 2 after every shell has been resolved.

The [previously inspected workflow](https://github.com/BKaperick/Bryan-Kaperick.me/blob/0e39b831042020ea373e5f4527c1b8165df38e26/.github/workflows/main.yml) contains `${{ github.head_ref || github.ref_name }}` and `${{ secrets.access_token }}` alongside a separate step with direct PR-body interpolation. These ordinary expressions currently prevent complete analysis under the two existing rules.

## Proposed change

Expand the expression syntax shared by `WH-R001` and `WH-R002`. Keep both rule IDs and their focus on PR titles and bodies. Add no rule for branch names, secrets, or other context values.

### Supported syntax

Accept a context reference or a single-quoted string, optionally followed by one or more `||` alternatives of those same forms:

```text
expression = term ("||" term)*
term       = dotted-context-reference | single-quoted-string
```

- A reference starts with one of these exact lowercase context names: `github`, `env`, `vars`, `secrets`, `inputs`, `steps`, `needs`, `matrix`, `runner`, `job`, or `strategy`. Require at least one property after the context name.
- Each property uses ASCII letters, digits, underscores, or hyphens and starts with a letter or underscore. Dots separate properties without internal whitespace. This is syntax recognition; do not resolve property values or verify their availability at runtime.
- Allow spaces, tabs, CRs, and LFs around terms and `||`. Preserve the current restriction on other whitespace outside string literals.
- Recognize single-quoted strings, including doubled apostrophes (`''`). Expression delimiters and `||` inside these strings are literal text. Locate the closing `}}` with awareness of strings rather than taking its first occurrence.
- Consume the complete expression. Reject missing operands, trailing tokens, unterminated strings or expressions, and nested expression openers outside strings.

The syntax follows GitHub's [expression operators and string literals](https://docs.github.com/en/actions/reference/workflows-and-actions/expressions) and [context property notation](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts). Only the subset above is proposed; expressions are never evaluated.

### Detection and coverage

Within a supported expression, an actual reference to `github.event.pull_request.title` produces `WH-R001`; an actual reference to `github.event.pull_request.body` produces `WH-R002`. This includes references used as `||` alternatives. Inspect every alternative conservatively without deciding which value would be selected at runtime. A PR path written only inside an expression string literal produces no finding.

Other supported references contribute no finding under these two rules. This does not establish that their values are safe for shell interpolation, or trace PR text through variables and outputs. Case variants of the two PR paths remain unsupported rather than becoming a clean non-match. Distinct property names such as `body_extra` are not PR-body matches.

Keep one finding per rule per step. Record each matching expression's original text once per matching rule in its evidence array, even if that expression repeats the same PR reference. Repeated separate expressions retain separate evidence entries. Preserve source locations, rule order, JSON field names, shell resolution, input limits, and exit-code precedence.

Functions, bracket notation, parentheses, wildcards, operators other than `||`, standalone context objects, unknown context roots, and numeric/boolean/null literals remain unsupported. Any unsupported expression still invalidates the entire step for both rules; findings from other supported steps remain in the report. Update the coverage message and report scope to describe this boundary.

### Examples and expected results

These expectations assume a supported Bash/sh shell and no other unsupported input:

| Expression in a run script | Result under the two PR rules |
|---|---|
| `${{ github.head_ref \|\| github.ref_name }}` | Analyzed, no PR finding |
| `${{ secrets.access_token }}` | Analyzed, no PR finding; no secret is read |
| `${{ github.event.pull_request.body \|\| '' }}` | `WH-R002` |
| `${{ github.event.pull_request.title \|\| github.event.pull_request.body }}` | Both rules |
| `${{ 'github.event.pull_request.body' }}` | Analyzed, no PR finding |
| `${{ toJSON(github.event.pull_request.body) }}` | Unsupported, exit 2 |

For the pinned workflow above, static inspection predicts seven analyzed run steps, one `WH-R002` finding, and no unsupported expressions: exit 1 instead of exit 2, assuming successful retrieval and no other errors. The manual GitHub workflow still marks exit 1 as failed because a finding exists. This proposal does not change that reporting behavior.

### Affected files

- `internal/hardener/pr_text.go` and `model.go`: recognize the supported expression subset and update coverage descriptions. A small parsing helper may be added within the same package, using the standard library.
- Detector, scanner, and repository tests, plus `cmd/hardener/main_test.go`: verify expression boundaries, findings, coverage totals, and local/repository parity.
- `testdata/`: update the mixed-container example and add focused inert `.workflow.txt` examples for mixed contexts and PR fallbacks. Retain examples that demonstrate incomplete analysis using syntax still outside scope.
- `README.md` and `docs/ARCHITECTURE.md`: document supported expressions, the parsing flow, remaining limitations, and changed example results.

## Acceptance criteria

- The branch fallback and secret-reference examples above no longer cause `unsupported_expression`.
- Direct PR expressions remain detectable when the same step also contains supported non-PR expressions, in either order.
- Both rules detect their PR reference in any `||` alternative, including a chain containing both fields. Finding grouping, evidence, and source attribution follow the behavior specified above.
- String contents, including quoted `}}`, `||`, `${{`, and escaped apostrophes, do not create false references or premature expression boundaries.
- Unsupported syntax and malformed expressions still produce exit 2 without findings from that step. Findings from supported steps are retained.
- The synthetic mixed-container example analyzes all seven run steps and retains one body finding with exit 1. Include a synthetic secret reference without any secret value or operational target script.
- Environment-variable examples retain their existing results. No new dependency, rule, application package, secret access, or script execution is introduced.

## Validation plan

- Review the parser and tests before execution. Check formatting and run `git diff --check`.
- Add focused cases for each supported term, fallback chains, both PR fields, repeated evidence, strings containing delimiters, malformed boundaries, case variants, and unsupported syntax. Include a long expression within the existing input limit to guard against truncation or an unbounded parsing strategy.
- Verify complete and incomplete analysis through scanner tests, simulated GitHub HTTP responses, and compiled CLI fixtures. Update expectations that currently treat every non-PR expression as unsupported while preserving genuine unsupported cases.
- Run tests, vet, build, and the existing demos in the approved GitHub-hosted Scanner CI after separately authorized commit/push. Preserve pinned actions, read-only permissions, and unpersisted checkout credentials.
- Do not execute project code or target scripts locally. Report unrun checks explicitly.

## Open questions

None. The proposed syntax boundary is explicit; broader GitHub expression support would require another change request.

## Implementation and validation results

- Added a forward-only expression parser in `internal/hardener/expressions.go` and connected it to both existing rules. It recognizes the approved context references, quoted strings, and `||` alternatives without evaluating values.
- Preserved whole-step unsupported results, finding grouping, evidence order, source locations, shell resolution, input limits, and both rule IDs. Updated the report scope and coverage message.
- Added focused expression tests and three synthetic fixtures, bringing compiled CLI coverage to twenty-one examples. Updated the mixed-container expectations to seven analyzed steps, one body finding, and exit 1. Retained partial-coverage checks using unsupported function syntax and added local/repository parity checks.
- Updated the README and code walkthrough to explain expression parsing, fallback findings, and remaining coverage limits.
- Reviewed the source and test changes. `gofmt -l` reported no formatting differences, and whitespace checks found no issues.
- Tests, vet, build, and the seven existing demos have not run for this change. GitHub CI validation is pending separately authorized commit/push. No project code or target scripts were executed locally.
