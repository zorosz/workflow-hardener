# 007: Require documentation updates for architecture changes

Status: Implemented

## Problem

The working agreements require the code walkthrough to stay aligned with the implementation, but do not explicitly require documentation updates whenever the architecture changes.

## Proposed change

Keep the instruction to read the README and relevant code and tests before editing. Replace its walkthrough sentence with a separate bullet in `AGENTS.md`:

> Always update documentation when the architecture is modified. Update `docs/ARCHITECTURE.md` and any affected README or usage guides in the same change so they remain aligned with the implementation.

Affected files: `AGENTS.md`, this spec, and `spec/README.md`. This changes agent instructions only.

## Acceptance criteria

- `AGENTS.md` explicitly requires documentation updates whenever architecture changes.
- The instruction names the architecture walkthrough and includes other affected documentation in the same change.

## Validation plan

Review the wording and run `git diff --check`. No project code execution is needed.

## Open questions

None.

## Implementation and validation results

Added the approved instruction to `AGENTS.md` and retained the requirement to read the README and relevant code and tests before editing. Reviewed the wording against this spec. Tracked and new-file whitespace checks passed. No project code or tests were run because this change only updates instructions and documentation.
