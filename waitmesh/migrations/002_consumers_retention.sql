-- Phase 3: durable consumer registration + retention helpers
CREATE TABLE IF NOT EXISTS consumers (
    id TEXT PRIMARY KEY,
    created_at_ns INTEGER NOT NULL,
    last_seen_ns INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_consumer_acks_output ON consumer_acks(output_event_id);
CREATE INDEX IF NOT EXISTS idx_outbox_created ON outbox(created_at_ns);
