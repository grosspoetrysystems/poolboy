---
type: concept
title: Build and render
description: How Poolboy validates, renders, stages, and publishes a deterministic corpus.
status: draft
sources:
  - resource: ../README.md
  - resource: ../poolboy.toml
  - resource: ../cmd/poolboy/product.go
  - resource: ../cmd/poolboy/commands.go
  - resource: ../cmd/poolboy/signing.go
  - resource: ../internal/compiler/build.go
  - resource: ../internal/compiler/graph.go
  - resource: ../internal/discover/discover.go
  - resource: ../internal/source/discover.go
  - resource: ../internal/compiler/ledger.go
  - resource: ../internal/compiler/validate.go
  - resource: ../internal/sign/sign.go
  - resource: ../internal/scaffold/scaffold.go
  - resource: ../internal/output/output.go
---
# Build and render

`poolboy build` is the publishing path. It reads the project configuration,
validates the authoring corpus and render mappings, optionally invokes the
trusted companion, and publishes Markdown, `graph.json`, `llms.txt`,
`index.html`, and a deterministic `corpus.zip` under the configured output
directory. The configured output is commonly `dist/`, but the compiler does
not hard-code that name. The landing page's `download_filename` changes the
browser's suggested name for `corpus.zip`; it does not rename the physical
asset.

Build is deterministic and keyless. After a successful build, `poolboy sign`
reads the exact output `graph.json` bytes and writes `graph.json.sig` plus
`poolboy.pub`; signing is never automatic. A later build may replace an
existing signed publication, because those two files are recognized Poolboy
output, but the new output is unsigned until `sign` runs again.

The ZIP contains only the candidate published Markdown, preserving relative
directories and links. Entries are sorted and use fixed metadata so identical
inputs produce identical download bytes.

## Ordered build flow

The compiler keeps validation and staging ahead of the live publication:

1. Resolve the project, corpus, and output roots. The roots must be contained,
   non-symlink directories with a corpus that does not sit inside the output.
2. Require spec `0.2`, reject unknown project settings, and validate each
   template/data/output mapping before a renderer is started.
3. Load `.poolboy/generated.json` and collect the authored Markdown corpus.
   Markdown is bounded, UTF-8 checked, normalized to LF, and keyed by canonical
   root-absolute paths such as `/index.md`.
4. Check case and supported Unicode canonical-path collisions.
5. Resolve the renderer when mappings exist, read bounded template and data
   files, render each mapping, normalize the result, and validate generated
   Markdown as OKF.
6. Re-read render targets and reconcile them with the generated-file ledger.
   An edited tracked file or an existing handwritten target that a render would
   overwrite is a blocking conflict.
7. Construct the candidate corpus, validate every candidate, and stage it in a
   temporary sibling directory. The live corpus is unchanged at this point.
8. Build and validate the staged index, derive the graph from the candidate
   Markdown bytes, and prepare the landing assets. With no `site_dir`,
   render the built-in page; with one, validate and copy its safe static files
   without executing a framework or build command.
9. Stage the complete publication: candidate Markdown, `graph.json`, `llms.txt`,
   the generated or copied `index.html`, the deterministic `corpus.zip`, and
   any additional copied static files.
10. Materialize generated Markdown with backups, stage the next ledger, replace
    the publication directory, and rename the ledger into place.

Validation, rendering, and staging happen before mutation; those failures leave
the live publication untouched. Once mutation begins, later filesystem failures
attempt to restore generated files and the publication, reporting restore
errors alongside the original error via `errors.Join`. This is staged
replacement with rollback attempts, not a crash- or power-loss recovery
guarantee, and it does not promise uninterrupted availability across the
directory renames.

Operational build failures preserve that boundary. Text and JSON carry the same
facts; JSON uses exit 2 and the fields `code`, `status`, `detail`, and
`next_action`. Missing companions, missing Node.js, and renderer
template/protocol/process failures are classified without matching error
strings. A foreign or occupied output is reported with its path and preserved
without deletion. Validation failures remain the issue array with exit 1.

## External publication discovery

```sh
poolboy build --discover
```

This opt-in build reads links in corpus Markdown and literal HTTPS URLs in
currently admitted source files. It uses the source scanner's exclusions and
does not accept or write a source baseline. A project-page URL is enough when
ordinary links lead to a Poolboy landing or its advertised `graph.json` or
`llms.txt`; source templates and client-side JavaScript are not executed.

Ordinary traversal stays on the seed origin. A directly linked foreign
entrypoint permits its own origin and path subtree, not unrelated site
navigation or further ordinary foreign links. Explicit graph/llms links and
validated graph references can cross that boundary. Discovery does not guess
beacon paths on arbitrary hosts. Once a publication is accepted, its declared
external references replace ordinary document crawling.

| CLI discovery boundary | Maximum |
| --- | --- |
| Network stage | 30 seconds total; 10 seconds per resource fetch |
| Fetches and redirects | 64 resource fetches; 5 redirects per fetch |
| Fetched body bytes | 64 MiB total; 32 MiB per graph; 2 MiB per page or llms; 8 MiB for the root document |
| Traversal | 3 link hops; 32 publications; 256 accepted links per page |

Missing, unsupported, unsafe or over-budget targets produce stderr diagnostics
and partial discovery results; cancelling the build still stops publication.
These limits bound best-effort discovery, not an exhaustive crawl.

Accepted `graph.external_references` entries retain the local declaring
path/line, the remote manifest URL and its exact SHA-256. The builder checks
the graph's v0 declarations and matching llms/root bytes, not every remote
document. `llms.txt` exposes the remote entrypoints; files, artifacts and ZIPs
remain local. References do not establish publisher trust or prompt safety.

Without `--discover`, builds stay offline and deterministic. Opted-in builds
capture the remote revisions observed during that run; rebuilding may update
their pins. Consumers must check those pins before traversing a publication,
as described in [Portable contracts](portable-contracts.md#publication-discovery-and-compatibility).

## Published files

`graph.json` has a fixed version (`"0"`), root (`"/index.md"`), a dedicated
`llms` digest, and a `files` map keyed by canonical Markdown document paths.
Each Markdown entry includes title, optional type, exact byte size, SHA-256,
sorted unique internal links, and normalized sources. Graph edges collapse to
canonical document identity: fragments are validated but are not separate
nodes. `graph.llms` contains the exact byte size and SHA-256 of `llms.txt`.

Derived traversal is a separate `graph.nav` map keyed by the same document
paths, so signing covers authored identity and computed edges without
conflating them. `backlinks` is the sorted inverse of the authored links,
excluding self-links. `prev` and `next` follow the order the root `index.md`
links documents, so the authored index is the sequence; a document the index
never links has no position and no `nav` entry at all. `llms.txt` publishes the
same sequence under a `Reading order` heading. Nothing here is guessed: a corpus
that states no relationship publishes no edge.

`graph.artifacts` is a sibling map of every other owned non-Markdown
publication file except `graph.json`, `graph.json.sig`,
`graph.json.sigstore.json`, and `poolboy.pub`. Its keys are clean,
publication-relative paths without a leading slash, such as `index.html`,
`assets/app.js`, and `corpus.zip`; each value contains only its exact byte
`bytes` and lowercase SHA-256 `sha256`. The map includes safe files copied from
a custom `site_dir` and the generated ZIP. It does not hash `graph.json`
itself. The configurable `download_filename` is only the built-in page's
browser suggestion for `corpus.zip`, and is not an artifact key; custom
`site_dir` HTML is copied verbatim and owns its own links.

The public release workflow writes `graph.json.sigstore.json` by signing the
exact `graph.json` bytes with its GitHub Actions OIDC identity. `poolboy
approve` requires that exact expected workflow identity, verifies Sigstore's
certificate and transparency evidence, then verifies `llms.txt`, every
Markdown file, and every artifact before prompting for the displayed exact
manifest digest. `poolboy verify` never mutates trust: it fully checks the
approved digest and reports any different valid digest as pending.

Stable Sigstore releases require 72 hours of trusted transparency-log age
before approval unless a human records an exact-digest `--override-age`.
An older trusted time is rejected as rollback. Identity changes require
explicit migration. An explicit `--lock` is the authoritative project/CI
record; without it, approval uses the local trust store.

Private/local publishers may explicitly use the weaker Ed25519 TOFU path.
`graph.json.sig` signs the exact bytes of `graph.json`; `poolboy.pub` contains
the standard-base64 public key. TOFU pins both key continuity and the exact
approved digest but provides no strong publisher identity or trusted release
time. Keep the private seed in its 0600 `--key` file or provide it via
`POOLBOY_SIGNING_KEY`, never in the publication.

`dist/` is a host-agnostic static publication. Provider configuration receives
prebuilt `dist/`, runs no build command or runtime/functions, and uses exact-file
routing without an SPA fallback. Deployment copies those bytes unchanged to
any HTTP file host; it does not run Poolboy, Node, Sigstore, a database, or
application code. Signing happens before upload, and the signature sidecars
travel with the corpus. `base_url` may pin a canonical publication root, but
leaving it empty lets the landing derive the deployed host and subpath at
runtime.

Poolboy has no publication target. `build` writes the configured `output`
directory; nothing detects a host, and `base_url` is a canonical-root hint for
share metadata, never a routing decision.

`publish` is the setup report for one documented destination, GitHub Pages. It
reads the configured output, derives a project-site URL only from a recognized
local `origin` remote, and names the admin action that enables Pages. It makes
no network call and changes no repository state; `--apply` writes
`.github/workflows/poolboy-docs.yml` and nothing else. The workflow contains an
exact Poolboy version string, but syntax is not proof that npm has published or
can resolve that version. JSON exposes the same bounded prerequisites as text
(`step`, `status`, `owner`, `detail`, and `next_action`); operational failures
use `code`, `status`, `detail`, and `next_action` and exit 2. Rewriting an
existing workflow requires `--force`.

The report keeps these adoption boundaries explicit and offline:

1. A missing companion or missing Node.js runtime blocks a render; install the
   trusted companion/Node.js or pass `--renderer`, and rerun.
2. A nonempty foreign or occupied output is refused and preserved; choose
   another output or move it deliberately.
3. A linked worktree or checkout without a resolvable GitHub remote gets no
   fabricated URL or command; resolve the owner/repository outside Poolboy.
4. The trigger branch comes from local `origin/HEAD` when available; an
   assumed branch is unverified and must be checked before committing.
5. Pages enablement and admin access remain a GitHub-admin action.
6. Auto-merge policy is outside Poolboy and is neither queried nor changed.
7. Branch protection and rulesets are not inspected or bypassed.
8. Existing required CI is not inspected; maintainers must inspect/fix it.
9. npm resolution of the exact workflow pin is checked only by the workflow
   run, never inferred from semver syntax.

Poolboy does not commit, push, merge, change Pages settings, change branch
protection, enable auto-merge, or inspect remote CI. It reports local facts and
unverified external steps; it does not claim deployment success.

The published project site lives under a `/REPO/` subpath, which the landing's
relative links and runtime-derived base already handle, so the bytes `poolboy
preview` serves are the bytes Pages serves. A custom domain is GitHub Pages
configuration — repository setting plus DNS — and cannot be expressed in the
workflow or the publication: with Actions publishing GitHub creates no `CNAME`
file and ignores one in the uploaded artifact. Neither path is
configuration-free; the project-site default still requires the Pages source
and workflow permissions, which only a repository admin can set. Pages is
public hosting and is not a route for documentation that must stay internal.

Because the publication is deterministic and self-contained, a released tool can
embed the same `dist/` it publishes (`//go:embed all:dist` for a Go binary) and
answer documentation queries with no network access, which is what CI,
sandboxes, and air-gapped hosts require. The SHA-256 of the embedded
`graph.json` identifies the embedded revision and matches the published one when
both come from the same build. The published site is then an opt-in mirror, not
a runtime dependency: the binary itself never fetches.

The built-in `index.html` derives its favicon from `logo`, or from the
effective nonempty `mark` when no logo is set. Empty mark plus no logo
omits the favicon; a custom `site_dir` owns its own favicon. This is
derived from existing settings and adds no artifact key or configuration field.

`llms.txt` is a small deterministic entry point. After the configured corpus
name is trimmed and line breaks are removed, it contains links relative to
itself (`index.md` and `graph.json`) so discovery works under a deployment
subpath. Generated graph, llms, and ledger serialization uses stable ordering
and final newlines; compiler-generated metadata does not include timestamps or
host filesystem roots.

The published directory is intended for a static HTTP consumer: fetch
`index.html`, `llms.txt`, `graph.json`, `graph.json.sigstore.json`, optional
TOFU sidecars, `corpus.zip`, a copied static asset, or a Markdown path with
`GET`. Poolboy does not serve those files or provide a runtime SDK.

## Generated ownership

The generated ledger records each generated Markdown path, its render data and
template, and the normalized output hash. A current file with no prior ledger
entry is treated as handwritten and cannot be overwritten. A tracked file that
was edited outside its template cannot be silently replaced; unchanged stale
outputs are the only generated files eligible for removal.

If generated Markdown is committed for portability, commit both
`.poolboy/generated.json` and `.poolboy/sources.lock.json` with it. The
generated ledger preserves ownership and the source lock preserves the evidence
baseline across a fresh checkout; the scaffold and root ignore policy retain
both files. Do not delete the state directory to force an overwrite or reset
drift: reconcile the rendered output and the ledgers instead.

## Build versus review

`build` validates and publishes. `check` is a separate CLI maintenance command:
it reports conformance and health issues without writing by default, while
`check --fix` applies its explicitly safe repairs. Source `scan`, `drift`, and
`affected` answer evidence and direct-provenance questions; none of those
states proves that the corpus was reviewed or compiled.
