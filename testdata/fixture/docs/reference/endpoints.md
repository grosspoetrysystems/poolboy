---
type: Reference
title: "Endpoint reference"
description: Deterministic endpoint reference from structured data.
---
# Endpoints

See [request handling](../architecture.md).

## Health

`GET` `/health`

Returns service health without exposing runtime secrets.
## Current session

`GET` `/session`

Returns the authenticated session; invalid tokens receive 401.
