package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type DB struct {
	SQL *sql.DB
}

func Open(path string) (*DB, error) {
	dsn := path
	switch {
	case path == ":memory:":
		dsn = "file:waitmesh?mode=memory&cache=shared"
	case !strings.Contains(path, "?"):
		dsn = path + "?_pragma=busy_timeout(5000)"
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection: SQLite single-writer; keeps transactional semantics simple.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	db := &DB{SQL: sqlDB}
	if err := db.configure(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := db.Migrate(context.Background()); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) configure() error {
	pragmas := []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA synchronous=FULL",
	}
	for _, p := range pragmas {
		if _, err := db.SQL.Exec(p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	// WAL is required for durable concurrent readers + crash-safe writes on file DBs.
	var mode string
	if err := db.SQL.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil {
		return fmt.Errorf("PRAGMA journal_mode=WAL: %w", err)
	}
	mode = strings.ToLower(mode)
	if mode != "wal" && mode != "memory" {
		return fmt.Errorf("expected journal_mode=WAL (or memory), got %q", mode)
	}
	var sync int
	if err := db.SQL.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil {
		return fmt.Errorf("PRAGMA synchronous: %w", err)
	}
	// FULL=2 (or EXTRA=3). REJECT OFF/NORMAL for durability defaults.
	if sync < 2 {
		return fmt.Errorf("expected synchronous>=FULL (2), got %d", sync)
	}
	return nil
}

// JournalMode returns the active SQLite journal mode (for tests/diagnostics).
func (db *DB) JournalMode() (string, error) {
	var mode string
	err := db.SQL.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	return mode, err
}

// Synchronous returns the active synchronous level (0=OFF … 3=EXTRA).
func (db *DB) Synchronous() (int, error) {
	var sync int
	err := db.SQL.QueryRow(`PRAGMA synchronous`).Scan(&sync)
	return sync, err
}

func (db *DB) Close() error { return db.SQL.Close() }

func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.SQL.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`); err != nil {
		return err
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		var version int
		if _, err := fmt.Sscanf(e.Name(), "%d_", &version); err != nil || version == 0 {
			continue
		}
		var applied int
		err := db.SQL.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version=?`, version).Scan(&applied)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("check migration %d: %w", version, err)
		}

		body, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := db.SQL.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			version, time.Now().UnixNano()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
