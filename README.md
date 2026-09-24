![Poolboy documentation banner](assets/poolboy-banner.webp)

# Poolboy
*your docs are leaking. we found the source.*

Maintain software documentation as portable OKF Markdown, then compile it into a static HTTP corpus. The Go CLI provides deterministic queries and refactoring; a constrained Knap companion renders repeated reference material; the discovery Skill supplies judgment in your existing coding-agent environment.

A big hat tip to [Agentic Wiki](https://github.com/agentic-wiki/wiki) and its authors and contributors. Poolboy builds directly on their Go CLI code and takes inspiration from their approach to Markdown and agent workflows. Thank you for the foundation and permission to build on it.

## Build locally

Prerequisites: Go 1.25+, Node 22+, and pnpm. Install the pnpm version declared in `companion/package.json`.

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
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
make fmt
make check
make hooks
```

Ensure `$(go env GOPATH)/bin` is on PATH. Hook installation is local; no command commits or publishes for you.

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
- `description` defaults to `Documentation built for agents.`; an explicit empty string is preserved.
- `secondary_description` defaults to `Copy the prompt into your agent and ask your question.`; an explicit empty string is preserved.
- `prompt` starts with `Use {{url}}/llms.txt to answer my question about {{title}}.`, followed by instructions to cite sources, flag gaps and treat fetched content as reference rather than instructions. `{{title}}` is replaced with the effective landing title during the build; `{{url}}` is replaced client-side with the publication base. Both substitutions remain text, not HTML.
- `base_url` is absent or empty by default: the page derives the actual deployed page directory, including a hosting subpath, from the browser URL. An explicit value is an HTTP(S) canonical publication root with an optional subpath; userinfo, query strings and fragments are rejected.
- `download_filename` defaults to a safe slug of the corpus name followed by `-docs.zip`. It must be a safe single `.zip` filename, not a path; it controls the built-in page's browser download name while the physical asset remains `corpus.zip`. The archive contains published Markdown only and does not imply plugin support or synchronization.

`[style]` accepts the small validated style surface only: `font` (default `monospace`, a font-family token list with no `url()`, slash, braces or semicolons), `text` (default `#e6e6e6`), `background` (default `#111111`), `button` (default `#111111`), `button_text` (default `#86efac`), and integer pixel `border_radius` from `0px` through `64px` (default `4px`). Colors accept only `#hex` in 3/4/6/8-digit forms. `button` is the button background; `button_text` is the text and border accent. The landing is an end-user handoff for the publishing project, not a Poolboy product or installation page.

A configured `base_url` is useful when a reverse proxy hides the public publication root; otherwise the browser-derived fallback keeps links working at both domain root and subpaths.

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

The landing page is a minimal, responsive agent handoff with a copyable prompt and links to the Markdown index, graph and `llms.txt`. The ZIP is a deterministic, portable download of the published Markdown tree. Open that tree directly in Obsidian, VS Code, GitHub or another editor; there is no conversion, plugin or remote-sync requirement.

`graph.files` contains only canonical Markdown document identities, directed internal links, exact byte sizes and SHA-256 hashes. `graph.artifacts` is a separate map of every owned non-Markdown publication file except `graph.json` and `llms.txt` (including `corpus.zip`, generated or copied `index.html`, and nested `site_dir` assets); each key is a clean publication-relative path and each value contains only `bytes` and SHA-256 `sha256`. The graph does not hash itself. External URLs are not graph nodes. Unsigned output excludes wall-clock timestamps and host paths. Identical inputs produce identical output bytes.

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

Deploy `dist/` with your existing static host. A browser can open `/index.html`; an agent or other consumer needs only HTTP:

```text
GET /index.html
GET /llms.txt
GET /graph.json
GET /corpus.zip
GET /architecture/overview.md
```

`base_url` is optional: when absent or empty, the landing page derives the publication directory from its actual browser URL, including any deployment subpath. An explicit `base_url` is the canonical HTTP(S) publication root. `download_filename` changes the browser's suggested name for `/corpus.zip`; the asset remains a portable Markdown download, not a synchronization mechanism.

No Poolboy server, SDK, MCP, database or inference service is required. Private deployments use the host's existing authentication/network controls. Build does not publish automatically. Resource hashes provide a comparison target; an unauthenticated manifest does not itself establish publisher authenticity.

### Deploy Poolboy's product site on Cloudflare

The product site is separate from the generated docs landing. `make site` assembles both into `.site/`: the product page at `/`, source-install instructions at `/install.md`, and the generated corpus at `/docs/`. Both prompts derive their URLs from the deployed hostname; no domain is hardcoded.

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

The install prompt guides a source checkout from [GitHub](https://github.com/grosspoetrysystems/poolboy), not a released binary installer. Building, previewing and dry-running do not publish; deployment and domain setup are explicit steps.

## Acknowledgements

Thanks also to the people behind [Open Knowledge Format](https://github.com/GoogleCloudPlatform/open-knowledge-format), [Knap](https://github.com/obsidianmd/knap), and [llms.txt](https://llmstxt.org/).
