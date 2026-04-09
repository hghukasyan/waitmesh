# Implementation Plan

## Phase 1 — Domain + SQLite + atomic ingestion

- [x] Domain types, validation, content hash
- [x] Migrations and schema (idempotent, versioned)
- [x] Event insert with idempotency / conflict
- [x] Transactional store API + single-writer pipeline
- [x] WAL + `synchronous=FULL` verified on open
- [x] Audit tests: durability pragmas, atomic rollback, content hash

## Phase 2 — Absence + durable timers

- [x] Absence match create / cancel in same ingest transaction
- [x] Durable deadline rows; conditional claim
- [x] Scheduler: bounded batch, deadline-aware wait, graceful stop
- [x] Mandatory absence test suite + 3s payment demo

## Phase 3 — Sequence / count / outbox / consumers

- [x] Sequence engine (A→B within T); oldest-match claim; expire without emit
- [x] Count sliding window with durable `counter_events` + watermark
- [x] Firings + output events + outbox in one transaction
- [x] Durable consumer registration
- [x] Independent poll / lease / ack / nack / lease-retry
- [x] Safe retention purge (all consumers acked)
- [x] Migration `002_consumers_retention`
- [x] Mandatory sequence/count/outbox tests
- [x] Phase 3 benchmarks (sequence, count, mixed, concurrent, outbox, restart)

## Phase 4 — REST / SDK / CLI / OpenAPI

- [x] HTTP API surface (incl. consumers, nack, purge)
- [x] Go SDK (`pkg/client`)
- [x] CLI commands
- [x] OpenAPI document

## Phase 5 — Benchmarks / metrics / hardening

- [x] Prometheus metrics
- [x] Benchmarks (0/10/100/1000 rules + Phase 3 suite)
- [x] Race tests and integration tests
- [x] Payment demo example
- [x] Profile-guided optimizations (rule cache, hash lookup, count prune)
- [x] Repro benches with p50/p95/p99 (`docs/BENCHMARKS.md`)
- [x] Stress + subprocess crash harness (`docs/RELIABILITY.md`)

## Phase 6 — Packaging / website / CI

- [x] Dockerfile, compose, Makefile
- [x] GitHub Actions
- [x] Marketing site in sibling `website/` folder
- [x] v0.1.0 release readiness audit (`docs/RELEASE_READINESS.md`)
- [ ] Optional React dashboard (deferred)
