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
  mappings, and exposes them as a `Bundle` the other packages consume.
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
   validated `landing.site_dir`, creates the deterministic Markdown `corpus.zip`,
   and stages then replaces the result in `output` (`dist/`), with rollback
   attempts for later I/O failures. See [Build and render](build-and-render.md).

Queries and refactors (`status`, `links`, `backlinks`, `move`, `tidy`, `check`)
operate on the index and never publish. Only `build` writes `dist/`.

## What a consumer needs

A published corpus is plain files behind HTTP: `GET /index.html`,
`GET /llms.txt`, `GET /graph.json`, `GET /corpus.zip`, copied landing assets,
and `GET /<path>.md`. No Poolboy server, SDK, database or inference service is
required to read it — the details are in [Build and render](build-and-render.md).

## Uncertainties

- `bundle` internals here are described from how `internal/compiler` and
  `internal/source` consume a `Bundle` (`Spec`, `Name`, `Dir`, `Output`,
  `Renders`, `Unknown`); `bundle/bundle.go` was not read line-by-line in this
  pass.
