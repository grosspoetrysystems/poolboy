# Poolboy self-dogfood — MEDIUM tier ground truth (M1–M4)

Specimen: `/tmp/poolboy-self-dogfood.XtFLMq` @ `66aafd9` (Poolboy 0.2.1).
Ground truth established from implementation (`cmd/`, `internal/`, `index/`), plus
canonical source docs under `docs/`. `dist/` was NOT read. Citations are
`path:lines` or `path#symbol`. Doc claims that a consumer answer may legitimately
rely on are cited to `docs/…`; every behavioral assertion is additionally anchored
to Go source.

No question in this tier is ambiguous or underdetermined by the source; each is
directly resolvable. Where a doc states a deliberate *ceiling* (e.g. "not a kernel
sandbox"), that ceiling is itself part of the correct answer and is noted.

---

## M1 — How do `scan`, `drift`, and `accept` relate to `check` and `build`?

### Correct answer
There are two orthogonal axes. `scan`/`drift`/`accept` operate on the **source
evidence baseline** (the project's input files, recorded in
`.poolboy/sources.lock.json`); `check`/`build` operate on the **corpus** (the
authored Markdown and its publication). They are separate concerns joined only by
a *workflow contract*, not by CLI enforcement.

Source-evidence side:
- **`scan`** computes a bounded, hashed source inventory and writes it as the
  accepted baseline. The first scan writes a missing lock; once a regular baseline
  exists a plain `scan` refuses to replace it (returns `ErrBaselineExists`) and
  prints no smell test — you must review first.
  `cmdScan` → `source.ScanEvidence(b, accept)`; the write is gated on `accept`.
- **`drift`** is read-only: it compares the written baseline against a fresh
  bounded scan and reports paths as `added` / `removed` / `modified` by SHA-256
  only (matching bytes ⇒ no change). It never writes the lock.
- **`accept`** is not a standalone command; it is the `--accept` flag on `scan`.
  `scan --accept` is the explicit baseline-advance step: it computes health
  against the *previous* baseline, then completes the fresh scan + atomic lock
  write, printing that pre-accept evidence so acceptance does not erase the
  findings that justified it.

Corpus side:
- **`check`** reports conformance and corpus health over the index; read-only by
  default, `check --fix` applies safe repairs (e.g. sync root `okf_version`) then
  reindexes. It is explicitly *not* equivalent to `build`.
- **`build`** is the separate publication transaction: it validates, renders
  generated documents, and atomically publishes the static output. A failed build
  leaves the previous publication untouched.

How they relate: `check` and `build` prove the corpus is conformant and
publishable; they say nothing about whether the *source inputs* have been
reviewed. `scan`/`drift`/`accept` track whether the reviewed source state matches
the working tree. The documented workflow ties them together: a refresh or
interrupted review never advances the baseline; **only after every outstanding
change has been reviewed and both `check` and `build` succeed may
`scan --accept` run**, and `drift` should be rerun immediately before acceptance
to catch changes during review. This is a workflow contract, not semantic
enforcement — there is one project-wide baseline, no per-document review DB, no
`resume`. The three states are kept deliberately separate: **scanned** (a lock
was written/compared), **reviewed** (a human/agent examined evidence), and
**compiled** (a build validated and published); the existing lock proves only the
first.

### Evidence
- `internal/source/source.go:1-16` (package doc: Scan writes baseline / `accept`
  gate / Drift compares without writing), `:39-41` (`ErrBaselineExists`),
  `:362-366` (`Scan`), `:368-425` (`ScanEvidence`), `:427-470`
  (`Drift`/`DriftEvidence` via non-writing `scanCurrent`), `:74-81`
  (`Added`/`Removed`/`Modified`).
- `internal/source/scan.go:43-52` (`Current`/`scanCurrent` — the shared
  non-writing path used by scan and drift).
- `cmd/poolboy/product.go:56-89` (`cmdBuild`), `:91-146` (`cmdScan`, `--accept`
  flag at `:93`, `ScanEvidence` call at `:105`), `:148-189` (`cmdDrift`).
- `cmd/poolboy/commands.go:451-511` (`cmdCheck`: `idx.Check()`, `--fix`, exit 1 on
  errors).
- `internal/compiler/build.go:42-226` (`Build` = validate + render + publish).
- `docs/source-inventory-and-drift.md:74-96` (Scan/drift/accept), `:115-134`
  (review & reconciliation workflow — the "both `check` and `build` succeed →
  `scan --accept`" contract), `:143-149` (scanned/reviewed/compiled).
- `docs/maintenance-commands.md:93-104` (`check` ≠ `build`; scan/drift/affected
  are evidence, build is the separate publication transaction).

### Minimum facts for `correct`
- scan/drift/accept act on the **source baseline** (`.poolboy/sources.lock.json`);
  check/build act on the **corpus/publication** — two separate axes.
- `drift` is read-only (added/removed/modified by SHA-256); it never writes.
- `accept` = the `scan --accept` flag; it is the only way to replace an existing
  baseline, and only after review (plain scan refuses / `ErrBaselineExists`).
- `check` is read-only conformance/health (`--fix` = safe repairs) and is **not**
  `build`; `build` additionally renders and publishes.
- The contract: `scan --accept` is intended to run only after review and after
  both `check` and `build` succeed (workflow, not CLI-enforced). Mentioning
  scanned/reviewed/compiled as distinct states strengthens the answer.

---

## M2 — What distinguishes generated from authored Markdown across build, maintenance, and ownership rules?

### Correct answer
The distinction is recorded in the **generated-file ownership ledger**
`.poolboy/generated.json`. A corpus Markdown file **with** a ledger entry is
*generated*; a file **without** one is *authored/handwritten* "by definition".
Each ledger entry keys a corpus-relative `.md` path to `{data, sha256, template}`
— the JSON data path, the SHA-256 of the last generated bytes, and the template
that produced it.

**Build.** Generated Markdown is produced by `[[render]]` mappings (template +
JSON data → output). Build enforces ownership before touching anything:
- A render output that would land on a path **without** a ledger entry is refused:
  *"render output would overwrite handwritten Markdown"*.
- A **tracked** generated file whose on-disk bytes no longer match its ledger hash
  is refused: *"generated file was edited outside its template"*.
- A previously generated file that is no longer produced (**stale**) is removed —
  but only if its bytes still match the ledger; a stale file edited by hand is
  kept (*"stale generated file was edited; refusing removal"*).
Generated files are materialized into the corpus with per-file backups and rolled
back on later failure; the new ledger is published atomically only after the swap.
Build refuses invalid generated Markdown rather than publishing a partial doc.

**Maintenance.** `checkout` records the "generated ownership base" alongside the
document and source observations. `checkin` treats **draft edits to generated
documents as conflicts** (never applied), together with divergent same-path edits
and delete/change pairs. Both `.poolboy/generated.json` and
`.poolboy/sources.lock.json` are retained by the ignore policy so ownership and
drift survive a checkout.

**Ownership rules.** `.poolboy/generated.json` is the authority for generated-file
ownership; authored Markdown is corpus content authority. Generated bytes are only
Poolboy's to overwrite/remove while they still match the ledger hash; the moment a
human edits a generated file, build stops touching it and reports the divergence.
Handwritten files are never overwritten or removed by rendering.

### Evidence
- `internal/compiler/ledger.go:17-28` (`ledger` / `ledgerEntry` = path→{data,
  sha256, template}), `:106-116` (`desiredLedger`), `:118-154` (`planLedger`:
  "a file without a ledger entry is handwritten by definition and is never
  overwritten"; the three refusal/removal rules), `:156-199`
  (`verifyRenderTargets` re-checks on disk), `:200-255` (`materializeGenerated`
  with backups/rollback).
- `internal/compiler/build.go:42-46` (all validation/render before mutation; failed
  build leaves prior publication untouched, no partial generated Markdown),
  `:102-129` (`desiredLedger`/`planLedger`/`verifyRenderTargets`/`validateDocuments`),
  `:182-220` (materialize + atomic ledger publish).
- `internal/compiler/graph.go:270-276` / renderer path — generated output validated
  as OKF (`internal/compiler/build.go:270-271`).
- `docs/maintenance-commands.md:46-57` (`checkout` records generated ownership base;
  `checkin` — draft edits to generated docs are conflicts), `:93-96` (check vs
  build renders generated docs).
- `docs/portable-contracts.md:87-100` (workspace-state table: `.poolboy/generated.json`
  = generated-file ownership, not regenerable-as-authority; authored Markdown =
  corpus content).
- `docs/security-boundaries.md:102-104` (both generated.json and sources.lock.json
  retained so ownership/drift survive a checkout).

### Minimum facts for `correct`
- Generated Markdown is tracked in `.poolboy/generated.json` (path → sha256 +
  template + data); authored Markdown has **no** ledger entry.
- Build never overwrites a handwritten (unledgered) file, and refuses a generated
  file edited outside its template (hash mismatch); stale generated files are
  removed only if unchanged.
- `checkin` treats draft edits to generated docs as conflicts (not applied).
- Ownership: generated.json is the ownership authority; a hand-edited generated
  file passes out of Poolboy's control until reconciled.

---

## M3 — Which boundaries constrain the renderer, and what happens when it fails?

### Correct answer
The renderer is a deliberately narrow Go→Node adapter (`internal/renderer`),
**one process per template**, constrained on several axes:

- **Protocol (v0), strict.** Go writes exactly one JSON object
  `{"template":…,"variables":{}}` to the child's stdin and reads exactly one JSON
  object from stdout. The decoder rejects empty, malformed, multiple, or trailing
  non-whitespace JSON. The request shape is exactly `template` (string) +
  `variables` (non-array object).
- **Trusted path only.** The companion is either an explicitly supplied path or
  the default `poolboy-knap.mjs` beside the executable, and must be a regular file.
  The renderer never reads an executable path from corpus metadata and invokes no
  project callback.
- **Fixed resource budgets (policy, not tunable):** 5-second hard per-template
  deadline; 2 MiB request cap; 8 MiB captured stdout / 256 KiB captured stderr;
  128 MiB Node old-generation heap.
- **Minimal environment:** child gets `PATH` only (plus `SYSTEMROOT` when present)
  — no proxy, credentials, or `NODE_OPTIONS` leak in.
- **No network access** is granted by the adapter.
- **Companion/Knap surface (enforced companion-side):** Knap `0.6.0` with a fixed
  filter allowlist only, regex disabled, and limits on nesting depth (50),
  operations (50,000), output length (100,000), template length (100,000), and
  value length (1,000,000). An unlisted filter is a template error.
- **Explicit ceiling:** this is *not* a kernel sandbox. The inspected source shows
  no OS-level filesystem/network isolation — an explicit `.mjs`/`.js` path is an
  ordinary Node child. The source-backed boundary is the trusted path + minimal
  env + strict protocol + fixed filters + budgets, not blanket isolation.

**On failure:** failures are classified into distinct diagnostics —
`template` (companion exit 1), `protocol` (companion exit 2), and `adapter`
(spawn/timeout/oversized/malformed, adapter-detected). Adapter codes include
`INVALID_REQUEST`, `REQUEST_TOO_LARGE`, `TIMEOUT`/`CANCELED`, `OVERSIZED_OUTPUT`,
`MALFORMED_OUTPUT`, and `PROCESS_EXIT`. **A failed render never carries partial
output back** — the caller discards the emitted document. The compiler propagates
the error out of `Build` *before any mutation*, so the prior publication is left
untouched and no partial generated Markdown is produced; the compiler also refuses
render output that is not valid UTF-8/OKF even on a nominally successful response.

### Evidence
- `internal/renderer/renderer.go:1-11` (package doc: one process per template,
  single JSON in/out, hard budgets, no network, no corpus executable path, no
  callback), `:30-37` (budgets: `deadline`=5s, `maxRequest`=2 MiB, `maxStdout`=8
  MiB, `maxStderr`=256 KiB, `nodeHeapMiB`=128), `:56-64` (`KindTemplate`/
  `KindProtocol`/`KindAdapter`), `:66-74` (`RenderError` "never carries partial
  output"), `:108-131` (`Resolve` trusted regular-file path), `:135-200` (`Render`:
  request cap, timeout, overflow, decode, error mapping), `:215-223` (`minimalEnv`
  = PATH only), `:253-269` (`decode` rejects empty/multiple/trailing).
- `internal/compiler/build.go:87-100` (render failure returns before mutation),
  `:42-46` (failed build leaves prior publication untouched, no partial Markdown),
  `:258-271` (render output validated UTF-8 + OKF).
- `docs/renderer.md:24-98` (protocol, Knap allowlist/limits, Limits and process
  boundary incl. the "not a kernel sandbox" ceiling, Failure behavior).
- `docs/security-boundaries.md:125-138` (Renderer boundary: budgets, minimal env,
  "does not demonstrate an OS-level filesystem or network sandbox").

### Minimum facts for `correct`
- One subprocess per template with a **strict single-request / single-response
  JSON** protocol over stdin/stdout.
- Fixed budgets: 5s deadline, 2 MiB request, 8 MiB stdout / 256 KiB stderr caps,
  128 MiB heap; minimal env (PATH only); trusted companion path; no network / no
  corpus-supplied executable / no callback; fixed Knap filter allowlist + Knap
  limits.
- On failure: **no partial output** — the document is discarded and the build
  aborts leaving the prior publication untouched.
- (Strengthening) It is explicitly *not* an OS/kernel sandbox.

---

## M4 — How does a consumer discover and verify a publication's identity?

### Correct answer
**Discovery.** The consumer fetches `graph.json`, the versioned publication
manifest, and requires `version: "0"` and `root: "/index.md"`, rejecting unknown
versions rather than partially interpreting them. It then resolves content by kind:
Markdown documents via `files` (keys are canonical root-absolute paths beginning
`/`), the discovery entrypoint `llms.txt` via the dedicated top-level `llms` hash,
and other owned static files via `artifacts`. All non-Markdown paths are relative
to the publication origin.

**Identity.** A document's **logical locator** is its canonical root-absolute path
(e.g. `/guide.md`); its **content revision** is the lowercase SHA-256 of the exact
bytes paired with the byte count (`graph.files[path].sha256`/`bytes`; artifacts use
the same rule). The **publication revision** is the SHA-256 digest of the exact
`graph.json` bytes. Locators and revisions are deliberately separate: editing keeps
the locator and changes the revision; renaming creates a new locator with no
implicit continuity; deletion is absence from a newer graph. A consumer must
re-resolve a locator and compare the expected revision before acting — a mismatch
or missing locator is stale input, not permission to use the latest bytes.

**Verification.** The exact `graph.json` bytes are signed, and verification
authenticates the graph plus **every byte it names** — transitively `llms.txt`,
every Markdown file, and every artifact (their hashes checked one by one). Two
trust modes:
- **Sigstore (strong; public default).** `graph.json.sigstore.json` is verified
  against the Sigstore trusted root for the exact `graph.json` digest, requiring
  the GitHub Actions OIDC issuer, the **explicitly supplied expected workflow
  identity** (the workflow identity — not the hosting domain — is the publisher),
  certificate-transparency/transparency-log inclusion, and a trusted log
  timestamp.
- **Ed25519 TOFU (weaker; explicit opt-in only).** `graph.json.sig`
  (`{alg:"ed25519", key, sig}`) + `poolboy.pub`: the public key must match the
  signature's key and the Ed25519 signature must verify over the graph bytes. This
  proves key continuity, **not** publisher identity or trusted release time. There
  is no automatic downgrade to TOFU.

Trust is scoped by publisher identity/key **plus channel** and stored in an
explicit `--lock` (authoritative, team/CI-reviewable) or otherwise
`$XDG_CONFIG_HOME/poolboy/known_publishers.json`. `poolboy verify <dir-or-url>`
never changes trust: an already-approved digest is fully content-verified; a
different but authenticated digest is reported `UPDATE PENDING` (exit 1),
exposing provenance/path changes while withholding content. `poolboy approve`
refetches, fully verifies, enforces the minimum release age (72h for stable
Sigstore unless an interactive exact-digest emergency override), and prompts for
the exact digest before recording it. Older signing timestamps are rollback
failures; a publisher key/identity change is a hard failure unless the operator
requests one-step `--migrate-identity`. Remote fetches follow same-origin
redirects only (≤5 hops); a scheme/host change is refused. A valid signature
proves integrity and (Sigstore) publisher workflow identity — **not** review,
correctness, license, permission, or prompt safety.

### Evidence
- `internal/compiler/graph.go:18-38` (`graph` shape: `version`, `root`, `llms`,
  `files{title,bytes,sha256,links,type,sources}`, `artifacts{bytes,sha256}`),
  `:46-110` (`buildGraph`: requires `/index.md`, emits `version:"0"`,
  `root:"/index.md"`).
- `internal/sign/sign.go:1-4` (package doc: Sigstore workflow identity OR Ed25519
  TOFU, then verifies every named byte), `:25-31` (`GraphName`/`SigName`/`PubName`,
  `Algorithm`="ed25519"), `:37-43` (`Signature{alg,key,sig}`), `:119-140` (`Graph`
  signs), `:142-168` (`VerifyGraph`: key match + Ed25519 verify over graph bytes),
  `:194-212` (`verifyFull` checks every manifest byte incl. llms.txt).
- `internal/sign/trust.go:20-26` (`sigstoreBundleName`=`graph.json.sigstore.json`,
  `githubOIDCIssuer`, `defaultChannel`=stable, `defaultReleaseAge`=72h), `:28-53`
  (`VerifyOptions`, `TrustRecord`), `:82-102` (`Verify`: full-verify only an
  approved digest, else `Pending`), `:104-137` (`PrepareApproval` + release-age
  gate), `:160-284` (`inspect`: mode selection, TOFU key-change / Sigstore rollback
  hard failures, `--migrate-identity`), `:286-318` (`verifySigstore`: issuer +
  identity + tlog + digest), `:320-361` (`manifestFiles` — name→sha incl.
  `llms.txt`).
- `cmd/poolboy/signing.go:81-131` (`cmdSign` — keyless build then explicit sign),
  `:140-151` (`--lock`/`--tofu`/`--identity`/`--channel`), `:153-176` (`cmdVerify`
  → `sign.Verify`, `UPDATE PENDING` exit 1, `verified sha256:… (N files)`).
- `docs/portable-contracts.md:47-85` (Identity; Publication discovery — fetch
  graph.json, require version 0 + root /index.md, resolve files/llms/artifacts;
  signed graph transitively covers all named bytes; signature proves integrity +
  Sigstore identity only).
- `docs/security-boundaries.md:29-61` (Sigstore vs TOFU, no auto-downgrade,
  same-origin ≤5-hop redirects, verify never changes trust, 72h age gate,
  rollback/key-change hard failures), `:184-196` (Publisher authenticity detail).

### Minimum facts for `correct`
- Discovery: fetch **`graph.json`**; require `version: "0"` and `root:
  "/index.md"`; resolve Markdown via `files`, `llms.txt` via `llms`, other static
  files via `artifacts`; reject unknown versions.
- Identity = logical locator (canonical `/…` path) + content revision
  (SHA-256 + bytes); publication revision = digest of the exact `graph.json`
  bytes; consumer must re-resolve + compare the expected revision (mismatch =
  stale).
- The exact `graph.json` bytes are **signed**, and the signature transitively
  authenticates `llms.txt` + every Markdown + every artifact via their hashes.
- Two verification modes: **Sigstore** (strong/default) proving the GitHub Actions
  **workflow identity**; **Ed25519 TOFU** (explicit opt-in, `graph.json.sig` +
  `poolboy.pub`) proving key continuity only — no auto-downgrade.
- `poolboy verify` (approved digest → full verify; other digest → UPDATE
  PENDING) and `poolboy approve` (full verify + digest prompt + age gate). A valid
  signature ≠ review/correctness/permission.
