package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/waitmesh/waitmesh/internal/domain"
)

func InsertFiring(ctx context.Context, tx *sql.Tx, f domain.Firing) error {
	var cause, match any
	if f.CauseEventID != "" {
		cause = f.CauseEventID
	}
	if f.PendingMatchID != "" {
		match = f.PendingMatchID
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO firings(id, rule_id, rule_version, correlation_key, cause_event_id, pending_match_id, emit_type, fired_at_ns)
		VALUES(?,?,?,?,?,?,?,?)`,
		f.ID, f.RuleID, f.RuleVersion, f.CorrelationKey, cause, match, f.EmitType, f.FiredAt.UnixNano(),
	)
	return err
}

func InsertOutputAndOutbox(ctx context.Context, tx *sql.Tx, o domain.OutputEvent) error {
	var data any
	if len(o.Data) > 0 {
		data = []byte(o.Data)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO output_events(id, firing_id, type, key, timestamp_ns, data, created_at_ns)
		VALUES(?,?,?,?,?,?,?)`,
		o.ID, o.FiringID, o.Type, o.Key, o.Timestamp.UnixNano(), data, o.CreatedAt.UnixNano(),
	); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO outbox(id, output_event_id, created_at_ns) VALUES(?,?,?)`,
		o.ID, o.ID, o.CreatedAt.UnixNano(),
	)
	return err
}

func ListFirings(ctx context.Context, db *DB, limit int) ([]domain.Firing, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.SQL.QueryContext(ctx, `
		SELECT id, rule_id, rule_version, correlation_key, cause_event_id, pending_match_id, emit_type, fired_at_ns
		FROM firings ORDER BY fired_at_ns DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Firing
	for rows.Next() {
		var f domain.Firing
		var cause, match sql.NullString
		var fired int64
		if err := rows.Scan(&f.ID, &f.RuleID, &f.RuleVersion, &f.CorrelationKey, &cause, &match, &f.EmitType, &fired); err != nil {
			return nil, err
		}
		if cause.Valid {
			f.CauseEventID = cause.String
		}
		if match.Valid {
			f.PendingMatchID = match.String
		}
		f.FiredAt = time.Unix(0, fired).UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

func ListOutputs(ctx context.Context, db *DB, limit int) ([]domain.OutputEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.SQL.QueryContext(ctx, `
		SELECT id, firing_id, type, key, timestamp_ns, data, created_at_ns
		FROM output_events ORDER BY created_at_ns DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OutputEvent
	for rows.Next() {
		var o domain.OutputEvent
		var ts, created int64
		var data []byte
		if err := rows.Scan(&o.ID, &o.FiringID, &o.Type, &o.Key, &ts, &data, &created); err != nil {
			return nil, err
		}
		o.Timestamp = time.Unix(0, ts).UTC()
		o.CreatedAt = time.Unix(0, created).UTC()
		if data != nil {
			o.Data = json.RawMessage(data)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func AddCounterEvent(ctx context.Context, tx *sql.Tx, ruleID, key, eventID string, ts time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO counter_events(rule_id, correlation_key, event_id, timestamp_ns)
		VALUES(?,?,?,?)`, ruleID, key, eventID, ts.UnixNano())
	return err
}

func PruneCounterWindow(ctx context.Context, tx *sql.Tx, ruleID, key string, cutoff time.Time) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM counter_events WHERE rule_id=? AND correlation_key=? AND timestamp_ns<?`,
		ruleID, key, cutoff.UnixNano())
	return err
}

func CountInWindow(ctx context.Context, tx *sql.Tx, ruleID, key string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM counter_events WHERE rule_id=? AND correlation_key=?`, ruleID, key).Scan(&n)
	return n, err
}

// CountInWindowSince counts events in the sliding window without requiring a prior prune.
func CountInWindowSince(ctx context.Context, tx *sql.Tx, ruleID, key string, cutoff time.Time) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM counter_events
		WHERE rule_id=? AND correlation_key=? AND timestamp_ns>=?`,
		ruleID, key, cutoff.UnixNano()).Scan(&n)
	return n, err
}

func GetLastFireEventID(ctx context.Context, tx *sql.Tx, ruleID, key string) (string, error) {
	var id sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT last_fire_event_id FROM counter_state WHERE rule_id=? AND correlation_key=?`,
		ruleID, key).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if id.Valid {
		return id.String, nil
	}
	return "", nil
}

func SetLastFireEventID(ctx context.Context, tx *sql.Tx, ruleID, key, eventID string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO counter_state(rule_id, correlation_key, last_fire_event_id, updated_at_ns)
		VALUES(?,?,?,?)
		ON CONFLICT(rule_id, correlation_key) DO UPDATE SET
			last_fire_event_id=excluded.last_fire_event_id,
			updated_at_ns=excluded.updated_at_ns`,
		ruleID, key, eventID, now.UnixNano())
	return err
}
