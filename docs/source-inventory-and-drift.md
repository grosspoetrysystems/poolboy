---
type: concept
title: Source inventory and drift
description: The bounded source baseline, drift comparison, and documentation health workflow.
status: draft
sources:
  - resource: ../README.md
  - resource: ../skills/poolboy-discovery/SKILL.md
  - resource: ../internal/source/scan.go
  - resource: ../internal/source/source.go
  - resource: ../internal/source/ignore.go
  - resource: ../internal/source/affected.go
  - resource: ../internal/health/health.go
  - resource: ../internal/compiler/publication.go
---
# Source inventory and drift

Poolboy's source inventory is evidence, not a review verdict. It records a
bounded view of project files so later work can identify changed inputs without
silently replacing the reviewed baseline.

## Baseline shape

The lock lives at `.poolboy/sources.lock.json` and uses schema version `0`.
Its `files` map is keyed by normalized project-relative slash paths. Each entry
records only:

- a coarse `type` classification;
- admitted byte length; and
- the SHA-256 digest of the admitted bytes.

The lock has no contents, timestamps, or host filesystem paths. An optional Git
revision identifies the best-effort repository revision; it is not a source
hash substitute.

The current repository already contains a baseline. This documentation does
not accept, rewrite, or reinterpret it. A fresh drift comparison is required
before claiming that the inventory describes the present working tree.
The corpus is intentionally outside this source baseline, so absent `docs/`
entries do not establish a fresh corpus or a review history; they establish
only that the scanner's source boundary excludes the corpus.

## Scan boundary

`scan` walks the project root, not only the documentation corpus. The corpus,
configured output, render templates/data, and render outputs are excluded, as
are inherited `.gitignore` and `.poolboyignore` matches. Dependency/private
directories such as `.git`, `.poolboy`, `node_modules`, `vendor`,
`.cache`, `.next`, virtualenvs, and Python caches are excluded at any depth.
Build/output names `target`, `build`, `dist`, `out`, `bin`, `obj`, and
`coverage` are excluded only at the project root, so source directories such
as `src/bin`, `src/internal`, and `src/build` remain inspectable.

Traversal and inventory caps are hard errors: reaching 1,000,000 traversed
entries or 100,000 admitted files aborts the whole scan rather than producing
a successful truncated inventory. Intentionally excluded files and directories
are outside this evidence boundary; exclusion is not evidence that their
contents were reviewed.

Symlinks and non-regular files are skipped rather than followed. Files larger
than 5 MiB are omitted. Binary detection probes up to the first 8 KiB for a NUL
byte. Filtering recognizes exact credential basenames, dotenv variants,
private-key container extensions, and complete private-key blocks. Filename
matches are identified without opening the file. Detecting a content match
requires one bounded in-memory read; matched content is never logged, retained,
or hashed. Generic words in paths and values, public identifiers, and public
certificate/signature files remain because they are common legitimate evidence.

Configured paths are canonicalized under the project root. Absolute paths are
accepted only when they remain inside that root (with the render-output
exclusion additionally requiring a relative output path). The state directory
and lock are also required to remain inside a real, non-symlink project root.

## Scan, drift, and accept

The first `scan` writes a missing lock and prints a compact documentation smell
test. Once a regular baseline exists, a scan without `--accept` preserves the
existing contract: it refuses to replace the lock and prints no smell test.
Use `drift` or `health` for repeat inspection before acceptance. The initial
scan marks source-drift evidence unavailable because no prior baseline exists.

When `scan` detects a likely-sensitive file, it appends the exact
project-relative path and a coarse reason to `.poolboyignore`, then rescans
before writing the baseline. That file is both an audit log and the rule that
prevents future content reads. Existing maintainer rules are preserved and
duplicate entries are not added. Review false positives before removing them.

`scan --accept` is the explicit baseline-advance step. It computes health
against the previous baseline, completes the fresh scan and atomic lock write,
then prints that pre-accept evidence. Accepting the new lock therefore does not
erase the findings from the review that justified it.

`drift` is read-only. It reports paths as `added`, `removed`, or `modified`
when the fresh bounded inventory differs from the baseline, then summarizes
the same documentation health evidence. It compares SHA-256 values only;
matching bytes produce no change. Results are sorted by path and status.

## Documentation health

`health` is the read-only, document-level view behind the compact `scan` and
`drift` summaries. It reports non-root documents with no declared sources,
declared local sources missing from the worktree or accepted baseline, declared
sources whose current SHA-256 differs from that baseline, and corpus orphans
from the same `ignore_orphans`-aware graph query used elsewhere. The root
`/index.md` is navigation metadata and is exempt from missing-source findings.

Findings are advisory. They identify observable review evidence; they do not
declare a document stale, exclude it, or rewrite it. A missing corpus or
baseline appears as `unavailable`, not as a clean result. The command still
returns the evidence it can compute and supports text, JSON, CSV, and TSV. If
the advisory inspection itself fails after `scan` or `drift` succeeds, JSON
includes `health_error` alongside `health: null`; the source operation remains
successful.

## Review and reconciliation workflow

A refresh or interrupted review never advances the project-wide baseline.
Reconcile current source drift with every outstanding source change, the
working Markdown and its provenance, template/data inputs, and the last
compiled graph's per-file byte hashes. The graph describes the last compiled
artifact, not necessarily the current corpus or deployed version.

For asynchronous editing, `checkout DIR` records the current document and
source observations before an editor or agent changes the copied Markdown.
`checkin DIR` reports draft, concurrent workspace, source, and generator
variance; `checkin DIR --apply` transports only a conflict-free file-level
result into the working corpus. Check-in is not the reconciliation defined
here: it records no evidence judgment and never advances the source baseline.

Only after every outstanding change has been reviewed and both `check` and
`build` succeed may the workflow run `scan --accept`. Rerun `drift` before
acceptance to catch source changes that occurred during review. There is no
per-document review database or `resume` command: this is a workflow contract,
not semantic enforcement by the CLI. Poolboy keeps one project-wide baseline.

`affected SOURCE` is a separate direct-provenance query. It scans corpus
Markdown frontmatter for `sources[].resource`, resolves explicit `./` and
`../` resources relative to the citing document, resolves other relative
resources from the project root, and returns matching corpus document IDs. It
does not infer imports, transitive dependencies, semantic staleness, or review
completion.

## Three separate states

Keep these statements separate:

- **scanned** means a lock was written or a current inventory was compared;
- **reviewed** means a human or agent examined the relevant evidence; and
- **compiled** means a build validated and published a corpus.

The existing lock proves none of the latter two. Secret filtering is a
high-confidence backstop, not a complete scanner. Maintainers must practice
repository hygiene, review `.poolboyignore` and the inventory, and add
project-specific exclusions. If a filed path contains a real credential,
revoke or rotate it; excluding it cannot undo prior exposure.
