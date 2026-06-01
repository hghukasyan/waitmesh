# Waitmesh Benchmarks

## Environment (measured)

| Item | Value |
|------|-------|
| Hardware | Apple M4, 24 GB RAM |
| OS | darwin/arm64 |
| Go | go1.26.0 |
| SQLite driver | `modernc.org/sqlite` v1.61.0 (pure Go) |
| Journal | **WAL** (verified on open) |
| Synchronous | **FULL** (=2, verified on open; not NORMAL/OFF) |
| Connections | `MaxOpenConns(1)` single writer |
| Transaction | one SQLite TX per ingest / timer batch / outbox op |

## Methodology audit

| Check | Status |
|-------|--------|
| WAL enabled | Pass — `Open` fails if not `wal`/`memory` |
| `synchronous=FULL` | Pass — open fails if `< 2` |
| Durable commits | Pass — every write path uses `WithTx` → `Commit` |
| Realistic payloads | Repro benches use JSON blobs (0 / 256 / 4KiB) |
| Warmup separated | `warmup()` before `b.ResetTimer()` in repro suite |
| DB not reset mid-measure | Temp DB per benchmark; no truncate during timed loop |
| Restart ≠ DB init | Crash tests use subprocess + reopen same file |

### Known methodology notes (pre-fix)

- Early Phase 3 benches used **empty** `data` and no warmup.
- `BenchmarkRestartRecoveryLoad` reopened the same DB after firings were exhausted (measures open+empty sweep after first iter).
- Throughput variance is high on short `-benchtime` windows.

## Reproduction

```bash
cd waitmesh

# Core suite (throughput + allocs)
go test ./benchmarks -bench='Benchmark(SequenceMatch|CountMatch|MixedWorkload|ConcurrentProducers|Ingest_)' \
  -benchmem -benchtime=300ms -count=1

# Reproducible suite (p50/p95/p99 ingest latency, payloads, pubs, rule scale)
go test ./benchmarks -bench='BenchmarkRepro_' -benchmem -benchtime=300ms -count=1

# CPU / memory profiles
go test ./benchmarks -bench=BenchmarkSequenceMatch -benchtime=500ms \
  -cpuprofile=/tmp/wm-cpu.out -memprofile=/tmp/wm-mem.out
go tool pprof -http=:8080 /tmp/wm-cpu.out
```

## Baseline → optimized (Apple M4, `-benchtime=300ms`)

| Benchmark | Baseline | Optimized | Δ |
|-----------|----------|-----------|---|
| Sequence events/s | 5,848 | **6,661** | +14% |
| Count events/s | 2,990 | **4,635** | +55% |
| Mixed events/s | 7,279 | **8,793** | +21% |
| Concurrent(8) events/s | 13,916 | **15,289** | +10% |
| Ingest 0 rules allocs/op | 108 | **89** | −18% |
| Sequence allocs/op | 450 | **292** | −35% |
| Count allocs/op | 280 | **190** | −32% |

### Latency sample (`BenchmarkRepro_SequenceAndCount`)

| Path | p50 | p95 | p99 |
|------|-----|-----|-----|
| Sequence start ingest | 83 µs | 127 µs | 504 µs |
| Sequence then ingest | 154 µs | 227 µs | 772 µs |
| Count ingest | 216 µs | 313 µs | 876 µs |

Ingest latency includes durable `Commit` (fsync). Rule-firing for absence timers is **not** included in ingest latency (separate scheduler path). Outbox ACK latency is measured in `BenchmarkOutboxDelivery`.
