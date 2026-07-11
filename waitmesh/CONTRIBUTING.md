# Contributing to Waitmesh

Thank you for contributing.

## Principles

Correctness > atomicity > durability > predictable performance > simplicity > features.

## Development

```bash
go test ./...
go test -race ./...
make demo
```

- Prefer real SQLite integration tests with an injectable clock.
- Do not invent benchmark numbers; measure and report.
- Keep changes small and deterministic.
- Document semantic changes in `docs/SEMANTICS.md`.

## Pull requests

1. Include tests for new behavior.
2. Run race detector for concurrency-related changes.
3. Update docs when APIs or semantics change.
