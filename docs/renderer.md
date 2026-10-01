---
type: concept
title: Renderer and companion
description: The bounded Go-to-Node renderer protocol and the Knap filter surface.
status: draft
sources:
  - resource: ../README.md
  - resource: ../internal/renderer/renderer.go
  - resource: ../companion/src/protocol.ts
  - resource: ../companion/src/renderer.ts
  - resource: ../companion/src/protocol.test.ts
  - resource: ../companion/src/renderer.test.ts
  - resource: ../companion/package.json
  - resource: ../companion/src/bin/cli.ts
  - resource: ../testdata/fixture/templates/endpoints.md.knap
---
# Renderer and companion

Poolboy keeps repeated reference material in a Knap template plus JSON data.
The Go compiler invokes one trusted companion process for each template. The
renderer is an adapter, not a general project plugin: the request carries only
a template string and a variables object.

## Protocol

The renderer protocol is version 0. Go sends one JSON object on stdin:

```json
{"template":"...","variables":{}}
```

The companion returns one JSON object on stdout. Success has `ok: true`, an
`output` string, and `warnings`. A template failure has `ok: false`,
`kind: "template"`, and structured errors; a protocol failure has
`kind: "protocol"`, a code, and a message. The Go decoder rejects empty,
malformed, multiple, or trailing non-whitespace JSON. A failed render carries
no partial output back to the compiler.

The request shape is strict: the companion accepts exactly `template` and
`variables`; the template must be a string and variables must be a non-array
object. Invalid JSON and invalid request shapes have distinct protocol error
codes. The nominal exit mapping is success `0`, template failure `1`, and
protocol failure `2`. The admitted companion CLI reads stdin with the same
2 MiB cap, emits exactly one JSON response followed by a newline, sets
`process.exitCode` from that response, and maps read, parse, render, and
unexpected top-level failures to protocol/internal responses.
The companion CLI entrypoint cited here was admitted by the current bounded
drift as an added source. The original lock remains unchanged, so this is
current drift evidence rather than a claim that the baseline was accepted.

## Knap surface

The companion pins Knap `0.6.0` and creates a fresh filter registry from this
allowlist only:

`trim`, `upper`, `lower`, `first`, `last`, `length`, `join`, `list`, `table`,
`yaml`, `yaml_property`, `h1`, `h2`, `code`, `code_block`, `indent`,
`escape_md`, and `link`.

The engine disables regex support (`allowRegex: false`) and does not expose a
caller-supplied filter registry or project callback. An unlisted filter such
as `date` is a template error (`UNKNOWN_FILTER`) and its partial prefix is
discarded. The fixture template demonstrates interpolation, YAML-safe
frontmatter values, and a `for` loop; loop and conditional grammar beyond the
permitted fixtures is supplied by the external Knap dependency and is not
specified here.

## Limits and process boundary

The visible Knap limits are:

- maximum nesting depth: `50`;
- maximum operations: `50,000`;
- maximum output length: `100,000`;
- maximum template length: `100,000`;
- maximum value length: `1,000,000`.

The Go side caps a serialized request at 2 MiB, enforces a five-second render
deadline, starts Node with a 128 MiB old-generation heap cap, and captures at
most 8 MiB of stdout and 256 KiB of stderr. The child environment is reduced to
`PATH` and, when present, `SYSTEMROOT`, avoiding inherited proxy, credential,
and `NODE_OPTIONS` values.

The adapter package documents no network access, no executable path from
corpus metadata, and no project callback. The inspected implementation does
not show an OS-level network or filesystem sandbox: an explicit `.mjs`/`.js`
path is an ordinary Node child, and other renderer paths can be executed
directly. The source-backed boundary is therefore the trusted renderer path,
minimal environment, strict protocol, fixed filters, and resource budgets—not
a blanket claim of kernel-level isolation.

## Failure behavior

Protocol, adapter, timeout, cancellation, output-size, malformed-output, and
Knap template failures are distinct diagnostics. A Knap limit error such as an
overlong template is reported as `LIMIT_EXCEEDED`; Go maps template responses
to a render error without output. The compiler refuses invalid generated
Markdown rather than publishing a partial document.

## What a render failure means for you

A failed render is not a partial publication. The build stops before replacing
anything, so the previously published `dist/`, the corpus, and the generated
ledger are all exactly as they were. Nothing needs cleaning up before retrying.

What to do depends on which diagnostic you get, and the cost of guessing wrong is
editing the wrong file:

- **Template failure** — the template or its data is wrong. Fix the `.knap`
  template or the JSON data the `[[render]]` mapping names, then build again.
  Nothing in the corpus needs touching.
- **`UNKNOWN_FILTER`** — the template uses a filter outside the fixed allowlist.
  The allowlist is not configurable, so the template must be rewritten to avoid
  it. Adding the filter is not an option.
- **`LIMIT_EXCEEDED`** — the template, output or a value crossed a Knap limit.
  These limits are fixed too; split the document or reduce the data rather than
  looking for a setting to raise.
- **Timeout or output-size failure** — the render exceeded the five-second
  deadline or the capture caps. The budgets are not configurable; the input has
  to get smaller.
- **Protocol failure** — the companion is missing, unreadable, or returned
  something that is not one JSON object. This is an installation problem, not a
  content problem: check that `poolboy-knap.mjs` sits beside the binary.

Because every budget above is fixed rather than configured, a render failure is
always resolved by changing the template, the data, or the installation — never
by tuning Poolboy. If a document genuinely cannot fit the limits, it is authored
as ordinary Markdown instead of generated.
