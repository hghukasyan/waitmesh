# Waitmesh

**Know when something didn't happen.**

Waitmesh is a fast, atomic, durable, self-hosted **event condition engine**.
It evaluates absence, sequence, and count patterns over correlated events — without custom timers, workers, or state machines.

**Version:** 0.1.0 · **License:** Apache-2.0 · **Website:** [waitmesh.com](https://waitmesh.com)

100% free and open source. No billing, no telemetry, no mandatory cloud.

## Quick start

```bash
cd waitmesh
go build -o bin/waitmesh ./cmd/waitmesh
./bin/waitmesh serve --db ./waitmesh.db
# listens on 127.0.0.1:7777
```

```bash
./bin/waitmesh rules add \
  --name missing-payment --type absence \
  --start payment.initiated --unless payment.completed \
  --within 5m --emit payment.timeout

./bin/waitmesh publish --type payment.initiated --key ord-1 --id e1
./bin/waitmesh pending list
```

## Rule types

| Type | Meaning |
|------|---------|
| **absence** | A occurs, B does not before deadline → emit C |
| **sequence** | A then matching B within deadline → emit C (oldest pending) |
| **count** | N occurrences of A with same key in a sliding window → emit C |

## Design guarantees (v0.1)

- SQLite WAL + `synchronous=FULL` (verified on open)
- One SQLite transaction per ingest / timer batch / outbox op
- Idempotent event IDs; content conflicts → 409
- Durable timers; cancel vs timeout decided by conditional updates
- Transactional outbox; at-least-once delivery; stable output IDs
- **Not claimed:** power-loss durability beyond SQLite FULL, exactly-once side effects, multi-node consensus

Docs: [ARCHITECTURE](docs/ARCHITECTURE.md) · [SEMANTICS](docs/SEMANTICS.md) · [RELIABILITY](docs/RELIABILITY.md) · [BENCHMARKS](docs/BENCHMARKS.md) · [RELEASE_READINESS](docs/RELEASE_READINESS.md)

## HTTP API

| Method | Path |
|--------|------|
| POST/GET | `/v1/events` |
| POST/GET/PATCH/DELETE | `/v1/rules`, `/v1/rules/{id}` |
| GET | `/v1/pending` `/v1/firings` `/v1/outputs` |
| POST | `/v1/consumers` |
| POST | `/v1/outputs/poll` `/ack` `/nack` `/purge` |
| GET | `/v1/health/live` `/v1/health/ready` (no auth) |
| GET | `/v1/metrics` `/openapi.yaml` |

Remote bind requires `WAITMESH_TOKEN` (Bearer).

## Go SDK

```go
c := client.New("http://127.0.0.1:7777")
_, err := c.Publish(ctx, domain.EventInput{ID: "e1", Type: "payment.initiated", Key: "ord-1"})
```

## Docker

```bash
docker compose up --build
# sets WAITMESH_TOKEN — required for 0.0.0.0 bind
```

## Not in v0.1

- Batch multi-event ingest API
- React dashboard (`web/` deferred)
- Clustering / broker adapters

## Development

```bash
go test ./...
go test -race ./... -short
go test ./internal/engine -run TestCrash_
go run ./examples/payment
make bench
```
