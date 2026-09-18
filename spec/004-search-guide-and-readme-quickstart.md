# 004: Document search queries and surface manual scanning

Status: Implemented

## Problem

The README places the manual GitHub scan instructions after the problem description, demos, and local scanning details. The GitHub search queries for finding candidate repositories are not documented in the repository. Readers need a visible starting point for running a scan and a reusable guide for choosing repositories to inspect.

## Proposed change

### README quickstart

Make **Run entirely from GitHub** the first section after the title and introductory paragraph, before **The problem**. Lead with these steps:

1. Open [Actions > Scan public repository](https://github.com/zorosz/workflow-hardener/actions/workflows/scan-repository.yml).
2. Choose **Run workflow**, select `main`, and enter a public repository URL or `OWNER/REPO` in the **repository** field.
3. Start the workflow and open its run summary to review findings and coverage/errors.
4. Download the **repository-scan** artifact for `report.json`, `stderr.txt`, and `exit-code.txt`.

State briefly that dispatch requires write access to the scanner repository and that exit 1 (findings) and exit 2 (incomplete analysis/errors) both mark the run failed. Link to the search guide and the detailed reporting explanation. The instructions follow [GitHub's manual workflow documentation](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow) and the existing `scan-repository.yml` input and artifact names.

Keep the existing detailed GitHub execution and reporting information further down under **GitHub scan details**, with a link back to the quickstart instead of a duplicate checklist. Keep artifact retention and summary limits documented. Move the PowerShell environment-variable example into **Scan a public repository**, alongside the other local CLI examples. Retain the existing `#run-entirely-from-github` anchor on the section moved to the top.

### Search guide

Add `docs/SEARCHING.md`, linked from the README quickstart. Include these three copyable, single-line queries with a short explanation and examples of the layouts targeted by the narrower queries.

Broad candidate search:

```text
path:.github/workflows/ (content:"github.event.pull_request.title" OR content:"github.event.pull_request.body") content:"shell: bash"
```

Explicit Bash immediately followed by a one-line `run` containing a direct PR expression:

```text
path:/^\.github\/workflows\/[^\/]+\.ya?ml$/ content:/(?-i)shell:[ \t]*bash[ \t]*\r?\n[ \t]+run:[^\r\n]*\$\{\{[ \t]*github\.event\.pull_request\.(title|body)[ \t]*\}\}/ NOT is:fork NOT is:archived
```

Explicit Bash immediately followed by `run: |` or `run: >`, with a direct PR expression in the first eight following lines:

```text
path:/^\.github\/workflows\/[^\/]+\.ya?ml$/ content:/(?-i)shell:[ \t]*bash[ \t]*\r?\n[ \t]+run:[ \t]*[|>][-+]?[ \t]*\r?\n([^\n]*\n){0,7}[^\n]*\$\{\{[ \t]*github\.event\.pull_request\.(title|body)[ \t]*\}\}/ NOT is:fork NOT is:archived
```

Explain that these queries are for GitHub's web code search and require signing in. The broad query combines matches at file level; the narrower queries constrain textual layout and exclude forks and archived repositories. They are candidate filters, not YAML analysis or confirmed scanner findings. The multiline pattern can cross step boundaries. The narrower queries miss inherited shells, `sh`, different field ordering, quoted shell values, longer scripts, and fallback expressions.

Show how to select a public repository from the results and pass its repository URL or `OWNER/REPO` to the manual scan. A file/blob URL is not accepted. Clarify that a finding may coexist with exit 2 elsewhere in the repository and that a search match does not establish exploitability. Do not claim a measured positive-match rate or that every search result is public.

Link GitHub's [code-search syntax](https://docs.github.com/en/search-github/github-code-search/understanding-github-code-search-syntax) and [code-search access and limitations](https://docs.github.com/en/search-github/github-code-search/about-github-code-search). No scanner, workflow, or dependency changes are included.

### Affected files

- `README.md`: put the manual quickstart first, link the search guide, and reorganize existing details.
- `docs/SEARCHING.md`: document the queries, examples, limitations, and search-to-scan steps.
- This spec and `spec/README.md`: track the change and its actual validation results.

## Acceptance criteria

- Manual GitHub scan instructions appear immediately after the README introduction, before the problem description and flowchart.
- The quickstart links directly to the correct workflow and names its existing input, summary, and artifact correctly.
- All three queries can be copied intact from the search guide, with the two narrower queries distinguished by script layout.
- The guide explains the tradeoff between narrower results and missed cases, including the multiline boundary limitation, without promising only positive findings.
- The README links to the guide and retains reporting details, local CLI instructions, and the existing manual-run anchor without duplicating the checklist.

## Validation plan

- Review Markdown, links, anchors, query escaping, and code fences. Compare the workflow instructions with `scan-repository.yml`.
- Check query syntax against the official GitHub documentation. Report live search results as unverified unless an authenticated search is actually inspected.
- Run `git diff --check` and inspect a rendered Markdown preview if available; otherwise report visual verification as outstanding.
- No Go tests are needed for this documentation-only change. Do not execute project code or dispatch a scan to validate documentation. Commit and push require separate authorization.

## Open questions

None. The manual run means the existing **Scan public repository** workflow; the **Scanner CI** demo instructions remain available separately.

## Implementation and validation results

- Moved the manual GitHub scan checklist to the first README section, with a direct workflow link, result meanings, and links to the search guide and reporting details. Retained the existing manual-run anchor.
- Kept artifact retention, summary limits, and workflow execution details under `GitHub scan details`. Moved the PowerShell environment-variable example alongside the local repository CLI examples.
- Added `docs/SEARCHING.md` with all three approved queries, matching layout examples, limitations, and instructions for passing a public repository from search results to the scanner.
- Reviewed the documentation against the existing workflow and GitHub's documentation. Confirmed that all three query lines exactly match this spec; checked README section order, linked headings, relative file links, code-fence pairs, and whitespace.
- Rendered Markdown and authenticated live search results were not inspected. Go tests were not run because this is a documentation-only change. No project code or target scripts were executed, and no scan was dispatched.
