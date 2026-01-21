package engine_test

import (
	"context"
	"encoding/json"
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

func testEngine(t *testing.T, clk clock.Clock) (*engine.Engine, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.New(db, clk, log, 256)
	return eng, func() { eng.Close(); db.Close() }
}

func TestAbsenceTimeoutAndCancel(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()

	rule, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "missing-payment", Type: domain.RuleAbsence,
		Start: "payment.initiated", Unless: "payment.completed",
		Within: "10s", Emit: "payment.timeout",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = rule

	_, err = eng.Ingest(ctx, domain.EventInput{ID: "e1", Type: "payment.initiated", Key: "pay-1"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := sqlite.ListPending(ctx, eng.DB(), 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}

	clk.Advance(10 * time.Second)
	n, err := eng.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("fired n=%d err=%v", n, err)
	}
	firings, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(firings) != 1 || firings[0].EmitType != "payment.timeout" {
		t.Fatalf("firings=%v", firings)
	}

	// Cancel path
	clk.Advance(1 * time.Second)
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "e2", Type: "payment.initiated", Key: "pay-2"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "e3", Type: "payment.completed", Key: "pay-2"})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(10 * time.Second)
	n, err = eng.ProcessOverdueTimers(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 fires after cancel, got %d", n)
	}
	firings, _ = sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(firings) != 1 {
		t.Fatalf("expected still 1 firing, got %d", len(firings))
	}
}

func TestDuplicateIDIdempotentAndConflict(t *testing.T) {
	clk := clock.NewFake(time.Now().UTC())
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()

	in := domain.EventInput{ID: "dup", Type: "a", Key: "k", Data: json.RawMessage(`{"x":1}`)}
	r1, err := eng.Ingest(ctx, in)
	if err != nil || !r1.Created {
		t.Fatalf("r1=%v err=%v", r1, err)
	}
	r2, err := eng.Ingest(ctx, in)
	if err != nil || !r2.Idempotent {
		t.Fatalf("r2=%v err=%v", r2, err)
	}
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "dup", Type: "a", Key: "k", Data: json.RawMessage(`{"x":2}`)})
	if err != domain.ErrEventConflict {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestSequence(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()

	_, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "seq", Type: domain.RuleSequence,
		Start: "order.created", Then: "order.paid", Within: "5s", Emit: "order.ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "s1", Type: "order.created", Key: "o1"})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(2 * time.Second)
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "s2", Type: "order.paid", Key: "o1"})
	if err != nil {
		t.Fatal(err)
	}
	firings, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(firings) != 1 {
		t.Fatalf("firings=%v", firings)
	}
}

func TestCount(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()

	_, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "burst", Type: domain.RuleCount,
		Event: "click", Count: 3, Within: "1m", Emit: "click.burst",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		_, err = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("c%d", i), Type: "click", Key: "u1"})
		if err != nil {
			t.Fatal(err)
		}
		clk.Advance(time.Second)
	}
	firings, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(firings) != 1 {
		t.Fatalf("want 1 firing, got %d", len(firings))
	}
}

func TestTimeoutCancelRace(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()

	_, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "race", Type: domain.RuleAbsence,
		Start: "a", Unless: "b", Within: "1s", Emit: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "r1", Type: "a", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(1 * time.Second)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = eng.ProcessOverdueTimers(ctx, 10)
	}()
	go func() {
		defer wg.Done()
		_, _ = eng.Ingest(ctx, domain.EventInput{ID: "r2", Type: "b", Key: "k"})
	}()
	wg.Wait()

	firings, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 0 {
		t.Fatalf("pending must be empty after race, got %d", len(pending))
	}
	if len(firings) > 1 {
		t.Fatalf("duplicate firings: %d", len(firings))
	}
}

func TestRestartRecovery(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery.db")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	db1, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	eng1 := engine.New(db1, clk, log, 64)
	_, err = eng1.CreateRule(ctx, domain.RuleInput{
		Name: "rec", Type: domain.RuleAbsence,
		Start: "a", Unless: "b", Within: "5s", Emit: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng1.Ingest(ctx, domain.EventInput{ID: "x1", Type: "a", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	eng1.Close()
	db1.Close()

	clk.Advance(5 * time.Second)
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
}

func TestIndependentConsumers(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()

	_, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs", Type: domain.RuleAbsence,
		Start: "a", Unless: "b", Within: "1s", Emit: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "a", Key: "k"})
	clk.Advance(time.Second)
	_, _ = eng.ProcessOverdueTimers(ctx, 10)

	d1, err := eng.PollOutputs(ctx, "consumer-a", 10, time.Minute)
	if err != nil || len(d1) != 1 {
		t.Fatalf("d1=%v err=%v", d1, err)
	}
	if err := eng.AckOutput(ctx, "consumer-a", d1[0].Output.ID); err != nil {
		t.Fatal(err)
	}
	d2, err := eng.PollOutputs(ctx, "consumer-b", 10, time.Minute)
	if err != nil || len(d2) != 1 {
		t.Fatalf("consumer-b should still see output: %v err=%v", d2, err)
	}
	d1b, err := eng.PollOutputs(ctx, "consumer-a", 10, time.Minute)
	if err != nil || len(d1b) != 0 {
		t.Fatalf("acked consumer should see nothing: %v", d1b)
	}
}

func TestConcurrentPublishers(t *testing.T) {
	clk := clock.NewFake(time.Now().UTC())
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	var wg sync.WaitGroup
	errCh := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := eng.Ingest(ctx, domain.EventInput{
				ID: fmt.Sprintf("p-%d", i), Type: "t", Key: "k",
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	evs, _ := sqlite.ListEvents(ctx, eng.DB(), 100)
	if len(evs) != 50 {
		t.Fatalf("want 50 events, got %d", len(evs))
	}
}

func TestOverlappingAbsenceMatches(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "ov", Type: domain.RuleAbsence,
		Start: "a", Unless: "b", Within: "10s", Emit: "c",
	})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "o1", Type: "a", Key: "same"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "o2", Type: "a", Key: "same"})
	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "o3", Type: "b", Key: "same"})
	pending, _ = sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 0 {
		t.Fatalf("cancel should clear all, got %d", len(pending))
	}
}
