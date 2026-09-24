---
type: Concept
title: Request handling
description: The fixture separates authentication from endpoint dispatch.
sources:
  - resource: ../data/endpoints.json
status: stable
---
# Request handling

Requests authenticate before endpoint dispatch. Invalid tokens produce a 401 response; successful requests reach the [endpoint reference](reference/endpoints.md#endpoints).

The [root index](index.md) connects the documentation. [HTTP semantics](https://www.rfc-editor.org/rfc/rfc9110) are external evidence, not another corpus node.
