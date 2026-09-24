---
okf_version: "0.2"
---
# Poolboy

Poolboy maintains software documentation as portable OKF Markdown and compiles
it into a deterministic static HTTP corpus. A Go CLI provides queries and
refactoring, a constrained Knap companion renders repeated reference material,
and the discovery Skill supplies judgment inside an existing coding agent.

A big hat tip to [Agentic Wiki](https://github.com/agentic-wiki/wiki) and its
authors and contributors. Poolboy builds directly on their Go CLI code and
takes inspiration from their approach to Markdown and agent workflows.
Thank you for the foundation and permission to build on it.

## Read the corpus

Start here, then follow the concept you need:

- [Architecture](architecture.md) — the components and how a corpus flows from Markdown to `dist/`.
- [Build and render](build-and-render.md) — the deterministic build, landing/ZIP publication, generated-file ledger, and what a failed build does.
- [Renderer and companion](renderer.md) — the bounded Go→Node template protocol, its filter allowlist, and its budgets.
- [Maintenance commands](maintenance-commands.md) — the CLI surface, the link model, and move semantics.
- [Source inventory and drift](source-inventory-and-drift.md) — the evidence baseline, the scan/drift/accept lifecycle, and reconciliation.
- [Security boundaries](security-boundaries.md) — the inspection boundary, scan safety limits, and the render boundary.
- [Command reference](reference/commands.md) — the generated, per-command CLI reference.
