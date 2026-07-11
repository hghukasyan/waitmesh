# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Defaults

- Bind `127.0.0.1:7777` (loopback).
- Non-loopback binds (`0.0.0.0`, `:port`, LAN addresses) **require** `WAITMESH_TOKEN`.
- Bearer authentication uses constant-time comparison.
- `/v1/health/live` and `/v1/health/ready` are intentionally unauthenticated.
- Request bodies are capped (`WAITMESH_MAX_BODY`, default 1 MiB).
- HTTP read/write/idle timeouts are enabled.
- Event payloads are **not** written to structured logs by the server.
- No telemetry or phone-home.
- Crash-injection (`WAITMESH_CRASH_POINT`) is compiled out unless built with `-tags crashinject`.

## Threat notes (v0.1)

- Waitmesh is a **single-node** trust boundary: anyone with DB file access can read events.
- Metrics and OpenAPI require the bearer token when auth is enabled.
- Expensive rule sets (many overlapping absence rules on a hot type) can amplify write cost — operators should size rules deliberately.
- SQL is parameterized; user input is not concatenated into queries.

## Reporting a vulnerability

Email **security@waitmesh.com** with description and reproduction steps.
Do not open a public issue for sensitive reports.
