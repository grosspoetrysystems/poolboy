---
type: Reference
title: Portable corpus and review contracts
sources:
  - resource: internal/compiler/graph.go
  - resource: internal/compiler/publication.go
  - resource: internal/sign/trust.go
  - resource: internal/source/source.go
  - resource: cmd/poolboy/main.go
---
# Portable corpus and review contracts

Poolboy has one public authority and one private workspace. `graph.json` is the
versioned publication manifest. Project files and `.poolboy/` are the mutable
workspace. A client must not treat source inventory, generated-file ownership,
or publisher trust as a review-decision store.

## Identity

A canonical root-absolute Markdown path such as `/guide.md` is a logical
locator. Its `graph.files[path].sha256` and `bytes` identify the exact published
content revision. Artifact locators are clean publication-relative paths and
use the same SHA-256-plus-size revision rule. The digest of the exact
`graph.json` bytes identifies a publication revision.

Locators and revisions are deliberately separate:

- editing a document keeps its locator and changes its revision;
- renaming creates a new locator and removes the old one; no implicit identity
  continuity is claimed;
- deletion is absence from a newer graph, not a tombstone;
- fragments are link targets, not independently revisioned sections;
- a consumer must re-resolve a locator and compare the expected revision before
  acting. A mismatch or missing locator is stale input, not permission to use
  the latest bytes.

## Publication discovery and compatibility

Fetch `graph.json`, require `version: "0"` and `root: "/index.md"`, then resolve
Markdown through `files`, `llms.txt` through `llms`, and other owned static
files through `artifacts`. Paths are relative to the publication origin;
Markdown keys alone begin with `/`. Unknown graph versions are unsupported and
must be rejected rather than partially interpreted.

The exact graph bytes are signed. Verification authenticates the graph and
every named byte. A valid signature proves integrity and, for Sigstore, the
configured publisher workflow identity. It does not prove review, correctness,
license, publication permission, or prompt safety. Static publication cannot
revoke bytes a consumer already downloaded.

Graph version 0 admits every validated corpus Markdown document, its normalized
links, type, title, and authored `sources` metadata. Those fields are public.
Private paths, raw queries, review records, source contents, repository/user
identity, and workspace timestamps must remain outside the corpus and
publication. A project containing private provenance must omit or redact it
before build; signing does not change its audience.

## Workspace state

State has one job each:

| State | Authority | Regenerable |
| --- | --- | --- |
| authored Markdown | corpus content | no |
| `.poolboy/sources.lock.json` | accepted source evidence baseline | no |
| `.poolboy/generated.json` | generated-file ownership | no |
| `.poolboy/reviews.json` version 0 | future durable review decisions and reconciliations | no |
| computed review plan | current candidate view | yes |
| `dist/` | static publication described by `graph.json` | yes |
| publisher trust lock | approval of an exact graph digest and publisher | no |

`reviews.json` is reserved for GPS-307. Until its schema and commands ship, no
Poolboy operation creates or consumes it. It must remain private, versioned,
and separate from the source baseline, generated ledger, and publisher trust.
A durable decision must bind the candidate locator, document revision, evidence
locator and revision, operation, and decision state. Changed or missing
preconditions make it stale. Unknown versions and private data presented as
publication input must be rejected.

## Process boundary

The implemented maintenance interface is the `poolboy` CLI. Use `--root DIR`
for discovery and `--format json` for command results. JSON shapes are
command-specific; there is no common envelope or JSON-stdin protocol. Standard
exit classes are:

- `0`: operation completed, including an empty diagnostic result;
- `1`: valid negative result such as no match, check findings, cancelled
  approval, or an authenticated but unapproved publication;
- `2`: usage, discovery, configuration, I/O, or unsupported-contract error.

Only commands listed by `poolboy help` and the generated command reference are
implemented operations. In particular, `review`, `decide`, `apply`, and
`reconcile` are not operations yet. Clients may inspect current evidence with
`drift`, `health`, and `affected`; they must not infer a durable decision from
those regenerable results.

## Contract fixture

`internal/compiler.TestPortableContractFixture` builds the repository's minimal
compiler fixture and locks the existing boundary: graph version/root discovery,
canonical locator plus exact revision, exclusion of private workspace state,
and rejection of stale published bytes and unknown graph versions. The fixture
uses the same compiler and publication validator as production; it introduces
no second manifest or SDK.
