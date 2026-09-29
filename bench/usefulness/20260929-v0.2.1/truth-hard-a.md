# Ground truth — Hard questions H1, H2, H3

Specimen: `/tmp/poolboy-self-dogfood.XtFLMq` @ `66aafd9` (Poolboy 0.2.1).
Established from implementation (`cmd/poolboy/`, `internal/`, `index/`) and canonical
source docs (`docs/`). `dist/` was NOT read. Read-only; no files modified.

Cross-cutting enforcement principle (recurs in all three answers): Poolboy keeps ONE
project-wide baseline and enforces gates in code at exactly two kinds of places —
(a) `source.ScanEvidence` refuses to replace an existing lock without `--accept`, and
(b) `compiler.Build` refuses to overwrite handwritten/foreign files and orders all
validation/render/staging before any mutation. The *review* workflow ("review, then
check + build, then accept") is a documented contract, NOT semantic enforcement.

---

## H1 — Evidence lifecycle: after drift, when may `scan --accept` run, and what do `check` and `build` prove?

### Correct answer

**The three inventory states are kept deliberately separate** (`docs/source-inventory-and-drift.md` §"Three separate states", lines 143-155):
- **scanned** — a lock was written, or a current inventory was compared;
- **reviewed** — a human or agent examined the relevant evidence;
- **compiled** — a `build` validated and published a corpus.

The existing lock proves none of the latter two.

**Baseline / drift shape.** The baseline lock is `.poolboy/sources.lock.json`, schema
version `"0"`, a `files` map keyed by normalized project-relative slash paths; each entry
records only a coarse `type`, admitted byte length, and the SHA-256 of admitted bytes —
no contents, timestamps, or host paths (doc lines 22-34; `internal/source/source.go`
`Inventory`/`Entry`, `Version = "0"` at line 34). `drift` is read-only: it computes a
fresh bounded scan (never writing the lock) and reports each path as `added`, `removed`,
or `modified` by comparing **SHA-256 only** — matching bytes produce no change — sorted by
path then status (`internal/source/source.go:DriftEvidence` lines 434-470; `scanCurrent`
comment lines 49-51 "so Drift cannot accidentally refresh the lock"). If no baseline
exists, `drift` errors "run scan first" (lines 437-439).

**When `scan --accept` may run.**
- *CLI-enforced precondition (the only one):* an existing regular lock file. `scan`
  without `--accept` on an existing baseline returns `ErrBaselineExists`
  ("baseline already exists; review drift, then scan with accept"); `--accept` is
  required to replace it (`internal/source/source.go:ScanEvidence` lines 388-390; the
  sentinel at line 41). The initial `scan` (no lock) writes the baseline *without*
  `--accept` via a create-only hard-link so a concurrent scan cannot clobber it
  (`writeLock(b, current, !accept)` line 421; `writeLock` createOnly semantics lines
  193-231). A non-regular/symlink lock is rejected (lines 385-387).
- *Documented workflow contract (NOT enforced by the CLI):* "Only after every
  outstanding change has been reviewed and both `check` and `build` succeed may the
  workflow run `scan --accept`. Rerun `drift` before acceptance to catch source changes
  that occurred during review. There is no per-document review database or `resume`
  command: this is a workflow contract, not semantic enforcement by the CLI."
  (`docs/source-inventory-and-drift.md` lines 130-134). `cmdBuild` and `cmdCheck`
  never call scan, and `ScanEvidence` never checks check/build state — confirming the
  gate is convention, not code.

**What `scan --accept` does when it runs.** It computes health against the *previous*
baseline first, completes the fresh scan, appends any newly-detected likely-sensitive
paths to `.poolboyignore` and rescans, then atomically replaces the lock and prints the
pre-accept evidence — so accepting the new lock does not erase the findings that
justified it (doc lines 88-91; `internal/source/source.go:ScanEvidence` lines 391,
399-424; `cmd/poolboy/product.go:cmdScan` line 109 `health.Inspect(b, previous, inventory)`).

**What `check` proves.** `check` reports OKF conformance and corpus-health issues from the
index; it is read-only by default and exits `1` if any issue is `Level=="error"`.
`check --fix` applies only explicitly-safe repairs (e.g. syncing the root `okf_version`)
then reindexes. It does NOT render generated documents and does NOT publish
(`cmd/poolboy/commands.go:cmdCheck` lines 451-511; `docs/maintenance-commands.md` lines
93-96; `docs/build-and-render.md` lines 161-167). So `check` proves the authored corpus
is conformant/healthy — nothing about rendering, publication, or review.

**What `build` proves.** `build` validates roots/spec/mappings, validates the authoring
corpus + rendered output as OKF, renders via the trusted companion, and atomically
publishes the deterministic static output; on validation failure it prints issues and
exits `1` (`cmd/poolboy/product.go:cmdBuild` lines 56-89 → `compiler.Build`). A
successful `build` is exactly the "compiled" state: it proves the corpus validated and a
publication was produced. It does NOT prove the sources were reviewed and does NOT
advance the source baseline (`docs/build-and-render.md` lines 161-167;
`docs/source-inventory-and-drift.md` lines 147-155). Neither `check` nor `build` writes
`sources.lock.json`.

**Reconciliation note.** A refresh or interrupted review never advances the project-wide
baseline; `checkout`/`checkin` transport corpus edits but record no evidence judgment and
never advance the lock (doc lines 115-134).

### Evidence
- `docs/source-inventory-and-drift.md` — §"Baseline shape" (22-34), §"Scan, drift, and accept" (74-96), §"Review and reconciliation workflow" (115-134, esp. 130-134 the un-enforced contract), §"Three separate states" (143-155).
- `internal/source/source.go` — package doc (1-16), `ErrBaselineExists` (39-41), `ScanEvidence` accept gate (370-425, esp. 388-390, 421), `DriftEvidence` (434-470), `Version` (34).
- `internal/source/scan.go` — `scanCurrent` "Drift cannot refresh the lock" (49-77).
- `cmd/poolboy/product.go` — `cmdScan` pre-accept health (91-146, esp. 109), `cmdDrift` read-only (148-189), `cmdBuild` (56-89).
- `cmd/poolboy/commands.go` — `cmdCheck` (451-511).
- `docs/build-and-render.md` — §"Build versus review" (161-167).

### Minimum facts a consumer answer MUST contain to grade `correct`
1. `scan --accept` is the explicit baseline-advance/replace step; replacing an existing baseline **requires** `--accept` (a non-accepting scan on an existing lock refuses / errors).
2. The "review + `check` + `build` succeed, rerun `drift`, then accept" sequence is a **workflow contract, not enforced by the CLI**.
3. `drift` is read-only and compares SHA-256 (added/removed/modified) without writing the lock; drift needs an existing baseline.
4. `check` proves conformance/health of the authored corpus only — it does not render or publish (and is read-only unless `--fix`).
5. `build` proves the corpus validated and was published (the "compiled" state); it does not advance the source baseline and does not prove "reviewed".
6. (Strong) The three states scanned/reviewed/compiled are distinct; the lock proves only "scanned".

---

## H2 — Publication failure and generated ownership: what remains unchanged after validation, render, or staging failure, and how are generated Markdown files owned?

### Correct answer

**The build orders all validation, rendering, and staging ahead of any mutation.** In
`compiler.Build` (`internal/compiler/build.go:46-226`) the first mutation of on-disk state
is `materializeGenerated` at **line 182**. Everything before it is read-only or writes only
into temporary sibling directories:

- **Validation failure → nothing on disk is mutated.** Root/containment checks
  (`validateRoots`, 53), existing-publication ownership check (`validateExistingPublication`,
  57), spec `0.2` and unknown-setting checks (60-67), mapping validation (`validateMappings`,
  68), landing resolution (71), ledger load (75), corpus collection + canonical-path
  collision checks (79-85), post-render ownership refusals (`verifyRenderTargets` 103,
  `planLedger` 106), candidate budget/collision/OKF validation (`validateDocuments`, 121-129)
  all return before line 182. Live publication, live corpus, and `.poolboy/generated.json`
  are untouched.
- **Render failure → nothing on disk is mutated.** `renderer.Resolve` + `renderDocuments`
  (89-101) run before staging; an error returns immediately (before line 182). The
  companion runs against inputs only; no output file is written.
- **Staging failure → nothing live is mutated.** The candidate corpus is written into a
  temp dir `os.MkdirTemp(...".poolboy-corpus-build-*")` (131) and the publication into
  `os.MkdirTemp(...".poolboy-publication-*")` (161), each with `defer os.RemoveAll` cleanup
  (135, 165). Staged index, landing artifacts, `graph.json`, `llms.txt` are all written into
  the temp publication dir (139-177). "The live corpus is unchanged at this point"
  (`docs/build-and-render.md` step 7, line 60). A failure here removes the temp dirs and
  returns; the live publication directory, the authoring corpus, and the ledger are untouched.

This is the doc's guarantee verbatim: "Validation, rendering, and staging happen before
mutation; those failures leave the live publication untouched" (`docs/build-and-render.md`
lines 71-72; `Build` doc comment 42-45 "A failed build leaves the previous publication
untouched and never returns partial generated Markdown").

**After mutation begins (line 182 onward): staged replacement with rollback attempts, not a
crash-safety guarantee.** `materializeGenerated` records a per-file `fileBackup` before
writing/removing each generated Markdown file and, on any error, restores the backups and
joins the errors (`internal/compiler/ledger.go:200-255`). `Build` defers
`rollbackGenerated()` until commit (build.go:186-193). The publication swap moves the old
output aside to a temp backup then renames the stage into place, restoring the backup if the
rename fails (`swapPublication` 420-453). If the final ledger rename fails, `swap.restore()`
puts the previous publication back and the error is joined (212-220). Only after both the
directory swap and the ledger rename succeed is the build committed; a failure to delete the
old backup at `commit` is harmless (221-224; `swapPublication.commit` 475-484). The doc is
explicit this is "staged replacement with rollback attempts, not a crash- or power-loss
recovery guarantee" and does not promise uninterrupted availability across the renames
(`docs/build-and-render.md` lines 72-77).

**Output-ownership refusal (what protects a foreign output directory).**
`validateExistingPublication` (`internal/compiler/publication.go:18-67`, called at build.go
57 and again at 204) permits replacing only an empty directory or a complete Poolboy-owned
publication (valid `graph.json` with version `"0"`, root `/index.md`, non-empty files). An
existing output dir that is not Poolboy-owned makes `build` fail before mutation — "Unknown
files are never silently removed by the directory swap" (doc comment 18-20;
`docs/security-boundaries.md` lines 174-178).

**How generated Markdown files are owned.** Ownership lives in the generated ledger
`.poolboy/generated.json` (schema version `"0"`), a `files` map keyed by clean relative
`.md` path; each entry records the render `data` path, the `template` path, and the SHA-256
of the normalized rendered output (`internal/compiler/ledger.go:19-28`, 106-116; doc §"Generated
ownership" lines 146-152). The ledger is the ownership authority, enforced in two places:
- `planLedger` (ledger.go:118-154) and `verifyRenderTargets` (156-199):
  - **A file with no ledger entry is handwritten by definition and is never overwritten** —
    a render whose output path already exists but is untracked fails "render output would
    overwrite handwritten Markdown" (ledger.go:149-151, 187-188).
  - **A tracked file edited outside its template cannot be silently replaced** — a hash
    mismatch fails "generated file was edited outside its template" (ledger.go:130-132,
    190-195).
  - **Only unchanged stale outputs** (tracked, no longer wanted, hash still matches) are
    eligible for removal (ledger.go:135-142); an edited stale file refuses removal (138-139).
- For portability, if generated Markdown is committed, commit `.poolboy/generated.json` and
  `.poolboy/sources.lock.json` with it; the scaffold/root-ignore policy retains both. Do not
  delete the state directory to force an overwrite or reset drift — reconcile the rendered
  output and the ledgers instead (`docs/build-and-render.md` lines 154-159).

### Evidence
- `internal/compiler/build.go` — `Build` (42-226): mutation boundary at 182; temp staging (131-177); rollback defer (186-193); ledger publish + restore (208-224). `stageLedger` (384-412), `swapPublication`/`restore`/`commit` (420-484).
- `internal/compiler/ledger.go` — `ledger`/`ledgerEntry` (19-28), `desiredLedger` (106-116), `planLedger` (118-154), `verifyRenderTargets` (156-199), `materializeGenerated` backups+rollback (200-255).
- `internal/compiler/publication.go` — `validateExistingPublication` (18-67).
- `docs/build-and-render.md` — §"Ordered build flow" (41-77, esp. 60, 71-77), §"Generated ownership" (146-159).
- `docs/security-boundaries.md` — output-ownership refusal (174-178).

### Minimum facts a consumer answer MUST contain to grade `correct`
1. Validation, render, and staging all happen **before** any mutation; a failure in any of them leaves the previous/live publication (and the authoring corpus and ledger) **unchanged** — no partial generated Markdown is left behind.
2. Staging writes into **temporary sibling directories** that are removed on failure; the live corpus/publication are only touched after staging succeeds.
3. After mutation begins it is **staged replacement with rollback attempts** (per-file backups, publication swap with restore, errors joined) — explicitly NOT a crash/power-loss safety guarantee.
4. Generated Markdown is owned via the ledger `.poolboy/generated.json` recording each generated path's template, data, and output hash.
5. A file with **no ledger entry is treated as handwritten and is never overwritten**; a tracked file **edited outside its template cannot be silently replaced**; only unchanged stale generated files may be removed.
6. (Supporting) `build` also refuses to overwrite an output directory that is not a Poolboy-owned publication.

---

## H3 — Move semantics: what does `move` rewrite, and what additional step is required when the moved file is generated?

### Correct answer

**What `move SRC DEST` (alias `mv`) rewrites** (`index/index.go:Move` 848-998;
`cmd/poolboy/commands.go:cmdMove` 795-815; `docs/maintenance-commands.md` §"Refactoring"
lines 74-85):
1. **Relocates the entry** on disk from `SRC` to a root-absolute `.md` `DEST` via `rename`
   (index.go:984-995). Guards: DEST must end `.md`, must differ from SRC, must stay inside
   the bundle, must not already exist, and SRC must be a regular file; the move is refused if
   any entry has invalid YAML (index.go:867-893, 875-879).
2. **Inbound Markdown links** — every internal link whose resolved target is SRC (relative or
   root-absolute alike) is respelled to point at DEST, relative from the *linking* file
   (index.go:919-920).
3. **The moved file's own outbound Markdown links** — respelled relative from its new
   directory so they don't dangle (index.go:921-922).
4. **Anchors and Markdown link titles are preserved** (index.go:920-922 `anchorSuffix`,
   954-961 keeps anchor + title).
5. **Structured OKF `sources[].resource` references are always migrated** — inbound citations
   to the moved file and explicit relative (`./`, `../`) citations owned by the moved file
   keep resolving to the same files (`idx.sourceResourceRewrites`, index.go:927; doc 77-79).
   This happens without `--include-frontmatter`.
6. **Optional `--include-frontmatter`** additionally rewrites other Markdown-valued
   (`*.md`) frontmatter fields heuristically, written root-absolute (index.go:928-937; doc
   80-83). Not required for `sources[].resource` identity.

**What `move` does NOT touch:** URL resources and non-Markdown metadata are left untouched
(doc 79-80). **Wikilinks are intentionally not rewritten** by `move` — they resolve by
basename each run, so a relocation needs no rewrite; use `tidy --wikilinks` for the explicit
conversion path (index.go:913-916; doc 83-85). `--dry-run` validates and reports the plan
without writing (index.go:857, 977-996; `emitMove` 817-831). There is no rollback: a mid-way
write error returns what was already done so `unresolved` can surface leftovers
(index.go:857-859).

**The additional step required when the moved file is generated.** `move` operates purely on
the index/corpus: it renames the file and rewrites links/`sources[].resource`, but it does
**not** update the generated ledger `.poolboy/generated.json` and does **not** change the
`[[render]]` mapping in `poolboy.toml`. (Confirmed: `Move` never references the ledger or
config; it ends at the on-disk rename, index.go:984-997.) A render mapping is
`template + data → output` (e.g. `templates/commands.md.knap` + `data/commands.json` →
`reference/commands.md`; `poolboy.toml` lines 6-9), and the ledger keys ownership by output
path.

So after moving a generated file, the render mapping still points at the OLD output path and
the ledger still records the OLD path. On the next `build`:
- The moved file now sits at DEST with **no ledger entry** → it is treated as handwritten and
  cannot be overwritten ("render output would overwrite handwritten Markdown" if a mapping
  ever targeted it) (`internal/compiler/ledger.go:149-151, 187-188`).
- The OLD path is a tracked generated entry whose file is gone; the mapping re-materializes
  the generated file back at the OLD location (`materializeGenerated`, ledger.go:200-246;
  build flow step 10, `docs/build-and-render.md` 68-69).

Therefore the required extra step is to **update the render mapping's `output` in
`poolboy.toml` to the new path (and re-run `build`) so the generator emits to DEST and the
ledger is regenerated for the new path** — reconciling the rendered output and the ledger
rather than deleting state. The docs state the general rule directly: "Do not delete the
state directory to force an overwrite or reset drift: reconcile the rendered output and the
ledgers instead" (`docs/build-and-render.md` 158-159), and generated ownership is keyed to
the output path in the ledger (146-152). `move` alone does not relocate the generator target.

### Evidence
- `index/index.go` — `MoveResult`/`FileRewrite` (831-846), `Move` (848-998): guards (867-893), inbound/outbound link rewrites (902-926, esp. 919-922), wikilink exclusion (913-916), `sources[].resource` migration (927), `--include-frontmatter` (928-937), on-disk rename (984-995), no-rollback note (857-859).
- `cmd/poolboy/commands.go` — `cmdMove`/`emitMove` (795-831).
- `docs/maintenance-commands.md` — §"Refactoring" `move` (74-85).
- `poolboy.toml` — `[[render]]` mapping template/data/output (6-9).
- `internal/compiler/ledger.go` — handwritten/untracked refusal (149-151, 187-188), re-materialization of tracked generated outputs (200-246).
- `docs/build-and-render.md` — §"Generated ownership" (146-159, esp. 158-159), build flow step 10 (68-69).

### Minimum facts a consumer answer MUST contain to grade `correct`
1. `move`/`mv SRC DEST` relocates the entry (root-absolute `.md` DEST) and rewrites **inbound** Markdown links pointing at it **and** the moved file's own **outbound** links (relative to its new directory), preserving anchors and titles.
2. Structured OKF `sources[].resource` references are **always migrated**; `--include-frontmatter` is an opt-in for other `*.md` frontmatter values; `--dry-run` previews without writing.
3. **Wikilinks are intentionally not rewritten** by `move` (use `tidy --wikilinks`); URL/other metadata untouched.
4. `move` does **not** update the generated ledger (`.poolboy/generated.json`) or the `poolboy.toml` `[[render]]` output mapping.
5. The extra step for a generated file: **update the render mapping's output path (in `poolboy.toml`) and re-run `build`** so the generator produces the file at the new path and the ledger is reconciled — because a moved generated file would otherwise be re-materialized at the old path and the new path treated as handwritten.
