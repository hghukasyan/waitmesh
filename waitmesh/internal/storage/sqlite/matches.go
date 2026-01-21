package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/waitmesh/waitmesh/internal/domain"
)

func InsertPendingMatch(ctx context.Context, tx *sql.Tx, m domain.PendingMatch) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO pending_matches(
			id, rule_id, rule_version, match_type, correlation_key, start_event_id,
			emit_type, deadline_ns, status, created_at_ns
		) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.RuleID, m.RuleVersion, string(m.MatchType), m.CorrelationKey, m.StartEventID,
		m.EmitType, m.Deadline.UnixNano(), string(m.Status), m.CreatedAt.UnixNano(),
	)
	return err
}

func CancelPendingByRuleKey(ctx context.Context, tx *sql.Tx, ruleID, key string, now time.Time) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE pending_matches SET status=?
		WHERE rule_id=? AND correlation_key=? AND status=?`,
		string(domain.MatchCancelled), ruleID, key, string(domain.MatchPending),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClaimPendingForFire atomically transitions pending → fired if still pending and overdue.
func ClaimPendingForFire(ctx context.Context, tx *sql.Tx, id string, now time.Time) (domain.PendingMatch, bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE pending_matches SET status=?
		WHERE id=? AND status=? AND deadline_ns<=?`,
		string(domain.MatchFired), id, string(domain.MatchPending), now.UnixNano(),
	)
	if err != nil {
		return domain.PendingMatch{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.PendingMatch{}, false, nil
	}
	m, err := getPending(ctx, tx, id)
	return m, true, err
}

// ClaimOldestSequenceMatch completes the oldest pending sequence match for rule+key
// whose deadline has not yet passed (deadline_ns >= now). Overdue matches are left
// for ExpireOverdueSequence and do not emit.
func ClaimOldestSequenceMatch(ctx context.Context, tx *sql.Tx, ruleID, key string, now time.Time) (domain.PendingMatch, bool, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM pending_matches
		WHERE rule_id=? AND correlation_key=? AND status=? AND match_type=?
		  AND deadline_ns>=?
		ORDER BY created_at_ns ASC LIMIT 1`,
		ruleID, key, string(domain.MatchPending), string(domain.RuleSequence), now.UnixNano(),
	).Scan(&id)
	if err == sql.ErrNoRows {
		return domain.PendingMatch{}, false, nil
	}
	if err != nil {
		return domain.PendingMatch{}, false, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE pending_matches SET status=?
		WHERE id=? AND status=? AND deadline_ns>=?`,
		string(domain.MatchCompleted), id, string(domain.MatchPending), now.UnixNano(),
	)
	if err != nil {
		return domain.PendingMatch{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.PendingMatch{}, false, nil
	}
	m, err := getPending(ctx, tx, id)
	return m, true, err
}

// ExpireOverdueSequence marks overdue sequence matches as expired (no emit).
func ExpireOverdueSequence(ctx context.Context, tx *sql.Tx, id string, now time.Time) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE pending_matches SET status=?
		WHERE id=? AND status=? AND match_type=? AND deadline_ns<=?`,
		string(domain.MatchExpired), id, string(domain.MatchPending),
		string(domain.RuleSequence), now.UnixNano(),
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetPendingMatch returns a match by id regardless of status.
func GetPendingMatch(ctx context.Context, q querier, id string) (domain.PendingMatch, error) {
	return getPending(ctx, q, id)
}

func CountFirings(ctx context.Context, db *DB) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM firings`).Scan(&n)
	return n, err
}

func CountOutbox(ctx context.Context, db *DB) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox`).Scan(&n)
	return n, err
}

func ListPending(ctx context.Context, db *DB, limit int) ([]domain.PendingMatch, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.SQL.QueryContext(ctx, `
		SELECT id, rule_id, rule_version, match_type, correlation_key, start_event_id,
			emit_type, deadline_ns, status, created_at_ns
		FROM pending_matches WHERE status=? ORDER BY deadline_ns ASC LIMIT ?`,
		string(domain.MatchPending), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPendingRows(rows)
}

func ListOverduePending(ctx context.Context, q querier, now time.Time, limit int) ([]domain.PendingMatch, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id, rule_id, rule_version, match_type, correlation_key, start_event_id,
			emit_type, deadline_ns, status, created_at_ns
		FROM pending_matches
		WHERE status=? AND deadline_ns<=?
		ORDER BY deadline_ns ASC LIMIT ?`,
		string(domain.MatchPending), now.UnixNano(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPendingRows(rows)
}

func NextDeadline(ctx context.Context, db *DB) (*time.Time, error) {
	var ns sql.NullInt64
	err := db.SQL.QueryRowContext(ctx, `
		SELECT MIN(deadline_ns) FROM pending_matches WHERE status=?`,
		string(domain.MatchPending),
	).Scan(&ns)
	if err != nil {
		return nil, err
	}
	if !ns.Valid {
		return nil, nil
	}
	t := time.Unix(0, ns.Int64).UTC()
	return &t, nil
}

func getPending(ctx context.Context, q querier, id string) (domain.PendingMatch, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, rule_id, rule_version, match_type, correlation_key, start_event_id,
			emit_type, deadline_ns, status, created_at_ns
		FROM pending_matches WHERE id=?`, id)
	var m domain.PendingMatch
	var mt, st string
	var deadline, created int64
	err := row.Scan(&m.ID, &m.RuleID, &m.RuleVersion, &mt, &m.CorrelationKey, &m.StartEventID,
		&m.EmitType, &deadline, &st, &created)
	if err != nil {
		return m, err
	}
	m.MatchType = domain.RuleType(mt)
	m.Status = domain.MatchStatus(st)
	m.Deadline = time.Unix(0, deadline).UTC()
	m.CreatedAt = time.Unix(0, created).UTC()
	return m, nil
}

func scanPendingRows(rows *sql.Rows) ([]domain.PendingMatch, error) {
	var out []domain.PendingMatch
	for rows.Next() {
		var m domain.PendingMatch
		var mt, st string
		var deadline, created int64
		if err := rows.Scan(&m.ID, &m.RuleID, &m.RuleVersion, &mt, &m.CorrelationKey, &m.StartEventID,
			&m.EmitType, &deadline, &st, &created); err != nil {
			return nil, err
		}
		m.MatchType = domain.RuleType(mt)
		m.Status = domain.MatchStatus(st)
		m.Deadline = time.Unix(0, deadline).UTC()
		m.CreatedAt = time.Unix(0, created).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}
