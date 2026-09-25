---
type: concept
title: Maintenance commands
description: Poolboy's CLI surface, query model, and corpus refactoring operations.
status: draft
sources:
  - resource: ../cmd/poolboy/main.go
  - resource: ../cmd/poolboy/commands.go
  - resource: ../cmd/poolboy/helpers.go
  - resource: ../cmd/poolboy/product.go
  - resource: ../parse/parse.go
  - resource: ../index/index.go
---
# Maintenance commands

The CLI is deliberately split between corpus queries, source evidence, and
publishing. Use a leading `--root DIR` or `--root=DIR` to select the project;
it is consumed before the command and does not change the process working
directory. Result commands accept `--format text|json|csv|tsv`, with text as
the default. Exit status is `0` for success, `1` for no match or check
failures, and `2` for command/configuration errors.

The generated [command reference](reference/commands.md) contains the full
flag table. This page explains the behavioral seams that matter when changing
a corpus.

## Reading and querying

`list`/`ls` enumerates entries and can filter by path prefix, repeated
frontmatter predicates (`key=value` or `key!=value`), sort by path or
newest-first timestamp, and reverse the result. JSON includes full frontmatter.
`read`, `outline`, and `table` inspect one entry; `search` finds text by all
words by default, or by any word/whole phrase with `--any`/`--exact`.
`checkboxes`, `tags`, `properties`, and `property` expose checklist and
frontmatter indexes. `unresolved`, `orphans`, `links`, and `backlinks` expose
the link graph.

Markdown links are resolved to canonical root-absolute graph targets, while
the authored spelling remains available for rewrites. Relative links resolve
from the linking document; URL links do not become graph edges. `links` reports
outbound occurrences, `backlinks` reports incoming occurrences, and
`unresolved` reports internal targets that do not exist. Orphan reporting
excludes reserved navigation/log entries and configured exemptions.

## Refactoring

`move`/`mv SRC DEST` relocates an entry and rewrites inbound Markdown links to
the new relative target. It also respells the moved file's outbound Markdown
links relative to its new directory. Anchors and Markdown link titles are
preserved. In-bundle structured OKF `sources[].resource` references are always
migrated: inbound citations to the moved file and explicit relative citations
owned by the moved file keep resolving to the same files. URL resources and
metadata remain untouched. `--dry-run` performs validation and reports the
rewrite plan without writing; `--include-frontmatter` independently opts into
heuristic rewrites of other Markdown-valued frontmatter fields. It is not
required for structured `sources[].resource` identity. Wikilinks are
intentionally not rewritten by `move`; use `tidy --wikilinks` for the explicit
conversion path.

`tidy` can preview all categories with no category flag. `--links` normalizes
link spellings, `--slug` renames spaced filenames and rewrites inbound links,
`--wikilinks` converts recognized wikilinks to Markdown, and `--all` applies
every category. A selected category writes files; the bare command is the
preview rather than a hidden default mutation.

`check` reports conformance and corpus health. It is read-only by default;
`check --fix` applies the command's safe repairs, such as synchronizing the
root `okf_version`, and then reindexes. It is not equivalent to `build`: build
also renders generated documents and publishes the static output.

## Evidence and publishing

`scan` creates the source evidence baseline; replacing an existing baseline
requires `scan --accept` after review. `drift` compares the baseline without
writing, and `affected SOURCE` finds documents that directly cite that source
in `sources[].resource`. These are evidence/review candidates, not semantic
staleness conclusions. `build` is the separate publication transaction.
`preview` runs that same build, then serves its static output from `127.0.0.1`
under a random URL prefix with no signing or upload. See
[Build and render](build-and-render.md).

## Reserved and compatibility behavior

The root `index.md` is the corpus entry point and may carry only its reserved
`okf_version` metadata. `log.md` is a reserved narrative file. Standard
Markdown links are the official graph format; wikilinks remain a compatibility
input so the graph can report them and the explicit tidy operation can migrate
them.
