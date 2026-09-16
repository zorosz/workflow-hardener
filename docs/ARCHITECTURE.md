# Code walkthrough

The program has one executable and one application package. Start with the direct-title example and follow the calls below.

```mermaid
flowchart TD
    MAIN[main] --> CLI[Run: parse arguments]
    CLI --> SCAN[Scan: process requested files]
    SCAN --> ONE[scanFile]
    ONE --> READ[Inputs.Read: read bytes]
    READ --> PARSE[ParseWorkflow: extract steps]
    PARSE --> RULE[AnalyzePRTitle: detect the pattern]
    RULE --> RESULT[FileResult: findings and coverage]
    RESULT --> REPORT[Run: write JSON and return exit code]
```

## Files from input to result

| File | Responsibility | Go concepts |
|---|---|---|
| [`main.go`](../cmd/hardener/main.go) | Passes arguments and output streams to `Run`, then exits with its result. | Packages, imports, function calls |
| [`cli.go`](../internal/hardener/cli.go) | Reads `scan`, `--root`, and repeatable `--file` flags; writes the report. | Standard `flag` package, slices, `io.Writer`, error handling |
| [`model.go`](../internal/hardener/model.go) | Defines the workflow, step, finding, and result structures. | Structs, methods, JSON field tags |
| [`input.go`](../internal/hardener/input.go) | Reads bounded files beneath an input root and rejects links and unsafe paths. | File I/O, `os.Root`, `defer`, explicit errors |
| [`workflow.go`](../internal/hardener/workflow.go) | Walks YAML nodes to extract jobs, run steps, shells, and locations. | External packages, maps, slices, recursion |
| [`pr_title.go`](../internal/hardener/pr_title.go) | Checks the shell and recognizes the supported title expression. | Loops, string operations, early returns |
| [`scan.go`](../internal/hardener/scan.go) | Connects reading, parsing, and detection; combines file results and exit codes. | Composition, sorting, `switch` |
| [`report.go`](../internal/hardener/report.go) | Writes indented JSON and limits diagnostic text. | `encoding/json`, interfaces, UTF-8 strings |

[`attrs_windows.go`](../internal/hardener/attrs_windows.go) and [`attrs_other.go`](../internal/hardener/attrs_other.go) provide the small platform-specific link checks. Go chooses the applicable file when building. The module path still matches the repository's name.

## Follow one example

For `hardener scan --file testdata/risky-title.workflow.txt`:

1. `main` calls `Run`. It parses the flags and opens the current directory as the input root.
2. `Scan` validates and sorts the requested filenames, then calls `scanFile` for each.
3. `Inputs.Read` checks the path and size and reads the file. It never runs the script.
4. `ParseWorkflow` extracts the `inspect` job's first step, its `bash` shell, its decoded `run` text, and the run value's source location.
5. `AnalyzePRTitle` finds the exact title expression. It returns one finding and records one analyzed step.
6. `scanFile` sets the status to `match`. `Scan` combines the totals, and `Run` writes JSON and returns exit 1.

With [the environment-variable example](../testdata/env-title.workflow.txt), the expression is outside `run`, so the supported step produces no finding and exit 0.

With [the inherited-shell example](../testdata/default-shell-title.workflow.txt), the step omits its own shell. The detector records `unsupported_shell`, and the scan exits 2. It does not infer the workflow-level default. An unsupported step and a finding can coexist; incomplete analysis takes precedence in the exit code.

## Tests

Go's `_test.go` files stay next to the code and are excluded from the normal executable. Table-driven tests describe several inputs and expected outcomes using a slice and a loop.

- Input and parser tests check file boundaries, malformed YAML, step extraction, and source locations.
- Detector tests check expression syntax, shell restrictions, repeated occurrences, and unsupported cases.
- Scanner and CLI tests check combined results, argument handling, and output errors.
- [`main_test.go`](../cmd/hardener/main_test.go) builds the real executable and checks the ten synthetic examples and process exit codes. It does not execute workflow scripts.

To understand the project incrementally, read `main.go` and `cli.go` first, follow the example above, then read the detector alongside its table-driven tests. The file checks and YAML validation can be studied after that central path is clear.
