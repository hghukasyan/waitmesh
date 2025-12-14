package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/waitmesh/waitmesh/internal/domain"
)

type ruleDef struct {
	Type   domain.RuleType `json:"type"`
	Start  string          `json:"start,omitempty"`
	Unless string          `json:"unless,omitempty"`
	Then   string          `json:"then,omitempty"`
	Event  string          `json:"event,omitempty"`
	Count  int             `json:"count,omitempty"`
	Within string          `json:"within"`
	Emit   string          `json:"emit"`
}

func InsertRule(ctx context.Context, tx *sql.Tx, r domain.Rule) error {
	def, err := marshalRuleDef(r)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO rules(id, name, type, definition_json, enabled, version, created_at_ns, updated_at_ns)
		VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, string(r.Type), def, boolToInt(r.Enabled), r.Version,
		r.CreatedAt.UnixNano(), r.UpdatedAt.UnixNano(),
	)
	if err != nil {
		return err
	}
	return upsertTriggers(ctx, tx, r)
}

func UpdateRule(ctx context.Context, tx *sql.Tx, r domain.Rule) error {
	def, err := marshalRuleDef(r)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE rules SET name=?, type=?, definition_json=?, enabled=?, version=?, updated_at_ns=?
		WHERE id=?`,
		r.Name, string(r.Type), def, boolToInt(r.Enabled), r.Version, r.UpdatedAt.UnixNano(), r.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrRuleNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM rule_triggers WHERE rule_id=?`, r.ID); err != nil {
		return err
	}
	return upsertTriggers(ctx, tx, r)
}

func SetRuleEnabled(ctx context.Context, tx *sql.Tx, id string, enabled bool, updatedAt time.Time) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE rules SET enabled=?, updated_at_ns=? WHERE id=?`,
		boolToInt(enabled), updatedAt.UnixNano(), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrRuleNotFound
	}
	return nil
}

func DeleteRule(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM rule_triggers WHERE rule_id=?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrRuleNotFound
	}
	return nil
}

func GetRule(ctx context.Context, q querier, id string) (domain.Rule, error) {
	return scanRule(q.QueryRowContext(ctx, `
		SELECT id, name, type, definition_json, enabled, version, created_at_ns, updated_at_ns
		FROM rules WHERE id=?`, id))
}

func GetRuleByName(ctx context.Context, q querier, name string) (domain.Rule, error) {
	return scanRule(q.QueryRowContext(ctx, `
		SELECT id, name, type, definition_json, enabled, version, created_at_ns, updated_at_ns
		FROM rules WHERE name=?`, name))
}

func ListRules(ctx context.Context, db *DB) ([]domain.Rule, error) {
	rows, err := db.SQL.QueryContext(ctx, `
		SELECT id, name, type, definition_json, enabled, version, created_at_ns, updated_at_ns
		FROM rules ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Rule
	for rows.Next() {
		r, err := scanRuleRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func RulesForEventType(ctx context.Context, tx *sql.Tx, eventType string) ([]domain.Rule, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT r.id, r.name, r.type, r.definition_json, r.enabled, r.version, r.created_at_ns, r.updated_at_ns
		FROM rules r
		JOIN rule_triggers t ON t.rule_id = r.id
		WHERE t.event_type = ? AND r.enabled = 1`, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Rule
	for rows.Next() {
		r, err := scanRuleRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListEnabledRules returns all enabled rules (for in-process rule cache rebuild).
func ListEnabledRules(ctx context.Context, q querier) ([]domain.Rule, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, name, type, definition_json, enabled, version, created_at_ns, updated_at_ns
		FROM rules WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Rule
	for rows.Next() {
		r, err := scanRuleRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func upsertTriggers(ctx context.Context, tx *sql.Tx, r domain.Rule) error {
	for _, t := range r.TriggerTypes() {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO rule_triggers(rule_id, event_type) VALUES(?,?)`, r.ID, t); err != nil {
			return err
		}
	}
	return nil
}

func marshalRuleDef(r domain.Rule) (string, error) {
	d := ruleDef{
		Type: r.Type, Start: r.Start, Unless: r.Unless, Then: r.Then,
		Event: r.Event, Count: r.Count, Within: r.WithinRaw, Emit: r.Emit,
	}
	if d.Within == "" && r.Within > 0 {
		d.Within = r.Within.String()
	}
	b, err := json.Marshal(d)
	return string(b), err
}

func scanRule(row *sql.Row) (domain.Rule, error) {
	var r domain.Rule
	var typ, def string
	var enabled int
	var created, updated int64
	err := row.Scan(&r.ID, &r.Name, &typ, &def, &enabled, &r.Version, &created, &updated)
	if err == sql.ErrNoRows {
		return r, domain.ErrRuleNotFound
	}
	if err != nil {
		return r, err
	}
	return hydrateRule(r, typ, def, enabled, created, updated)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRuleRows(rows rowScanner) (domain.Rule, error) {
	var r domain.Rule
	var typ, def string
	var enabled int
	var created, updated int64
	if err := rows.Scan(&r.ID, &r.Name, &typ, &def, &enabled, &r.Version, &created, &updated); err != nil {
		return r, err
	}
	return hydrateRule(r, typ, def, enabled, created, updated)
}

func hydrateRule(r domain.Rule, typ, def string, enabled int, created, updated int64) (domain.Rule, error) {
	var d ruleDef
	if err := json.Unmarshal([]byte(def), &d); err != nil {
		return r, fmt.Errorf("rule def: %w", err)
	}
	dur, err := time.ParseDuration(d.Within)
	if err != nil {
		return r, err
	}
	r.Type = domain.RuleType(typ)
	r.Enabled = enabled == 1
	r.CreatedAt = time.Unix(0, created).UTC()
	r.UpdatedAt = time.Unix(0, updated).UTC()
	r.Start = d.Start
	r.Unless = d.Unless
	r.Then = d.Then
	r.Event = d.Event
	r.Count = d.Count
	r.Within = dur
	r.WithinRaw = d.Within
	r.Emit = d.Emit
	return r, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
