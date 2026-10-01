---
type: concept
title: Architecture
description: The Poolboy components and how a documentation corpus flows from Markdown to a published dist/.
status: draft
sources:
  - resource: ../README.md
  - resource: ../cmd/poolboy/main.go
  - resource: ../bundle/bundle.go
  - resource: ../parse/parse.go
  - resource: ../index/index.go
  - resource: ../internal/compiler/build.go
  - resource: ../internal/source/source.go
  - resource: ../internal/checkout/checkout.go
---
# Architecture

Poolboy is three cooperating surfaces: a Go CLI, a constrained Knap companion,
and a discovery Skill. The CLI maintains and compiles a documentation corpus;
the companion renders repeated reference material from data; the Skill supplies
the human/agent judgment that neither program encodes. This document maps the
Go/TypeScript components a maintainer actually edits or reasons about.

## Surfaces

- **Go CLI** (`cmd/poolboy`) — a single static binary. `main.go` parses a
  leading `--root` and dispatches one subcommand to a handler. See
  [Maintenance commands](maintenance-commands.md).
- **Knap companion** (`companion/`) — a pinned TypeScript renderer that turns a
  template plus JSON data into a Markdown document. It runs only when a build
  has configured template renders. See [Renderer and companion](renderer.md).
- **Discovery Skill** (`skills/poolboy-discovery/`) — instructions for a coding
  agent to reconcile the corpus against source evidence. It is guidance, not a
  runtime component.

## Components

- **`bundle`** — discovers `poolboy.toml`, resolves the corpus directory
  (`corpus`), the publication directory (`output`), and the ordered `[[render]]`
  mappings, reads the optional sibling `landing.toml` for the landing page
  configuration (built-in defaults when absent), and exposes them as a `Bundle`
  the other packages consume.
- **`parse`** — parses OKF YAML frontmatter and Markdown structure (links,
  headings, checkboxes, tables), and validates OKF 0.2 conformance
  (`ValidateOKF`). YAML decoding is bounded (see
  [Security boundaries](security-boundaries.md)).
- **`index`** — builds the in-memory document graph: entries, resolved links,
  backlinks, orphans and unresolved links. It resolves relative Markdown links
  to canonical root-absolute `/path.md` identities and performs `move` and
  `tidy` link rewrites.
- **`internal/compiler`** — validates the combined corpus and publishes `dist/`:
  `validate.go` (roots, mappings, OKF and landing settings), `graph.go`
  (`graph.json`, `llms.txt` and Markdown-only `files`), `publication.go`
  (owned static publication files and artifact hashes), and `build.go`
  (staged publication with rollback attempts).
- **`internal/renderer`** — the Go→Node adapter that invokes the companion under
  a hard time, request and output budget.
- **`internal/source`** — the source inventory: `scan.go` builds the bounded,
  hashed baseline; `source.go` reads/writes it and computes `drift`;
  `affected.go` answers direct-provenance questions.
- **`internal/checkout`** — creates private asynchronous Markdown checkouts,
  records their exact document/source/generated base, computes file-level
  three-way check-in plans, and applies only conflict-free corpus changes.
- **`internal/output`** — renders command results as text, JSON, CSV or TSV.
- **`internal/scaffold`** — the `init` starter project.
- **`internal/wikilink`** — a quarantined compatibility layer that lets `tidy`
  convert Obsidian `[[wikilinks]]` to canonical Markdown; the graph itself is
  Markdown-link only.

## Data flow

1. `bundle.Discover` reads `poolboy.toml` and locates the corpus (`docs/` here)
   and the render mappings.
2. The corpus is ordinary handwritten Markdown plus any generated Markdown that
   a prior build materialized in place.
3. `index.Build` parses every document into the graph that read-only commands
   query directly, without publishing.
4. `build` validates the combined corpus, renders configured templates, derives
   `graph.json` and `llms.txt`, generates the built-in landing or copies the
   validated custom `site_dir`, creates the deterministic Markdown `corpus.zip`,
   and stages then replaces the result in `output` (`dist/`), with rollback
   attempts for later I/O failures. See [Build and render](build-and-render.md).

Queries and refactors (`status`, `links`, `backlinks`, `move`, `tidy`, `check`)
operate on the index and never publish. `checkout` copies current Markdown and
records a private comparison base; `checkin` previews or explicitly applies a
file-level plan without accepting source evidence. `build` writes `dist/`;
`preview` runs the same build and exposes that output through a temporary
loopback-only HTTP server without signing or uploading it.

## Guarantees

These hold for every build and are the properties a maintainer can rely on before
mutating anything.

- **Reads never write.** Query and refactor commands operate on the in-memory index and never write to the publication.
- **Failure before mutation changes nothing.** Validation, rendering and staging happen in a temporary sibling, so a failure before mutation leaves the live publication, the corpus and the generated ledger untouched.
- **Replacement is staged, not transactional.** Once replacement begins, failures trigger rollback attempts rather than a transaction: the sequence is not atomic and is not safe against crash or power loss.
- **Authored files are never clobbered.** A current file with no prior ledger entry is treated as handwritten and is never overwritten by a render.
- **Check-in does not close source review.** Applying a check-in plan never accepts the source baseline; source review remains outstanding afterwards.
- **Preview is local and unsigned.** `preview` binds a loopback-only server and never signs or uploads what it serves.
- **Publication is keyless.** `build` is keyless and produces an unsigned publication; signing is a separate explicit step.
- **The graph is Markdown-link only.** The document graph is Markdown-link only; wikilinks are a quarantined compatibility layer and are never graph edges.

## What a consumer needs

A published corpus is plain files behind HTTP: `GET /index.html`,
`GET /llms.txt`, `GET /graph.json`, `GET /corpus.zip`, copied landing assets,
and `GET /<path>.md`. No Poolboy server, SDK, database or inference service is
required to read it — the details are in [Build and render](build-and-render.md).

## What each choice costs you

The architecture's shape has consequences a maintainer inherits whether or not
they wanted them.

- **Markdown-link-only graph** — Obsidian wikilinks resolve in your editor but
  are not graph edges, so `links`, `backlinks` and `orphans` will not see them.
  A vault-authored corpus needs `tidy --wikilinks` before its graph is accurate.
- **Canonical root-absolute identity** — renaming a file creates a new locator
  and removes the old one. Anything outside the corpus that pointed at the old
  path breaks, and Poolboy cannot fix what it cannot see. Use `move` so at least
  the in-corpus links and citations travel with the file.
- **Generated Markdown lives in the corpus** — generated files sit beside
  authored ones, so `.poolboy/generated.json` must be committed or the next
  build treats them as handwritten and refuses to replace them. Moving a
  generated file also means updating the `[[render]]` mapping that produces it.
- **One process per template** — rendering cost scales with template count, not
  document size, and the companion must be installed beside the binary. A
  missing `poolboy-knap.mjs` fails the build rather than skipping renders.
- **Static publication** — there is no server to invalidate, which also means no
  revocation and no change feed. Consumers poll `graph.json` or they act on
  stale bytes.
- **The Skill is guidance, not a component** — nothing executes it and nothing
  checks that it was followed. Judgment it encodes is lost if the human or agent
  skips it.
- **`--root` does not change directory** — it is consumed before the command, so
  relative paths in your shell still resolve against your actual working
  directory, not the project root.

## Uncertainties

- `bundle` internals here are described from how `internal/compiler` and
  `internal/source` consume a `Bundle` (`Spec`, `Name`, `Dir`, `Output`,
  `Renders`, `Unknown`); `bundle/bundle.go` was not read line-by-line in this
  pass.
