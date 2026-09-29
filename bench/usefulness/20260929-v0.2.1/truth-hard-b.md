# Ground Truth — Hard Questions H4 & H5

Specimen: `/tmp/poolboy-self-dogfood.XtFLMq` @ `66aafd9` (Poolboy 0.2.1). Source-only; `dist/` not read.

---

## H4 — Asynchronous checkout / check-in

**Question:** What state can conflict at check-in, and what does applying a valid plan do and not do?

### The model
`poolboy checkout DIR` copies the current Markdown corpus into an external directory and records a **private base snapshot**; `poolboy checkin DIR [--apply]` computes a **file-level three-way merge** between that base, the draft (checkout dir), and the current workspace, then optionally applies it.

- CLI wiring: `cmd/poolboy/product.go` — `cmdCheckout` (271–304) → `checkout.Create`; `cmdCheckin` (306–363) → `checkout.Checkin`. Registered in `cmd/poolboy/main.go` command table lines 86–87.
- Core: `internal/checkout/checkout.go` — `Create` (98–162), `Checkin` (164–229), `planActions` (231–258), `applyActions` (260–312).
- The base snapshot (`state`, checkout.go:73–80) records three separate maps: `Corpus` (Markdown revisions), `Sources` (source-inventory revisions), `Generated` (generated-ledger revisions). Written to a **private** path `<root>/.poolboy/checkouts/<id>.json` mode 0600 (`statePath` 419–421; write 154–159).

### What state can conflict at check-in
Two conflict classes are produced by `planActions` (checkout.go:231–258); a file only becomes an action when the draft changed from base **and** the draft differs from the current workspace (line 240):

1. **Both sides changed** — a file changed in the draft *and* changed in the workspace since the base → `Conflict{Reason: "draft and workspace both changed from the checkout base"}` (checkout.go:251–253). Proven by `TestCheckinRefusesConflictingAndGeneratedChanges` / "both sides changed" (checkout_test.go:64–86): conflicting apply leaves the corpus at `# Workspace\n`, unmodified.

2. **Generated document edited** — the draft edits a file that was generated at base *or* is generated now → `Conflict{Reason: "draft changes a generated document; edit its template or data"}` (checkout.go:243–250). Proven by the "generated document" subtest (checkout_test.go:88–107), keyed off `.poolboy/generated.json`.

A **third, apply-time race** is detected in `applyActions`, not during planning: before each write it re-reads the target and compares it against the `current` snapshot captured during planning; any drift aborts with `"workspace changed while applying %s"` (checkout.go:292–299). Proven by `TestApplyActionsRollsBackWhenWorkspaceChangesAfterPlanning` (checkout_test.go:151–173).

**Source variance does NOT block apply.** `SourceChanges` and `SourceQuarantined` set `RequiresSourceReview` (checkout.go:210–215) and are reported, but `CanApply` depends solely on `len(Conflicts)==0` (checkout.go:217). Check-in explicitly never accepts the source baseline (product.go:345–347: "Source review remains required; check-in does not accept the source baseline.").

### What applying a valid plan DOES
- Writes only draft files that (a) differ from base, (b) differ from the current workspace copy, (c) are unchanged in the workspace since base, and (d) are not generated (checkout.go:240–255).
- **Preserves concurrent, disjoint workspace edits** to *other* files — they are never in the action set. Proven: `notes/other.md`'s independent workspace change survives an apply that lands `index.md` (checkout_test.go:45–57).
- Applies **draft deletions** (`remove: !draftOK`, checkout.go:255 → `os.Remove`, 302–303).
- Writes atomically per file (`writeAtomic`, checkout.go:305, 606–634).
- On success sets `plan.Applied = true` (224) and **closes the session** by removing the private state file `<root>/.poolboy/checkouts/<id>.json` (checkout.go:225). If that removal fails after the corpus is already written, it returns the specific error `"corpus applied but checkout state could not be closed"` (226). State-file removal proven by checkout_test.go:58–60.

### What applying a valid plan does NOT do
- Does **not** touch generated documents (they conflict instead).
- Does **not** accept the source baseline or clear source review — `RequiresSourceReview` persists (checkout.go:215; product.go:345–347).
- Does **not** overwrite files the draft didn't change, and does **not** clobber concurrent workspace edits to unrelated files.
- Does **not** delete the external checkout **directory**. `Checkin` only removes the private state *file* (checkout.go:225); the checkout dir and its `.poolboy-checkout.json` marker remain on disk. (`Create`'s `os.RemoveAll(path)` cleanup fires only on Create failure — deferred `cleanup` flag, checkout.go:131–137, 160.) Consequence: a second `checkin --apply` after a successful apply fails, because the state file needed at checkout.go:177–181 is gone.
- Preview mode (no `--apply`, or when conflicts exist) mutates nothing — returns before `applyActions` (checkout.go:218–219). Proven at checkout_test.go:41–43 and commands_test.go:1221–1227.

### Race protection & rollback (transactional apply)
`applyActions` (checkout.go:260–312) records a `backup` per file before writing, and on any failure — unsafe path (289), unreadable target (293), post-plan drift (297), or failed write/remove (307) — invokes `rollback()` which restores prior contents or removes created files in **reverse order** (267–287), joining the original and rollback errors. Test asserts the already-applied first file is rolled back while the concurrent change to the second file is left intact (checkout_test.go:164–172).

### Supporting integrity / boundary guarantees
- Checkout target must be **outside** the project root (`checkoutPath` 377–398; `within` guard 394–396). Inside-root checkout fails (checkout_test.go:119–121).
- Published output (`dist/`) and ignored paths are excluded from the copy (`readDocuments` → `index.MatchIgnore`, checkout.go:350). `dist/published.md` is not copied (checkout_test.go:130–132).
- Untrusted marker IDs are rejected via `validID` (checkout.go:174, 526–532); a `../../generated` marker is refused (checkout_test.go:144–147). Marker/state cross-check requires matching version, id, and path (182–184).
- Symlinks in the corpus are refused (readDocuments 341–343); path collisions detected (359–361); size bounded by `maxMarkdownBytes` (8 MiB), `maxCorpusBytes` (64 MiB), `maxCorpusDocuments` (10 000) — checkout.go:27–29, 366–368.

### Minimum facts a consumer answer MUST contain (H4)
1. Check-in is a **three-way merge**: base (recorded at checkout) vs draft (checkout dir) vs current workspace.
2. Conflict when the **same file changed in both draft and workspace** since base.
3. Conflict when the draft **edits a generated document** (must edit its template/data instead).
4. A valid apply writes draft changes and **preserves concurrent workspace edits to other files** (disjoint merge); it also applies draft deletions.
5. Applying does **not** accept the source baseline — source variance/review remains required and is only reported.
6. **Apply-time race guard**: re-checks each target against the planned state and aborts ("workspace changed while applying") if the workspace changed since planning.
7. Failure during apply is **rolled back** (transactional/atomic).
8. On success the **private checkout state file** (`.poolboy/checkouts/<id>.json`) is removed (session closed); the **external checkout directory is retained**, not deleted.
9. Preview (no `--apply`) mutates nothing.

---

## H5 — Versioned structured help

**Question:** How is help sourced, checked, snapshotted, released as an asset, and reconstructed at deploy time?
*(This failed the prior 0.2.0 benchmark; the full pipeline is traced below.)*

### Source of truth + embedded fallback
- **Canonical catalog:** `data/commands.json` — a schema-1 document with `title`, `description`, `intro`, and a `commands[]` array; each command carries `name`, `summary`, `mutates`, `aliases`, `source`, `flags[]`, `usage`, `effects`, `does_not`, `verify_with[]`, `related[]` (data/commands.json:1–38).
- **Embedded fallback:** compiled into the binary via `//go:embed commands.json` in `data/embed.go` (package `commanddata`); exposed by `commanddata.Commands()` (embed.go:6–10). This is the offline copy the CLI always has.

### How help is served at runtime (`cmd/poolboy/help.go`)
Entry points: `poolboy help [command]` (`cmdHelpEntry`, main.go:128–136) and any `poolboy CMD --help|-h` (`run` main.go:147–149; `commandHelpRequested` help.go:54–61). Both call `cmdHelp` (help.go:63–94). The CLI's own version is `main.Version`, stamped at build via ldflags `-X main.Version={{ .Version }}` (.goreleaser.yaml:29–30), default `"dev"` (main.go:13).

`cmdHelp` order of operations:
1. Load + validate the embedded catalog: schema must equal `helpSchema=1` and be non-empty (`embeddedHelpCatalog`, help.go:96–105, const 19).
2. Resolve the canonical command name, honoring `aliases` (`canonicalHelpCommand`, 107–124).
3. **Try hosted help first** (`fetchHelp`, 156–165): builds `https://poolboy.sh/cli/help/v<Version>/index.json` or `.../v<Version>/commands/<name>.json` (base URL 22). Skipped for non-semver/dev builds (157–159). Transport is hardened: 2-second timeout, redirect cap of 5 and same-host only (helpClient 22–31), 2 MiB body limit (182). **There is a single request with a fixed timeout — no retry/backoff (F2 is false).**
4. If the remote returns 200 **and** `validateHelpDocument` passes — schema==1, `cli_version==Version`, index has commands or command doc has matching `name`+non-empty `usage` (186–207) — print the remote doc (77–78).
5. If the remote returns 404/410, `warnUnsupportedHelp` (80–82, 222–239) fetches `.../index.json`, reads the support manifest, and if this CLI's version < `minimum_supported` prints an upgrade nudge to stderr (237).
6. **Fallback to embedded** otherwise: whole catalog via `embeddedCatalogDocument` (126–135, injects `schema` + `cli_version=Version`) or a single command via `embeddedCommandDocument` (137–146). Output is normalized/pretty-printed by `writeHelp` (209–220).

Net guarantee: a binary either gets **version-matched hosted help** or its **own embedded copy** — never a mismatched document.

### How help is checked (CI)
- `scripts/check-command-docs.mjs` — run in lefthook pre-commit (`lefthook.yml:22–23`) and `make check` (`Makefile:22`). It validates the catalog: required string fields `name/summary/usage/source/effects/does_not` (9–13), `aliases` string + `mutates` boolean (14–15), `flags/verify_with/related` arrays (16–18), `schema==1` (30), `related` and `verify_with` reference real commands/aliases (34–46), **and cross-checks documented flags against each command's actual Go handler** by parsing the `cmd<Name>` function body — any undocumented or undeclared flag fails (48–72).
- `scripts/generate-help-snapshot.mjs --self-test` — run in `make check` (`Makefile:23`); exercises snapshot + manifest and asserts a malformed retained artifact fails the manifest (`selfTest`, generate-help-snapshot.mjs:151–197).

### How help is snapshotted (`scripts/generate-help-snapshot.mjs`)
Pure Node stdlib, no deps/network (header 1–14). Two subcommands (`main`, 199–221):
- `snapshot <version> <helpRoot>` (52–65): loads+validates the catalog (`loadCatalog` 23–29, `validateCommands` 38–45), wipes `v<version>/`, writes `v<version>/index.json = {...catalog, schema:1, cli_version:version}` (59), and one `v<version>/commands/<name>.json = {...command, schema:1, cli_version:version}` per command (61–63). `commandName` blocks path traversal (`""`, `.`, `..`, `/`, `\`) (31–36).
- `manifest <helpRoot>` (122–149): scans `v*` dirs, validates each snapshot (`validateSnapshot` 99–120), keeps **only stable (non-prerelease)** versions (134–139), and writes `<helpRoot>/index.json = {schema:1, latest, minimum_supported (oldest stable), versions[] newest-first}` (140–147). Semver-aware ordering with release > prerelease (`parseVersion`/`compareVersions` 69–88).

The local site build also snapshots the current version: `Makefile` `site` target runs `generate-help-snapshot.mjs snapshot $(SITE_VERSION) .site/cli/help` (Makefile:46).

### How help is released as an asset (`.github/workflows/release.yml`)
Triggered by a `v*` tag push (3–6). The **"Create immutable CLI help snapshot"** step (93–107):
- `ASSET="poolboy-help-v${VERSION}.tar.gz"` (102).
- Runs `node scripts/generate-help-snapshot.mjs snapshot "${VERSION}" "${SNAPSHOT_ROOT}"` (104).
- `tar -czf "${ASSET}" -C "${SNAPSHOT_ROOT}" "v${VERSION}"` and asserts non-empty (105–106).
- `gh release upload "${TAG}" "${ASSET}"` (107).

So every release carries an **immutable tarball of that version's help snapshot** as a GitHub release asset. The release is created as a **draft** by GoReleaser (73–80) and only published (`--draft=false`) after the npm smoke test passes (`publish-release`, 231–241) — the help asset therefore accompanies the version whose binary reports it.

### How help is reconstructed at deploy time (`.github/workflows/deploy.yml`)
Triggered on a successful `release` `workflow_run` or manual dispatch (10–14, 25). The **"Rebuild hosted CLI help from release snapshots"** step (73–130):
1. `HELP_ROOT=.site/cli/help`, wiped and recreated (79–81).
2. Lists all **published, non-draft, non-prerelease** releases, newest-first (85–91).
3. For each `v*` tag: looks for asset `poolboy-help-v${version}.tar.gz` (99–105), downloads it (106–109), validates the tar and extracts into `HELP_ROOT` (114–115), and verifies `v${version}/index.json` is present (116–119).
4. Retains up to the **12 most recent** stable snapshots (`retained==12` break, 120–124) and requires at least one, else fails (125–128).
5. Runs `node scripts/generate-help-snapshot.mjs manifest "${HELP_ROOT}"` to rebuild the support index `.site/cli/help/index.json` (130).
Site content is built by `make site` (68–72) and deployed to Cloudflare via `wrangler-action` (133–138), producing the `https://poolboy.sh/cli/help/...` tree the runtime CLI fetches.

### Invariants that tie the pipeline together
- The runtime URL scheme (`v<Version>/index.json`, `v<Version>/commands/<name>.json`, help.go:160–162) **exactly matches** the snapshot layout (generate-help-snapshot.mjs:59–63) and the deploy extraction path check (deploy.yml:116).
- Every hosted doc's `cli_version` must equal the requesting CLI's `Version`, or the CLI rejects it (validateHelpDocument:197) and falls back to embedded — old binaries always get version-matched or embedded help.
- Hosted help is **additive and immutable**: each release freezes its snapshot as an asset; deploy reassembles the newest 12 stable snapshots plus a manifest whose `minimum_supported` drives the upgrade nudge.

### Minimum facts a consumer answer MUST contain (H5)
1. Help is sourced from the canonical catalog **`data/commands.json`**, embedded in the binary via **go:embed** (`data/embed.go`) as the offline fallback.
2. CI **checks** the catalog with `scripts/check-command-docs.mjs` (structure + flags cross-checked against the Go handlers) and self-tests the snapshot generator.
3. At **release** (v* tag), `generate-help-snapshot.mjs snapshot` produces a per-version snapshot (`v<version>/index.json` + `commands/<name>.json`, each stamped `schema` + `cli_version`), tarred as **`poolboy-help-v<version>.tar.gz`** and uploaded as a **GitHub release asset**.
4. At **deploy**, all published stable releases' help assets are downloaded and extracted (up to 12 newest retained), then `generate-help-snapshot.mjs manifest` builds `index.json` (`latest` / `minimum_supported` / `versions`), and the site is deployed to Cloudflare.
5. At **runtime**, the CLI fetches hosted help at `poolboy.sh/cli/help/v<Version>/...` first, validates `schema`/`cli_version` match, and **falls back to the embedded catalog** otherwise (dev builds and network failures always use embedded).

*(Non-features confirmed absent while tracing: no exponential backoff/retry on help fetch — single 2 s request (F2 false); no CDN upload config; no post-publish webhook.)*
