# Waitmesh v0.1.0 — Release Readiness

**Date:** 2026-10-23 (America/New_York)  
**Auditor role:** Principal Go Infrastructure / Security  
**Recommendation:** **GO** for public open-source v0.1.0 (with stated limitations)

---

## 1. Implemented features (verified in tree)

| Area | Status |
|------|--------|
| Absence / sequence / count engines | Implemented + tested |
| SQLite WAL + `synchronous=FULL` | Enforced on open |
| Single-writer atomic ingest | Implemented |
| Idempotent event IDs / conflicts | Implemented + tested |
| Durable timers + cancel/timeout races | Implemented + tested |
| Transactional outbox, poll/ack/nack/purge | Implemented + tested |
| Independent consumers | Implemented + tested |
| REST API + OpenAPI | Implemented (OpenAPI updated for consumers/nack/purge) |
| Go SDK (`pkg/client`) | Implemented |
| CLI (`waitmesh serve|publish|rules|…`) | Implemented; smoke verified |
| Metrics / live / ready | Implemented |
| Docker + compose | Present; Dockerfile HEALTHCHECK added |
| Crash subprocess tests | Present (`-tags crashinject`) |
| Web dashboard | **Not shipped** (`web/README` deferred) |
| Batch multi-event ingest API | **Not shipped** (explicitly out of 0.1) |
| Clustering / cloud / billing | **Not present** (by design) |

Phase 5 packaging (API/SDK/CLI/Docker) is **complete enough for v0.1**; batching is intentionally deferred.

---

## 2. Verified guarantees

- Atomic ingest + rule side effects per SQLite transaction  
- Idempotent duplicate IDs; content mismatch → conflict  
- Rule cache invalidated **before** rule mutations (regression test added)  
- Absence cancel vs timeout: conditional updates, ≤1 logical firing  
- Sequence oldest-match; count sliding window + watermark  
- Outbox at-least-once; per-consumer ack; lease retry / nack  
- Default loopback bind; token required for non-loopback  
- Process-crash TX rollback/durability tests (not a power-loss proof)

---

## 3. Test results (executed this audit)

Environment: **Apple M4, darwin/arm64, Go 1.26.0**, `modernc.org/sqlite` v1.61.0

| Command | Result |
|---------|--------|
| `go vet ./...` | OK |
| `go test ./... -timeout 10m` | OK |
| `go test -race ./... -short` | OK |
| `go test ./internal/engine -run TestCrash_` | OK |
| `go run ./examples/payment` | DEMO OK |
| CLI smoke (`serve` + `rules add` + `publish` + `pending list`) | OK |
| Representative benches (`-benchtime=200ms`) | Sequence ~7765 e/s; Count ~4910 e/s; Ingest0 ~12832 e/s |
| `docker build -t waitmesh:0.1.0-rc` | OK |
| Docker run smoke (health public, `/v1/rules` 401/200) | OK |

---

## 4. Security findings

| ID | Severity | Finding | Disposition |
|----|----------|---------|-------------|
| S1 | **High** | `WAITMESH_CRASH_POINT` could `os.Exit` in production builds | **Fixed** — gated behind `-tags crashinject` |
| S2 | **High** | Rule cache could serve stale rules between commit and refresh | **Fixed** — invalidate before mutate + regression test |
| S3 | Medium | Bearer compare was not constant-time | **Fixed** — `subtle.ConstantTimeCompare` |
| S4 | Medium | Health probes blocked when token set | **Fixed** — live/ready public |
| S5 | Low | README/OpenAPI omitted some outbox routes | **Fixed** |
| S6 | Low | `*.test` binary artifact risk | **Fixed** — gitignore + removed |
| S7 | Info | Anyone with DB file access reads all events | Documented limitation |
| S8 | Info | Metrics exposed with token when auth on | Acceptable for v0.1 |
| S9 | Info | No dedicated disk-full integration harness | Documented; ENOSPC surfaces as driver errors |
| S10 | Info | `security@waitmesh.com` mailbox must exist operationally | Process note |

SQL uses bound parameters. No dashboard to secure. Direct deps are Apache/MIT/BSD-compatible (cobra, prometheus, modernc sqlite).

---

## 5. Open release blockers

**None remaining in code.**

Operational before public announce:

1. Confirm `security@waitmesh.com` is monitored.  
2. Human review of [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md) then tag (do not auto-publish).

---

## 6. Known limitations (must stay in README)

- Single-node SQLite writer; no clustering  
- At-least-once outbox only  
- No batch ingest API  
- No web UI  
- Crash tests ≠ power-loss certification  
- Backup is file-copy / `VACUUM INTO` (no backup API)

---

## 7. Performance snapshot

See [BENCHMARKS.md](BENCHMARKS.md). Durability settings unchanged (`FULL`). Throughput remains fsync-bound on one writer — expected.

---

## 8. GO / NO-GO

### **GO — release Waitmesh v0.1.0 as free Apache-2.0 source**

Rationale: core correctness paths are implemented and tested; release-blocking security/correctness issues found in audit were fixed; docs and packaging are honest about scope.

**Do not** auto-publish a GitHub Release from this session. Tag after checklist in [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md) and a local Docker smoke.
