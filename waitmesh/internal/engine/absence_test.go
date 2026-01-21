package engine_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

func absenceRule(t *testing.T, eng *engine.Engine, within string) domain.Rule {
	t.Helper()
	r, err := eng.CreateRule(context.Background(), domain.RuleInput{
		Name: "missing-payment", Type: domain.RuleAbsence,
		Start: "payment.initiated", Unless: "payment.completed",
		Within: within, Emit: "payment.timeout",
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAbsence_CreatesPendingMatchAtomically(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "5m")

	_, err := eng.Ingest(ctx, domain.EventInput{ID: "init-1", Type: "payment.initiated", Key: "ord-1"})
	if err != nil {
		t.Fatal(err)
	}

	evs, err := sqlite.ListEvents(ctx, eng.DB(), 10)
	if err != nil || len(evs) != 1 {
		t.Fatalf("events=%v err=%v", evs, err)
	}
	pending, err := sqlite.ListPending(ctx, eng.DB(), 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	m := pending[0]
	if m.CorrelationKey != "ord-1" || m.StartEventID != "init-1" || m.EmitType != "payment.timeout" {
		t.Fatalf("match=%+v", m)
	}
	wantDeadline := clk.Now().Add(5 * time.Minute)
	if !m.Deadline.Equal(wantDeadline) {
		t.Fatalf("deadline=%s want=%s", m.Deadline, wantDeadline)
	}
}

func TestAbsence_CancelsBeforeDeadline(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "5m")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "ord-1"})
	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	matchID := pending[0].ID

	_, err := eng.Ingest(ctx, domain.EventInput{ID: "c1", Type: "payment.completed", Key: "ord-1"})
	if err != nil {
		t.Fatal(err)
	}

	m, err := sqlite.GetPendingMatch(ctx, eng.DB().SQL, matchID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != domain.MatchCancelled {
		t.Fatalf("status=%s", m.Status)
	}
	clk.Advance(5 * time.Minute)
	n, err := eng.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 0 {
		t.Fatalf("firings=%d", firings)
	}
}

func TestAbsence_FiresAfterDeadline(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "3s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "ord-1"})
	clk.Advance(3 * time.Second)
	n, err := eng.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}

	firings, err := sqlite.ListFirings(ctx, eng.DB(), 10)
	if err != nil || len(firings) != 1 {
		t.Fatalf("firings=%v err=%v", firings, err)
	}
	if firings[0].EmitType != "payment.timeout" || firings[0].CorrelationKey != "ord-1" {
		t.Fatalf("firing=%+v", firings[0])
	}
	outs, err := sqlite.ListOutputs(ctx, eng.DB(), 10)
	if err != nil || len(outs) != 1 || outs[0].Type != "payment.timeout" {
		t.Fatalf("outputs=%v err=%v", outs, err)
	}
	obox, err := sqlite.CountOutbox(ctx, eng.DB())
	if err != nil || obox != 1 {
		t.Fatalf("outbox=%d err=%v", obox, err)
	}
}

func TestAbsence_DoesNotFireTwice(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "1s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "k"})
	clk.Advance(time.Second)
	n1, _ := eng.ProcessOverdueTimers(ctx, 10)
	n2, _ := eng.ProcessOverdueTimers(ctx, 10)
	n3, _ := eng.ProcessOverdueTimers(ctx, 10)
	if n1 != 1 || n2 != 0 || n3 != 0 {
		t.Fatalf("n1=%d n2=%d n3=%d", n1, n2, n3)
	}
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 1 {
		t.Fatalf("firings=%d", firings)
	}
	outs, _ := sqlite.CountOutbox(ctx, eng.DB())
	if outs != 1 {
		t.Fatalf("outbox=%d", outs)
	}
}

func TestAbsence_RestartBeforeDeadline(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "before.db")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	db1, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	eng1 := engine.New(db1, clk, log, 64)
	absenceRule(t, eng1, "10s")
	_, _ = eng1.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "k"})
	eng1.Close()
	db1.Close()

	clk.Advance(3 * time.Second) // still before deadline
	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	eng2 := engine.New(db2, clk, log, 64)
	defer eng2.Close()

	n, err := eng2.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 0 {
		t.Fatalf("should not fire early: n=%d err=%v", n, err)
	}
	pending, _ := sqlite.ListPending(ctx, eng2.DB(), 10)
	if len(pending) != 1 {
		t.Fatalf("pending restored=%d", len(pending))
	}

	clk.Advance(7 * time.Second) // now at deadline
	n, err = eng2.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestAbsence_RestartAfterDeadline(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "after.db")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	db1, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	eng1 := engine.New(db1, clk, log, 64)
	absenceRule(t, eng1, "2s")
	_, _ = eng1.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "k"})
	eng1.Close()
	db1.Close()

	clk.Advance(5 * time.Second) // overdue while down
	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	eng2 := engine.New(db2, clk, log, 64)
	defer eng2.Close()

	n, err := eng2.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	// Second open must not duplicate
	eng2.Close()
	db2.Close()
	db3, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db3.Close()
	eng3 := engine.New(db3, clk, log, 64)
	defer eng3.Close()
	n, _ = eng3.ProcessOverdueTimers(ctx, 10)
	if n != 0 {
		t.Fatalf("duplicate fire on re-open: %d", n)
	}
	firings, _ := sqlite.CountFirings(ctx, eng3.DB())
	if firings != 1 {
		t.Fatalf("firings=%d", firings)
	}
}

func TestAbsence_ConcurrentCancelVsTimeout(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "1s")
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "k"})
	clk.Advance(time.Second)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = eng.ProcessOverdueTimers(ctx, 10)
	}()
	go func() {
		defer wg.Done()
		_, _ = eng.Ingest(ctx, domain.EventInput{ID: "c1", Type: "payment.completed", Key: "k"})
	}()
	wg.Wait()

	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 0 {
		t.Fatalf("pending left=%d", len(pending))
	}
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings > 1 {
		t.Fatalf("duplicate firings=%d", firings)
	}
}

func TestAbsence_MultipleCorrelationKeys(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "1s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a1", Type: "payment.initiated", Key: "ord-a"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b1", Type: "payment.initiated", Key: "ord-b"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a2", Type: "payment.completed", Key: "ord-a"})

	clk.Advance(time.Second)
	n, err := eng.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("want only ord-b timeout: n=%d err=%v", n, err)
	}
	firings, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(firings) != 1 || firings[0].CorrelationKey != "ord-b" {
		t.Fatalf("firings=%v", firings)
	}
}

func TestAbsence_DuplicateInputEvents(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "5s")

	in := domain.EventInput{ID: "dup", Type: "payment.initiated", Key: "k"}
	r1, err := eng.Ingest(ctx, in)
	if err != nil || !r1.Created {
		t.Fatalf("r1=%v err=%v", r1, err)
	}
	r2, err := eng.Ingest(ctx, in)
	if err != nil || !r2.Idempotent {
		t.Fatalf("r2=%v err=%v", r2, err)
	}
	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 1 {
		t.Fatalf("idempotent ingest must not duplicate pending: %d", len(pending))
	}
}

func TestAbsence_ExactDeadlineBoundary(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "1s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "k"})
	deadline := start.Add(time.Second)

	// One nanosecond before deadline: must not fire
	clk.Set(deadline.Add(-time.Nanosecond))
	n, _ := eng.ProcessOverdueTimers(ctx, 10)
	if n != 0 {
		t.Fatalf("fired before deadline: %d", n)
	}

	// Exact deadline: fires (deadline_ns <= now)
	clk.Set(deadline)
	n, err := eng.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("exact boundary n=%d err=%v", n, err)
	}
}

func TestAbsence_LateUnlessAfterFire(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "1s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "k"})
	clk.Advance(time.Second)
	_, _ = eng.ProcessOverdueTimers(ctx, 10)
	_, err := eng.Ingest(ctx, domain.EventInput{ID: "late", Type: "payment.completed", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 1 {
		t.Fatalf("late unless must not undo firing: %d", firings)
	}
}

func TestAbsence_RuleDisableStopsNewMatches(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	rule := absenceRule(t, eng, "5s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "old"})
	if err := eng.SetRuleEnabled(ctx, rule.ID, false); err != nil {
		t.Fatal(err)
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i2", Type: "payment.initiated", Key: "new"})
	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 1 || pending[0].CorrelationKey != "old" {
		t.Fatalf("disabled rule must not create matches: %v", pending)
	}
	// Existing pending still fires
	clk.Advance(5 * time.Second)
	n, _ := eng.ProcessOverdueTimers(ctx, 10)
	if n != 1 {
		t.Fatalf("existing pending should still fire: %d", n)
	}
}

func TestAbsence_RuleUpdateKeepsMatchEmitSnapshot(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	rule := absenceRule(t, eng, "1s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "k"})
	_, err := eng.UpdateRule(ctx, rule.ID, domain.RuleInput{
		Name: "missing-payment", Type: domain.RuleAbsence,
		Start: "payment.initiated", Unless: "payment.completed",
		Within: "1s", Emit: "payment.timeout.v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Second)
	_, _ = eng.ProcessOverdueTimers(ctx, 10)
	firings, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(firings) != 1 || firings[0].EmitType != "payment.timeout" {
		t.Fatalf("match must fire with snapshot emit, got %+v", firings)
	}
}

func TestAbsence_CrashRecoveryNoDuplicate(t *testing.T) {
	// Simulate crash after fire committed: reopen and sweep again.
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "crash.db")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(db, clk, log, 64)
	absenceRule(t, eng, "1s")
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "k"})
	clk.Advance(time.Second)
	n, _ := eng.ProcessOverdueTimers(ctx, 10)
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
	eng.Close()
	db.Close()

	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	eng2 := engine.New(db2, clk, log, 64)
	defer eng2.Close()
	n, _ = eng2.ProcessOverdueTimers(ctx, 10)
	if n != 0 {
		t.Fatalf("crash recovery duplicate: %d", n)
	}
	firings, _ := sqlite.CountFirings(ctx, eng2.DB())
	outs, _ := sqlite.CountOutbox(ctx, eng2.DB())
	if firings != 1 || outs != 1 {
		t.Fatalf("firings=%d outbox=%d", firings, outs)
	}
}

func TestAbsence_OverlappingSameKeyFireIndependently(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "1s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "payment.initiated", Key: "same"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i2", Type: "payment.initiated", Key: "same"})
	clk.Advance(time.Second)
	n, _ := eng.ProcessOverdueTimers(ctx, 10)
	if n != 2 {
		t.Fatalf("want 2 firings for 2 starts, got %d", n)
	}
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 2 {
		t.Fatalf("firings=%d", firings)
	}
}

func TestAbsence_ManyKeysStress(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	absenceRule(t, eng, "1s")
	const n = 100
	for i := 0; i < n; i++ {
		_, err := eng.Ingest(ctx, domain.EventInput{
			ID: fmt.Sprintf("e-%d", i), Type: "payment.initiated", Key: fmt.Sprintf("k-%d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	clk.Advance(time.Second)
	got, err := eng.ProcessOverdueTimers(ctx, n+10)
	if err != nil || got != n {
		t.Fatalf("got=%d err=%v", got, err)
	}
}
