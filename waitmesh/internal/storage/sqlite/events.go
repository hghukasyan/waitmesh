package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/waitmesh/waitmesh/internal/domain"
)

func InsertEvent(ctx context.Context, tx *sql.Tx, e domain.Event) error {
	var producer *int64
	if e.ProducerTimestamp != nil {
		v := e.ProducerTimestamp.UnixNano()
		producer = &v
	}
	var data any
	if len(e.Data) > 0 {
		data = []byte(e.Data)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO events(id, type, key, timestamp_ns, producer_timestamp_ns, data, content_hash, created_at_ns)
		VALUES(?,?,?,?,?,?,?,?)`,
		e.ID, e.Type, e.Key, e.Timestamp.UnixNano(), producer, data, e.ContentHash, e.Timestamp.UnixNano(),
	)
	return err
}

// LookupContentHash returns only the content hash for idempotency checks.
func LookupContentHash(ctx context.Context, q querier, id string) (string, error) {
	var hash string
	err := q.QueryRowContext(ctx, `SELECT content_hash FROM events WHERE id=?`, id).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", domain.ErrEventNotFound
	}
	return hash, err
}

func GetEvent(ctx context.Context, q querier, id string) (domain.Event, error) {
	var e domain.Event
	var ts, created int64
	var producer sql.NullInt64
	var data []byte
	err := q.QueryRowContext(ctx, `
		SELECT id, type, key, timestamp_ns, producer_timestamp_ns, data, content_hash, created_at_ns
		FROM events WHERE id=?`, id).Scan(
		&e.ID, &e.Type, &e.Key, &ts, &producer, &data, &e.ContentHash, &created,
	)
	if err == sql.ErrNoRows {
		return e, domain.ErrEventNotFound
	}
	if err != nil {
		return e, err
	}
	e.Timestamp = time.Unix(0, ts).UTC()
	if producer.Valid {
		t := time.Unix(0, producer.Int64).UTC()
		e.ProducerTimestamp = &t
	}
	if data != nil {
		e.Data = json.RawMessage(data)
	}
	return e, nil
}

func ListEvents(ctx context.Context, db *DB, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.SQL.QueryContext(ctx, `
		SELECT id, type, key, timestamp_ns, producer_timestamp_ns, data, content_hash, created_at_ns
		FROM events ORDER BY timestamp_ns DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var ts, created int64
		var producer sql.NullInt64
		var data []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.Key, &ts, &producer, &data, &e.ContentHash, &created); err != nil {
			return nil, err
		}
		e.Timestamp = time.Unix(0, ts).UTC()
		if producer.Valid {
			t := time.Unix(0, producer.Int64).UTC()
			e.ProducerTimestamp = &t
		}
		if data != nil {
			e.Data = json.RawMessage(data)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
