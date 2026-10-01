---
type: reference
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

## Domain vocabulary

- A **corpus** is the authored set of portable Markdown documents governed by
  one `poolboy.toml`.
- A **logical locator** is a canonical root-absolute corpus path such as
  `/guide.md`. It names where a document is addressed, not particular bytes,
  and does not survive a rename automatically.
- A **content revision** is the lowercase SHA-256 of exact document or artifact
  bytes, paired with their byte count. It identifies bytes, not truth,
  approval, or stable document identity.
- A **workspace** is the private mutable project containing authored Markdown,
  source evidence, and generated-file ownership.
- A **checkout** is a private mutable Markdown copy plus an exact base manifest
  created from one workspace. It records observed state; it is not a
  publication, lock, review decision, or source baseline.
- A **check-in plan** is a regenerable file-level comparison of a checkout base,
  its edited draft, and the current workspace. Applying a conflict-free plan
  transports corpus bytes into the workspace; it does not perform source
  reconciliation.
- A **publication** is an immutable set of static files described by one
  `graph.json`.
- A **review plan** is a regenerable view of maintenance candidates derived
  from current evidence. It is not durable authority.
- A **review decision** is a durable private choice about a candidate, bound to
  exact document and evidence revisions.
- A **reconciliation** records that exact reviewed document bytes were
  considered against exact evidence bytes. It does not claim semantic truth or
  advance the project-wide source baseline.

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
| computed review plan | current candidate view | yes |
| `.poolboy/checkouts/*.json` | private checkout bases and locations | no |
| `dist/` | static publication described by `graph.json` | yes |
| publisher trust lock | approval of an exact graph digest and publisher | no |

GPS-307 owns the schema and location for future durable review decisions and
reconciliations. Checkout state is not that schema: it records exact bytes for
conflict detection but no review judgment. Durable review state must remain
private and separate from checkout bases, the source baseline, generated
ledger, and publisher trust. Changed or missing preconditions make a prior
decision stale; unknown versions and private data presented as publication
input must be rejected.

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
implemented operations. `checkout` and `checkin` transport asynchronously
edited Markdown; `checkin --apply` is a flag-controlled mutation, not an
`apply` command or a durable review decision. `review`, `decide`, `apply`, and
`reconcile` are not operations yet. Clients may inspect current evidence with
`drift`, `health`, and `affected`; they must not infer a durable decision from
those regenerable results or from a successful check-in.

## Contract fixture

`internal/compiler.TestPortableContractFixture` builds the repository's minimal
compiler fixture and locks the existing boundary: graph version/root discovery,
canonical locator plus exact revision, exclusion of private workspace state,
and rejection of stale published bytes and unknown graph versions. The fixture
uses the same compiler and publication validator as production; it introduces
no second manifest or SDK.

## What the CLI enforces, and what it only states

Several rules on this page are obligations on the client. Poolboy cannot check
them from the other side of an HTTP boundary, and confusing the two kinds is how
a consumer ends up trusting something that was never verified.

Enforced by the tool, and a failure is observable:

- Graph version and root are validated at build; an unknown version is rejected
  rather than partially interpreted.
- Signature verification authenticates the exact graph bytes and every byte the
  graph names. A tampered artifact fails.
- Approval binds an exact graph digest; a different digest is reported as
  pending rather than silently accepted.
- Private workspace state is excluded from the publication by the compiler, and
  the contract fixture locks that exclusion.
- Exit classes are produced by the CLI, so a script can branch on them.

Stated but not enforced, and entirely the client's responsibility:

- **Re-resolving a locator before acting.** Nothing stops a client caching a
  path and using stale bytes. A mismatch is stale input, not permission to use
  the latest.
- **Not treating regenerable views as decisions.** `drift`, `health` and
  `affected` answer about current evidence. Nothing prevents a client recording
  one as a durable judgment, and nothing will warn when it goes stale.
- **Redacting private provenance before build.** The compiler excludes
  `.poolboy/` state, not secrets a maintainer wrote into corpus prose. Signing
  does not change a document's audience.
- **Treating a signature as integrity only.** A valid signature proves the bytes
  are the publisher's. It proves nothing about review, correctness, licensing or
  prompt safety, and no command will tell you otherwise.

## What a client must build for itself

Because the contract is files over HTTP rather than a service, several things a
client might expect from an API do not exist, and each has to be handled on the
consumer side:

- **There is no revocation.** A publication cannot withdraw bytes a consumer
  already downloaded. A client that must not act on superseded content has to
  re-fetch `graph.json` and compare the publication revision itself.
- **There is no change feed.** Discovering that a corpus moved means fetching
  the graph again and diffing; nothing will notify.
- **There is no common response envelope.** JSON shapes are command-specific, so
  a client parses per command and cannot write one generic decoder.
- **There is no stdin protocol.** Integration is process invocation with
  `--root` and `--format json`, which means a client owns process lifetime,
  timeouts and concurrency.
- **Renames break locators deliberately.** A client holding `/old.md` gets
  absence, not a redirect, and must resolve the new locator from the graph.

If a future release adds `review`, `decide`, `apply` or `reconcile`, they will be
listed by `poolboy help` and the generated command reference. Until a command
appears there, it is not an operation, whatever this or any other document
implies about the concept behind it.
