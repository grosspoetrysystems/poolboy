# Poolboy benchmarks

This file is the index for every Poolboy benchmark. Two exist, and they measure
different things. Do not read a pass in one as evidence for the other.

| Benchmark | Question it answers | Subject |
| --- | --- | --- |
| **Usefulness** | Can a publication answer maintainer questions without source access? | Poolboy's own corpus |
| **Compatibility fanout** | What would it take to convert a foreign repository into a canonical corpus? | Ten pinned external repositories |

Keep this file outside the configured `docs/` corpus and outside the export-only
reader's scope. It holds the frozen questions and prior grades; publishing it with
the candidate corpus would leak the benchmark, and leaving it in the pinned
worktree puts an answer key within a reader's reach.

## Where things live

| Artifact | Location | Retained |
| --- | --- | --- |
| Both protocols and all recorded results | This file | Yes |
| Question bank and scorer | `bench/questions.json`, `bench/usefulness.py` | Yes |
| Cohort manifest and fanout runner | `bench/cohort.json`, `bench/fanout.py` | Yes |
| Per-run role reports and grades | `bench/usefulness/<date>-v<version>/` | Yes |
| Per-run fanout summaries | `bench/fanout/<date>-v<version>/` | Yes |
| Cohort clones, worktrees, and raw command output | Sandbox outside the repository | No |
| Findings turned into work | Linear `GPS-296` and its children | Yes |

Everything needed to replay is versioned. The sandbox holds only bulk scratch:
the ten upstream clones and the multi-megabyte raw `scan` and `check` output. The
runner refuses to run with a sandbox inside the repository, so a replay cannot
accidentally commit vendored upstream source.

Two cohort repositories are access-restricted, `aragon-app` and
`decentralised-treasury`. The manifest marks them, so a replayer without access
gets an explicit gap rather than an opaque clone failure on two of ten cases.

The `cohort_sha256` digest covers the case definitions only — id, repo, revision,
and corpus path — not the whole manifest file. Editing a clone URL or prose must
not look like a cohort change. The `c5931ac3…` digest recorded for the 0.2.1
fanout below predates this change and was computed over the entire file; the same
cases now digest to `cde9cbbe…`.

## Grade vocabulary

Four grades: `correct`, `partial`, `incorrect`, `unsupported`.

`unsupported` is tier-dependent and the one trap in the vocabulary. On a refusal
tier it is the **pass** — the reader correctly declined. On a graded tier it is a
**failure** — the reader declined a question the corpus was supposed to answer,
which is honest but still a gap. `H3` and `H5` carry `unsupported` from the second
grader and are failures, not passes.

The scorer disambiguates by tier, so a grades file is only meaningful alongside
the question bank that assigns the tiers.

## Replay

Both benchmarks pin a specimen and name an explicit released version. Neither
accepts an unversioned executable found on `PATH`.

Usefulness, against Poolboy's own corpus. The repository contains two answer
keys — this file, and the retained role reports and grades under
`bench/usefulness/` — and both must leave the specimen before a reader runs:

```sh
SOURCE_REV=$(git rev-parse HEAD)
PROJECT=$(mktemp -d /tmp/poolboy-self-dogfood.XXXXXX)
git worktree add --detach "$PROJECT" "$SOURCE_REV"
rm -rf "$PROJECT/DOGFOOD.md" "$PROJECT/bench"   # remove both answer keys
```

Then follow "Reproducible flow" below.

Compatibility fanout, against the pinned external cohort. Recreate the sandbox
from the manifest, then run:

```sh
SANDBOX=~/Local/poolboy-test-fan
mkdir -p "$SANDBOX"
jq -r '.repos | to_entries[] | "\(.key)\t\(.value.url)"' bench/cohort.json |
  while IFS=$'\t' read -r name url; do
    [ -d "$SANDBOX/$name" ] || git clone "$url" "$SANDBOX/$name"
  done

# Detach each clone at its pinned revision; the runner refuses anything else.
jq -r '[.cases[] | "\(.repo)\t\(.revision)"] | unique[]' bench/cohort.json |
  while IFS=$'\t' read -r name revision; do
    git -C "$SANDBOX/$name" checkout --quiet --detach "$revision"
  done

gh release download "v<version>" --repo grosspoetrysystems/poolboy \
  --pattern 'poolboy_<version>_<os>_<arch>.tar.gz' --dir /tmp/poolboy-<version>
tar -xzf /tmp/poolboy-<version>/poolboy_<version>_*.tar.gz -C /tmp/poolboy-<version>

python3 bench/fanout.py --sandbox "$SANDBOX" --poolboy /tmp/poolboy-<version>/poolboy
```

Each clone must sit at its pinned revision and be clean; the runner refuses
otherwise. It writes one collision-safe result directory into the sandbox
carrying the cohort digest, the binary digest, and per-case command exits. Copy
the top-level and per-case `summary.json` files into `bench/fanout/` and discard
the rest.

## The usefulness benchmark

It separates corpus usefulness from source correctness: source reviewers establish
the expected answers, an export-only reader answers from `dist/`, and an
independent grader compares the two. Retained role reports and grades for each run
live under `bench/usefulness/`, which is also why that directory is an answer key
and must be stripped from the pinned specimen.

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

Record the exact Poolboy version. Pin it once as a variable and use that
everywhere; never benchmark an unversioned executable found on `PATH`.

```sh
POOLBOY="npx -y @grosspoetrysystems/poolboy@<version>"
$POOLBOY version
```

### 2. Capture the mechanical baseline

Run these commands from the detached worktree. Do not accept source drift during
the benchmark.

```sh
for command in drift status check build list unresolved orphans; do
  $POOLBOY "$command" --format json
done
```

Record the source pin, tool version, configuration and source-lock hashes, build
wall time, corpus counts, output bytes, unresolved links, orphans, and complete
drift result. Hash the built `graph.json` so later runs can identify the exact
candidate publication.

### 3. Freeze the questions

The question bank is `bench/questions.json`, not prose. Read it there; it carries
each question's id, tier, text, and any note that affects grading. Freeze it
before a reviewer starts, and change it in its own commit so a run always cites a
committed bank.

Six tiers, in two groups. `E` lookup, `M` synthesis, and `H` operational
reasoning are graded on being right. `U` in-domain-unsupported, `F` false
premise, and `X` out of scope are graded on refusing — a confident answer there
is the failure, not the success.

`F` premises name features that do not exist. Verify each `absent_term` is absent
from both source and export before freezing, or the question tests nothing.

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

### 5. Score the run

Write the grader's verdicts to `bench/usefulness/<date>-v<version>/grades.json`
as a flat map of question id to grade, then compute the result rather than
tallying it:

```sh
python3 bench/usefulness.py bench/usefulness/<date>-v<version>/grades.json
```

It prints the per-tier table and the gate verdict, and exits non-zero when the
gate fails. An ungraded or unknown question id fails the gate too; a gate that
ignores gaps is not a gate.

### 6. Record the run without laundering failures

Record every grade, material miss, elapsed time, reported model usage or cost,
manual intervention, and harness limitation. Unknown usage is `unknown`, not
zero. Preserve a failing baseline. Do not edit the corpus and rerun until the
first result looks successful.

## Reusable architecture

The reusable parts, and the parts that must not be reused, are recorded here.
Tracked in `GPS-334` through `GPS-337`.

**Extracted and versioned.** The question bank is data (`bench/questions.json`),
keyed by id and tier. Scoring is computed (`bench/usefulness.py`), including the
refusal-tier inversion, and carries its own self-test. The cohort manifest and
fanout runner are versioned, and the fanout result schema already records run id,
cohort digest, binary digest, invocation flags, effective commands, and
partial-run state.

**Still manual.** The role fan-out and the grading pass. Their shape is stable —
independent source reviewers per tier, one export-only reader, one grader that
never repairs the reader's answer from repository knowledge — but nothing drives
them yet.

**Not reusable, and deliberately so.** The questions themselves. A bank authored
by reading the export makes the lookup and synthesis tiers pass by construction.
Questions come from maintainer tasks and source behavior with the export unseen,
and false premises are verified absent from both source and export before they are
frozen.

**Two controls the procedure still lacks.**

The export-only boundary is self-reported. The pinned worktree contains two answer
keys — this file and `bench/usefulness/` — and filesystem access lets a reader
enumerate the whole export. A real consumer gets a base URL and must navigate from
`llms.txt` and `graph.json` without a directory listing.

A single reader run is one sample, not a measurement. Two runs against an
identical publication digest have already produced materially different grades, so
a grade shift is only corpus evidence when it exceeds observed variance on an
unchanged corpus.

## Making this a release dependency

The fanout began as an externality: a disposable sandbox, run by hand, consulted
when someone remembered. The measurement is good enough to stop treating it that
way. This is the intended end state, not what exists today; tracked in `GPS-341`,
`GPS-342`, and `GPS-343`, with `GPS-336` as the prerequisite that sets the
deadband.

### Two gates: an absolute floor and a ratchet

The second-grader result decides the shape. Refusal and graded tiers are not the
same kind of measurement and cannot be gated the same way — but neither of them
gets to regress silently.

**The refusal tiers are an absolute floor.** Two independent graders agreed 7/7
at a mean confidence of `1.00` on whether the reader declined. Whether a
published corpus induces confabulation is an objective property, cheap to
measure, and it is the failure that actually endangers a consumer: a corpus that
answers confidently when it should decline. Any confabulation on a release
candidate holds the draft, and there is no override.

**The graded tiers are a ratchet.** Not a threshold — the distinction is the
whole point. An absolute bar ("score must exceed 0.8") is indefensible here,
because two careful graders scored the same artifacts `0.50` and `0.56` and
disagreed on 8 of 13 labels. But a *delta* against the previous release is
defensible precisely where an absolute number is not: systematic grader strictness
cancels when you compare a grader against itself. The second grader being harsher
than the first does not matter if both releases are read by the same grader.

So the rule is the one that governs coverage and performance budgets: the score
may rise or hold; it may not fall. A drop beyond the deadband stops the release.

### What the ratchet compares against

A ratchet is only meaningful if the comparison is like-for-like. The committed
baseline records the high-water score together with the three inputs that
determine it:

| Input | Why it is pinned |
| --- | --- |
| Question bank digest | Adding a harder question legitimately lowers the score. |
| Grader model and version | A grader upgrade is not a corpus regression. |
| Publication digest | Identifies which build produced the score. |

Changing the bank or the grader **requires an explicit re-baseline commit**, not a
silent pass. That is the moment the ratchet could be gamed, so it is the moment
that has to be visible in history: re-baselining is a diff with a reason, reviewed
like any other.

### The deadband, and why it is still missing

The ratchet needs a deadband, or normal run-to-run noise trips it on every
release. The deadband is the measured spread of the score across repeated reader
runs on an identical publication — `GPS-336`.

Until that number exists the ratchet cannot be set honestly. Two options, and the
second is wrong:

1. Ship the ratchet in **recording mode**: compute the delta, publish it, fail
   nothing. Measure variance, then enable enforcement.
2. Guess a deadband now. This fabricates the baseline the gate depends on, and a
   guessed value either flaps on noise or is so wide it never fires — both of
   which teach everyone to ignore the gate.

Take the first. The gate's credibility is the asset; a gate that cries wolf is
worse than no gate.

### Regression is a stop, not a veto

A release can be legitimate while scoring worse: a deliberately harder bank, a
corpus reorganisation that pays off later, an urgent fix to something unrelated.
The gate's job is to make that a **decision** rather than an accident.

A regression beyond the deadband halts the draft and requires a recorded override
naming the reason, stored with the release alongside the score. The release can
proceed; it cannot proceed quietly. An override with no stated reason is a failed
release, and a sequence of overrides is itself the signal that the ratchet is
being routed around rather than respected.

### What good looks like, derived rather than asserted

Patching the documents a question bank happens to fail produces a corpus tuned to
that bank. The loop only improves the product if each iteration yields a property
that generalises to documents nobody asked about — and to corpora generated from
foreign repositories, where no bank exists at all.

Classifying the 0.2.1 misses by the *kind* of fact that was missing, rather than
by subject, produced one. Mean continuous score by fact kind:

| Fact kind | The question it answers | Mean score |
| --- | --- | --- |
| mechanism | How does it work, step by step? | `0.65` |
| boundary | What does it explicitly NOT do? | `0.61` |
| enforcement | Enforced by the tool, or only a convention? | `0.53` |
| guarantee | What is promised to hold, and what survives failure? | `0.52` |
| consequence | What else must I do, and what does this change? | `0.17` |

The correlation between a question's score and whether a maintainer needs that
fact *before performing a mutating action* is **`-0.59`**. The corpus is weakest
exactly where it is load-bearing. Describing how something works is the easy half,
and it is the half already done.

Auditing the published documents directly on the same five axes — different unit,
different objects, no questions involved — ranks them the same way: `guarantee`
`2.71` and `consequence` `2.73` weakest, `boundary` `2.95` and `mechanism` `2.91`
strongest, on a 0–4 scale. Two independent measurements agreeing is the reason to
treat this as a property of the corpus rather than an artifact of the bank.

The rubric is `bench/rubric.json`. It is deliberately not tied to the questions.

### The loop

1. **Audit** every document on the five axes. Cheap, no reader required.
2. **Repair** the weakest axis of the weakest document, from source evidence.
3. **Re-audit** to confirm the axis moved.
4. **Re-measure** with the question bank at release, where the reader runs.
5. **Keep** the repair when movement exceeds variance; otherwise it was noise.

The two measurements check each other, and that coupling is the point:

- Rubric rises, questions do not → the rubric is wrong, or the repair was cosmetic.
- Questions rise, rubric does not → the bank is being fitted. Treat as a defect.
- Both rise → a real property improved, and it improved for documents the bank
  never probed.

The first target the rubric found is one no question pointed at: `architecture.md`
scores `1.58` on guarantee against a corpus mean of `2.71`.

### The flywheel: A/B where the answer is unknown

Most repairs are obvious once the rubric names the gap — the fact is missing, add
it. Some are not. When the uncertainty is *how* to state something rather than
*what* to state, guessing and shipping teaches nothing, and the next person
guesses again.

Those cases get an experiment. Both arms carry identical facts, verified by
substring rather than by eye, and differ in exactly one dimension. Each arm is
audited `k` times so the comparison has a noise floor, and the winner ships only
when the gap exceeds that floor.

This is cheap enough to be routine: a document audit is sub-second, so a
three-arm experiment with five replicates costs about as much as reading the file.

**First experiment — `architecture.md` guarantees.** The rubric scored it `1.58`,
the weakest axis of any document and one no question in the bank probed. The open
question was placement: one named section, or each guarantee attached to the
mechanism it constrains?

| Arm | Placement | guarantee | all-axis mean |
| --- | --- | --- | --- |
| base | — | `1.66` | `2.48` |
| **A** | one named `Guarantees` section | **`3.22`** | **`3.07`** |
| B | inline beside each mechanism | `2.75` | `2.93` |

Within-arm noise was `0.019` standard deviations. A beat B by `0.47` on the target
axis — **24 standard deviations** — and also won boundary, consequence and
enforcement. Identical facts. Only placement differed.

**Rule learned:** a named, discoverable section outperforms contextual embedding
for guarantees. That is a property of documents, not of this document, so it
applies to every corpus Poolboy generates, including from repositories that have
no documentation to convert.

**The caveat that keeps this honest.** The audit measures what a grader can
*locate* in a document. Whether it helps a reader *answer* is the separate, slower
measurement at release. The cheap proxy chooses between variants; the expensive
test validates the proxy. If question scores ever rise while the rubric does not —
or the reverse — the proxy is wrong and gets rebuilt, not explained away.

Experiments are retained under `bench/usefulness/` with their replicate counts,
noise floor, and margin in standard deviations. An experiment reported without its
noise floor is an anecdote.

### The coupling check, and what it indicted

Three rubric-driven iterations ran before any question-level validation. That was
too many: the A/B result showed placement alone moves the rubric, so the proxy
could have been rewarding structure rather than usefulness. Two readers were run
against the repaired publication (`0a450237…`) and graded against the same bank.

Graded mean rose `0.556` → `0.666`. Reader spread between the two readers was
`0.048` on the graded tiers and `0.001` on the refusal tiers, so roughly `0.10`
is the smallest movement worth believing.

**Validated.** Two repairs moved far beyond that floor:

| Question | Before | After | Repair |
| --- | --- | --- | --- |
| `H5` versioned help | `0.01` | `0.96` | the missing document, written from source |
| `H3` move semantics | `0.17` | `0.49` | the `[[render]]` consequence of moving a generated file |

`H5` moved twenty times the reader spread. A fact that was absent became
available, and the reader used it.

**Not validated, and this is the important half.** The iteration-3 repairs raised
the enforcement axis `3.12` → `3.49` and guarantee `2.93` → `3.22`, yet the
questions that probe those properties did not move: `M1` `-0.06`, `H1` `-0.01`,
`H2` `+0.01`, `H4` `+0.03`, and `M2` fell `-0.13`. The rubric moved; the reader
did not benefit.

That is the divergence the design predicted and said would mean the proxy is
wrong rather than the repair being good. The honest reading: adding a section
headed `Guarantees` reliably raises an axis named guarantee, and for facts the
reader could already infer, it adds nothing it can use.

So the rubric earns a narrower claim than three iterations of rising numbers
suggested. It is a reliable detector of **absence** — `H5` and `H3` were genuinely
missing facts and fixing them moved everything. It is not yet evidence that
restating an existing fact more prominently helps anyone, and it should not be
treated as a quality score on its own.

What changes as a result:

- A rubric gain counts as an improvement only once a question-level run confirms
  it. Rubric-only iterations stop at one before validating.
- The axis means stay as a repair-targeting signal, not as a premium gate. The
  premium table's rubric column is provisional until more repairs are validated
  against readers.
- `M2` regressing while its documents' rubric scores rose is the sharpest single
  warning in this run and needs its own investigation.

### Two axes, deliberately not combined

Both measurements run from here on, at every iteration. They are reported as a
pair and never averaged into one number.

The temptation is obvious: one score is easier to track and easier to gate on.
But this run is the argument against it. A composite would have read "rubric up
`0.21`, questions up `0.11`, steady progress" and buried the only finding worth
having — that the structural repairs moved one axis and not the other. The
disagreement is the diagnostic. Averaging deletes it.

They answer different questions and have different costs, so they get different
jobs:

| | Rubric | Question bank |
| --- | --- | --- |
| Asks | does the document *state* it? | can a reader *use* it? |
| Cost | sub-second, no reader | minutes, model calls |
| Role | leading indicator: finds and ranks candidate gaps | lagging indicator: decides whether anything improved |
| Authority | targeting only | the measurement of record |

The pair has four outcomes, and three of them are informative:

| Rubric | Questions | Reading | Action |
| --- | --- | --- | --- |
| up | up | a real gap was closed | keep; this is the only state that counts as improvement |
| up | flat | restating, not adding | do not count it; ask what fact is still missing |
| flat | up | the rubric missed an axis that mattered | extend the rubric, not the corpus |
| flat | flat | no effect | revert the change rather than leave it |

The second row is where this run landed for five of thirteen questions, and it is
the row a single composite score cannot express.

### Attribution, so the gain is not oversold

The `+0.11` graded gain is not spread across the repairs. It decomposes almost
entirely into two content additions:

| Source | Contribution to graded mean |
| --- | --- |
| `H5`, a missing document written from source | `+0.073` |
| `H3`, a missing consequence stated | `+0.025` |
| every structural repair combined | `+0.013`, inside reader spread |

Three iterations of rubric work produced a change indistinguishable from noise at
the question level, except where a fact was genuinely absent. The rubric's
demonstrated power is finding absence. Its power to improve presentation of facts
already present is, so far, unproven — and will stay labelled unproven until a
structural repair clears the reader spread on its own.

### Premium

The thresholds that define done, rather than merely better. Three columns, read
together and never summed:

| Gate | Refusal | Graded score | Rubric |
| --- | --- | --- | --- |
| **Floor** | `1.00`, no override | — | — |
| **Working** | `1.00` | ≥ `0.75` | no axis mean < `3.0` |
| **Premium** | `1.00` | ≥ `0.90`, every tier ≥ `0.85`, no question < `0.60` | every non-index document ≥ `3.0` on every axis |

Premium additionally requires the graded score to hold across three consecutive
runs. A single run clearing `0.90` is a sample, not a property, and the reason
this section exists is that a single run already fooled us once.

The rubric column is **advisory**. It is listed because a corpus that cannot state
its own guarantees is not premium whatever it scores, but it does not gate on its
own and a rubric gain is not progress until a reader run confirms it. Only the
refusal and graded columns can hold a release.

Measured now: refusal `1.00` (floor met), graded `0.666` (working not yet met),
rubric 44 of 45 axes at or above `3.0` with one generated-document gap open.

`index.md` is exempt from the rubric. A navigation page describes no behavior of
its own, so its low axis scores are correct rather than a defect.

### Score on the mass, not the label

Four grade labels across thirteen questions produced three distinct values. The
same grades as probability distributions produced twelve. `H2` and `H4` are both
`partial` by label, but score `0.53` and `0.36` — a real difference in how close
the corpus is, invisible to the label.

The scorer therefore accepts either a label or a probability map per question.
The argmax still decides pass and fail, so the gate stays crisp; the mass decides
the score, so movement is visible before it crosses a bucket boundary. This is
where the gradation the benchmark needs actually comes from — finer questions
help, but finer *reading* of the same questions helps more.

### Where it runs

Not per pull request: the reader needs a built publication, and the roles cost
model calls. The natural seam is the release workflow, which already builds the
publication, already creates the GitHub release as a draft, and already publishes
that draft only after a separate job proves the artifact works. The usefulness
benchmark becomes another such job: it runs against the built publication, the
refusal gate can hold the draft, and the graded evidence is uploaded as a release
asset beside the help snapshot.

That makes the measurement a dependency of shipping rather than a thing someone
remembers to run.

### Cohort gradation

The present cohort varies framework and size, which predicts conversion mechanics
but not corpus usefulness. The axes that should vary, because they are what makes
documentation hard:

- **Documentation state** — none, sparse, fragmented and stale, or actively maintained.
- **Derivation** — authored prose, generated from code, or reference tables.
- **Genre** — how-to guide, API reference, architecture narrative, operational runbook.
- **Coupling** — a docs-only repository against documentation living beside the implementation it describes.

The most important missing tier is the **undocumented repository**, where there is
no prose to convert and the corpus must be derived from code. Nothing in the
current cohort tests it, and it is the strongest form of the product claim. A
repository with good documentation mostly tests conversion; a repository with
none tests whether Poolboy can produce documentation worth reading.

### From verdicts to repairs

The reason this loop can improve the product rather than just score it: a failing
question already carries its own repair instruction. Ground truth states the
minimum facts and cites the source that proves them, so a miss identifies the
missing fact, the evidence for it, and which document should have carried it.

That is a draft corpus patch, not a grade. It was already done by hand once —
`GPS-328` through `GPS-332` and `GPS-340` were written directly from recorded
misses. The loop is to make that step mechanical: emit a gap record per failing
question, land the repair, regrade, and confirm the score moved by more than
variance.

For foreign corpora the same loop points at conversion rules instead of
documents. A reader that cannot answer because a framework route was dropped is
evidence about an ingestion utility, not about prose, and it routes to `GPS-339`.

### The obvious way this goes wrong

A loop that writes documents until its own benchmark passes is reward hacking
with extra steps. Three guards, all of which exist today and must survive
automation:

- Questions are authored from maintainer tasks and source behavior **with the
  export unseen**. A bank written by reading the corpus passes by construction.
- Ground truth comes from source reviewers reading implementation and tests, not
  from the corpus under test. The corpus never grades itself.
- The refusal tiers punish the degenerate strategy directly. A corpus padded
  until it answers everything starts answering the false premises too, and that
  is the blocking signal.

A repair is legitimate when it adds a fact the source supports and a maintainer
needs. It is illegitimate when it adds text shaped like the question.

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

The difficulty gradient is also new information, and it holds under both graders
even though the per-question grades do not: lookup is strongest, synthesis is
weaker, and operational reasoning is weakest. Do not read the hand grader's
`M` 3/4 as "synthesis nearly solved" — the second grader scores that tier 0/4.
What is durable is the ordering, not the rate.

### Second grader: what is objectively gradable

The same artifacts were regraded independently by a calibrated structured-decision
model (`jev-1.13`), one decision per question, scored against the same stated
minimum facts. Verdicts are retained as `grades-jev.json` beside the hand grades.

Both graders return the same gate: **fail**. They agree on 12 of 20 questions, and
the split is not random.

| Tier group | Agreement | Mean grader confidence |
| --- | --- | --- |
| Refusal (`U`, `F`, `X`) | 7/7 | 1.00 |
| Graded (`E`, `M`, `H`) | 5/13 | 0.74 |

The refusal tiers are objectively gradable. Both graders agree unanimously and at
full confidence that the reader refused every invented feature and every
out-of-domain question. Whether a reader confabulates is a fact about the answer.

The graded tiers are not. The second grader is systematically stricter, reading
`M1`, `M2`, `M3`, `H1`, and `H2` as partial where the hand grader read them as
correct. Neither verdict is obviously right: "materially complete against the
minimum facts" is a judgment call, and two careful graders land differently on it.

Calibration separates the two cases cleanly. Where the graders agreed, mean
confidence was `0.96` and the mean margin over the runner-up grade was `0.94`.
Where they disagreed, confidence fell to `0.64` and the margin to `0.51`. The
contested questions were flagged as contested without being told which ones they
were.

What this changes:

- The **headline is robust**. The gate fails under both graders, and the tier
  ordering — lookup strongest, operational reasoning weakest — survives.
- A **single per-question grade is not evidence**. Reporting `M1` as correct was
  one grader's reading, not a measured property of the corpus.
- The **refusal tiers are the reliable instrument**. They are cheap, unanimous,
  and measure the failure mode that actually endangers an agent: a corpus that
  answers confidently when it should decline.

Cost of the second opinion: 7.1 seconds for the graded tier and 0.3 for the
refusal tier. The hand grader took 2m 48s and died before writing its report.

### What is not new

`H3`, `H4`, and `H5` reproduce the `0.2.0` misses against an identical corpus, and
they map to the gaps already filed as `GPS-330`, `GPS-331`, and `GPS-332`. `M4`
adds one more: the export omits `verify` and `approve` operational mechanics,
including the approved-digest behavior, the release-age gate, and the guarantee
that verification never silently downgrades to key-continuity trust.

`H1` and `H2` graded correct here after grading partial at `0.2.0` on the same
bytes. That difference is reader variance, not corpus improvement, and is the
reason grade movement on an unchanged corpus cannot be read as progress.

## Compatibility fanout: 2026-09-29, Poolboy 0.2.1

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
