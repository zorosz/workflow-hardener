# 005: Add a repository-local session-review skill

Status: Implemented

## Problem

Session checkpoints need a reusable way to compare progress with the intended goal, identify the most consequential uncertainty, and examine a possible blind spot. Repeating the questions alone does not establish a consistent standard for evidence, candor, or useful next steps.

## Proposed change

Add an instruction-only Codex skill named `session-review` at `.agents/skills/session-review/SKILL.md`. Keep it inside this repository. The skill will be invoked explicitly as `$session-review` and will review the current session rather than perform the work it recommends.

Use `.agents/skills/session-review/agents/openai.yaml` to set:

```yaml
policy:
  allow_implicit_invocation: false
```

This proposal selects explicit invocation so routine development requests do not trigger a session review. The repository skill location and invocation policy follow [OpenAI's skill documentation](https://learn.chatgpt.com/docs/build-skills).

### Proposed skill instructions

```markdown
---
name: session-review
description: Review the current session's progress, main uncertainty, and possible blind spot against its intended goal. Use when explicitly invoked for a candid session checkpoint.
---

# Session review

Review the current session using the conversation and relevant available evidence. Keep the focus on the user's intended outcome, including accepted changes in direction. Do not treat the latest completed task as proof that the overall goal has been achieved.

Use narrow read-only checks when they would materially improve the assessment and existing permissions allow them. Identify the basis of the review and any important missing context. Treat earlier assistant conclusions as claims to assess, not independent evidence.

Address these questions:

1. Where are we now? Compare the intended goal with what is completed, what is verified, and what remains unresolved.
2. What am I least confident about? Choose the most consequential uncertainty. Explain the evidence gap, why it matters, and what would increase or reduce confidence. Distinguish missing context from a demonstrated project problem.
3. What might we be overlooking? Identify one material assumption, tradeoff, or framing issue that deserves attention. Consider whether the current work serves the intended goal. Present a possible blind spot as an inference, not a claim about what the user knows.
4. What should we check next? Recommend the smallest useful check or decision that addresses the uncertainty or possible blind spot. State what its result would help decide.

Be candid and specific. Separate observed facts, inferences, and unknowns. Do not invent criticism to fill a section, assign unsupported numerical confidence, or defend earlier recommendations merely because you made them. If the evidence does not support a meaningful blind spot, say so. Do not mistake passing checks for proof of outcomes they do not measure.

Keep the response short by default: a status paragraph followed by the main uncertainty, one possible blind spot, and a next step. Expand only when the user asks or the situation needs it. Avoid a generic risk inventory or a recap of every action in the session.

Review and recommend; do not edit files, execute project code, launch remediation, or create new tasks solely because the review identifies a next step. Continue to respect the repository's existing permissions and working agreements.
```

The skill will not contain a frozen project status, test counts, commit IDs, or conclusions about this repository. Its assessment should come from the session and current evidence each time it is used.

### Affected files

- `.agents/skills/session-review/SKILL.md`: the reusable review instructions and required name/description frontmatter.
- `.agents/skills/session-review/agents/openai.yaml`: explicit invocation policy.
- `README.md`: a short contributor-facing note near the code documentation, linking the skill and showing `$session-review`. Keep the manual scanner quickstart at the top.
- This spec and `spec/README.md`: track scope and actual validation results.

No scripts, dependencies, global Codex configuration changes, or scanner/CI behavior changes are included.

## Acceptance criteria

- The skill is stored inside the repository under the exact name `session-review`, in Codex's repository skill location.
- `$session-review` is documented as the invocation, with implicit invocation disabled by the policy file.
- The instructions address current status, the most consequential uncertainty, one possible blind spot, and a useful next check.
- Reviews distinguish evidence from inference, expose material missing context, and do not assume the user's knowledge or manufacture criticism.
- Invoking the skill requests an assessment without authorizing implementation of its recommendations.
- The skill stays concise and self-contained, and the README usage note is clearly about Codex rather than a new scanner command.

## Validation plan

- Check frontmatter, the skill name and path, policy syntax, README links, and whitespace with `git diff --check`.
- Run the existing skill-creator frontmatter validator against the completed skill if its Python/YAML dependencies are available. Report any unavailable validation without silently treating it as passed.
- Review the instructions against three situations: substantial missing context; passing CI with an unverified real-world outcome; and no evidence for a meaningful blind spot. Check that the guidance supports a useful, appropriately qualified response without forcing criticism or starting implementation.
- Report whether actual skill discovery/invocation was verified; file validation alone does not prove host discovery or review quality.
- No Go tests are needed for this instruction-only change. Do not execute project code or dispatch CI/scans for validation. Commit and push require separate authorization.

## Open questions

None. This proposal places the skill in the repository and uses explicit invocation.

## Implementation and validation results

- Added `.agents/skills/session-review/SKILL.md` with the approved instructions and `agents/openai.yaml` with implicit invocation disabled. Added the README usage note for `$session-review`.
- Confirmed that the skill text exactly matches the approved proposal. Reviewed the name and description frontmatter, invocation policy, file locations, and README link. Whitespace checks passed for tracked changes and new files.
- Manually reviewed the instructions against missing context, passing CI with an unverified real-world outcome, and no supported blind spot. The instructions distinguish missing evidence from a demonstrated problem, limit conclusions to what checks establish, and allow the review to report no meaningful blind spot. This was an instruction review, not an independent behavioral test.
- Attempted the bundled skill-creator validator; it could not run because the available Python environment lacks the `yaml` module (`ModuleNotFoundError`). No dependencies were installed.
- An explicit `$session-review` invocation loaded the skill and produced a review covering progress, uncertainty, a possible blind spot, and a next check. It used read-only repository checks and made no changes. Discovery in a fresh session has not been separately verified.
- No Go tests were needed for this instruction-only change before commit. No project code was executed locally and no manual repository scan was dispatched.
