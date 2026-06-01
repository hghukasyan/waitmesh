# Waitmesh Performance

## Configuration invariants

These are **not** tunable for benchmark inflation:

- `PRAGMA journal_mode=WAL`
- `PRAGMA synchronous=FULL`
- Single-writer pipeline (`MaxOpenConns(1)` + bounded queue)
- One transaction per logical write batch

## Profiling summary (pre-optimization)

CPU (`pprof` on Sequence/Count/Ingest, ~3s sample):

1. **`database/sql.(*Tx).Commit` ~36%** — durable fsync under `synchronous=FULL` (kept).
2. Syscall / pthread cond — writer handoff noise.
3. Application work small vs commit; gains come from doing **less work per TX**.

Memory (alloc_space):

1. **`RulesForEventType` + `hydrateRule` ~26%** — JSON rule reload every event.
2. **`GetEvent` ~20%** — full row fetch for idempotency.
3. `database/sql.driverArgs` / sqlite bind — per-statement args.
4. `Engine.do` channel allocations.

## Optimizations applied (evidence-based)

| Change | Why | Durability impact |
|--------|-----|-------------------|
| In-process **rule cache** by event type | Eliminates SQL+JSON hydrate on hot path | None — refreshed after successful rule commits |
| `LookupContentHash` before insert | Avoids full event decode on new IDs | None |
| Count window: `COUNT … timestamp>=cutoff` + prune every 32 | Removes DELETE-per-event when window is wide | None — filtered count is authoritative |
| `sync.Pool` for write result channels | Fewer allocs per ingest | None |
| Cheaper monotonic IDs | Less `crypto/rand` per match | None — IDs still unique |

**Not done (intentionally):** `synchronous=NORMAL/OFF`, multi-writer SQLite, dropping fsync, in-memory-only timers.

## Architectural limit

End-to-end ingest throughput is dominated by **SQLite durable commit latency** on one writer. Further gains without weakening durability require:

- Optional batched ingest API (multiple events / one TX) — API change
- Application-level sharding by key across processes
- Hardware with faster fsync

SQLite remains an appropriate **default** for self-hosted single-node correctness.

## Measured results

See [BENCHMARKS.md](BENCHMARKS.md) for tables and reproduction commands.
