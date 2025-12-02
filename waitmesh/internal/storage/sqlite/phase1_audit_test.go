package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

func TestPhase1_WALAndDurabilityDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dur.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mode, err := db.JournalMode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("journal_mode=%q want wal", mode)
	}
	sync, err := db.Synchronous()
	if err != nil {
		t.Fatal(err)
	}
	if sync < 2 {
		t.Fatalf("synchronous=%d want >= FULL(2)", sync)
	}
}

func TestPhase1_EventAndPendingAtomicRollback(t *testing.T) {
	// Pre-seed a pending row with start_event_id=e-new, then attempt to insert
	// event e-new + pending in one transaction. Pending UNIQUE must fail and
	// roll back the event insert as well.
	path := filepath.Join(t.TempDir(), "atomic.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		rule := domain.Rule{
			ID: "rule-1", Name: "r", Type: domain.RuleAbsence, Enabled: true, Version: 1,
			Start: "payment.initiated", Unless: "payment.completed", Within: time.Minute,
			WithinRaw: "1m", Emit: "payment.timeout", CreatedAt: now, UpdatedAt: now,
		}
		if err := sqlite.InsertRule(ctx, tx, rule); err != nil {
			return err
		}
		return sqlite.InsertPendingMatch(ctx, tx, domain.PendingMatch{
			ID: "pm-seed", RuleID: rule.ID, RuleVersion: 1, MatchType: domain.RuleAbsence,
			CorrelationKey: "k", StartEventID: "e-new", EmitType: "payment.timeout",
			Deadline: now.Add(time.Minute), Status: domain.MatchPending, CreatedAt: now,
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		ev := domain.Event{
			ID: "e-new", Type: "payment.initiated", Key: "k",
			Timestamp: now, ContentHash: "hash",
		}
		if err := sqlite.InsertEvent(ctx, tx, ev); err != nil {
			return err
		}
		return sqlite.InsertPendingMatch(ctx, tx, domain.PendingMatch{
			ID: "pm-new", RuleID: "rule-1", RuleVersion: 1, MatchType: domain.RuleAbsence,
			CorrelationKey: "k", StartEventID: "e-new", EmitType: "payment.timeout",
			Deadline: now.Add(time.Minute), Status: domain.MatchPending, CreatedAt: now,
		})
	})
	if err == nil {
		t.Fatal("expected unique constraint failure")
	}

	_, err = sqlite.GetEvent(ctx, db.SQL, "e-new")
	if err != domain.ErrEventNotFound {
		t.Fatalf("event must be rolled back, got err=%v", err)
	}
}

func TestPhase1_IdempotentContentHashAndConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idemp.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	h1, err := domain.CanonicalContentHash("a", "k", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		return sqlite.InsertEvent(ctx, tx, domain.Event{
			ID: "e1", Type: "a", Key: "k", Timestamp: now, ContentHash: h1,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	existing, err := sqlite.GetEvent(ctx, db.SQL, "e1")
	if err != nil || existing.ContentHash != h1 {
		t.Fatalf("existing=%+v err=%v", existing, err)
	}
	h2, _ := domain.CanonicalContentHash("a", "k2", nil)
	if h1 == h2 {
		t.Fatal("different keys must hash differently")
	}
}
