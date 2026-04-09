# Waitmesh Architecture

Waitmesh is a self-hosted **event condition engine**. It evaluates correlated event patterns (absence, sequence, count) and emits derived output events when conditions are met.

It is not a message broker, workflow engine, or stream processor. It complements Kafka, NATS, Redis, and Temporal.

## Goals

1. **Correctness** — deterministic outcomes for races (timeout vs cancel).
2. **Atomicity** — all effects of accepting one input event commit in one SQLite transaction.
3. **Durability** — WAL + strong synchronous defaults; timers survive restarts.
4. **Predictable performance** — bounded write pipeline, indexed lookups.
5. **Simplicity** — single binary, one SQLite file, no mandatory cloud.

## Process layout

```
┌─────────────┐     ┌──────────────────────────────────────────┐
│  Publishers │────▶│  REST / CLI / SDK                        │
└─────────────┘     │                                          │
                    │  Write pipeline (single writer)          │
┌─────────────┐     │    └─ Engine.Ingest (BEGIN…COMMIT)       │
│  Consumers  │◀────│         ├─ events                        │
└─────────────┘     │         ├─ pending_matches / counters    │
                    │         ├─ firings + output_events       │
                    │         └─ outbox                        │
                    │  Timer scheduler (durable deadlines)     │
                    │  Outbox poller (at-least-once delivery)  │
                    └──────────────────────────────────────────┘
                                      │
                                      ▼
                                 SQLite (WAL)
```

## Packages

| Path | Role |
|------|------|
| `internal/domain` | Types, validation, canonical hashing, rule semantics helpers |
| `internal/storage/sqlite` | Schema, migrations, transactional repositories |
| `internal/engine` | Rule evaluation inside a transaction |
| `internal/scheduler` | Overdue timer processing; optional min-heap cache |
| `internal/outbox` | Consumer lease / ack semantics |
| `internal/api` | HTTP handlers, auth, limits |
| `internal/config` | Config loading |
| `internal/metrics` | Prometheus metrics |
| `internal/clock` | Injectable clock |
| `pkg/client` | Go SDK |
| `cmd/waitmesh` | CLI |

## Write path

SQLite allows one writer. Waitmesh serializes mutations through a bounded channel:

1. Request enqueued (backpressure when full → `503` / retryable error).
2. Writer goroutine opens a transaction.
3. Engine applies idempotency checks, rule matching, match create/cancel, firings, outbox inserts.
4. Commit. Response returns committed state.

Readers (list APIs, metrics) use separate connections and do not block the writer longer than necessary.

## Atomic ingestion

For each accepted event, one transaction may:

- insert the event (or short-circuit on idempotent duplicate / conflict);
- create pending matches (absence / sequence starts);
- cancel pending matches (unless / completing event);
- update sliding-window counters and emit on threshold;
- insert firings with uniqueness constraints;
- insert derived output events;
- insert outbox rows for consumers.

Timer expiry uses the same transactional patterns: claim a pending match with a conditional `UPDATE … WHERE status='pending' AND deadline<=?`, then write firing + output + outbox.

## Durable timers (Phase 2)

- Source of truth: `pending_matches.deadline_ns` with index `(status, deadline_ns)`.
- Scheduler runs a **bounded batch** (`LIMIT`) of overdue rows; no full-table scan of history.
- Wake interval is `min(maxWait, time-until-next-deadline)`; heap is a hint only.
- On restart, SQLite restores all pending rows; overdue rows are claimed with:

  `UPDATE pending_matches SET status='fired' WHERE id=? AND status='pending' AND deadline_ns<=?`

- Firing uniqueness: partial unique index on `firings(pending_match_id)`.
- Cancel vs expire race: whichever conditional update commits first wins; the other affects 0 rows.
- Graceful shutdown: context cancel stops the scheduler loop; engine `Close` drains the write queue once.

## Phase 1 durability

- `PRAGMA journal_mode=WAL` (required for file DBs; verified on open).
- `PRAGMA synchronous=FULL` (required; open fails if weaker).
- Single writer connection + bounded ingest queue for backpressure.

## Outbox & consumers (Phase 3)

- Output events are durable and immutable; written with firing + outbox in one TX.
- `consumers` table provides durable registration (poll auto-registers).
- Each consumer has independent lease/ack state in SQLite.
- Ack by consumer A does not remove the output for consumer B.
- Nack or lease expiry (`leased_until <= now`) enables at-least-once retry.
- Retention purge removes **outbox** rows only after all registered consumers ack
  (and age ≥ retain); `output_events` kept for audit.
- Delivery is **at-least-once**; stable output IDs enable idempotent consumer handling.
- Exactly-once external side effects are **not** claimed.

## Sequence & count

- Sequence pending matches reuse `pending_matches` (same durable timer index).
- Count windows use `counter_events` + `counter_state` watermark; prune by time index.
- No scan of the global `events` table for rule evaluation.

## Security defaults

- Listen `127.0.0.1:7777`.
- Optional bearer token required when binding non-loopback.
- Request body size limits and HTTP timeouts.
- No telemetry.

## Observability

- Structured logs via `log/slog`.
- Prometheus metrics at `GET /v1/metrics`.
- Health: `live` (process up), `ready` (DB open + writer running).

## Extension points (non-goals for v1)

- Broker adapters (Kafka/NATS ingest).
- Multi-node consensus.
- Horizontal shard by key.
- Built-in dashboard (optional under `web/`).
