-- Waitmesh schema v1
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    key TEXT NOT NULL,
    timestamp_ns INTEGER NOT NULL,
    producer_timestamp_ns INTEGER,
    data BLOB,
    content_hash TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_type_ts ON events(type, timestamp_ns);
CREATE INDEX IF NOT EXISTS idx_events_key_ts ON events(key, timestamp_ns);

CREATE TABLE IF NOT EXISTS rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    definition_json TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    version INTEGER NOT NULL DEFAULT 1,
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS rule_triggers (
    rule_id TEXT NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    PRIMARY KEY (event_type, rule_id)
);

CREATE TABLE IF NOT EXISTS pending_matches (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL,
    rule_version INTEGER NOT NULL,
    match_type TEXT NOT NULL,
    correlation_key TEXT NOT NULL,
    start_event_id TEXT NOT NULL,
    emit_type TEXT NOT NULL,
    deadline_ns INTEGER NOT NULL,
    status TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL,
    UNIQUE (rule_id, start_event_id)
);

CREATE INDEX IF NOT EXISTS idx_pending_deadline ON pending_matches(status, deadline_ns);
CREATE INDEX IF NOT EXISTS idx_pending_rule_key ON pending_matches(rule_id, correlation_key, status);

CREATE TABLE IF NOT EXISTS counter_events (
    rule_id TEXT NOT NULL,
    correlation_key TEXT NOT NULL,
    event_id TEXT NOT NULL,
    timestamp_ns INTEGER NOT NULL,
    PRIMARY KEY (rule_id, correlation_key, event_id)
);

CREATE INDEX IF NOT EXISTS idx_counter_window ON counter_events(rule_id, correlation_key, timestamp_ns);

CREATE TABLE IF NOT EXISTS counter_state (
    rule_id TEXT NOT NULL,
    correlation_key TEXT NOT NULL,
    last_fire_event_id TEXT,
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (rule_id, correlation_key)
);

CREATE TABLE IF NOT EXISTS firings (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL,
    rule_version INTEGER NOT NULL,
    correlation_key TEXT NOT NULL,
    cause_event_id TEXT,
    pending_match_id TEXT,
    emit_type TEXT NOT NULL,
    fired_at_ns INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_firings_match ON firings(pending_match_id) WHERE pending_match_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS output_events (
    id TEXT PRIMARY KEY,
    firing_id TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    key TEXT NOT NULL,
    timestamp_ns INTEGER NOT NULL,
    data BLOB,
    created_at_ns INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_outputs_created ON output_events(created_at_ns);

CREATE TABLE IF NOT EXISTS outbox (
    id TEXT PRIMARY KEY,
    output_event_id TEXT NOT NULL UNIQUE REFERENCES output_events(id),
    created_at_ns INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS consumer_acks (
    consumer_id TEXT NOT NULL,
    output_event_id TEXT NOT NULL,
    acked_at_ns INTEGER NOT NULL,
    PRIMARY KEY (consumer_id, output_event_id)
);

CREATE TABLE IF NOT EXISTS consumer_leases (
    consumer_id TEXT NOT NULL,
    output_event_id TEXT NOT NULL,
    leased_until_ns INTEGER NOT NULL,
    delivery_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (consumer_id, output_event_id)
);

CREATE INDEX IF NOT EXISTS idx_leases_until ON consumer_leases(consumer_id, leased_until_ns);
