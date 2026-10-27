![Poolboy documentation banner](assets/poolboy-banner.webp)

# Poolboy
*your docs are leaking. we found the source.*

Maintain software documentation as portable OKF Markdown, then compile it into a static HTTP corpus. The Go CLI provides deterministic queries and refactoring; a constrained Knap companion renders repeated reference material; the discovery Skill supplies judgment in your existing coding-agent environment.

A big hat tip to [Agentic Wiki](https://github.com/agentic-wiki/wiki) and its authors and contributors. Poolboy builds directly on their Go CLI code and takes inspiration from their approach to Markdown and agent workflows. Thank you for the foundation and permission to build on it.

## Install

Poolboy requires Node 22 or newer:

```sh
npm install --global @grosspoetrysystems/poolboy
poolboy version
```

Every npm package contains the platform binary it executes; installation runs
no lifecycle scripts and downloads no executable from another host. Published
versions carry npm provenance and correspond to the signed archives on the
[GitHub Releases](https://github.com/grosspoetrysystems/poolboy/releases) page.

## First useful result

Run this inside a Git repository containing Markdown, or in an empty directory:

```sh
poolboy init .
poolboy check
poolboy build
poolboy read /getting-started.md
```

Open `dist/index.html` to use the generated landing page. `poolboy init`
preserves existing files and creates a `docs/` corpus only when no configuration
selects another location. Query paths such as `/getting-started.md` are
corpus-relative, not project-relative.

Review and keep `poolboy.toml`, `docs/`, `templates/`, `data/`, and the tracked
`.poolboy` ledgers. Delete and rebuild `dist/` freely. Malformed YAML is reported
at the document instead of being rewritten; fix it, rerun `check`, then build.
If a source build cannot find the renderer, keep `bin/poolboy-knap.mjs` beside
`bin/poolboy` or pass its absolute path with `build --renderer`.

## Build locally

Prerequisites: Go 1.26+, Node 24+, and pnpm. Install the pnpm version declared in `companion/package.json`.

```sh
git clone https://github.com/grosspoetrysystems/poolboy.git
cd poolboy
pnpm --dir companion install --frozen-lockfile
make build
./bin/poolboy --help
```

`bin/poolboy` and `bin/poolboy-knap.mjs` belong together. Node is needed when compiling Knap templates, not for reading the published corpus. Builds do not install dependencies or fetch content. If the companion lives elsewhere, pass `build --renderer /absolute/path/to/cli.mjs`.

Development tooling follows [rack-mount-go](https://github.com/grosspoetrysystems/rack-mount-go) and [rack-mount-ts](https://github.com/grosspoetrysystems/rack-mount-ts): native Go, golangci-lint v2, pnpm, strict TypeScript, tsdown, Biome/Ultracite, Vitest/v8, Knip and Lefthook.

```sh
make fmt
make check
make hooks
```

Hook installation is local; no command commits or publishes for you.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the reproducible setup, local hooks,
generated-file policy, and pull-request checks.

## Author and build

`poolboy.toml` locates the working corpus and explicit template inputs:

```toml
spec = "0.2"
name = "Acme"
corpus = "docs"
output = "dist"

[[render]]
template = "templates/endpoints.md.knap"
data = "data/endpoints.json"
output = "reference/endpoints.md"
```

### Configure the published docs landing

The generated `index.html` handoff page uses built-in defaults. To customize it,
copy `landing.example.toml` to `landing.toml` beside `poolboy.toml` and set only
the options you want; anything you leave out keeps its default, and deleting
`landing.toml` returns to defaults. `poolboy init` writes `landing.example.toml`
for you. Every value is a string:

- `title` defaults to the corpus name and must be nonempty when supplied.
- `mark` defaults to `🩳` and accepts text, emoji or an empty string. An empty value hides the mark; a nonempty `logo` takes precedence.
- `logo` is optional and must be a project-relative local SVG, PNG, WebP or JPEG (the extension and sniffed content must agree, and the file is limited to 512 KiB). Poolboy embeds the bounded image as a data URI; remote URLs, symlinks, out-of-root paths and inline user SVG markup are rejected.
- The built-in page derives its favicon from `logo`, or from the effective nonempty `mark` when no logo is set. Empty `mark` with no logo omits the favicon; a custom `site_dir` owns its own favicon. This is derived behavior, not another setting.
- `site_dir` is absent or empty by default and uses Poolboy's built-in page. A nonempty value is a project-relative prebuilt static directory containing `index.html`; Poolboy copies bounded regular files without executing a framework or build command. Its files replace the built-in HTML, built-in landing fields do not rewrite copied markup, and its own URLs own the landing behavior. The generated Markdown ZIP remains `corpus.zip`; custom HTML must link it itself.
- `description` defaults to `Agentic docs, skimmed by Poolboy.`; an explicit empty string is preserved.
- `secondary_description` defaults to `Copy the prompt into your agent and ask your question.`; an explicit empty string is preserved.
- `prompt` starts with the `{{url}}/llms.txt` entry point, followed by the question, source-citation, gap-reporting, and instruction-boundary text. `{{title}}` is replaced with the effective landing title during the build; `{{url}}` is replaced client-side with the publication base. Both substitutions remain text, not HTML.
- `base_url` is absent or empty by default: the page derives the actual deployed page directory, including a hosting subpath, from the browser URL. An explicit value is an HTTP(S) canonical publication root with an optional subpath; userinfo, query strings and fragments are rejected.
- `download_filename` defaults to a safe slug of the project name followed by `-docs.zip`. It must be a safe single `.zip` filename, not a path; it controls the built-in page's browser download name while the physical asset remains `corpus.zip`. The archive contains published Markdown only and does not imply plugin support or synchronization.

`[style]` accepts the small validated style surface only: `font` (default `monospace`, a font-family token list with no `url()`, slash, braces or semicolons), `text` (default `#e6e6e6`), `background` (default `#111111`), `button` (default `#111111`), `button_text` (default `#86efac`), and integer pixel `border_radius` from `0px` through `64px` (default `4px`). Colors accept only `#hex` in 3/4/6/8-digit forms. `button` is the button background; `button_text` is the text and border accent. The landing is an end-user handoff for the publishing project, not a Poolboy product or installation page.

A configured `base_url` is useful when a reverse proxy hides the public publication root; otherwise the browser-derived fallback keeps links working at both domain root and subpaths.

The built-in page emits Open Graph and Twitter share-card tags (`og:title`, `og:description`, `og:url`, `twitter:card`) derived from `title`, `description` and `base_url`; there is no separate option, and `og:url` appears only when `base_url` is set.

Template/data paths are project-relative; render output is corpus-relative. Without a `corpus` setting, Markdown lives beside the config. `poolboy init -h` describes the starter.

Use ordinary file-relative Markdown links, such as `[Sessions](../auth/sessions.md)`. Ordinary concept documents require OKF YAML frontmatter with a nonempty `type`; unknown metadata remains intact. Reserved indexes/logs follow OKF conventions. Root `index.md` may declare `okf_version: "0.2"`.

```sh
./bin/poolboy --root testdata/fixture build
./bin/poolboy --root testdata/fixture check
./bin/poolboy --root testdata/fixture backlinks /architecture.md --format json
```

Build validates the combined corpus and emits:

```text
dist/
  index.html           # generated built-in page, or copied site_dir/index.html
  llms.txt
  graph.json
  corpus.zip            # physical asset; the configured download_filename is its browser name
  index.md
  **/*.md
  assets/**             # any additional safe files copied from landing.site_dir
```

When a build cannot proceed, text and JSON report the same next action. With
`--format json`, operational failures exit 2 as an object containing
`code`, `status`, `detail`, and `next_action`: a missing companion names the
path or `--renderer` remedy, a missing Node.js runtime names the PATH remedy,
renderer failures preserve their kind/code and template diagnostics, and a
foreign or occupied output names the preserved path. Validation failures keep
their existing issue-array JSON and exit 1. Poolboy never deletes an output it
cannot prove it owns.
`poolboy build` is deterministic and keyless. The public deploy workflow signs
the exact `graph.json` bytes with Sigstore and publishes
`graph.json.sigstore.json`. For a private/local Ed25519 publication:

```sh
poolboy keygen --out poolboy.key
poolboy --root . sign --key poolboy.key
poolboy approve dist --tofu
poolboy verify dist
```

`poolboy keygen` writes a base64 Ed25519 seed with mode `0600` and prints the
public key. `poolboy sign` reads a seed from the `--key` file, or directly from
`POOLBOY_SIGNING_KEY`, then writes `graph.json.sig` and `poolboy.pub`; it never
copies the private key into `dist/`. Re-run `sign` after every build because the
signature covers that build's exact manifest.

The landing page is a minimal, responsive agent handoff with a copyable prompt and links to the Markdown index, graph and `llms.txt`. The ZIP is a deterministic, portable download of the published Markdown tree. Open that tree directly in Obsidian, VS Code, GitHub or another editor; there is no conversion, plugin or remote-sync requirement.

`graph.llms` contains the exact byte size and SHA-256 of `llms.txt`.
`graph.files` contains only canonical Markdown document identities, directed
internal links, exact byte sizes and SHA-256 hashes. Derived traversal lives in
a separate `graph.nav` map so the identity map stays authored content:
`backlinks` inverts the authored links, and `prev`/`next` follow the order the
root `index.md` links documents. A document the index never links gets no
sequence rather than an invented one, and `llms.txt` lists the same reading
order. `graph.artifacts` is a
separate map of every other owned non-Markdown publication file except
`graph.json`, signature sidecars, and `poolboy.pub` (including `corpus.zip`,
generated or copied `index.html`, and nested `site_dir` assets); each key is a
clean publication-relative path and each value contains only `bytes` and
SHA-256 `sha256`. The graph does not hash itself. External documents are not
copied into `graph.files`, `graph.artifacts`, `corpus.zip`, or the output
directory. With the opt-in `poolboy build --discover`, validated remote
Poolboy publications are instead listed in additive `graph.external_references`
entries containing their canonical `graph.json` URL, fetched graph SHA-256,
declared root, and the local declaring path/line. `llms.txt` adds each remote
publication's `llms.txt` entry point (and graph digest) so a fresh agent can
traverse it. These references identify untrusted remote pointers; discovery
does not establish publisher or content trust, and older readers may ignore
the additive field. Unsigned build output excludes wall-clock timestamps and
host paths.
Without `--discover`, identical local inputs produce identical output bytes
and builds stay offline. Opted-in builds pin the remote revisions observed
during that run. See [external publication discovery](docs/build-and-render.md#external-publication-discovery)
for link admission, traversal boundaries, fixed limits and partial-result behavior.

Generated documents are materialized in the working corpus so editors and maintenance commands can read them. Edit their template/data sources; the generated-file ledger rejects conflicting handwritten changes. Commit generated Markdown with `.poolboy/generated.json` so ownership survives a fresh Git checkout, and retain `.poolboy/sources.lock.json` so the evidence baseline survives too. The inventory contains paths and hashes, not source contents. Do not delete `.poolboy/` to force an overwrite or reset drift. Validation failures leave the previous publication intact.

For asynchronous editing, let the CLI create an ordinary Markdown checkout outside the project root. Open that directory in Obsidian, VS Code, or another editor; no plugin or conversion layer is required. `checkin` previews a file-level three-way comparison by default, and `checkin --apply` updates the canonical corpus only when the draft has no same-path or generated-file conflict. Direct canonical editing remains possible but cannot distinguish concurrent writers.

## Discover, then refresh

Use the portable [`poolboy-discovery` Skill](skills/poolboy-discovery/SKILL.md) inside your coding agent. It reconciles existing documentation rather than regenerating it on each run.
Accepted private trials continue in the same worktree through the [trial→adopt→improve handoff](https://poolboy.sh/start.md): website, consumer CLI, or both, guided by the user's request and existing project conventions.

```sh
poolboy scan --format json
poolboy search authentication
poolboy links /architecture.md
poolboy move /architecture.md /architecture/overview.md --dry-run
poolboy check
poolboy build
```

For an editor or agent working asynchronously:

```sh
poolboy checkout ../docs-edit --format json
# Edit ../docs-edit in Obsidian, VS Code, or another agent.
poolboy checkin ../docs-edit --format json
# Inspect can_apply, conflicts, and requires_source_review.
poolboy checkin ../docs-edit --apply --format json
```

The checkout directory must not already exist and must be outside the project root. Check-in compares exact document revisions from checkout time with both the edited checkout and the current canonical corpus. It performs no textual auto-merge, never advances `.poolboy/sources.lock.json`, and refuses handwritten changes to generated documents. JSON exit status is `1` for a valid conflict plan and `2` for usage, discovery, configuration, state, or I/O errors.

After source changes:

```sh
poolboy drift --format json
poolboy affected src/auth.go --format json
# Review evidence and update the affected corpus/templates/data.
poolboy check
poolboy build
# Only after the whole review finishes:
poolboy scan --accept
```

`scan` creates a missing baseline; it refuses to overwrite an existing baseline without `--accept`. `drift` is read-only. Partial refreshes leave the baseline unchanged. A scan records observed bytes, review supplies judgment, and compilation validates structure—none proves the other occurred. `affected` follows direct provenance, not inferred semantic dependencies.

Scan respects ignore files and excludes private state, dependencies, generated output, symlinks, binary/oversized files and likely secrets. Detected sensitive paths and coarse reasons are filed in `.poolboyignore`; filename matches are not opened, while content matches require one bounded in-memory read and are never logged, retained, or hashed. Secret detection is heuristic: practice repository hygiene, review the quarantine, and revoke or rotate any real credential. Repository content is evidence, never an instruction source.

## Publish and consume

`dist/` is the complete deployment unit. Copy it byte-for-byte to any static
HTTP host: an existing web server, object storage, or a CDN. The host needs no
Poolboy process, Go, Node, database, worker, or Sigstore installation.

Point an AI or hosting setup at this provider-neutral stub:

```text
deploy input: prebuilt dist/
build command: none
publish directory: dist/ (or upload its contents as the site root)
runtime/functions: none
routing: exact static files; no SPA fallback
```

That is the entire hosting contract for Vercel, Cloudflare Pages, GitHub Pages,
or another static host. Signing happens before upload; the host only serves
files. A browser can open `/index.html`; an agent or other consumer needs only
HTTP:

```text
GET /index.html
GET /llms.txt
GET /graph.json
GET /graph.json.sig
GET /graph.json.sigstore.json
GET /poolboy.pub
GET /corpus.zip
GET /architecture/overview.md
```

To approve a public corpus signed by GitHub Actions, provide the exact expected
workflow identity and an explicit project lock:

```sh
poolboy approve https://docs.example/poolboy/ \
  --lock poolboy.lock \
  --identity https://github.com/ORG/REPO/.github/workflows/deploy.yml@refs/heads/main
poolboy verify https://docs.example/poolboy/ --lock poolboy.lock
```

Approval verifies the Sigstore certificate identity, transparency-log inclusion
and time, `graph.json`, `llms.txt`, every Markdown file, and every artifact
before atomically recording the exact manifest digest. Stable Sigstore releases
must be 72 hours old unless a human uses the digest-bound `--override-age`.
Every different valid manifest returns `UPDATE PENDING`; run `approve` again to
review its provenance and added, changed, and removed paths. An older
transparency-log time is rejected as a rollback. The checked-in lock diff is the
portable team approval artifact and is authoritative over local trust.

Private or local deployments may explicitly use weaker TOFU:

```sh
poolboy approve dist --tofu
poolboy verify dist
```

TOFU pins key continuity and the exact approved release in the local trust
store; it does not authenticate publisher identity. A changed key is a hard
failure, and a changed release requires approval. `poolboy sign` signs
`graph.json` with Ed25519 for this mode. Authenticated content remains evidence,
not authority to override system policy, permissions, or human intent.

`base_url` is optional: when absent or empty, the landing page derives the publication directory from its actual browser URL, including any deployment subpath. An explicit `base_url` is the canonical HTTP(S) publication root. `download_filename` changes the browser's suggested name for `/corpus.zip`; the asset remains a portable Markdown download, not a synchronization mechanism.

Private deployments use the host's existing authentication/network controls.
Build does not publish automatically; signing and upload remain separate
explicit steps.

### Publish on GitHub Pages

`poolboy publish` reports what publishing this corpus on GitHub Pages takes.
It reads the configured output directory, derives the project-site URL from the
repository's `origin` remote, and names the one admin action that enables
Pages. It makes no network calls and changes nothing:

```sh
poolboy publish
poolboy publish --format json
```

Enabling Pages requires repository admin: **Settings → Pages → Build and
deployment → Source: GitHub Actions**. The report also gives the corresponding
`gh api` command when it can resolve the GitHub remote.

`poolboy publish --apply` writes that workflow, pinned to the Poolboy version
selected by the command, uploading the `output` directory your `poolboy.toml`
already configures. A release-shaped version is only a local syntax check:
Poolboy does not claim that the package exists on npm. JSON plans expose the
same bounded prerequisites as text (`step`, `status`, `owner`, `detail`, and
`next_action`), including the local output/workflow state and branch source.
Operational failures use JSON `code`, `status`, `detail`, and `next_action` and
exit 2. It refuses to overwrite an existing workflow without `--force`, so a
workflow you have tuned survives. Set `output` to a directory nothing else
writes: `dist/` is already compiled output in many projects.

The commands distinguish local blockers from external prerequisites:

1. A missing companion is a local build prerequisite; install
   `poolboy-knap.mjs` beside the binary or pass `--renderer`.
2. A nonempty foreign or occupied output is refused and preserved; choose
   another output or move it deliberately.
3. A linked worktree or checkout without a resolvable GitHub remote has no
   fabricated URL or `gh` command; resolve the owner/repository yourself.
4. The trigger branch comes from local `origin/HEAD` when available; an assumed
   `main` is marked unverified and must be checked before committing.
5. Pages enablement and repository-admin access are GitHub-admin steps, not
   Poolboy checks.
6. Auto-merge policy belongs to the repository maintainer and is not queried or
   changed.
7. Branch protection and rulesets remain in force; Poolboy does not bypass them.
8. Existing required CI is not inspected; maintainers must inspect/fix it before
   merging.
9. The exact Poolboy package version is written into the workflow, but npm
   resolution is checked only by the workflow run, never inferred from syntax.

Poolboy does not detect your hosting or pick a publication target. There is no
fallback and no inference from `base_url`: `build` writes a directory and the
workflow uploads it. `poolboy init` writes no CI, and `publish` without
`--apply` writes nothing either. Poolboy also never commits, pushes, changes
Pages settings, changes branch protection, enables auto-merge, or inspects
remote CI.

This is the cheapest public surface for a repository that has none: a CLI, a
library, an internal utility. A project that already publishes documentation
elsewhere should keep publishing it there.

The project-site subpath needs no configuration. The generated landing links
`index.md`, `graph.json`, `llms.txt` and `corpus.zip` relatively and derives its
prompt base from `new URL('.', location.href)`, so `OWNER.github.io/REPO/`
resolves correctly; `poolboy preview` serves the same bytes under its own path
segment. Leave `base_url` unset for that; set it only to pin a canonical root.

Serving the same corpus at your own domain is GitHub Pages
[custom domain](https://docs.github.com/pages/configuring-a-custom-domain-for-your-github-pages-site/managing-a-custom-domain-for-your-github-pages-site)
setup, not a Poolboy or workflow setting. With Actions publishing, GitHub
creates no `CNAME` file and **ignores one in the artifact**, so a domain written
into the workflow or into the published output has no effect. Enter the domain
under **Settings → Pages → Custom domain**, point DNS at it (`CNAME` to
`OWNER.github.io` for a subdomain; `A`/`AAAA`/`ALIAS` for an apex), then enable
**Enforce HTTPS**. Nothing in the corpus changes; the landing resolves its base
from wherever it is served.

Neither path is configuration-free, and both need repository admin. The
project-site default needs the Pages source set to GitHub Actions and the
`pages`/`id-token` permissions the generated workflow requests; a custom domain adds DNS and
the domain setting. What you avoid is a hosting account, a server, and a build
service.

Pages is public hosting. A private repository publishes a public site unless the
plan supports private Pages, so do not use this workflow for documentation that
must stay internal.

### Ship the corpus inside a binary

A CLI runs in CI, sandboxes and air-gapped hosts where HTTP is unavailable or
forbidden. Publication is for discovery; the released tool should answer for
itself. `dist/` is deterministic and self-contained, so a Go tool embeds the
same bytes it publishes:

```go
//go:embed all:dist
var docs embed.FS // built by `poolboy build` before `go build`
```

Serve or print from that `embed.FS` for `tool docs`, and derive the embedded
revision from the SHA-256 of the embedded `graph.json`—the same digest
`poolboy approve` pins for the published corpus. Build the corpus before
compiling in the same job that uploads the Pages artifact, and the binary and
the site carry one revision.

The rule that keeps it trustworthy: the binary never fetches. Normal commands
read the embedded corpus only; the published site is opt-in enrichment a user or
agent requests explicitly. Embed published documentation, never local working
notes, telemetry or credentials.

### Deploy Poolboy's product site on Cloudflare

The product site is separate from the generated docs landing and is only this
repository's Cloudflare configuration. `make site` assembles a portable
`.site/` directory: the product page and agent discovery file at `/` and
`/llms.txt`, the private worktree trial at `/try.md`, the repository setup
workflow at `/start.md`, source-install instructions at `/install.md`, and the
generated corpus at `/docs/`. Another static host can publish that directory unchanged.
Both prompts derive their URLs from the deployed hostname; no domain is
hardcoded.

After installing the build prerequisites above:

```sh
make site
npx --yes wrangler@4.138.0 dev --local
```

The checked-in `wrangler.jsonc` uses [Cloudflare Workers Static Assets](https://developers.cloudflare.com/workers/static-assets/) without Worker application code, bindings or a backend. Missing files return 404 rather than the landing HTML; `/docs` redirects to `/docs/`.

Check the deployment without publishing:

```sh
npx --yes wrangler@4.138.0 deploy --dry-run
```

When ready to publish to your Cloudflare account:

```sh
npx --yes wrangler@4.138.0 login
npx --yes wrangler@4.138.0 deploy
```

After registering the domain, open **Workers & Pages → poolboy-site → Settings → Domains & Routes → Add → Custom Domain**. Add the actual hostname; Cloudflare provisions its DNS record and certificate. Apex and `www` are separate hostnames. See [Custom Domains](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/).

The primary prompt starts from an agent session already open in the target repository. It creates a detached temporary worktree at committed `HEAD`, installs Poolboy from [GitHub](https://github.com/grosspoetrysystems/poolboy) only when needed, produces a real corpus there, and serves it through `poolboy preview` on a private loopback URL. The current checkout remains unchanged. If the user adopts Poolboy, the same worktree becomes the reviewed implementation instead of regenerating the work. Released binaries are published to npm as `@grosspoetrysystems/poolboy`. Building and previewing do not sign, upload, or publish; those remain explicit steps.

## Acknowledgements

Thanks also to the people behind [Open Knowledge Format](https://github.com/GoogleCloudPlatform/open-knowledge-format), [Knap](https://github.com/obsidianmd/knap), and [llms.txt](https://llmstxt.org/).
