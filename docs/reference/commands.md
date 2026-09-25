---
type: Reference
title: "Poolboy command reference"
description: "Generated reference for Poolboy's CLI commands and flags."
status: draft
sources:
  - resource: ../../cmd/poolboy/main.go
  - resource: ../../cmd/poolboy/commands.go
  - resource: ../../cmd/poolboy/helpers.go
  - resource: ../../cmd/poolboy/product.go
---
# Poolboy command reference

Use a leading --root DIR or --root=DIR to select a project. Result commands accept --format text|json|csv|tsv; exit status is 0 for success, 1 for no match or check failures, and 2 for errors.

## `init`

Scaffold a documentation project in an optional directory.

- Mutation: `true`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `[dir]` — Target directory; defaults to the current directory.
- `--force` — Write into a non-empty directory.
- `--format text|json|csv|tsv` — Choose result output format.
## `build`

Render and publish the configured corpus.

- Mutation: `true`
- Aliases: `none`
- Source: `cmd/poolboy/product.go`

### Flags and arguments

- `--renderer PATH` — Use a trusted Knap companion path.
- `--format text|json|csv|tsv` — Choose result output format.
## `preview`

Build and serve the configured corpus through a private loopback URL.

- Mutation: `true`
- Aliases: `none`
- Source: `cmd/poolboy/preview.go`

### Flags and arguments

- `--port N` — Use a loopback port; 0 chooses an available port.
- `--renderer PATH` — Use a trusted Knap companion path.
## `scan`

Record a bounded source inventory baseline.

- Mutation: `true`
- Aliases: `none`
- Source: `cmd/poolboy/product.go`

### Flags and arguments

- `--accept` — Replace an existing baseline after review.
- `--format text|json|csv|tsv` — Choose result output format.
## `drift`

Compare the source inventory with current files.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/product.go`

### Flags and arguments

- `--format text|json|csv|tsv` — Choose result output format.
## `affected`

Find documents citing a source resource directly.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/product.go`

### Flags and arguments

- `SOURCE` — One project-relative source path.
- `--format text|json|csv|tsv` — Choose result output format.
## `status`

Report corpus counts for entries, links, tags, checkboxes, broken links, and orphans.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `--format text|json|csv|tsv` — Choose result output format.
## `list`

List indexed entries.

- Mutation: `false`
- Aliases: `ls`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter; filters are ANDed.
- `--sort path|timestamp` — Sort by path or newest-first timestamp.
- `--reverse` — Reverse the selected ordering.
- `--format text|json|csv|tsv` — Choose result output format.
## `read`

Print one entry body with frontmatter stripped.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `FILE` — One corpus entry.
- `--format text|json|csv|tsv` — Choose result output format.
## `outline`

Print one entry's heading hierarchy.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `FILE` — One corpus entry.
- `--format text|json|csv|tsv` — Choose result output format.
## `table`

Extract one Markdown table from an entry.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `FILE` — One corpus entry.
- `--n N` — Select a table by one-based index when there are several.
- `--format text|json|csv|tsv` — Choose result output format.
## `search`

Search entry text; every query word must match by default.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `QUERY` — One non-empty query; quote multi-word phrases.
- `--any` — Match any query word.
- `--exact` — Match the verbatim phrase; mutually exclusive with --any.
- `--lines` — Show matching lines instead of entries.
- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter.
- `--format text|json|csv|tsv` — Choose result output format.
## `checkboxes`

List GFM checklist items; an optional file scopes the result.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `[FILE]` — Optionally limit checklist results to one entry.
- `--all` — Include completed tasks.
- `--done` — Show only completed tasks.
- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter.
- `--format text|json|csv|tsv` — Choose result output format.
## `tags`

List tags in use across selected entries.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `--counts` — Show entry counts per tag.
- `--sort name|count` — Sort by tag name or count.
- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter.
- `--format text|json|csv|tsv` — Choose result output format.
## `properties`

List frontmatter property keys in use.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `--counts` — Show entry counts per property.
- `--sort name|count` — Sort by property name or count.
- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter.
- `--format text|json|csv|tsv` — Choose result output format.
## `property`

List values for one frontmatter property.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `NAME` — One frontmatter property name.
- `--counts` — Show entry counts per value.
- `--sort name|count` — Sort by value name or count.
- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter.
- `--format text|json|csv|tsv` — Choose result output format.
## `unresolved`

List broken internal links.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `--format text|json|csv|tsv` — Choose result output format.
## `orphans`

List entries with no incoming links.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `--format text|json|csv|tsv` — Choose result output format.
## `links`

List unique outgoing link targets from one entry.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `FILE` — One corpus entry.
- `--format text|json|csv|tsv` — Choose result output format.
## `backlinks`

List every incoming link reference to one entry.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `FILE` — One corpus entry.
- `--format text|json|csv|tsv` — Choose result output format.
## `move`

Relocate an entry and rewrite Markdown links.

- Mutation: `true`
- Aliases: `mv`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `SRC DEST` — Source and destination entry paths.
- `--dry-run` — Preview the rewrite plan without writing.
- `--include-frontmatter` — Also rewrite Markdown-valued frontmatter references.
- `--format text|json|csv|tsv` — Choose result output format.
## `tidy`

Preview or apply canonical link, filename, and wikilink cleanup.

- Mutation: `true`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `--links` — Normalize links to relative form.
- `--slug` — Slugify spaced filenames and rewrite inbound links.
- `--wikilinks` — Convert wikilinks to Markdown links.
- `--all` — Apply every category; bare tidy is preview-only.
- `--format text|json|csv|tsv` — Choose result output format.
## `check`

Report conformance and corpus health issues.

- Mutation: `true`
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`

### Flags and arguments

- `--fix` — Apply safe repairs, such as syncing okf_version.
- `--format text|json|csv|tsv` — Choose result output format.
## `keygen`

Generate a private Ed25519 key for explicit TOFU publishing.

- Mutation: `true`
- Aliases: `none`
- Source: `cmd/poolboy/signing.go`

### Flags and arguments

- `--out PATH` — Private key destination; defaults to poolboy.key.
- `--force` — Overwrite an existing key file.
## `sign`

Sign the built graph for explicit Ed25519 TOFU verification.

- Mutation: `true`
- Aliases: `none`
- Source: `cmd/poolboy/signing.go`

### Flags and arguments

- `--key PATH` — Private key file; alternatively set POOLBOY_SIGNING_KEY.
## `verify`

Verify an approved exact corpus release or report a pending update.

- Mutation: `false`
- Aliases: `none`
- Source: `cmd/poolboy/signing.go`

### Flags and arguments

- `DIR-OR-URL` — Local publication directory or HTTP(S) publication root.
- `--lock PATH` — Use an authoritative project trust lock.
- `--identity URI` — Require an exact Sigstore workflow identity on first contact.
- `--tofu` — Explicitly use weaker Ed25519 trust on first use.
- `--channel NAME` — Trust channel; defaults to stable.
## `approve`

Fully verify and interactively approve one exact corpus digest.

- Mutation: `true`
- Aliases: `none`
- Source: `cmd/poolboy/signing.go`

### Flags and arguments

- `DIR-OR-URL` — Local publication directory or HTTP(S) publication root.
- `--lock PATH` — Write the authoritative project trust lock.
- `--identity URI` — Require an exact Sigstore workflow identity.
- `--tofu` — Explicitly use weaker Ed25519 trust on first use.
- `--channel NAME` — Trust channel; defaults to stable.
- `--minimum-release-age DURATION` — Minimum trusted release age; stable Sigstore defaults to 72h.
- `--override-age` — Record a human emergency override for this exact digest.
- `--migrate-identity` — Explicitly replace an existing publisher identity or TOFU key.
## `version`

Print the Poolboy version as a bare string.

- Mutation: `false`
- Aliases: `--version, -v`
- Source: `cmd/poolboy/main.go`

### Flags and arguments

## `help`

Print the top-level usage and command list.

- Mutation: `false`
- Aliases: `-h, --help`
- Source: `cmd/poolboy/main.go`

### Flags and arguments

