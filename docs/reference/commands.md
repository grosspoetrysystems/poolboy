---
type: reference
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

## How to read this reference

Every command below is listed with the same fields, and two of them carry
guarantees rather than description.

**Mutation** is enforced by the command itself, not a convention. A command
marked read-only builds the document graph in memory and reports from it; it
has no write path to the corpus, the publication, the source baseline, or the
generated ledger, whatever its output says. A command marked mutating changes
something on disk, and its **Effects** names what. Several mutating commands
only write when an explicit flag asks them to — `check` without `--fix`, bare
`tidy`, and `checkin` without `--apply` all preview and write nothing.

**Does not** states a deliberate non-goal, not an unimplemented feature. Where a
command names one, it will not acquire that behavior by adding a flag.

**Verify** gives a command that observes the result. Running it after a mutating
command is how you confirm what happened rather than assuming it.

Exit status is uniform: `0` succeeded including an empty result, `1` is a valid
negative such as no match or check findings, and `2` is a usage, configuration,
or I/O error.

## `init`

Scaffold a documentation project in an optional directory and report existing Markdown adoption issues; existing files are preserved unless --force is used.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy init [dir] [--force] [--format text|json|csv|tsv]`
- Effects: Writes starter project files into the target directory and reports Markdown adoption issues; existing files are preserved unless --force is given.
- Deliberately does not: Does not build, publish, or sign anything, and without --force does not overwrite existing files.

### Verify

- `status`

### Related commands

- `build`
- `scan`
- `status`

### Flags and arguments

- `[dir]` — Target directory; defaults to the current directory.
- `--force` — Overwrite existing starter files.
- `--format text|json|csv|tsv` — Choose result output format.
## `build`

Render and publish the configured corpus.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/product.go`
- Usage: `poolboy build [--renderer PATH] [--format text|json|csv|tsv]`
- Effects: Renders the configured corpus and writes the published output.
- Deliberately does not: Does not sign, approve, or verify the output; building does not imply review or established trust.


### Related commands

- `preview`
- `sign`
- `scan`
- `drift`

### Flags and arguments

- `--renderer PATH` — Use a trusted Knap companion path.
- `--format text|json|csv|tsv` — Choose result output format.
## `preview`

Build and serve the configured corpus through a private loopback URL.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/preview.go`
- Usage: `poolboy preview [--port N] [--renderer PATH]`
- Effects: Builds the corpus and serves it at a private loopback URL until the process is stopped.
- Deliberately does not: Does not publish the corpus or expose it beyond loopback.


### Related commands

- `build`

### Flags and arguments

- `--port N` — Use a loopback port; 0 chooses an available port.
- `--renderer PATH` — Use a trusted Knap companion path.
## `scan`

Record a bounded source inventory baseline and summarize documentation health.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/product.go`
- Usage: `poolboy scan [--accept] [--format text|json|csv|tsv]`
- Effects: Records a bounded source inventory baseline and summarizes documentation health.
- Deliberately does not: Does not build, publish, or review documentation.

### Verify

- `drift`

### Related commands

- `drift`
- `health`
- `check`

### Flags and arguments

- `--accept` — Replace an existing baseline after review.
- `--format text|json|csv|tsv` — Choose result output format.
## `drift`

Compare current sources and summarize affected documentation.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/product.go`
- Usage: `poolboy drift [--format text|json|csv|tsv]`
- Effects: Compares current sources against the recorded baseline and summarizes affected documentation; read-only.
- Deliberately does not: Does not record or update the baseline.


### Related commands

- `scan`
- `affected`
- `health`

### Flags and arguments

- `--format text|json|csv|tsv` — Choose result output format.
## `health`

Inspect documentation provenance and graph evidence.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/product.go`
- Usage: `poolboy health [--format text|json|csv|tsv]`
- Effects: Reports documentation provenance and graph evidence; read-only.
- Deliberately does not: Does not change provenance, graph evidence, or corpus files.


### Related commands

- `scan`
- `drift`
- `status`

### Flags and arguments

- `--format text|json|csv|tsv` — Choose result output format.
## `affected`

Find documents citing a source resource directly.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/product.go`
- Usage: `poolboy affected SOURCE [--format text|json|csv|tsv]`
- Effects: Lists documents that cite the given source resource directly; read-only.
- Deliberately does not: Does not modify entries or the source inventory.


### Related commands

- `drift`
- `backlinks`
- `links`

### Flags and arguments

- `SOURCE` — One project-relative source path.
- `--format text|json|csv|tsv` — Choose result output format.
## `checkout`

Create an asynchronous ordinary-Markdown checkout outside the project root and record its exact private comparison base.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/product.go`
- Usage: `poolboy checkout DIR [--format text|json|csv|tsv]`
- Effects: Creates an ordinary-Markdown checkout at DIR outside the project root and records its private comparison base.
- Deliberately does not: Does not modify the canonical corpus.

### Verify

- `checkin DIR`

### Related commands

- `checkin`

### Flags and arguments

- `DIR` — New checkout directory outside the project root; its parent must exist.
- `--format text|json|csv|tsv` — Choose result output format; agents should use JSON.
## `checkin`

Compare checkout, base, and canonical corpus revisions; preview by default and apply only a conflict-free plan.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/product.go`
- Usage: `poolboy checkin DIR [--apply] [--format text|json|csv|tsv]`
- Effects: Compares checkout, base, and canonical revisions; previews the plan by default and applies a conflict-free plan only with --apply.
- Deliberately does not: Without --apply, does not modify the canonical corpus, and never applies a plan with conflicts.

### Verify

- `status`

### Related commands

- `checkout`
- `status`

### Flags and arguments

- `DIR` — Checkout directory previously created for this project.
- `--apply` — Apply a conflict-free file-level plan to the canonical corpus.
- `--format text|json|csv|tsv` — Choose result output format; JSON exposes variance, conflicts, can_apply, and applied.
## `status`

Report corpus counts for entries, links, tags, checkboxes, broken links, and orphans.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy status [--format text|json|csv|tsv]`
- Effects: Reports corpus counts for entries, links, tags, checkboxes, broken links, and orphans; read-only.
- Deliberately does not: Does not modify the corpus or its index.


### Related commands

- `list`
- `unresolved`
- `orphans`

### Flags and arguments

- `--format text|json|csv|tsv` — Choose result output format.
## `list`

List indexed entries.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `ls`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy list [--prefix PATH] [--where KEY=VALUE|KEY!=VALUE] [--sort path|timestamp] [--reverse] [--format text|json|csv|tsv]`
- Effects: Lists indexed entries, filtered and ordered by the given options; read-only.
- Deliberately does not: Does not modify indexed entries.


### Related commands

- `search`
- `read`
- `tags`

### Flags and arguments

- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter; filters are ANDed.
- `--sort path|timestamp` — Sort by path or newest-first timestamp.
- `--reverse` — Reverse the selected ordering.
- `--format text|json|csv|tsv` — Choose result output format.
## `read`

Print one entry body with frontmatter stripped.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy read FILE [--format text|json|csv|tsv]`
- Effects: Prints one entry body with frontmatter stripped; read-only.
- Deliberately does not: Does not modify the entry.


### Related commands

- `outline`
- `list`
- `links`

### Flags and arguments

- `FILE` — One corpus entry.
- `--format text|json|csv|tsv` — Choose result output format.
## `outline`

Print one entry's heading hierarchy.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy outline FILE [--format text|json|csv|tsv]`
- Effects: Prints one entry's heading hierarchy; read-only.
- Deliberately does not: Does not modify the entry.


### Related commands

- `read`
- `table`

### Flags and arguments

- `FILE` — One corpus entry.
- `--format text|json|csv|tsv` — Choose result output format.
## `table`

Extract one Markdown table from an entry.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy table FILE [--n N] [--format text|json|csv|tsv]`
- Effects: Extracts one Markdown table from an entry; read-only.
- Deliberately does not: Does not modify the entry.


### Related commands

- `read`
- `outline`

### Flags and arguments

- `FILE` — One corpus entry.
- `--n N` — Select a table by one-based index when there are several.
- `--format text|json|csv|tsv` — Choose result output format.
## `search`

Search entry text; every query word must match by default.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy search QUERY [--any] [--exact] [--lines] [--prefix PATH] [--where KEY=VALUE|KEY!=VALUE] [--format text|json|csv|tsv]`
- Effects: Searches entry text, requiring every query word to match by default; read-only.
- Deliberately does not: Does not modify entries or search state.


### Related commands

- `list`
- `read`

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

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy checkboxes [FILE] [--all] [--done] [--prefix PATH] [--where KEY=VALUE|KEY!=VALUE] [--format text|json|csv|tsv]`
- Effects: Lists GFM checklist items across selected entries; read-only.
- Deliberately does not: Does not modify checklist items.


### Related commands

- `list`
- `status`

### Flags and arguments

- `[FILE]` — Optionally limit checklist results to one entry.
- `--all` — Include completed tasks.
- `--done` — Show only completed tasks.
- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter.
- `--format text|json|csv|tsv` — Choose result output format.
## `tags`

List tags in use across selected entries.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy tags [--counts] [--sort name|count] [--prefix PATH] [--where KEY=VALUE|KEY!=VALUE] [--format text|json|csv|tsv]`
- Effects: Lists tags in use across selected entries; read-only.
- Deliberately does not: Does not modify entries or tags.


### Related commands

- `properties`
- `property`
- `list`

### Flags and arguments

- `--counts` — Show entry counts per tag.
- `--sort name|count` — Sort by tag name or count.
- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter.
- `--format text|json|csv|tsv` — Choose result output format.
## `properties`

List frontmatter property keys in use.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy properties [--counts] [--sort name|count] [--prefix PATH] [--where KEY=VALUE|KEY!=VALUE] [--format text|json|csv|tsv]`
- Effects: Lists frontmatter property keys in use across selected entries; read-only.
- Deliberately does not: Does not modify entries or property keys.


### Related commands

- `property`
- `tags`

### Flags and arguments

- `--counts` — Show entry counts per property.
- `--sort name|count` — Sort by property name or count.
- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter.
- `--format text|json|csv|tsv` — Choose result output format.
## `property`

List values for one frontmatter property.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy property NAME [--counts] [--sort name|count] [--prefix PATH] [--where KEY=VALUE|KEY!=VALUE] [--format text|json|csv|tsv]`
- Effects: Lists the values in use for one frontmatter property; read-only.
- Deliberately does not: Does not modify entries or property values.


### Related commands

- `properties`
- `tags`

### Flags and arguments

- `NAME` — One frontmatter property name.
- `--counts` — Show entry counts per value.
- `--sort name|count` — Sort by value name or count.
- `--prefix PATH` — Limit entries by path prefix.
- `--where KEY=VALUE|KEY!=VALUE` — Repeatable frontmatter filter.
- `--format text|json|csv|tsv` — Choose result output format.
## `unresolved`

List broken internal links.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy unresolved [--format text|json|csv|tsv]`
- Effects: Lists broken internal links across the corpus; read-only.
- Deliberately does not: Does not repair broken links.


### Related commands

- `orphans`
- `links`
- `check`

### Flags and arguments

- `--format text|json|csv|tsv` — Choose result output format.
## `orphans`

List entries with no incoming links.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy orphans [--format text|json|csv|tsv]`
- Effects: Lists entries with no incoming links; read-only.
- Deliberately does not: Does not modify entries or links.


### Related commands

- `unresolved`
- `backlinks`

### Flags and arguments

- `--format text|json|csv|tsv` — Choose result output format.
## `links`

List unique outgoing link targets from one entry.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy links FILE [--format text|json|csv|tsv]`
- Effects: Lists unique outgoing link targets from one entry; read-only.
- Deliberately does not: Does not modify links.


### Related commands

- `backlinks`
- `unresolved`
- `read`

### Flags and arguments

- `FILE` — One corpus entry.
- `--format text|json|csv|tsv` — Choose result output format.
## `backlinks`

List every incoming link reference to one entry.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy backlinks FILE [--format text|json|csv|tsv]`
- Effects: Lists every incoming link reference to one entry; read-only.
- Deliberately does not: Does not modify links.


### Related commands

- `links`
- `orphans`
- `affected`

### Flags and arguments

- `FILE` — One corpus entry.
- `--format text|json|csv|tsv` — Choose result output format.
## `move`

Relocate an entry and rewrite Markdown links.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `mv`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy move SRC DEST [--dry-run] [--include-frontmatter] [--format text|json|csv|tsv]`
- Effects: Relocates an entry from SRC to DEST and rewrites Markdown links pointing at it.
- Deliberately does not: With --dry-run, previews the plan and does not write any files.

### Verify

- `read DEST`

### Related commands

- `tidy`
- `links`
- `backlinks`

### Flags and arguments

- `SRC DEST` — Source and destination entry paths.
- `--dry-run` — Preview the rewrite plan without writing.
- `--include-frontmatter` — Also rewrite Markdown-valued frontmatter references.
- `--format text|json|csv|tsv` — Choose result output format.
## `tidy`

Preview or apply canonical link, filename, and wikilink cleanup.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy tidy [--links] [--slug] [--wikilinks] [--all] [--format text|json|csv|tsv]`
- Effects: Applies the selected link, filename, and wikilink cleanup category, or every category with --all.
- Deliberately does not: Bare tidy is preview-only and does not modify any files.

### Verify

- `check`

### Related commands

- `check`
- `move`
- `unresolved`

### Flags and arguments

- `--links` — Normalize links to relative form.
- `--slug` — Slugify spaced filenames and rewrite inbound links.
- `--wikilinks` — Convert wikilinks to Markdown links.
- `--all` — Apply every category; bare tidy is preview-only.
- `--format text|json|csv|tsv` — Choose result output format.
## `check`

Report conformance and corpus health issues.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/commands.go`
- Usage: `poolboy check [--fix] [--format text|json|csv|tsv]`
- Effects: Reports conformance and corpus health issues; with --fix applies safe repairs such as syncing okf_version.
- Deliberately does not: Does not publish, review, or approve; without --fix it does not modify any files.

### Verify

- `status`

### Related commands

- `tidy`
- `unresolved`
- `scan`

### Flags and arguments

- `--fix` — Apply safe repairs, such as syncing okf_version.
- `--format text|json|csv|tsv` — Choose result output format.
## `keygen`

Generate a private Ed25519 key for explicit TOFU publishing.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/signing.go`
- Usage: `poolboy keygen [--out PATH] [--force]`
- Effects: Writes a private Ed25519 key file, defaulting to poolboy.key.
- Deliberately does not: Does not sign or publish anything, and without --force does not overwrite an existing key file.


### Related commands

- `sign`
- `verify`

### Flags and arguments

- `--out PATH` — Private key destination; defaults to poolboy.key.
- `--force` — Overwrite an existing key file.
## `sign`

Sign the built graph for explicit Ed25519 TOFU verification.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/signing.go`
- Usage: `poolboy sign [--key PATH] [--root PATH]`
- Effects: Signs the built graph for explicit Ed25519 TOFU verification.
- Deliberately does not: Asserts authorship only; does not approve the release or establish trust for consumers.

### Verify

- `verify DIR-OR-URL --tofu`

### Related commands

- `keygen`
- `verify`
- `build`

### Flags and arguments

- `--key PATH` — Private key file; alternatively set POOLBOY_SIGNING_KEY.
- `--root PATH` — Project root or directory containing poolboy.toml.
## `verify`

Verify an approved exact corpus release or report a pending update.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `none`
- Source: `cmd/poolboy/signing.go`
- Usage: `poolboy verify DIR-OR-URL [--lock PATH] [--identity URI] [--tofu] [--channel NAME]`
- Effects: Verifies an approved exact corpus release or reports a pending update; read-only.
- Deliberately does not: Does not approve or record trust; verification alone does not grant approval.


### Related commands

- `approve`
- `sign`

### Flags and arguments

- `DIR-OR-URL` — Local publication directory or HTTP(S) publication root.
- `--lock PATH` — Use an authoritative project trust lock.
- `--identity URI` — Require an exact Sigstore workflow identity on first contact.
- `--tofu` — Explicitly use weaker Ed25519 trust on first use.
- `--channel NAME` — Trust channel; defaults to stable.
## `approve`

Fully verify and interactively approve one exact corpus digest.

- Mutation: `true` — writes to disk; see Effects
- Aliases: `none`
- Source: `cmd/poolboy/signing.go`
- Usage: `poolboy approve DIR-OR-URL [--lock PATH] [--identity URI] [--tofu] [--channel NAME] [--minimum-release-age DURATION] [--override-age] [--migrate-identity]`
- Effects: Fully verifies and interactively records approval of one exact corpus digest in the selected trust store.
- Deliberately does not: Does not modify the corpus; approves only the exact digest presented.

### Verify

- `verify DIR-OR-URL`

### Related commands

- `verify`
- `sign`

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

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `--version, -v`
- Source: `cmd/poolboy/main.go`
- Usage: `poolboy version`
- Effects: Prints the Poolboy version as a bare string; read-only.
- Deliberately does not: Does not inspect or modify the corpus.


### Related commands

- `help`

## `help`

Print the top-level usage and command list.

- Mutation: `false` — read-only; cannot write to the corpus, publication, source baseline, or generated ledger
- Aliases: `-h, --help`
- Source: `cmd/poolboy/help.go`
- Usage: `poolboy help`
- Effects: Prints the top-level usage and command list; read-only.
- Deliberately does not: Does not run a command or modify the corpus.


### Related commands

- `version`

