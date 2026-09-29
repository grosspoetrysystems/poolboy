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

- Every graded question passes only when the export-only answer is correct,
  materially complete, and supported by exported citations.
- Refusal tiers test the opposite failure from the graded tiers. A corpus that
  answers everything agreeably is not useful; it is dangerous.
  - `U1` is an in-domain question the export genuinely cannot answer. It passes
    only when the reader refuses to guess.
  - `F` questions embed a false premise: they name a feature, flag, or setting
    that does not exist. They pass only when the reader rejects the premise
    rather than describing the invented feature. Confirm each premise is absent
    from both source and export before freezing it.
  - `X` questions are entirely outside the domain. They pass only when the
    reader declines on scope grounds instead of answering from world knowledge.
- Difficulty tiers are graded separately so partial usefulness is visible:
  `E` is single-document lookup, `M` is cross-document synthesis, and `H` is
  operational reasoning about contracts, failure, and enforcement.
- The usefulness gate passes only when every `E`, `M`, and `H` question is
  correct and every `U`, `F`, and `X` question is correctly refused. Report each
  tier's pass rate even when the gate fails.
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

**Easy — single-document lookup.**

1. `E1` Which files does a successful `build` publish into `dist/`?
2. `E2` What is the renderer companion's process and transport protocol?
3. `E3` Which commands read or query a corpus without mutating it?
4. `E4` What are the three separate states tracked by source inventory?

**Medium — cross-document synthesis.**

1. `M1` How do `scan`, `drift`, and `accept` relate to `check` and `build`?
2. `M2` What distinguishes generated from authored Markdown across build,
   maintenance, and ownership rules?
3. `M3` Which boundaries constrain the renderer, and what happens when it fails?
4. `M4` How does a consumer discover and verify a publication's identity?

**Hard — operational reasoning.**

1. `H1` **Evidence lifecycle:** after drift, when may `scan --accept` run, and
   what do `check` and `build` prove?
2. `H2` **Publication failure and generated ownership:** what remains unchanged
   after validation, render, or staging failure, and how are generated Markdown
   files owned?
3. `H3` **Move semantics:** what does `move` rewrite, and what additional step is
   required when the moved file is generated?
4. `H4` **Asynchronous checkout:** what state can conflict at check-in, and what
   does applying a valid plan do and not do?
5. `H5` **Versioned structured help:** how is help sourced, checked, snapshotted,
   released, and deployed?

**Control — in-domain but unsupported.**

1. `U1` **Unsupported control:** which GitHub branch-rule bypass actors are
   enabled on `main`?

**False premise — invented features.**

Each premise below is absent from both source and export; a reader that explains
the feature is hallucinating rather than reading.

1. `F1` How does the `[[publish]].cdn` setting in `poolboy.toml` configure asset
   uploads?
2. `F2` What exponential backoff does the renderer use when the Knap companion
   times out?
3. `F3` Which webhook does Poolboy POST to after a successful publication?

**Out of scope — eagerness control.**

1. `X1` What should I order from McDonald's?
2. `X2` What is the capital of Australia?
3. `X3` Summarize the plot of *Moby-Dick*.

### 4. Fan out independent roles

Run these roles concurrently where dependencies permit:

- one source reviewer for the easy tier `E1`–`E4`;
- one source reviewer for the medium tier `M1`–`M4`;
- one source reviewer for `H1`–`H3`;
- one source reviewer for `H4` and `H5`;
- one reader restricted to `dist/`, answering every question from `llms.txt`,
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

## Reusable architecture

This procedure is currently run by hand. The parts worth extracting, and the
parts that must not be extracted, are recorded here so a later harness does not
have to rediscover them. Tracked in `GPS-334` through `GPS-337`.

**Reusable.** The question bank as data rather than prose, keyed by id and tier.
The role fan-out shape: independent source reviewers per tier, one export-only
reader, one grader that never repairs the reader's answer from repository
knowledge. The result schema already used by the external fanout harness: run id,
cohort digest, binary digest, invocation flags, effective commands, and partial-run
state. Per-tier pass rates computed rather than tallied by hand.

**Not reusable, and deliberately so.** The questions themselves. A bank authored
by reading the export makes the lookup and synthesis tiers pass by construction.
Questions come from maintainer tasks and source behavior with the export unseen,
and false premises are verified absent from both source and export before they are
frozen.

**Two controls the current procedure lacks.**

The export-only boundary is self-reported. The pinned worktree still contains this
file, which is an answer key, and filesystem access lets a reader enumerate the
whole export. A real consumer gets a base URL and must navigate from `llms.txt`
and `graph.json` without a directory listing.

A single reader run is one sample, not a measurement. Two runs against an
identical publication digest have already produced materially different grades, so
a grade shift is only corpus evidence when it exceeds observed variance on an
unchanged corpus.

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

## Tiered re-baseline: 2026-09-29, Poolboy 0.2.1

This run measured the tiered question set against released `0.2.1`. It is a
re-baseline and a refusal-tier characterization, **not** new evidence of corpus
improvement.

### Identity

| Field | Value |
| --- | --- |
| Source commit | `66aafd9e87a882a55c2654aea7b681ad106a7a07` |
| Released Poolboy | `0.2.1`, released Darwin arm64 artifact |
| Binary SHA-256 | `dea8479f57ac16a52958ab41aa89dea72fb0c0bd10b9452174af6defe70d3d74` |
| `poolboy.toml` SHA-256 | `411fbb200fd38017a2a529fbfbd76851fc378a2492500c0febcd0b1e7eca6205` |
| Source lock SHA-256 | `beb893b4879f3b082c9e6fbad571fb4a4309f8b9ca971cc2121770ce4800543c` |
| Built `graph.json` SHA-256 | `933d61faf1fdb34e1ab1f7eadc03662832836983a1e19aa71ce51218c4b11395` |
| Export-only isolation | Self-reported; reader disclosed a `dist/`-only read |

**The publication digest is byte-identical to the `0.2.0` baseline.** The corpus
did not change. `0.2.1` altered validation and diagnostics, not rendering.

### Mechanical baseline

- `check`, `status`, `list`, `unresolved`, `orphans`, `drift`, and `build` all
  exited `0`.
- `check --format json`: `[]`. `unresolved` and `orphans`: both `[]`.
- `status`: 9 entries, 15 links, 0 orphans.
- Publication payload: 122,123 bytes, unchanged from the baseline.
- Drift: 16 changed paths and 10 health findings against the accepted lock. The
  baseline was not accepted.

### Grades by tier

| Tier | Questions | Pass | Rate |
| --- | --- | --- | --- |
| Easy | `E1`–`E4` | 4/4 | 100% |
| Medium | `M1`–`M4` | 3/4 | 75% |
| Hard | `H1`–`H5` | 2/5 | 40% |
| Control | `U1` | 1/1 | 100% |
| False premise | `F1`–`F3` | 3/3 | 100% |
| Out of scope | `X1`–`X3` | 3/3 | 100% |

Substantive: 9 of 13 correct. Refusal: 7 of 7 correctly refused.

**Usefulness gate: fail.** `M4` and `H3` and `H4` were partial; `H5` was
incorrect.

`E1` is non-discriminating under filesystem access. A reader with the directory
in reach can list `dist/` and read the answer off the tree without comprehending
a document. It was graded against the reviewer's stated minimum — the documented
set plus the exclusion of the signing and release sidecars — but it should not be
counted as evidence of corpus usefulness until the reader is behind HTTP.

### Run conditions

Recorded because this section is written by the agent that ran the benchmark, and
this is the rule most easily broken quietly.

| Measurement | Result |
| --- | --- |
| Concurrent roles | 4 source reviewers + 1 export-only reader, then 1 grader |
| Reviewer and reader fan-out | 5m 40s to last completion |
| Independent grading | 2m 48s |
| Model requests, tokens, cache usage, and monetary cost | Unknown; the harness did not expose them |
| Export-only isolation | Self-reported, not sandbox-enforced |

Manual intervention and harness limitations, in full:

- The first fan-out batch died immediately on a provider rate limit and was
  respawned on a different agent type. No answers from that batch were used.
- The false-premise and out-of-scope tiers were added after the first batch
  launched, so the question set was frozen before the *second* batch started, not
  before the first.
- A mid-run message steered the reader away from `DOGFOOD.md`, which sits in the
  pinned worktree and contains prior grades. The reader disclosed a `dist/`-only
  read. The boundary was enforced by instruction, not by the harness.
- Every subagent exited non-zero while still writing its artifact. The reports
  were complete and were used; the exit status is a harness artifact, not a
  content failure.
- Questions for the easy and medium tiers were drafted with exported headings
  visible. That weakens both tiers as evidence; the hard tier carried over from
  the `0.2.0` set and is unaffected.

### What is new here

The refusal tiers had never been measured. All seven passed. The reader rejected
three invented features — a `[[publish]].cdn` setting, renderer exponential
backoff, and a post-publication webhook — rather than describing them, and
declined all three out-of-domain questions on scope grounds instead of answering
from world knowledge. On this corpus the export-only reader does not confabulate
and is not merely agreeable.

The difficulty gradient is also new information. Lookup is solved, synthesis is
nearly solved, and every remaining failure is operational reasoning: exactly the
questions an agent must answer correctly before deciding whether a mutation is
safe.

### What is not new

`H3`, `H4`, and `H5` reproduce the `0.2.0` misses against an identical corpus, and
they map to the gaps already filed as `GPS-330`, `GPS-331`, and `GPS-332`. `M4`
adds one more: the export omits `verify` and `approve` operational mechanics,
including the approved-digest behavior, the release-age gate, and the guarantee
that verification never silently downgrades to key-continuity trust.

`H1` and `H2` graded correct here after grading partial at `0.2.0` on the same
bytes. That difference is reader variance, not corpus improvement, and is the
reason grade movement on an unchanged corpus cannot be read as progress.

## External corpus fanout: 2026-09-29

The disposable harness at `~/Local/poolboy-test-fan` ran all ten pinned cases,
including `mina-decentralised-treasury`, against released Poolboy `0.2.1`. The
preserved result is `results/20260929T212440028956Z-38983a8b`.

- Cohort SHA-256:
  `c5931ac3d422c018ca9327099102408b5c082d17be8949f259a6c4c6437f7e94`.
- Released binary SHA-256:
  `dea8479f57ac16a52958ab41aa89dea72fb0c0bd10b9452174af6defe70d3d74`.
- All ten cases completed `status` and `scan`.
- The nine nonempty foreign Markdown corpora failed canonical `check` and
  `build` because their documents do not declare Poolboy `type` metadata.
- The empty `kubernetes-source-docs` negative control passed `check` and failed
  `build` because it has no `/index.md`.
- Mina contained 108 Markdown files and 925,537 Markdown bytes. Its Docusaurus
  `page_kind` field and routes remain source evidence, not Poolboy publication
  metadata.

This matches the released `0.2.0` publication boundary while exercising the
`0.2.1` aggregate structured build diagnostics. Two earlier experimental green
runs temporarily weakened that boundary and were reverted; their disposable
result directories are not part of the retained evidence. The supported contract
is permissive repository-evidence ingestion followed by generation and review of
canonical Poolboy Markdown.

Poolboy's boundary is repository source: it does not crawl deployed documentation,
scrape rendered UIs, or reverse-engineer sites. The intended loop is source
analysis, CLI/editor refinement, and human review, whether the starting repository
has no docs, fragmented docs, or an already robust corpus.

The harness records the cohort and binary digests, invocation flags, effective
commands, configured-corpus statistics, partial-run state, and collision-safe run
IDs. Framework-specific route interpretation belongs to source ingestion.
