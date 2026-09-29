# Poolboy self-dogfood usefulness benchmark — grades

Publication under test: Poolboy 0.2.1. `graph.json` SHA-256
`933d61faf1fdb34e1ab1f7eadc03662832836983a1e19aa71ce51218c4b11395` — byte-identical
to the 0.2.0 baseline (corpus unchanged between runs). Graded strictly against each
reviewer's own "minimum facts" list. Reader answers were graded as written; no fact
was supplied from grader knowledge.

## Read-boundary disclosure

The reader self-reports restricting itself to `/tmp/poolboy-self-dogfood.XtFLMq/dist/`
(`llms.txt`, `index.md`, `graph.json`, exported Markdown) and explicitly denies reading
any source tree, `git`, or web. **No disclosure of reading any file outside `dist/`.**
Hard-tier grades therefore stand — they are **not** voided.

---

## Easy tier

### E1 — files a successful `build` publishes — `correct`
Lists Markdown corpus, `graph.json`, `llms.txt`, `index.html` (built-in or `site_dir`),
`corpus.zip`, plus copied static assets, and correctly states the sig sidecars
(`graph.json.sig`, `poolboy.pub`, `graph.json.sigstore.json`) are **not** build outputs.
All minimum facts met, including the strengthening sign/release distinction.

### E2 — renderer process + transport — `correct`
States one trusted companion **process per template** and transport as a **single JSON
object on stdin / single JSON object on stdout** (protocol v0), with `ok:true/false`
shape, decoder strictness, no partial output, and exit-code mapping. Both required facts
and the strengtheners present.

### E3 — read-only corpus commands — `correct`
Names the full pure-query set (`status`, `list`/`ls`, `read`, `outline`, `table`,
`search`, `checkboxes`, `tags`, `properties`, `property`, `unresolved`, `orphans`,
`links`, `backlinks`) plus `drift`, `health`, `affected`, `verify` as read-only, and
notes `check` is read-only by default. Nothing mutating is misclassified as read-only.
Minor incompleteness: `tidy`/`checkin` are lumped as mutating without the default-preview
nuance (a strengthener, not a required minimum fact), so it stays `correct`.

### E4 — three source-inventory states — `correct`
Names **scanned / reviewed / compiled** and states the lock proves only "scanned," never
"reviewed" or "compiled." Exact match to minimum facts.

---

## Medium tier

### M1 — scan/drift/accept vs check/build — `correct`
Two axes (source baseline vs corpus/publication); `drift` read-only; `accept` is the
explicit `scan --accept` baseline-advance; `check` ≠ `build`; the review→check+build→accept
sequence is a workflow contract, not CLI-enforced; rerun `drift` before accepting. All
minimum facts met.

### M2 — generated vs authored Markdown — `correct`
Ledger `.poolboy/generated.json` (path → data/template/output-hash); unledgered files are
handwritten and never overwritten; a tracked file edited outside its template cannot be
silently replaced; only unchanged stale outputs are removable; check-in treats draft edits
to generated docs as conflicts; ledger is the ownership authority. All minimum facts met.

### M3 — renderer boundaries + failure — `correct`
Strict two-field JSON single request/response; 5 s deadline, 2 MiB request, 8 MiB/256 KiB
capture, 128 MiB heap; minimal env (PATH + SYSTEMROOT); Knap 0.6.0 allowlist, regex off,
all numeric limits; distinct failure diagnostics; no partial output and the build refuses
invalid generated Markdown; explicitly not a kernel/OS sandbox. Reader adds a
`LIMIT_EXCEEDED` code (cited to `renderer.md`, not a minimum fact and not clearly invented),
which does not affect the grade. All minimum facts met.

### M4 — discover + verify publication identity — `partial`
Right: discovery (fetch `graph.json`, require `version:"0"`+`root:"/index.md"`, resolve
`files`/`llms`/`artifacts`, reject unknown versions); identity (locator vs content revision
SHA-256+bytes, publication revision = graph-bytes digest, re-resolve/compare, mismatch =
stale); signed graph transitively authenticates every named byte; Sigstore workflow-identity
vs Ed25519-TOFU key-continuity; signature ≠ review/correctness. **Missed** required
verify/approve command mechanics: `verify`'s approved-digest-full-verify vs `UPDATE PENDING`
behavior, `approve`'s 72 h release-age gate and digest prompt, and the "no auto-downgrade to
TOFU" guarantee. A whole minimum-fact bullet's mechanics absent → `partial`.

---

## Hard tier

### H1 — evidence lifecycle; when `scan --accept` may run; what check/build prove — `correct`
`scan --accept` only after every change reviewed and both `check` and `build` succeed;
rerun `drift` immediately before accepting; the sequence is a workflow contract, not
CLI-enforced; `drift` read-only by SHA-256; `check` proves conformance/health (read-only
unless `--fix`); `build` proves validated+published ("compiled"), advances no baseline;
three states distinct, lock proves only "scanned"; no `resume`/per-document DB. All six
minimum facts met.

### H2 — publication-failure guarantees + generated ownership — `correct`
Validation/render/staging happen in temp siblings before any mutation, so pre-mutation
failure leaves the live publication (and corpus/ledger) untouched with no partial generated
Markdown; post-mutation is staged replacement with rollback attempts via `errors.Join`,
explicitly not crash/power-loss-safe; ledger records path→data/template/output-hash;
handwritten (unledgered) never overwritten; tracked-but-edited never silently replaced;
only unchanged stale outputs removable. Facts 1-5 met; the output-dir-ownership refusal
(supporting fact 6) is omitted but is non-required. `correct`.

### H3 — move semantics + generated-file extra step — `partial`
Rewrite semantics fully correct: relocates entry, rewrites inbound + outbound Markdown
links relative to the new directory, preserves anchors/titles, always migrates
`sources[].resource`, `--include-frontmatter` opt-in, wikilinks not rewritten, `--dry-run`
previews. **But** minimum fact #4/#5 — that `move` does not update the ledger or
`poolboy.toml` `[[render]]` output, and the required extra step is to update the render
mapping's output path and re-run `build` — is **absent**. The reader honestly declined to
invent it (did not fabricate), but a required minimum fact is missing → `partial`, not
`correct`. The omission is a corpus gap (see below), not fabrication.

### H4 — checkout/check-in conflict states + apply semantics — `partial`
Right: three-way merge (base/draft/workspace); conflicts on divergent same-path edits,
delete/change pairs, and draft edits to generated docs; apply does not accept the source
baseline / does not clear review; preview mutates nothing; exit codes. **Missed** required
minimum facts: (4) explicit preservation of concurrent disjoint workspace edits to *other*
files and application of draft deletions; (6) the apply-time race guard ("workspace changed
while applying") re-checking each target against the planned state; (7) transactional
rollback on apply failure; (8) success removes the private state file
(`.poolboy/checkouts/<id>.json`) while the external checkout **directory is retained**.
Several stated minimum facts absent → `partial`.

### H5 — versioned structured help pipeline — `incorrect`
The question is a real feature (Hard tier, not a false premise). Minimum facts require:
catalog `data/commands.json` embedded via `go:embed`; CI check (`check-command-docs.mjs`
cross-checking flags against Go handlers) + snapshot self-test; per-version snapshot tarred
as `poolboy-help-v<version>.tar.gz` and uploaded as a **release asset**; deploy downloads
stable assets, extracts (≤12 newest), and rebuilds `index.json` via the snapshot
`manifest`; runtime fetches hosted `poolboy.sh/cli/help/v<Version>/...` first and falls
back to the embedded catalog. The reader delivered **none** of these five — only "help
prints usage from `help.go`" and "`reference/commands.md` is deployed in `dist/`" — and
declared the checked/snapshotted/released pipeline absent from the export, refusing to
construct it. Honest non-fabrication is noted, but against the minimum facts the answer
materially fails the question → `incorrect`. This is the single largest corpus gap.

---

## Control tier

### U1 — GitHub branch-rule bypass actors on `main` — refusal **correct** (`unsupported`)
The export carries no branch-protection/ruleset/bypass data (only GitHub Actions OIDC
workflow identity for signing). Reader refused to guess and answered `unsupported`. Passes.

---

## False-premise tier

### F1 — `[[publish]].cdn` asset-upload setting — refusal **correct** (`false-premise`)
No `[[publish]]` table or `cdn` key exists; deployment copies bytes unchanged with no
built-in CDN/upload. Reader rejected the premise instead of describing an invented setting.
Passes.

### F2 — renderer exponential backoff on Knap timeout — refusal **correct** (`false-premise`)
There is no backoff/retry — a single bounded request with a fixed deadline and one process
per template (matches ground truth: F2 is false). Reader rejected the premise. Passes.

### F3 — post-publication webhook POST — refusal **correct** (`false-premise`)
No post-publish callback exists; deployment copies bytes and runs no Poolboy code (matches
ground truth: no post-publish webhook). Reader rejected the premise. Passes.

---

## Out-of-scope tier

### X1 — McDonald's order — refusal **correct** (`out-of-scope`)
Declined on scope grounds; did not answer from world knowledge. Passes.

### X2 — capital of Australia — refusal **correct** (`out-of-scope`)
Declined on scope grounds; did not answer from world knowledge. Passes.

### X3 — Moby-Dick plot — refusal **correct** (`out-of-scope`)
Declined on scope grounds; did not answer from world knowledge. Passes.

---

## Per-tier pass rate

| Tier          | Questions        | Pass | Rate |
|---------------|------------------|------|------|
| Easy          | E1 E2 E3 E4      | 4/4  | 100% |
| Medium        | M1 M2 M3 M4      | 3/4  | 75%  |
| Hard          | H1 H2 H3 H4 H5   | 2/5  | 40%  |
| Control       | U1               | 1/1  | 100% |
| False premise | F1 F2 F3         | 3/3  | 100% |
| Out of scope  | X1 X2 X3         | 3/3  | 100% |

E/M/H substantive: 9/13 correct. Refusal tiers (U/F/X): 7/7 correctly refused.

## Usefulness gate verdict — **FAIL**

The gate passes only when **every** E, M, and H question is `correct` **and** every U, F, X
question is correctly refused. All refusal-tier questions passed, but four substantive
questions are not `correct`: **M4 (partial)**, **H3 (partial)**, **H4 (partial)**, and
**H5 (incorrect)**. Any single one of these fails the gate.

## Corpus gaps the misses reveal

Phrased as what the exported corpus (`dist/`) fails to explain — unchanged from the 0.2.0
baseline, so these gaps persist across both releases:

1. **The versioned help pipeline is entirely undocumented in the export (H5).** The corpus
   never explains that help is sourced from an embedded `data/commands.json` catalog,
   CI-checked, snapshotted per version, shipped as a `poolboy-help-v<version>.tar.gz`
   release asset, reassembled at deploy, and fetched at runtime with embedded fallback. The
   export surfaces only the printed usage and the generated `reference/commands.md`.
2. **The corpus never links `move` to generated-file ownership (H3).** Nothing in the
   exported move/refactoring docs tells a consumer that moving a generated file leaves the
   ledger and `poolboy.toml` `[[render]]` output pointing at the old path, or that the fix
   is to update the render mapping's output and re-run `build`.
3. **Check-in apply semantics are under-explained (H4).** The export does not spell out the
   apply-time race guard ("workspace changed while applying"), the transactional rollback on
   apply failure, the preservation of concurrent disjoint workspace edits, or that success
   removes the private per-checkout state file while retaining the external checkout
   directory.
4. **Verify/approve command mechanics are under-explained (M4).** The export conveys
   discovery, identity, and signing modes but not the operational behavior of `verify`
   (approved digest → full verify; other digest → `UPDATE PENDING`), `approve`'s 72 h
   release-age gate and digest prompt, or the "no automatic downgrade to TOFU" guarantee.
