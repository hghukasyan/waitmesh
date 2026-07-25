# Changelog

## [0.1.0] — 2026-10-23

First public open-source release of Waitmesh.

### Added

- Absence, sequence, and count rule engines on SQLite (WAL + `synchronous=FULL`)
- Single-writer atomic ingest pipeline with idempotent event IDs
- Durable timers, transactional outbox, independent consumer poll/ack/nack
- REST API, Go SDK (`pkg/client`), CLI (`waitmesh`)
- Prometheus metrics, liveness/readiness probes
- Docker image + compose example
- Crash-injection tests (build tag `crashinject` only)

### Security

- Default bind `127.0.0.1:7777`
- Bearer token required for non-loopback binds
- Constant-time token compare; request body size limits; HTTP timeouts
- Health endpoints remain unauthenticated for probes

### Not in 0.1.0

- Batch multi-event ingest API
- Web dashboard
- Clustering / multi-node
- Broker adapters (Kafka/NATS)
- Exactly-once external delivery claims
