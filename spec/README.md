# Change requests

This folder holds small, reviewable descriptions of user-requested changes before implementation. Start with [TEMPLATE.md](TEMPLATE.md) and use the next available number in a filename such as `001-readme-flowchart.md`.

## Process

1. Investigate the request without changing its implementation. Draft a spec that explains the problem, proposed changes, affected files, acceptance criteria, and planned validation. Describe the technical problem directly; omit conversational history and statements about who requested the change. Note any questions that affect scope.
2. Set the status to `Proposed`, link the spec in the conversation, and ask the owner to review it. Do not implement while review is pending. The initial request authorizes drafting, not implementation of the resulting spec.
3. Revise the spec as needed. An explicit reply such as "Approve spec 001" authorizes that version's scope. Set the status to `Approved` and implement it without asking for the same approval again.
4. If a material change to scope is needed, update the spec, return it to `Proposed`, and request another review before implementing the added scope.
5. Record the actual validation results in the same file. Set the status to `Implemented` when the approved work is complete, clearly identifying any checks that were not run. Specs do not grant permission to execute project code locally; the existing execution rules still apply.

Read-only investigation, spec drafting and revisions, and the owner's explicitly requested setup of this process do not require a separate spec. Keep one file per change request; do not create separate approval logs or research stages. Approval to implement does not authorize committing or pushing.

## Requests

- [001: Make the README flowchart vertical](001-readme-flowchart.md)
- [002: Resolve Bash and sh shells for the existing PR rules](002-shell-resolution.md)
- [003: Support context references and fallback expressions](003-expression-support.md)
- [004: Document search queries and surface manual scanning](004-search-guide-and-readme-quickstart.md)
- [005: Add a repository-local session-review skill](005-session-review-skill.md)
- [006: Automate candidate repository search](006-automated-candidate-search.md)
- [007: Require documentation updates for architecture changes](007-architecture-documentation.md)
- [008: Run candidate discovery from the GitHub web UI](008-manual-candidate-search.md)
- [009: Make candidate-search rate limits actionable](009-candidate-rate-limit-diagnostics.md)

Each request's file contains its current status.
