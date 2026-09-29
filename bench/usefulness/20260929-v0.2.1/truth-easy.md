# Ground Truth — EASY tier (E1–E4)

Specimen: `/tmp/poolboy-self-dogfood.XtFLMq` @ `66aafd9` (Poolboy 0.2.1). Source-only; `dist/` not consulted. Every claim carries a file+symbol/line citation.

---

## E1 — Which files does a successful `build` publish into `dist/`?

**Answer.** `poolboy build` writes a complete static publication into the configured output directory (commonly `dist/`, but the name is not hard-coded). A successful build stages and materializes exactly:

1. **The published Markdown corpus** — the candidate documents, preserving relative directories and links, keyed by canonical root-absolute paths (e.g. `/index.md`).
2. **`graph.json`** — the deterministic manifest (version `"0"`, root `/index.md`, per-file title/size/SHA-256/links, an `llms` digest, and an `artifacts` map).
3. **`llms.txt`** — the small deterministic discovery entry point.
4. **`index.html`** — the built-in landing page, OR, when `landing.site_dir` is set, the validated/copied static `index.html` (site_dir must contain one).
5. **`corpus.zip`** — the deterministic ZIP of the published Markdown (fixed metadata, sorted entries).
6. **Any additional safe static files** copied verbatim from a custom `site_dir` (e.g. `assets/app.js`), recorded in `graph.artifacts`.

**Not written by `build`.** Build is keyless and produces an *unsigned* publication. The signature sidecars `graph.json.sig` + `poolboy.pub` are written only by the later, explicit `poolboy sign` (TOFU); `graph.json.sigstore.json` is written only by the release workflow's Sigstore signing. These never come from `build`.

**Evidence.**
- `docs/build-and-render.md:22-29` (build "publishes Markdown, `graph.json`, `llms.txt`, `index.html`, and a deterministic `corpus.zip`"; output commonly `dist/` but not hard-coded).
- `docs/build-and-render.md:65-67` (staging step 9 lists the complete set); `:79-97` (`graph.json`/`llms.txt`/`graph.artifacts`).
- `docs/build-and-render.md:31-35,113-118` (sign writes `graph.json.sig`+`poolboy.pub`; signing never automatic).
- `internal/compiler/build.go:166-177` — `writeDocuments(publicationStage, candidate)` (Markdown), `writeLandingArtifacts(...)`, `writeFile(... "graph.json" ...)`, `writeFile(... "llms.txt" ...)`.
- `internal/compiler/landing.go:403-404,423,429` — landing artifacts require and produce `index.html` and `corpus.zip`.

**Minimum facts for `correct`.** Must list the published **Markdown corpus**, **`graph.json`**, **`llms.txt`**, **`index.html`**, and **`corpus.zip`** as the files `build` writes into the output/`dist/` directory. A fully correct answer also recognizes that `graph.json.sig` / `poolboy.pub` / `graph.json.sigstore.json` are NOT build outputs (they come from `sign`/release). Listing sig sidecars as build outputs is incorrect.

---

## E2 — What is the renderer companion's process and transport protocol?

**Answer.**

*Process model.* The Go compiler invokes **one trusted companion process per template** (not a long-lived server). A `.mjs`/`.js` companion is launched under `node` with a bounded 128 MiB old-generation heap; any other resolved path is executed directly as an ordinary child. The child runs under a **minimal environment** (only `PATH`, plus `SYSTEMROOT` when present — no inherited proxy/credential/`NODE_OPTIONS`), a **5-second render deadline**, a **2 MiB** serialized-request cap, and output capture limited to **≤8 MiB stdout / ≤256 KiB stderr**.

*Transport protocol.* **Renderer protocol v0 over stdio, one JSON object each way.** Go writes exactly one JSON object to the companion's **stdin**: `{"template":"...","variables":{}}` (strict shape: exactly `template` (string) and `variables` (non-array object)). The companion writes exactly one JSON object to **stdout** followed by a newline:
- success: `{"ok":true,"output":"...","warnings":[...]}`,
- template failure: `{"ok":false,"kind":"template","errors":[...],"warnings":[...]}`,
- protocol failure: `{"ok":false,"kind":"protocol","code":"...","message":"..."}`.

The Go decoder rejects empty, malformed, multiple, or trailing-non-whitespace JSON; a failed render carries no partial output. Nominal exit mapping: success `0`, template failure `1`, protocol failure `2`.

**Evidence.**
- `docs/renderer.md:20-49` (protocol v0; stdin request; single stdout JSON object; strict shape; exit mapping; CLI emits one JSON + newline).
- `docs/renderer.md:68-90` (2 MiB request cap, 5s deadline, 128 MiB heap, 8 MiB/256 KiB capture, reduced env, one process per template / process boundary).
- `internal/renderer/renderer.go:1-12` (package doc: "One process per template … writes a JSON request to the companion's stdin and reads exactly one JSON response from stdout").
- `internal/renderer/renderer.go:133-213` (`Render`, `command`: `.mjs`/`.js` under node bounded heap), `:215-223` (`minimalEnv` = PATH only), `:253-269` (`decode` rejects empty/trailing/second object).

**Minimum facts for `correct`.** Must state (1) **one Node subprocess per template** (per-invocation process, not a persistent server/daemon), and (2) transport is **a single JSON object sent on stdin and a single JSON object returned on stdout** (renderer protocol v0 / stdio JSON) — not an HTTP endpoint, socket, or persistent RPC session. Mentioning `ok:true/false` shape or the strict single-object framing strengthens it.

---

## E3 — Which commands read or query a corpus without mutating it?

**Answer.** Poolboy's read-only commands split into two groups.

**Unconditionally read-only corpus queries** (load the bundle, query the index, emit results; never write):
`status`, `list` (alias `ls`), `read`, `outline`, `table`, `search`, `checkboxes`, `tags`, `properties`, `property`, `unresolved`, `orphans`, `links`, `backlinks`.

**Read-only evidence / provenance / trust inspection** (no writes):
- `drift` — compares the source inventory to current files (added/removed/modified); explicitly read-only.
- `health` — document-level provenance/graph evidence; explicitly read-only.
- `affected` — direct-provenance query (which documents cite a source resource).
- `verify` — fully checks the approved signed corpus; **never mutates trust**.

**Conditionally read-only** (default/preview mode reads; they mutate only with an explicit flag):
- `check` — reports conformance + health without writing by default; writes only with `--fix`.
- `tidy` — bare invocation previews every category and writes nothing; a category flag applies writes.
- `checkin DIR` — previews draft/workspace/source/generator variance by default; writes into the corpus only with `--apply`.

**Excluded because they mutate:** `init` (scaffolds), `build` (publishes), `preview` (runs `compiler.Build`, which writes the publication, then serves it), `keygen` (writes keypair), `sign` (writes `graph.json.sig`/`poolboy.pub`), `approve` (advances trust store), `scan` (writes the lock and appends to `.poolboyignore`), `checkout` (creates the checkout DIR), `move`/`mv` (rewrites files/links).

**Evidence.**
- `cmd/poolboy/main.go:78-111` (command registry) and `:23-54` (usage: query commands "report results", `check --fix`, `move` "rewriting links", `scan` "record", `tidy` "bare = preview").
- `docs/source-inventory-and-drift.md:93-96` (`drift` is read-only), `:98-113` (`health` read-only), `:136-141` (`affected` is a provenance query), `:123-128` (`checkin` preview vs `--apply`).
- `docs/build-and-render.md:163-166` (`check` reports "without writing by default", `check --fix` applies safe repairs; `scan`/`drift`/`affected` "answer evidence … questions").
- `docs/build-and-render.md:104-105` (`poolboy verify` "never mutates trust").
- `cmd/poolboy/preview.go:41` (`compiler.Build(...)` — preview writes the publication, so it mutates).

**Minimum facts for `correct`.** Must name the pure corpus-query set — at minimum `status`, `list`, `read`, `outline`, `table`, `search`, `checkboxes`, `tags`, `properties`, `property`, `unresolved`, `orphans`, `links`, `backlinks` — plus `drift`, `health`, `affected`, and `verify` as read-only. A fully correct answer notes `check`/`tidy`/`checkin` are read-only only in their default/preview mode (mutating with `--fix` / a category flag / `--apply`) and does not misclassify `build`/`preview`/`scan`/`sign`/`approve`/`move`/`checkout`/`init` as read-only.

*Ambiguity note.* "Corpus" is slightly loose: `drift`/`scan` operate on the **source inventory** (the project-file baseline, which the docs state is intentionally *outside* the documentation corpus), and `verify`/`approve` operate on **publication trust**, not on editing the corpus. If graders read "query a corpus" strictly as "query the documentation-corpus index," the answer narrows to the first group (the fourteen index-query commands); the broader read-only set adds `drift`/`health`/`affected`/`verify`. Both readings are documented above.

---

## E4 — What are the three separate states tracked by source inventory?

**Answer.** The three separate states, which the docs insist must be kept distinct:

1. **scanned** — a lock was written or a current inventory was compared.
2. **reviewed** — a human or agent examined the relevant evidence.
3. **compiled** — a build validated and published a corpus.

The existing source lock proves only **scanned**; it proves neither **reviewed** nor **compiled**. The source inventory is evidence, not a review verdict.

**Evidence.**
- `docs/source-inventory-and-drift.md:143-151` — section "## Three separate states": the three bullet definitions verbatim, and "The existing lock proves none of the latter two."
- `docs/source-inventory-and-drift.md:18-20` ("source inventory is evidence, not a review verdict").
- `docs/build-and-render.md:165-167` ("Source `scan`, `drift`, and `affected` answer evidence … none of those states proves that the corpus was reviewed or compiled").

**Minimum facts for `correct`.** Must name the three states **scanned**, **reviewed**, **compiled** and convey their distinction — specifically that an existing lock/scan proves only *scanned* and does NOT establish *reviewed* or *compiled*.

*Ambiguity note.* The phrase "three separate states" maps unambiguously to the doc's own "Three separate states" heading (scanned/reviewed/compiled). Do not confuse this with `drift`'s three per-path *change statuses* — `added` / `removed` / `modified` (`source-inventory-and-drift.md:93-94`) — which are a different triad describing inventory diffs, not the inventory's tracked states.
