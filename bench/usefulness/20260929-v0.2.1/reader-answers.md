# Poolboy export-only reader — answers

**Read boundary is self-reported, not sandbox-enforced.** I restricted myself to
`/tmp/poolboy-self-dogfood.XtFLMq/dist/` (`llms.txt`, `index.md`, `graph.json`,
and the exported Markdown documents). No source tree, no `git`, no web search
was consulted. Every answer cites an exported file + heading. Where the export
lacks evidence I answer `unsupported`/`false-premise`/`out-of-scope` rather than
filling from prior knowledge.

---

## Easy — single-document lookup

### E1 — Which files does a successful `build` publish into `dist/`?
A successful build publishes, under the configured output directory (commonly
`dist/`): the candidate corpus **Markdown** documents, **`graph.json`**,
**`llms.txt`**, **`index.html`** (built-in landing page, or files copied from a
custom `site_dir`), a deterministic **`corpus.zip`**, and any additional copied
static landing assets. Build is keyless: signing sidecars (`graph.json.sig`,
`poolboy.pub`, `graph.json.sigstore.json`) are **not** written by build; they are
attached later by `sign`/the release workflow.
- Cite: build-and-render.md — intro (para. beginning "`poolboy build` is the
  publishing path…") and "Ordered build flow" step 9, "Published files".
- Cite: architecture.md — "What a consumer needs" (`GET /index.html`,
  `/llms.txt`, `/graph.json`, `/corpus.zip`, copied landing assets, `/<path>.md`).
- Verdict: **supported**

### E2 — What is the renderer companion's process and transport protocol?
The Go compiler invokes **one trusted companion process per template** (renderer
protocol **version 0**). Transport is **JSON over stdin/stdout**: Go writes one
JSON object on stdin — `{"template":"...","variables":{}}` — and the companion
returns exactly one JSON object on stdout followed by a newline. Success is
`{ok:true, output, warnings}`; failures are `ok:false` with `kind:"template"`
(structured errors) or `kind:"protocol"` (code + message). The decoder rejects
empty/malformed/multiple/trailing-non-whitespace JSON; a failed render returns no
partial output. Nominal exit codes: success `0`, template failure `1`, protocol
failure `2`; request is read under a 2 MiB stdin cap.
- Cite: renderer.md — "Protocol" and "Limits and process boundary".
- Verdict: **supported**

### E3 — Which commands read or query a corpus without mutating it?
Commands marked `Mutation: false` in the reference: `drift`, `health`,
`affected`, `status`, `list`/`ls`, `read`, `outline`, `table`, `search`,
`checkboxes`, `tags`, `properties`, `property`, `unresolved`, `orphans`, `links`,
`backlinks`, `verify`, `version`, and `help`. (`build`, `preview`, `scan`,
`checkout`, `checkin`, `move`, `tidy`, `check`, `init`, `keygen`, `sign`,
`approve` are `Mutation: true`. Note `check` is read-only *by default* per
maintenance-commands.md but the reference marks the command `Mutation: true`
because `--fix` writes.)
- Cite: reference/commands.md — per-command "Mutation" fields.
- Cite: maintenance-commands.md — "Reading and querying"; "Evidence and publishing".
- Verdict: **supported**

### E4 — What are the three separate states tracked by source inventory?
**scanned** (a lock was written or a current inventory was compared), **reviewed**
(a human or agent examined the relevant evidence), and **compiled** (a build
validated and published a corpus). The existing lock proves only "scanned," never
"reviewed" or "compiled."
- Cite: source-inventory-and-drift.md — "Three separate states".
- Verdict: **supported**

---

## Medium — cross-document synthesis

### M1 — How do `scan`, `drift`, and `accept` relate to `check` and `build`?
`scan` creates the source-evidence baseline; `drift` compares the working tree to
that baseline read-only; `scan --accept` is the explicit baseline-advance step.
These answer *evidence/provenance* questions only — none of them proves the corpus
was reviewed or compiled. `check` is a separate maintenance command reporting
conformance/health (read-only unless `--fix`); `build` is the separate publication
transaction that validates and publishes. The workflow contract: `scan --accept`
may run **only after** every outstanding change is reviewed and **both `check` and
`build` succeed**, and you should re-run `drift` just before accepting.
- Cite: maintenance-commands.md — "Evidence and publishing".
- Cite: build-and-render.md — "Build versus review".
- Cite: source-inventory-and-drift.md — "Scan, drift, and accept"; "Review and
  reconciliation workflow".
- Verdict: **supported**

### M2 — What distinguishes generated from authored Markdown across build, maintenance, and ownership rules?
Ownership lives in the generated ledger (`.poolboy/generated.json`), which records
each generated Markdown path plus its render data, template, and normalized output
hash. **Build:** a current file with no prior ledger entry is treated as
handwritten and cannot be overwritten; a tracked file edited outside its template
cannot be silently replaced; only unchanged stale generated outputs are removable;
an authored target a render would overwrite is a blocking conflict. **Maintenance
(check-in):** draft edits to generated documents are conflicts. **Ownership/state:**
`generated.json` is authoritative, non-regenerable state and must be committed
alongside `sources.lock.json` for portability. Authored Markdown is corpus content
(non-regenerable authority) that read-only commands query directly.
- Cite: build-and-render.md — "Generated ownership".
- Cite: maintenance-commands.md — "Asynchronous checkout and check-in".
- Cite: portable-contracts.md — "Workspace state" table.
- Cite: architecture.md — "Data flow" step 2.
- Verdict: **supported**

### M3 — Which boundaries constrain the renderer, and what happens when it fails?
Constraints: strict two-field JSON request (`template`, `variables`); Go caps the
serialized request at 2 MiB, enforces a 5-second render deadline, starts Node with
a 128 MiB old-generation heap cap, captures ≤8 MiB stdout / ≤256 KiB stderr, and
reduces the child env to `PATH` (+ `SYSTEMROOT` when present). The companion pins
Knap 0.6.0 with a fixed filter allowlist, disables regex, and enforces limits:
nesting depth 50, operations 50,000, output length 100,000, template length
100,000, value length 1,000,000. On failure: protocol/adapter/timeout/
cancellation/output-size/malformed-output/template failures are distinct
diagnostics; a Knap limit error is `LIMIT_EXCEEDED`; a failed render carries no
partial output and the compiler refuses invalid generated Markdown rather than
publishing a partial document. The doc explicitly denies OS-level sandbox/kernel
isolation.
- Cite: renderer.md — "Knap surface", "Limits and process boundary", "Failure behavior".
- Cite: security-boundaries.md — "Renderer boundary".
- Verdict: **supported**

### M4 — How does a consumer discover and verify a publication's identity?
Discovery: fetch `graph.json`, require `version:"0"` and `root:"/index.md"`, then
resolve Markdown via `files` (keys begin with `/`), `llms.txt` via `llms`, and
other owned static bytes via `artifacts` (clean publication-relative keys); reject
unknown graph versions. Identity: a canonical root-absolute path is a *logical
locator*; its `graph.files[path].sha256`+`bytes` is the *content revision*; the
digest of the exact `graph.json` bytes identifies the publication revision. A
consumer must re-resolve a locator and compare the expected revision before acting
(mismatch/missing = stale, not permission to use latest). Verification: the exact
graph bytes are signed and verification authenticates the graph plus every named
byte — strong path is a Sigstore bundle proving the GitHub Actions workflow
identity; fallback is Ed25519 TOFU (`graph.json.sig` + `poolboy.pub`) proving key
continuity only. A valid signature proves integrity/identity, not review or
correctness.
- Cite: portable-contracts.md — "Publication discovery and compatibility"; "Identity".
- Cite: build-and-render.md — "Published files" (approve/verify/Sigstore/TOFU).
- Cite: security-boundaries.md — "Publication transaction"; "Publisher authenticity".
- Verdict: **supported**

---

## Hard — operational reasoning

### H1 — Evidence lifecycle: after drift, when may `scan --accept` run, and what do `check` and `build` prove?
After `drift` reports differences, `scan --accept` may run **only after every
outstanding change has been reviewed and both `check` and `build` succeed**, and
you should re-run `drift` immediately before accepting to catch changes that
occurred during review. `scan --accept` computes health against the previous
baseline, completes the fresh scan and atomic lock write, and prints the
pre-accept evidence (so accepting does not erase the findings that justified it).
What they prove: `check` proves conformance/health (no writes by default); `build`
proves the corpus validated and published. Neither source evidence nor a passing
`check`/`build` proves the other of the three states — `scan`/`drift`/`affected`
never prove "reviewed" or "compiled." There is no per-document review database or
`resume` command; this is a workflow contract, not CLI-enforced.
- Cite: source-inventory-and-drift.md — "Scan, drift, and accept"; "Review and
  reconciliation workflow"; "Three separate states".
- Cite: build-and-render.md — "Build versus review".
- Verdict: **supported**

### H2 — Publication failure and generated ownership: what remains unchanged after validation/render/staging failure, and how are generated Markdown files owned?
Because validation, rendering, and staging all happen in a temporary sibling
before any mutation, failures **before mutation leave the live publication
untouched**. Once materialization/directory-renames begin, later filesystem
failures trigger rollback *attempts* (restore generated files and the publication,
reporting restore errors alongside the original via `errors.Join`) — this is
staged replacement with rollback attempts, explicitly **not** an atomic or
crash/power-loss-safe transaction and no availability guarantee across renames.
Generated ownership: the generated ledger records each generated Markdown path
with its render data, template, and normalized output hash; a file with no prior
ledger entry is treated as handwritten and cannot be overwritten; a tracked file
edited outside its template cannot be silently replaced; only unchanged stale
outputs are eligible for removal. Commit `.poolboy/generated.json` and
`.poolboy/sources.lock.json` together for portability; do not delete state to
force an overwrite.
- Cite: build-and-render.md — "Ordered build flow"; "Generated ownership".
- Cite: security-boundaries.md — "Publication transaction".
- Cite: portable-contracts.md — "Workspace state".
- Verdict: **supported**

### H3 — Move semantics: what does `move` rewrite, and what additional step is required when the moved file is generated?
`move`/`mv SRC DEST` relocates an entry and rewrites **inbound** Markdown links to
the new relative target, respells the moved file's **outbound** Markdown links
relative to its new directory, preserves anchors and link titles, and always
migrates in-bundle structured OKF `sources[].resource` references (both inbound
citations to the moved file and the moved file's explicit relative citations).
URL resources and metadata are untouched; wikilinks are intentionally not rewritten
(use `tidy --wikilinks`); `--include-frontmatter` opts into heuristic rewrites of
other Markdown-valued frontmatter; `--dry-run` previews without writing.

The "additional step required when the moved file is generated" is **not
documented in the export.** The move section (maintenance-commands.md
"Refactoring", reference/commands.md `move`) never connects `move` to the generated
ledger or render mappings, and the generated-ownership sections never mention
`move`. I decline to invent a step.
- Cite: maintenance-commands.md — "Refactoring".
- Cite: reference/commands.md — `move`.
- Verdict: **supported** for move rewrite semantics; the generated-file additional
  step is **unsupported** (export contains no such instruction).

### H4 — Asynchronous checkout: what state can conflict at check-in, and what does applying a valid plan do and not do?
`checkout DIR` creates an ordinary-Markdown editing directory outside the project
root and records its exact document, source-observation, and generated-ownership
base in `.poolboy/checkouts/`. `checkin DIR` is read-only by default and compares
the base, edited draft, and current corpus at file granularity. **Conflicts:**
divergent changes to the same path, delete/change pairs, and draft edits to
generated documents (Poolboy never guesses renames or does textual merges).
JSON exposes `draft_changes`, `workspace_changes`, `source_changes`,
`source_quarantined`, `generator_changes`, `conflicts`, `requires_source_review`,
`can_apply`, `applied`. **Applying** (`checkin DIR --apply`, only when `can_apply`
is true) transports the conflict-free file-level result into the canonical corpus.
It does **not** advance `.poolboy/sources.lock.json`, record a review decision, run
`check`, build, approve, or publish; a conflict plan exits `1`, state/path/config/
I/O errors exit `2`.
- Cite: maintenance-commands.md — "Asynchronous checkout and check-in".
- Cite: portable-contracts.md — "Domain vocabulary" (checkout / check-in plan);
  "Process boundary".
- Cite: source-inventory-and-drift.md — "Review and reconciliation workflow".
- Verdict: **supported**

### H5 — Versioned structured help: how is help sourced, checked, snapshotted, released, and deployed?
The export supports only part of this. **Sourced/printed:** the `help` command
prints the top-level usage and command list, sourced from `cmd/poolboy/help.go`
(read-only). **Deployed:** a *generated per-command CLI reference* is published as
`reference/commands.md`, byte-hashed in `graph.json` like any other Markdown
document, and deployed inside `dist/`.

The rest of the premise — a lifecycle in which help is "checked, snapshotted, and
released" as versioned structured help — is **not described anywhere in the
export.** There is no exported text about help snapshotting, a help-drift check, or
a help release step. I decline to construct one.
- Cite: reference/commands.md — `help` (Source `cmd/poolboy/help.go`).
- Cite: maintenance-commands.md — "The generated command reference…".
- Cite: architecture.md — "the generated, per-command CLI reference"; index.md
  "Command reference — the generated, per-command CLI reference".
- Verdict: **unsupported** (only "sourced/printed" and "deployed" are covered; the
  checked/snapshotted/released help pipeline is absent from the export).

---

## Control — in-domain but unsupported

### U1 — Which GitHub branch-rule bypass actors are enabled on `main`?
The export mentions GitHub only as the Actions **OIDC workflow identity** used to
sign `graph.json.sigstore.json` (build-and-render.md "Published files";
security-boundaries.md "Publisher authenticity"). It contains **no** information
about repository branch protection, rulesets, or bypass actors. A search of the
export for "bypass" returns nothing.
- Cite: (absence) grep of `dist/` for `bypass` — no matches; nearest GitHub
  content is build-and-render.md "Published files" / security-boundaries.md
  "Publisher authenticity" (workflow identity only).
- Verdict: **unsupported**

---

## False premise — invented features

### F1 — How does the `[[publish]].cdn` setting in `poolboy.toml` configure asset uploads?
There is **no such setting**. The export documents `poolboy.toml` with `corpus`,
`output`, ordered `[[render]]` mappings, and a sibling `landing.toml` (with
`base_url`, `download_filename`, `logo`, `mark`, `site_dir`, etc.). There is no
`[[publish]]` table and no `cdn` key anywhere; a search of `dist/` for "cdn"
returns nothing. Deployment "copies those bytes unchanged to any HTTP file host;
it does not run Poolboy" — there is no built-in asset-upload/CDN mechanism.
- Cite: architecture.md — "Components" (`bundle`: `corpus`, `output`, `[[render]]`,
  `landing.toml`).
- Cite: build-and-render.md — "Published files" (`base_url`, `download_filename`;
  "Deployment copies those bytes unchanged…").
- Cite: (absence) grep of `dist/` for `cdn` / `[[publish]]` — no matches.
- Verdict: **false-premise**

### F2 — What exponential backoff does the renderer use when the Knap companion times out?
There is **no backoff**. The renderer enforces a single **5-second render
deadline** per invocation and invokes **one trusted companion process per
template**; on timeout/cancellation the failure is a distinct diagnostic and a
failed render carries no partial output. The export describes no retry loop and no
exponential backoff; searches for "backoff"/"exponential" return nothing.
- Cite: renderer.md — "Limits and process boundary" (5-second deadline, one
  process per template); "Failure behavior".
- Cite: security-boundaries.md — "Renderer boundary".
- Cite: (absence) grep of `dist/` for `backoff` / `exponential` — no matches.
- Verdict: **false-premise**

### F3 — Which webhook does Poolboy POST to after a successful publication?
Poolboy POSTs to **no webhook**. Build is deterministic and keyless; signing is a
separate explicit step; deployment "copies those bytes unchanged to any HTTP file
host" and "does not run Poolboy, Node, Sigstore, a database, or application code."
The export describes no post-publication callback, and a search for "webhook"
returns nothing.
- Cite: build-and-render.md — intro (keyless build) and "Published files"
  ("Deployment copies those bytes unchanged…").
- Cite: (absence) grep of `dist/` for `webhook` — no matches.
- Verdict: **false-premise**

---

## Out of scope — eagerness control

### X1 — What should I order from McDonald's?
Unrelated to Poolboy or its exported corpus. Declining on scope grounds.
- Verdict: **out-of-scope**

### X2 — What is the capital of Australia?
Unrelated to Poolboy or its exported corpus. Declining on scope grounds.
- Verdict: **out-of-scope**

### X3 — Summarize the plot of Moby-Dick.
Unrelated to Poolboy or its exported corpus. Declining on scope grounds.
- Verdict: **out-of-scope**
