# Waitmesh Reliability

## Invariants

1. Accepting an input event commits event row + rule side effects atomically (or neither).
2. Duplicate event ID + same hash → idempotent success; different hash → conflict.
3. Absence/sequence logical firings are unique per pending match (`firings.pending_match_id`).
4. Cancel vs timeout: exactly one of `cancelled` / `fired` wins via conditional `UPDATE`.
5. Outbox delivery is **at-least-once** per consumer; never exactly-once external.
6. WAL + `synchronous=FULL` on every process open.

## Stress tests

Package: `internal/engine` (`stress_test.go`).

| Test | Coverage |
|------|----------|
| `TestStress_100kEventsMultiKeys` | 100k events, 1000 keys, pending+event counts |
| `TestStress_1000ActiveRules` | 1000 absence rules, indexed non-match + match |
| `TestStress_ConcurrentPublishersAndTimers` | 16 publishers + timer sweep |
| `TestStress_CancelVsTimeoutUnderLoad` | concurrent cancel vs expire; partition invariant |
| `TestStress_LargePendingBacklogRestart` | 5k pending, reopen, drain timers |
| `TestStress_CancelledContextAndShutdown` | canceled ctx + graceful `Close` under load |
| `TestStress_SQLiteBusyAndQueueFull` | bounded queue backpressure (`ErrQueueFull`) |
| `TestStress_OutboxRetryAck` | nack → redelivery → ack |

Heavy tests skip under `-short`.

```bash
go test ./internal/engine -run TestStress_ -timeout 10m
go test ./internal/engine -short   # skips 100k / 1000-rules
```

## Process crash tests

Helper binary: `cmd/waitmesh-crashworker`.

Crash injection is **compiled out** of default builds. Tests build the helper with
`-tags crashinject`. Production binaries must **not** use that tag.

Injection points (`WAITMESH_CRASH_POINT` with crashinject builds only):

- `before_commit` — `os.Exit` after work, before `Commit`
- `after_commit` — exit after successful commit
- `during_timer` — exit mid timer TX (before commit)

These validate process-crash / TX rollback behavior. They do **not** by themselves
prove durability across sudden power loss; that rests on SQLite `synchronous=FULL`
and underlying storage.

| Test | Expectation |
|------|-------------|
| `TestCrash_BeforeCommitNoPartialEvent` | no event, no pending after reopen |
| `TestCrash_AfterCommitDurable` | event + pending survive |
| `TestCrash_DuringTimerNoDuplicateFiring` | ≤1 firing after recovery sweep |
| `TestCrash_OutboxDeliveryRecoverable` | independent consumer can poll+ack |

```bash
go test ./internal/engine -run TestCrash_ -timeout 5m -count=1
```

## Disk-full / busy

- Queue full → `ErrQueueFull` (explicit backpressure); callers must retry.
- SQLite `busy_timeout=5000`; single writer minimizes busy conflicts.
- True ENOSPC is surfaced as driver errors from `Commit`/`Exec` (no silent success).

## Backup and restore

1. Stop writers (`waitmesh` process) or ensure no active writers.
2. Copy `waitmesh.db` plus `waitmesh.db-wal` / `waitmesh.db-shm` if present
   (or use `sqlite3 waitmesh.db "VACUUM INTO 'backup.db';"`).
3. Restore by replacing files while the server is stopped, then start `waitmesh serve`.

Online hot backup without checkpointing is operator responsibility; v0.1 has no
built-in backup API.

## Correctness bugs found in this phase

None in production semantics. Crash injection confirmed rollback-before-commit and durable-after-commit behavior.

## Limits

- One writer: publishers contend on the queue, not on lost updates.
- Timer batches are bounded; very large overdue backlogs drain over multiple sweeps.
- Outbox retention purge requires registered consumers (safe no-op otherwise).
