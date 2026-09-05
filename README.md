# deintroverter4go

An exploratory static analyzer for Go tests, inspired by
[Uncle Bob's deintroverter4clj](https://github.com/unclebob/deintroverter4clj).
It asks whether an assertion depends on production code, or merely checks
literals and values constructed by the test itself.

```go
// extroverted: the failure condition uses a production result.
got := Add(1, 2)
if got != 3 {
    t.Fatalf("got %d, want 3", got)
}

// introverted: calling production code is insufficient on its own.
Add(1, 2)
if 1+2 != 3 {
    t.Fatal("bad arithmetic")
}
```

This is a first implementation, not a feature-complete port. Use it to guide
manual review. An extroverted verdict does not prove a test is useful, that
an assertion executes, or that a test catches bugs. Mutation testing addresses
that last question more directly.

## Run

Requires Go 1.27 or newer and a target project that can be loaded by Go tooling.
From this checkout:

```sh
go run . -dir /path/to/your/module ./...
go run . -dir /path/to/your/module -verbose ./...
go run . -dir /path/to/your/module -format json ./...
```

Or install locally with `go install .`, then use `deintroverter4go` from a Go
module. The binary is installed in `GOBIN` (or `~/go/bin` by default).

Arguments are Go package patterns, defaulting to `./...`. Put flags before
patterns. Package loading follows the active Go build configuration and may
download target dependencies; test bodies are never executed. Use `GOFLAGS`
for build tags if needed. Nested modules require separate invocations.

Text output hides extroverted tests unless `-verbose` is supplied. JSON always
includes all findings, with file, line, test name, verdict, and reason.
Verdicts do not cause a nonzero exit status. Loading/output errors exit 1;
invalid flags or format exit 2.

## Verdicts

| Verdict | Meaning |
| --- | --- |
| `extroverted` | At least one recognized assertion traces to production code. |
| `introverted` | Recognized assertions have no traced production dependency. |
| `cloistered` | Assertions reach test helpers or test declarations, whose bodies are not followed. |
| `questionable` | No recognized assertions, or analysis encountered uncertainty on the assertion path. |

Production means functions, methods, constants, and package variables in the
loaded package's module, excluding test declarations. Standard-library and
third-party calls do not themselves establish production reach, but production
values passed through them retain their origin. Types alone do not count.

## What it handles

- `TestXxx(*testing.T)` functions, including external `package foo_test` tests.
- Local assignments, derived values, shadowing, and overwritten variables.
- `if` conditions leading to `t.Error`, `t.Errorf`, `t.Fatal`, `t.Fatalf`,
  `t.Fail`, and `t.FailNow`. Diagnostic message arguments do not count.
- Testify `assert` and `require` calls, including import aliases and assertion
  objects. Variadic diagnostic messages do not count.
- Inline `t.Run` callbacks, reported separately and with captured local values.
  A parent containing only subtests is omitted. Dynamic names use `<dynamic>`.

Symbol resolution uses `golang.org/x/tools/go/packages`, so import aliases,
package boundaries, and local shadowing are resolved through Go type information.
The regression fixture uses a small Testify signature stub; it does not exercise
the real assertion library at runtime.

## Known limits

This is a syntax-based provenance analysis, not full control-flow or alias
analysis. Origins are coarse: an aggregate shares the origins of its components,
and a call result inherits its argument origins even if the function discards
them. A production reference in a condition does not prove it affects failure.
Conditional reachability and early returns are not proven.

Loops and some opaque calls or mutations make findings questionable. Helper
bodies, pointer effects, closures, deferred/concurrent assertions, switches,
selects, Testify suites, custom assertion frameworks, fuzz tests, and examples
are not fully analyzed. Assertions embedded in expression positions (such as
`if assert.Equal(...)`) are not recognized. Subtest reachability is not proven.
Production outside the current module is not inferred as the system under test.
These limits can produce false positives and false negatives; review the code
behind each finding. This tool is not intended as a CI quality gate.

## Code layout

The root `main` package handles CLI flags, exit codes (`main.go`), and report
formatting (`report.go`). Analysis lives in `internal/analyzer`, exposing only
`Analyze` and `Finding`:

- `analyzer.go`: package loading and test discovery.
- `provenance.go`: symbol resolution and value origins.
- `statements.go`: assignments and control-flow traversal.
- `assertions.go`: assertion and subtest recognition.
- `findings.go`: per-test analysis state and verdict classification.

Analyzer tests live alongside the package; report tests stay with the CLI.

## Validate

```sh
go test ./...
go vet ./...
```

GitHub Actions runs tests and `go vet` on pushes and pull requests, using
the Go version in `go.mod`.

Related tool: [testifylint](https://github.com/Antonboom/testifylint) checks
Testify assertion usage. Its scope differs from tracing assertions to production
code. No direct Go equivalent was found in the initial search; that is not proof
that none exists.
