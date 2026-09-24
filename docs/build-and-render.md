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
  - resource: ../internal/compiler/build.go
  - resource: ../internal/compiler/graph.go
  - resource: ../internal/compiler/ledger.go
  - resource: ../internal/compiler/validate.go
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
   Markdown bytes, and prepare the landing assets. With no `landing.site_dir`,
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

## Published files

`graph.json` has a fixed version (`"0"`), root (`"/index.md"`), and a `files`
map keyed by canonical Markdown document paths. Each file records normalized
byte length, a SHA-256 digest, sorted/deduplicated outbound links, and optional
`title`, `type`, and frontmatter `sources` values. Graph edges collapse to
canonical document identity: a link's `#fragment` is removed from the stored
target, so an authored link to `/reference/endpoints.md#endpoints` becomes the
edge `/reference/endpoints.md`. The fragment is not a separate node. Anchors
are still validated against the target document's headings during the build,
and the authored Markdown keeps its fragment in the published body.

`graph.artifacts` is a sibling map of every owned non-Markdown publication
file except `graph.json` and `llms.txt`. Its keys are clean,
publication-relative paths without a leading slash, such as `index.html`,
`assets/app.js`, and `corpus.zip`; each value contains only its exact byte
`bytes` and lowercase SHA-256 `sha256`. The map includes safe files copied
from `landing.site_dir` and the generated ZIP. It does not hash `graph.json`
itself. The configurable `download_filename` is only the built-in page's
browser suggestion for `corpus.zip`, and is not an artifact key; custom
`site_dir` HTML is copied verbatim and owns its own links.

The built-in `index.html` derives its favicon from `landing.logo`, or from the
effective nonempty `landing.mark` when no logo is set. Empty mark plus no logo
omits the favicon; a custom `landing.site_dir` owns its own favicon. This is
derived from existing settings and adds no artifact key or configuration field.

`llms.txt` is a small deterministic entry point. After the configured corpus
name is trimmed and line breaks are removed, it contains links relative to
itself (`index.md` and `graph.json`) so discovery works under a deployment
subpath. Generated graph, llms, and ledger serialization uses stable ordering
and final newlines; compiler-generated metadata does not include timestamps or
host filesystem roots.

The published directory is intended for a static HTTP consumer: fetch
`index.html`, `llms.txt`, `graph.json`, `corpus.zip`, a copied static asset, or
a Markdown path with `GET`. Poolboy does not serve those files or provide a
runtime SDK.

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
