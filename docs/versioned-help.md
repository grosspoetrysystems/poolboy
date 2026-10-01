---
type: concept
title: Versioned structured help
description: How command help is sourced, checked, snapshotted, released, and served.
status: draft
sources:
  - resource: ../data/commands.json
  - resource: ../data/embed.go
  - resource: ../cmd/poolboy/help.go
  - resource: ../scripts/check-command-docs.mjs
  - resource: ../scripts/generate-help-snapshot.mjs
  - resource: ../.github/workflows/release.yml
  - resource: ../.github/workflows/deploy.yml
---
# Versioned structured help

`poolboy help` answers from structured data, not prose embedded in the command
code. One catalog is the source of truth, every release freezes an immutable
copy of it, and a running binary gets either help that matches its own version
or the copy compiled into it. It never shows a document belonging to a
different version.

This is separate from the generated [command reference](reference/commands.md),
which is corpus Markdown built by a render mapping. Both derive from the same
catalog; one is published documentation, the other is the CLI's runtime answer.

## The catalog

`data/commands.json` is the canonical catalog. It declares `schema: 1` and a
`commands` array; each command carries its name, summary, aliases, whether it
mutates, its source file, flags, usage, effects, what it explicitly does not do,
how to verify it, and related commands.

`data/embed.go` compiles that file into the binary. The embedded copy is the
offline fallback, so help works with no network, no hosted site, and no
configuration.

## Checked before it ships

`scripts/check-command-docs.mjs` runs in pre-commit and `make check`. It
validates the catalog's shape, confirms `related` and `verify_with` reference
commands that exist, and cross-checks every documented flag against the actual
Go handler for that command. A flag added in code but not in the catalog fails
the check, and so does a flag documented but never declared.

`scripts/generate-help-snapshot.mjs --self-test` runs in `make check` and
asserts that a malformed retained snapshot fails the manifest rather than being
published.

The catalog cannot drift from the CLI without failing a check.

## Snapshotted per version

`generate-help-snapshot.mjs snapshot <version> <root>` writes
`v<version>/index.json` for the whole catalog and one
`v<version>/commands/<name>.json` per command. Every document is stamped with
its `schema` and its `cli_version`. Command names that could escape the
directory are rejected.

`generate-help-snapshot.mjs manifest <root>` scans the snapshot directories,
validates each one, keeps only stable versions, and writes a support index
naming `latest`, `minimum_supported`, and every retained version newest-first.

## Released as an asset

A `v*` tag release builds that version's snapshot, packs it as
`poolboy-help-v<version>.tar.gz`, and uploads it as a release asset. The release
is created as a draft and published only after the released package passes its
smoke test, so the help asset always accompanies the binary that reports that
version.

Each release's snapshot is immutable. Help for an old version is never
regenerated from current data; it is the artifact that shipped with it.

## Reconstructed at deploy

Deployment does not generate help from the working tree. It lists published
stable releases newest-first, downloads each one's help asset, validates and
extracts it, and keeps up to the twelve most recent. It then rebuilds the
support manifest across everything retained and deploys the result.

A deploy that cannot assemble at least one snapshot fails rather than publishing
an empty help tree.

## Served at runtime

`poolboy help [command]` and `poolboy <command> --help` both resolve the command
name through the catalog's aliases, then try hosted help first at
`poolboy.sh/cli/help/v<version>/`. The request is deliberately austere: a single
attempt with a two-second timeout, at most five redirects, same host only, and a
bounded response body. There is no retry and no backoff — a slow or missing host
falls back immediately rather than delaying the command.

A hosted document is used only when its `schema` and its `cli_version` both
match the running binary. Anything else falls back to the embedded catalog.
Development builds skip the network entirely.

When the host reports that a version is gone, the CLI reads the support manifest
and, if the running version is older than `minimum_supported`, prints an upgrade
notice to stderr while still answering from the embedded catalog.

## What this guarantees

- Help always answers, with or without a network.
- A binary never displays help belonging to a different version.
- Published help for a released version is the artifact that shipped with it.
- A flag that exists in code but not in the catalog fails a check before release.
