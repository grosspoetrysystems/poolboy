---
type: concept
title: Test coverage gates
description: The project-wide Go statement floor, companion metric floors, commands, and recovery paths.
status: draft
sources:
  - id: okf-spec
    resource: https://github.com/GoogleCloudPlatform/open-knowledge-format/blob/ad30107c31c06aec8a7d5636e0d1058118604e6f/SPEC.md
    title: Google Open Knowledge Format v0.2 specification
  - id: makefile
    resource: ../Makefile
    title: Repository check and verification targets
  - id: companion-coverage
    resource: ../companion/vitest.config.ts
    title: Companion Vitest coverage configuration
  - id: coverage-gate
    resource: ../scripts/coverage_gate.py
    title: Go coverprofile gate
  - id: coverage-gate-tests
    resource: ../tests/test_coverage_gate.py
    title: Coverage gate boundary tests
  - id: hooks
    resource: ../lefthook.yml
    title: Repository hooks
---
# Test coverage gates

See `CONTRIBUTING.md` for coverage-specific contribution rules.

## Gate contract

The required floor is **90% project-wide Go statement coverage** across all
`./...` packages. Go's gate measures statements only. It is not a branch,
function, or per-package floor: coverage from tests in one package can cover
statements in another package when the aggregate profile includes them.

The companion must meet **90% for each of its four current Vitest metrics**:
branches, functions, lines, and statements. Its existing `src/bin/**`
exclusion remains the only companion coverage exclusion. The CLI smoke test is
a separate consumer check; passing the smoke test neither replaces the
companion coverage report nor counts as a coverage exclusion.

The Go decision is exact integer arithmetic:

```text
covered * 100 >= total * 90
```

The checker merges repeated `path:span` blocks emitted by the aggregate
profile across test binaries, counting each location once. Duplicate blocks
must agree on `numStmt`; any positive hit marks that location covered. It then
compares the cross-products and never rounds a percentage up to pass: for
example, 89,999 covered statements out of 100,000 is 89.999% and fails even
if a report formats it as 90.00%, while 90 out of 100 passes. Empty,
missing-header, or malformed profiles fail. Zero-statement blocks are accepted
and ignored in totals; a profile with no positive-statement blocks fails rather
than silently passing.

## Commands

### Focused feedback

While iterating on a gate change, run the boundary suite for the checker:

```sh
python3 tests/test_coverage_gate.py
```

For behavior changes, run the affected Go package directly (for example,
`go test -race ./parse`) or the affected companion test with
`pnpm --dir companion test -- path/to/file.test.ts`. These focused commands
provide fast feedback; they do **not** claim the project-wide 90% gate.

### Full gate

`make check` is the project check target: it runs the aggregate Go profile
gate, companion coverage, and the other checks. Its Go coverage recipe is
equivalent to:

```sh
cover=$(mktemp)
trap 'rm -f "$cover"' EXIT
go test -race -coverpkg=./... -coverprofile="$cover" ./...
python3 scripts/coverage_gate.py "$cover"
```

The Makefile uses `$(PYTHON)` for the last command, defaulting to `python3`.
The companion portion runs `pnpm --dir companion coverage`, so all four
configured metrics are checked at 90% while retaining `src/bin/**` as the
existing exclusion.

`make verify` is the pre-push/CI-level path: it runs `make check`, then builds
the site and runs the CLI smoke test. A smoke failure after coverage passes is
a build or consumer failure, not a reason to lower a coverage threshold.
See the [repository navigation](index.md). The owning files are `Makefile`
and `companion/vitest.config.ts`.

## Failure recovery

1. **Go gate failure:** preserve a profile while diagnosing, then inspect it
   with `go tool cover -func=/tmp/poolboy.cover` and rerun
   `python3 scripts/coverage_gate.py /tmp/poolboy.cover`. The profile must
   come from the aggregate `-coverpkg=./...` run; a per-package report cannot
   substitute for it. Add a test for the uncovered behavior, boundary, or
   error path rather than changing the gate arithmetic.
2. **Companion metric failure:** run `pnpm --dir companion coverage` and use
   the reported metric and source location to choose a meaningful behavioral
   or boundary test. Keep the existing `src/bin/**` exclusion unchanged; do
   not add another exclusion or lower one metric.
3. **Hook discovery failure:** from the repository root, run
   `make hooks`, or invoke
   `companion/node_modules/.bin/lefthook install` directly. Validate from the
   same root with `companion/node_modules/.bin/lefthook validate`. Lefthook
   must discover the checked-in `lefthook.yml` from that
   root; no manual `LEFTHOOK_CONFIG` setting is required or recommended.
4. **Full verification failure:** `make verify` runs the coverage gate before
   site generation and CLI smoke. Fix the first failing stage and rerun the
   same full command so the evidence still represents the enforced path.

A baseline is an observation from one run, not the acceptance floor. Record
an observed baseline with its command, profile context, and time in the issue
or pull request; do not turn a stale per-package or companion number into a
new threshold. The floor remains 90% until the project contract changes.

## Guidance for agents and contributors

Tests should exercise consumer-visible behavior: outputs, persisted effects,
protocol results, boundary handling, and actionable errors. Before adding a
test, read the implementation and its callers, then choose a scenario that
would catch a regression. Prefer a small assertion-rich test over a line-hit
stub, wiring-only test, source/default snapshot, or tautological assertion.

Git-backed fixtures must clear inherited `GIT_*` repository context before
creating a temporary repository, and restore the test environment on cleanup.
A passing pre-push test must leave the caller's `HEAD` and index unchanged.

Coverage work must not inflate the score. Do not exclude production paths,
weaken thresholds, alter production code solely to make a line reachable, or
replace the aggregate profile with a narrower package run. Do not count the
CLI smoke test as a substitute for the Go or companion metrics. An agent
handoff should name the meaningful behavior covered, the focused command used,
and the full `make check` or `make verify` result observed by the verifying
agent.
