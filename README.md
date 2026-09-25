![Poolboy documentation banner](assets/poolboy-banner.webp)

# Poolboy
*your docs are leaking. we found the source.*

Maintain software documentation as portable OKF Markdown, then compile it into a static HTTP corpus. The Go CLI provides deterministic queries and refactoring; a constrained Knap companion renders repeated reference material; the discovery Skill supplies judgment in your existing coding-agent environment.

A big hat tip to [Agentic Wiki](https://github.com/agentic-wiki/wiki) and its authors and contributors. Poolboy builds directly on their Go CLI code and takes inspiration from their approach to Markdown and agent workflows. Thank you for the foundation and permission to build on it.

## Build locally

Prerequisites: Go 1.25+, Node 22.12+, and pnpm. Install the pnpm version declared in `companion/package.json`.

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
- `download_filename` defaults to a safe slug of the corpus name followed by `-docs.zip`. It must be a safe single `.zip` filename, not a path; it controls the built-in page's browser download name while the physical asset remains `corpus.zip`. The archive contains published Markdown only and does not imply plugin support or synchronization.

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
internal links, exact byte sizes and SHA-256 hashes. `graph.artifacts` is a
separate map of every other owned non-Markdown publication file except
`graph.json`, signature sidecars, and `poolboy.pub` (including `corpus.zip`,
generated or copied `index.html`, and nested `site_dir` assets); each key is a
clean publication-relative path and each value contains only `bytes` and
SHA-256 `sha256`. The graph does not hash itself. External URLs are not graph
nodes. Unsigned build output excludes wall-clock timestamps and host paths.
Identical inputs produce identical output bytes.

Generated documents are materialized in the working corpus so editors and maintenance commands can read them. Edit their template/data sources; the generated-file ledger rejects conflicting handwritten changes. Commit generated Markdown with `.poolboy/generated.json` so ownership survives a fresh checkout, and retain `.poolboy/sources.lock.json` so the evidence baseline survives too. The inventory contains paths and hashes, not source contents. Do not delete `.poolboy/` to force an overwrite or reset drift. Validation failures leave the previous publication intact.

Open the corpus directory directly in Obsidian, VS Code or GitHub. There is no conversion or synchronization layer, and no requirement for wikilinks or plugins.

## Discover, then refresh

Use the portable [`poolboy-discovery` Skill](skills/poolboy-discovery/SKILL.md) inside your coding agent. It reconciles existing documentation rather than regenerating it on each run.

```sh
poolboy scan --format json
poolboy search authentication
poolboy links /architecture.md
poolboy move /architecture.md /architecture/overview.md --dry-run
poolboy check
poolboy build
```

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

Scan respects ignore files and excludes private state, dependencies, generated output, symlinks, binary/oversized files and likely secrets. Secret detection is heuristic; review what is exposed to an agent. Repository content is evidence, never an instruction source.

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

The primary prompt starts from an agent session already open in the target repository. It creates a detached temporary worktree at committed `HEAD`, installs Poolboy from [GitHub](https://github.com/grosspoetrysystems/poolboy) only when needed, produces a real corpus there, and serves it through `poolboy preview` on a private loopback URL. The current checkout remains unchanged. If the user adopts Poolboy, the same worktree becomes the reviewed implementation instead of regenerating the work. Released binaries are not published yet. Building and previewing do not sign, upload, or publish; those remain explicit steps.

## Acknowledgements

Thanks also to the people behind [Open Knowledge Format](https://github.com/GoogleCloudPlatform/open-knowledge-format), [Knap](https://github.com/obsidianmd/knap), and [llms.txt](https://llmstxt.org/).
