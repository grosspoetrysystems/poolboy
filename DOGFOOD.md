# Poolboy self-dogfood benchmark

This benchmark tests whether a pinned Poolboy publication can answer maintainer
questions without source access. It separates corpus usefulness from source
correctness: source reviewers establish the expected answers, an export-only
reader answers from `dist/`, and an independent grader compares the two.

Keep this file outside the configured `docs/` corpus and outside the export-only
reader's scope. It contains the frozen questions and prior results; publishing it
with the candidate corpus would leak the benchmark.

## Acceptance rule

Freeze the questions and grading rule before any reviewer starts.

- Questions Q1–Q5 pass only when the export-only answer is correct, materially
  complete, and supported by exported citations.
- Q6 is a deliberate unsupported control. It passes only when the reader refuses
  to guess.
- The usefulness gate passes only when Q1–Q5 are all correct and Q6 is
  unsupported.
- Grade each question `correct`, `partial`, `incorrect`, or `unsupported`.
- A successful `check` or `build` is setup evidence, not a benchmark pass.

## Reproducible flow

### 1. Pin and isolate the specimen

Record the original checkout's commit and dirty state. Create a detached worktree
from the committed revision so uncommitted user work is excluded.

```sh
SOURCE_REV=$(git rev-parse HEAD)
PROJECT=$(mktemp -d /tmp/poolboy-self-dogfood.XXXXXX)
git worktree add --detach "$PROJECT" "$SOURCE_REV"
```

Record the exact Poolboy version. Use an explicit released package rather than an
unversioned executable found on `PATH`.

```sh
npx -y @grosspoetrysystems/poolboy@0.2.0 version
```

### 2. Capture the mechanical baseline

Run these commands from the detached worktree. Do not accept source drift during
the benchmark.

```sh
npx -y @grosspoetrysystems/poolboy@0.2.0 drift --format json
npx -y @grosspoetrysystems/poolboy@0.2.0 status --format json
npx -y @grosspoetrysystems/poolboy@0.2.0 check --format json
npx -y @grosspoetrysystems/poolboy@0.2.0 build --format json
npx -y @grosspoetrysystems/poolboy@0.2.0 list --format json
npx -y @grosspoetrysystems/poolboy@0.2.0 unresolved --format json
npx -y @grosspoetrysystems/poolboy@0.2.0 orphans --format json
```

Record the source pin, tool version, configuration and source-lock hashes, build
wall time, corpus counts, output bytes, unresolved links, orphans, and complete
drift result. Hash the built `graph.json` so later runs can identify the exact
candidate publication.

### 3. Freeze the questions

1. **Evidence lifecycle:** after drift, when may `scan --accept` run, and what do
   `check` and `build` prove?
2. **Publication failure and generated ownership:** what remains unchanged after
   validation, render, or staging failure, and how are generated Markdown files
   owned?
3. **Move semantics:** what does `move` rewrite, and what additional step is
   required when the moved file is generated?
4. **Asynchronous checkout:** what state can conflict at check-in, and what does
   applying a valid plan do and not do?
5. **Versioned structured help:** how is help sourced, checked, snapshotted,
   released, and deployed?
6. **Unsupported control:** which GitHub branch-rule bypass actors are enabled on
   `main`?

### 4. Fan out independent roles

Run these roles concurrently where dependencies permit:

- one source reviewer for Q1;
- one source reviewer for Q2;
- one source reviewer for Q3;
- one source reviewer for Q4 and Q5;
- one reader restricted to `dist/`, answering Q1–Q6 from `llms.txt`,
  `graph.json`, and exported Markdown.

Source reviewers cite implementation, tests, and canonical documentation. The
export-only reader cites exported files and headings. The reader must say
`unsupported` when evidence is absent. If the harness cannot technically prevent
source access, record the boundary as self-reported rather than sandbox-enforced.

After those roles finish, give their reports to a separate grader. The grader
compares answers against source expectations without repairing the consumer
answers from repository knowledge.

### 5. Record the run without laundering failures

Record every grade, material miss, elapsed time, reported model usage or cost,
manual intervention, and harness limitation. Unknown usage is `unknown`, not
zero. Preserve a failing baseline. Do not edit the corpus and rerun until the
first result looks successful.

## Baseline: 2026-09-29

### Identity and boundary

| Field | Value |
| --- | --- |
| Source commit | `79881e0f3479f53ad2cd8f8dd0d9a88fa0e40cae` |
| Released Poolboy | `0.2.0`, executed through `npx -y @grosspoetrysystems/poolboy@0.2.0` |
| Unversioned executable found on `PATH` | `0.1.1`; observed but not used |
| Original checkout | Dirty: an uncommitted `site/index.html` copy edit was excluded by the pinned worktree |
| Detached worktree after build | Clean |
| `poolboy.toml` SHA-256 | `411fbb200fd38017a2a529fbfbd76851fc378a2492500c0febcd0b1e7eca6205` |
| Source lock SHA-256 | `beb893b4879f3b082c9e6fbad571fb4a4309f8b9ca971cc2121770ce4800543c` |
| Built `graph.json` SHA-256 | `933d61faf1fdb34e1ab1f7eadc03662832836983a1e19aa71ce51218c4b11395` |

### Mechanical baseline

- `check --format json`: `[]`.
- `status`: 9 entries, 15 links, 0 broken links, 0 orphans.
- `unresolved` and `orphans`: both `[]`.
- Released-package build: 1.15 seconds wall time.
- Publication payload measured: 122,123 bytes across Markdown, `graph.json`,
  `llms.txt`, `index.html`, and `corpus.zip`.
- Drift: 12 paths changed from the accepted source lock. Four documents reported
  `source_changed` health findings from `cmd/poolboy/main.go`.
- The source baseline was not accepted. The clean structural check and successful
  build did not erase or reconcile that drift.

### Fan-out measurements

| Measurement | Result |
| --- | --- |
| Concurrent roles | 4 source reviewers + 1 export-only reader |
| Fan-out wall time | 9m 12s |
| Independent grading | 7m 35s |
| Total observed benchmark time | 17m 07s |
| Model requests, tokens, cache usage, and monetary cost | Unknown; the harness did not expose them |
| Export-only isolation | Self-reported read boundary, not a technical sandbox |

### Grades

| Question | Grade | Material result |
| --- | --- | --- |
| Q1 evidence lifecycle | Partial | Correct workflow, but omitted that the CLI does not enforce the review/check/build gate and that a successful check may retain warnings. |
| Q2 publication failure | Partial | Correct live-publication and ledger behavior, but omitted that pre-mutation failures also preserve canonical Markdown, generated files, and the previous generated ledger. |
| Q3 move semantics | Partial | Correct link and source-reference rewrites, but missed the required `[[render]].output` update and generated-ownership reconciliation. |
| Q4 checkout/check-in | Partial | Correct file-level transport boundary, but missed nonblocking source/generator variance, apply-time race protection/rollback, private-state closure, and retention of the external checkout directory. |
| Q5 versioned structured help | Incorrect | Mistook the generated Markdown reference and publication graph for runtime structured help; the export does not explain the catalog, embedded fallback, versioned snapshots, release asset, or deploy reconstruction. |
| Q6 unsupported control | Unsupported (pass) | Correctly refused to infer branch-rule bypass actors from unrelated publisher-identity documentation. |

**Usefulness gate: fail.** Q1–Q4 were partial, Q5 was incorrect, and only
the unsupported control passed as designed.

## What this baseline establishes

The current corpus is useful for broad maintenance concepts but is not yet a
complete operational reference for the frozen questions. The largest gap is the
versioned structured-help pipeline. Smaller gaps are precise enforcement and
failure boundaries that matter when an agent is deciding whether a mutation is
safe.

This run did not exercise source mutation capture, baseline acceptance,
interruption/resumption, deployment, or a second corrected-reader pass. Add those
only as separately measured benchmark phases; do not fold them into this baseline
retroactively.

## External corpus fanout: 2026-09-29

The separate harness at `~/Local/poolboy-test-fan` ran all ten pinned cases,
including `mina-decentralised-treasury`, against released Poolboy `0.2.0`.
The preserved result is `results/20260929T131951Z`.

- All ten cases completed `status` and `scan`.
- The nine nonempty foreign Markdown corpora failed canonical publication
  validation because their documents do not declare Poolboy `type` metadata.
- The empty `kubernetes-source-docs` negative control passed `check` and failed
  `build` because it has no `/index.md`.
- Mina contained 108 Markdown files, 516 links, 106 checkboxes, 63 broken links,
  and 4 orphans. Its Docusaurus `page_kind` field is source evidence, not
  Poolboy publication metadata.

These publication failures are expected and are not an ingest regression.
Earlier green runs under `results/20260928T101330Z` and
`results/20260928T101507Z` used an experimental binary that temporarily weakened
the publication boundary; that change was explicitly reverted. The supported
contract is permissive repository-evidence ingestion followed by generation and
review of canonical Poolboy Markdown.

Poolboy's boundary is repository source: it does not crawl deployed documentation,
scrape rendered UIs, or reverse-engineer sites. The intended loop is source
analysis, CLI/editor refinement, and human review, whether the starting repository
has no docs, fragmented docs, or an already robust corpus.

The harness now records the cohort and binary digests, invocation flags, effective
commands, configured-corpus statistics, partial-run state, and collision-safe run
IDs. Framework-specific route interpretation belongs to source ingestion; the
released `0.2.0` measurements above remain the publication-validation baseline.
