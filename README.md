# deintroverter4go

An exploratory static analyzer for Go tests, inspired by
[Uncle Bob's deintroverter4clj](https://github.com/unclebob/deintroverter4clj).
It asks whether each assertion depends on production code, or merely checks
literals and values constructed by the test itself.

```go
got := Add(1, 2)
require.Equal(t, 3, got) // extroverted
require.Equal(t, 3, 3)   // introverted, still reported in this mixed test
```

Use the report to guide manual review. Production provenance does not prove that
an assertion executes, that the expected value is independent, or that the test
catches bugs. Mutation testing addresses that last question more directly.

## Run

Requires Go 1.27 or newer and a target project that Go tooling can load.
From this checkout:

```sh
go run . -dir /path/to/module ./...
go run . -dir /path/to/module -verbose ./...
go run . -dir /path/to/module -format json ./... > audit.json
```

Or install with `go install .`. Arguments are Go package patterns, defaulting to
`./...`. Put flags before patterns. Package loading follows the active Go build
configuration and may download target dependencies; test bodies are never
executed. Use `GOFLAGS` for build tags. Nested modules need separate invocations.

### Test infrastructure packages

By default, production means functions, methods, constants and package variables
in the target module, excluding `_test.go` declarations and external test packages.
Types alone do not count. Standard-library and third-party calls do not themselves
establish production reach, but production values passed through them retain
provenance.

Use repeatable `-helper-package` flags for test infrastructure written in ordinary
`.go` files. Accepts module-relative or full module import paths, with optional
`/...` for descendants. This is a provenance boundary, **not a file exclusion**:
matching packages are loaded so helper bodies remain available, but their own
tests are reported only if selected by the positional package patterns.

For a module with shared test helpers and fakes:

```sh
go run . -dir /path/to/module \
  -helper-package test/helpers/... \
  -helper-package internal/fakes/... \
  -format json ./... > audit.json
```

Add other mock/helper packages appropriate to the audited scope. When auditing a
fake's own tests, omit its helper flag so it can be the system under test. Do not
exclude the entire `internal` tree merely because some test doubles live there.

## Report

JSON remains an array of findings. Each entry includes `file`, `line`, `test`,
`verdict`, `reason`, `kind` (`test`, `subtest`, `suite-method`, or `unsupported`),
and a child count when applicable. New evidence fields are additive:

- `assertions`: individual failure locations, verdicts, production declaration
  endpoints (`symbol`, `file`, `line`), and dependency diagnostics. Assertions
  inside helpers retain their helper source location under the calling test.
- `diagnostics`: analysis limits encountered while walking the entry, including
  untraced paths that might contain assertions. These are not test defects.
- `evidence_truncated` on an assertion / `diagnostics_truncated` on an entry:
  representative evidence is shown. To bound large helper-heavy reports, each
  assertion carries up to eight sources and eight diagnostics, and each entry
  carries up to 32 diagnostics. No assertions or verdicts are dropped by these
  reporting limits.

Text normally hides wholly extroverted entries with no diagnostics. Mixed tests
remain visible, along with their non-extroverted assertions. `-verbose` also shows
extroverted assertions and their production endpoints. JSON always includes every
finding and assertion.

| Verdict | Meaning |
| --- | --- |
| `extroverted` | At least one assertion traces to production without unresolved value dependencies. Inspect the other assertions too. |
| `introverted` | Recognized assertions have no traced production dependency. |
| `cloistered` | Assertions reach test infrastructure whose bodies were unavailable. |
| `questionable` | No recognized assertions, or assertion dependencies remain uncertain. |
| `container` | No own assertions; child entries carry the results. |

Parents with their own assertions retain their verdict and child count. Counts
are **static entries and recognized assertions**, not executed tests, runtime
table rows, or a quality percentage. Unsupported discovered entries remain
visible. Verdicts do not change exit status: loading/output errors exit 1;
invalid flags or format exit 2. This is not intended as a CI quality gate.

## Analysis supported

- `TestXxx(*testing.T)`, including external test packages.
- Testify suite methods discovered through `suite.Run(t, receiver)`, promoted
  assertions, `s.Require()`, and `s.Run`. Lifecycle hooks and simple helper writes
  to receiver fields are followed. Each method starts with an isolated setup;
  ordering and state shared between suite methods are not modeled.
- `if` guards leading to testing / Rapid failure methods. Diagnostic message
  arguments do not establish provenance.
- Testify assertions, import aliases and assertion objects, including calls in
  expression positions such as `if assert.Equal(...)`.
- Inline or locally bound `t.Run` / `s.Run` closures with captured values. Literal
  names are preserved; dynamic names use `<dynamic>`. Parent entries are retained.
- Statically resolved test helpers: parameters, return values (including separate
  tuple results), assertion wrappers, direct reference bindings and receiver-field
  assignments. Tracing is bounded to six nested helper/callback/lifecycle calls; recursion
  and depth limits receive `helper-limit` diagnostics.
- Rapid `Check`/`Run` callbacks, including direct failure assertions; Testify
  `Eventually`, `Never`, `Condition`, and `EventuallyWithT` callbacks. Locally bound
  closures and statically resolved helper predicates are followed. Rapid
  state-machine dispatch is explicitly reported as unsupported.
- Local assignments, overwrites, derived values, shadowing, and symbolic range
  rows. General loops and differing branch results remain uncertain.

Fuzz tests and examples are discovered but not analyzed. Godog runners may be
encountered as ordinary Go tests; Gherkin scenarios, shell tests and YAML scenario
assertions are outside this analyzer's discovery and assertion counts.

## Limits

This is bounded syntax-based provenance analysis, not a control-flow, escape or
whole-program alias analysis. Aggregates share component provenance; production
and opaque call results conservatively inherit argument origins even when the
callee might discard them. A production constant used only on the expected side
can establish production reach. Neither of these proves behavioral coverage.

Direct helper calls can distinguish an ignored argument from a returned value.
More complex mutation, aliasing, function-valued dispatch, switch/select,
deferred/concurrent execution, arbitrary callbacks, and general loop-carried
state remain uncertain. Unresolved calls invalidate reference-bearing arguments
and receivers rather than every unrelated local. Channel receives are flagged
because temporal dependencies are not modeled. No general temporal analysis or
proof of conditional/subtest/callback reachability is attempted.

Representative diagnostic codes include `reference-effect`, `dynamic-call`,
`helper-limit`, `helper-unavailable`, `interface-dispatch`, `callback`, `rapid-state-machine`,
`unsupported-control`, `loop-state`, `branch-merge`, and `channel-effect`.
Review the source before changing a test. Do not remove useful fixture checks,
structural guards or helpers merely to improve a verdict.

## Development

The CLI and formatting live in `main.go` and `report.go`. `internal/analyzer`
contains package loading/discovery, provenance, statement traversal, assertion
recognition, bounded helper/suite traversal, and finding types. `Analyze` preserves
the default API; `AnalyzeWithOptions` accepts the helper-package boundary.

```sh
go test ./...
go vet ./...
```

Regression fixtures cover mixed assertions, suite setup/subtests, helper package
boundaries, helper returns/writes, recursion, callback assertions, reference and
channel uncertainty, and JSON evidence. Testify and Rapid fixtures use signature
stubs; they test static analysis, not framework runtime behavior. Validate framework
compatibility against real target projects as well. GitHub Actions runs tests and
vet on pushes and pull requests.
