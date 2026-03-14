package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/waitmesh/waitmesh/internal/domain"
)

// Delivery is a leased output for a consumer.
type Delivery struct {
	Output        domain.OutputEvent `json:"output"`
	LeaseUntil    time.Time          `json:"lease_until"`
	DeliveryCount int                `json:"delivery_count"`
}

// UpsertConsumer registers or touches a durable consumer record.
func UpsertConsumer(ctx context.Context, tx *sql.Tx, consumerID string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO consumers(id, created_at_ns, last_seen_ns) VALUES(?,?,?)
		ON CONFLICT(id) DO UPDATE SET last_seen_ns=excluded.last_seen_ns`,
		consumerID, now.UnixNano(), now.UnixNano(),
	)
	return err
}

func ListConsumers(ctx context.Context, db *DB) ([]string, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT id FROM consumers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func PollOutbox(ctx context.Context, tx *sql.Tx, consumerID string, limit int, now time.Time, lease time.Duration) ([]Delivery, error) {
	if limit <= 0 {
		limit = 10
	}
	if err := UpsertConsumer(ctx, tx, consumerID, now); err != nil {
		return nil, err
	}
	// Available when never leased, or lease expired (now >= leased_until).
	rows, err := tx.QueryContext(ctx, `
		SELECT o.id, o.firing_id, o.type, o.key, o.timestamp_ns, o.data, o.created_at_ns
		FROM outbox x
		JOIN output_events o ON o.id = x.output_event_id
		LEFT JOIN consumer_acks a
			ON a.consumer_id = ? AND a.output_event_id = o.id
		LEFT JOIN consumer_leases l
			ON l.consumer_id = ? AND l.output_event_id = o.id
		WHERE a.output_event_id IS NULL
		  AND (l.output_event_id IS NULL OR l.leased_until_ns <= ?)
		ORDER BY o.created_at_ns ASC
		LIMIT ?`,
		consumerID, consumerID, now.UnixNano(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	var outs []domain.OutputEvent
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
		outs = append(outs, o)
		ids = append(ids, o.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	leaseUntil := now.Add(lease)
	var deliveries []Delivery
	for i, id := range ids {
		var count int
		err := tx.QueryRowContext(ctx, `
			SELECT delivery_count FROM consumer_leases
			WHERE consumer_id=? AND output_event_id=?`, consumerID, id).Scan(&count)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		count++
		_, err = tx.ExecContext(ctx, `
			INSERT INTO consumer_leases(consumer_id, output_event_id, leased_until_ns, delivery_count)
			VALUES(?,?,?,?)
			ON CONFLICT(consumer_id, output_event_id) DO UPDATE SET
				leased_until_ns=excluded.leased_until_ns,
				delivery_count=excluded.delivery_count`,
			consumerID, id, leaseUntil.UnixNano(), count,
		)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, Delivery{
			Output:        outs[i],
			LeaseUntil:    leaseUntil,
			DeliveryCount: count,
		})
	}
	return deliveries, nil
}

func AckOutput(ctx context.Context, tx *sql.Tx, consumerID, outputID string, now time.Time) error {
	if err := UpsertConsumer(ctx, tx, consumerID, now); err != nil {
		return err
	}
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM output_events WHERE id=?`, outputID).Scan(&exists)
	if err == sql.ErrNoRows {
		return domain.ErrLeaseNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO consumer_acks(consumer_id, output_event_id, acked_at_ns)
		VALUES(?,?,?)`, consumerID, outputID, now.UnixNano())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		DELETE FROM consumer_leases WHERE consumer_id=? AND output_event_id=?`,
		consumerID, outputID)
	return err
}

// NackOutput releases a lease immediately so the message can be retried
// (at-least-once; may redeliver to the same consumer).
func NackOutput(ctx context.Context, tx *sql.Tx, consumerID, outputID string, now time.Time) error {
	if err := UpsertConsumer(ctx, tx, consumerID, now); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE consumer_leases SET leased_until_ns=?
		WHERE consumer_id=? AND output_event_id=?`,
		now.UnixNano(), consumerID, outputID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// No active lease — still OK for retry semantics (idempotent nack).
		return nil
	}
	return nil
}

// PurgeRetainedOutbox removes outbox rows that every registered consumer has
// acknowledged and that are older than retainBefore. output_events rows are kept
// for audit; leases/acks for purged outputs are cleaned up.
// If no consumers are registered, nothing is purged (safe default).
func PurgeRetainedOutbox(ctx context.Context, tx *sql.Tx, retainBefore time.Time) (int64, error) {
	var consumerCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM consumers`).Scan(&consumerCount); err != nil {
		return 0, err
	}
	if consumerCount == 0 {
		return 0, nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT x.output_event_id
		FROM outbox x
		JOIN output_events o ON o.id = x.output_event_id
		WHERE o.created_at_ns < ?
		  AND (
			SELECT COUNT(*) FROM consumer_acks a WHERE a.output_event_id = x.output_event_id
		  ) >= (
			SELECT COUNT(*) FROM consumers
		  )`,
		retainBefore.UnixNano(),
	)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var purged int64
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE output_event_id=?`, id); err != nil {
			return purged, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM consumer_leases WHERE output_event_id=?`, id); err != nil {
			return purged, err
		}
		// Keep consumer_acks for idempotent re-ack; optional prune:
		if _, err := tx.ExecContext(ctx, `DELETE FROM consumer_acks WHERE output_event_id=?`, id); err != nil {
			return purged, err
		}
		purged++
	}
	return purged, nil
}
