# Waitmesh Semantics

## Clocks

- **Processing time** (`timestamp`): server ingestion clock. Authoritative for deadlines, windows, ordering of evaluation.
- **Producer time** (`producer_timestamp`): optional, informational only. Never used for deadlines.

Injectable clocks are used in tests for determinism.

## Events

Fields: `id`, `type`, `key`, `timestamp` (server), `producer_timestamp?`, `data`.

### Validation

- `id`, `type`, `key` required, non-empty, length-bounded.
- `data` JSON object or null; total request size limited.
- Timestamps must be non-negative; future producer timestamps allowed but ignored for logic.

### Idempotency

Canonical content hash = SHA-256 over stable encoding of `(type, key, data)` (sorted JSON keys for `data`).

| Case | Result |
|------|--------|
| Same `id`, same content hash | Idempotent success (HTTP 200, existing event) |
| Same `id`, different content hash | Conflict (HTTP 409) |
| New `id` | Insert and evaluate |

### Ordering

Events are processed serially by the write pipeline in arrival order. Concurrent publishers are linearized by the writer. There is no global total order across restarts beyond committed timestamps and row IDs.

## Correlation

All rule matching correlates by **exact string equality of `key`**. Different keys never match each other.

## Rule types

### ABSENCE

```
start: A, unless: B, within: D, emit: C
```

When event type `A` with key `K` is ingested:

1. Create pending match `M` with `deadline = now + D`, status `pending`.
2. If a pending match already exists for `(rule, K, start_event_id)`, do not duplicate.

When event type `B` with key `K` is ingested:

1. Cancel all `pending` matches for this rule and key `K` (status → `cancelled`).
2. Cancelled matches never fire.

When a timer claims `M` at `deadline <= now` and status still `pending`:

1. Atomically set status → `fired`.
2. Emit one logical firing and output event type `C` with key `K`.
3. If another transaction already fired/cancelled, the conditional update affects 0 rows → no duplicate firing.

**Boundary:** `deadline == now` is overdue (fires). One nanosecond before does not.
Late `B` after fire does not undo the firing.
Late `B` before the timer claim still cancels (conditional update on `status='pending'`).

**Multiple starts, same key:** each start event creates its own pending match
(`UNIQUE(rule_id, start_event_id)`). Cancel clears all pending for rule+key.
Each match can fire independently if not cancelled.

**Duplicate start id:** idempotent ingest does not create a second pending match.

**Rule disable:** no new matches; existing pending continue until cancel/fire.

**Rule update:** pending matches keep `emit_type` / `rule_version` snapshot from creation.

### SEQUENCE

```
start: A, then: B, within: D, emit: C
```

When `A` with key `K` arrives: create pending match with deadline `now + D`
(`UNIQUE(rule_id, start_event_id)`).

When `B` with key `K` arrives:

1. Conditionally claim the **oldest** pending match for `(rule, K)` with
   `status='pending' AND deadline_ns >= now`.
2. On success: status → `completed`, emit one firing + output `C` + outbox row
   in the same transaction.
3. If no in-deadline pending exists: no-op (covers out-of-order `B` before `A`,
   and `B` after expiry).

When deadline passes without a successful `B`: scheduler sets status → `expired`
(no emit). `B` at exact deadline succeeds (`deadline_ns >= now`).

**Overlapping:** multiple starts for the same key create multiple pending matches;
each `B` completes exactly one (oldest). Extra `B` after all completed is a no-op.

### COUNT

```
event: A, count: N, within: W, emit: C
```

Sliding window of processing time length `W` per key, stored in `counter_events`
(indexed by `rule_id, correlation_key, timestamp_ns`). No full history scan.

On each `A` with key `K`:

1. `INSERT OR IGNORE` into the window (duplicate event ids do not double-count).
2. Prune rows with `timestamp_ns < now - W` (boundary: event at exactly `now-W` remains).
3. If `COUNT(*) >= N` and cause `event_id != last_fire_event_id` watermark → fire.
4. Persist firing + output + outbox + update watermark atomically.
5. Firing id = `StableCountFiringID(rule, key, cause_event_id)` (unique).

**Repeated firing:** while the window still contains ≥ N events, **each new**
distinct event that arrives causes another firing (watermark advances to that
event id). Re-ingest of the same event id is idempotent and does not re-fire.

## Overlapping matches

Multiple pending absence/sequence matches for the same rule and key are allowed when multiple start events arrive. Each start event creates its own pending match. Cancellation (absence) cancels **all** pending for that rule+key. Sequence completion consumes **one** match (oldest by deadline/created_at).

## Late events

Events with old producer timestamps are still evaluated at ingestion time. They can cancel/complete matches if they arrive before the match is claimed. They cannot retroactively change already-committed firings.

## Rule versions and updates

- Creating a rule: version 1.
- PATCH that changes definition: version++.
- Existing pending matches retain `rule_version` at creation; they fire/cancel with the definition they were created under (stored snapshot fields on the match).
- Disabled rules: no new matches; existing pending continue until cancel/fire/expire unless `cancel_pending=true` on disable.

## Derived event recursion

Output events are written to `output_events` and the outbox. By default they are **not** re-ingested into the engine. Optional `ingest_outputs` (future) would re-enter through the same write path with a recursion depth limit (default max depth 8) and cycle detection by rule chain.

## Cancellation vs timeout race

Exactly one outcome:

```sql
UPDATE pending_matches
SET status = 'fired', ...
WHERE id = ? AND status = 'pending' AND deadline <= ?
```

or

```sql
UPDATE pending_matches
SET status = 'cancelled', ...
WHERE id = ? AND status = 'pending'
```

Whichever transaction commits first wins; the other updates 0 rows.

## Outbox delivery

- Stable output ID = `out_` + firing id (deterministic).
- Output + outbox row are written in the **same transaction** as the logical firing.
- Consumers register durably (`consumers` table); poll auto-registers.
- Poll returns unacked messages for that consumer with a lease.
- Ack is per-consumer; ack by A does not remove delivery for B.
- Lease expiry (`leased_until <= now`) or Nack → at-least-once redelivery.
- Exactly-once external side effects are **not** claimed.
- Retention: `PurgeRetainedOutbox` removes outbox rows only when **all registered**
  consumers have acked and the output is older than the retain window.
  `output_events` remain for audit. With zero registered consumers, purge is a no-op.
