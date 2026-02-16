package engine_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"log/slog"
	"os"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

func seqRule(t *testing.T, eng *engine.Engine, within string) domain.Rule {
	t.Helper()
	r, err := eng.CreateRule(context.Background(), domain.RuleInput{
		Name: "order-flow", Type: domain.RuleSequence,
		Start: "order.created", Then: "order.paid", Within: within, Emit: "order.ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSequence_Success(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	seqRule(t, eng, "5s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a1", Type: "order.created", Key: "o1"})
	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 1 {
		t.Fatalf("pending=%d", len(pending))
	}
	clk.Advance(2 * time.Second)
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b1", Type: "order.paid", Key: "o1"})

	firings, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(firings) != 1 || firings[0].EmitType != "order.ready" {
		t.Fatalf("firings=%v", firings)
	}
	outs, _ := sqlite.CountOutbox(ctx, eng.DB())
	if outs != 1 {
		t.Fatalf("outbox=%d", outs)
	}
	pending, _ = sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 0 {
		t.Fatalf("pending after complete=%d", len(pending))
	}
}

func TestSequence_TimeoutNoEmit(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	seqRule(t, eng, "2s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a1", Type: "order.created", Key: "o1"})
	clk.Advance(2 * time.Second)
	n, err := eng.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("expire n=%d err=%v", n, err)
	}
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 0 {
		t.Fatalf("sequence timeout must not emit, firings=%d", firings)
	}
	// Late B after expire: no fire
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b1", Type: "order.paid", Key: "o1"})
	firings, _ = sqlite.CountFirings(ctx, eng.DB())
	if firings != 0 {
		t.Fatalf("late then after expire: %d", firings)
	}
}

func TestSequence_OutOfOrderThenFirst(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	seqRule(t, eng, "5s")

	// B before A: no-op
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b1", Type: "order.paid", Key: "o1"})
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 0 {
		t.Fatal("then-before-start must not fire")
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a1", Type: "order.created", Key: "o1"})
	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 1 {
		t.Fatalf("pending=%d", len(pending))
	}
	// Prior B does not complete the new match
	firings, _ = sqlite.CountFirings(ctx, eng.DB())
	if firings != 0 {
		t.Fatal("historical then must not complete new start")
	}
}

func TestSequence_ExactDeadlineBoundary(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	seqRule(t, eng, "1s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a1", Type: "order.created", Key: "o1"})
	deadline := start.Add(time.Second)
	clk.Set(deadline) // then at exact deadline succeeds
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b1", Type: "order.paid", Key: "o1"})
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 1 {
		t.Fatalf("exact deadline then should succeed: %d", firings)
	}
}

func TestSequence_OverlappingSameKey(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	seqRule(t, eng, "10s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a1", Type: "order.created", Key: "same"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a2", Type: "order.created", Key: "same"})
	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 2 {
		t.Fatalf("pending=%d", len(pending))
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b1", Type: "order.paid", Key: "same"})
	pending, _ = sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 1 {
		t.Fatalf("one then consumes oldest only: pending=%d", len(pending))
	}
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 1 {
		t.Fatalf("firings=%d", firings)
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b2", Type: "order.paid", Key: "same"})
	firings, _ = sqlite.CountFirings(ctx, eng.DB())
	if firings != 2 {
		t.Fatalf("second then: %d", firings)
	}
}

func TestSequence_MultipleKeys(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	seqRule(t, eng, "5s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a1", Type: "order.created", Key: "x"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a2", Type: "order.created", Key: "y"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b1", Type: "order.paid", Key: "y"})
	firings, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(firings) != 1 || firings[0].CorrelationKey != "y" {
		t.Fatalf("firings=%v", firings)
	}
}

func TestSequence_DuplicateStartIdempotent(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	seqRule(t, eng, "5s")
	in := domain.EventInput{ID: "a1", Type: "order.created", Key: "o1"}
	_, _ = eng.Ingest(ctx, in)
	r, err := eng.Ingest(ctx, in)
	if err != nil || !r.Idempotent {
		t.Fatalf("r=%v err=%v", r, err)
	}
	pending, _ := sqlite.ListPending(ctx, eng.DB(), 10)
	if len(pending) != 1 {
		t.Fatalf("pending=%d", len(pending))
	}
}

func TestSequence_FiresOnce(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	seqRule(t, eng, "5s")
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a1", Type: "order.created", Key: "o1"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b1", Type: "order.paid", Key: "o1"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b2", Type: "order.paid", Key: "o1"})
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 1 {
		t.Fatalf("extra then must not re-fire: %d", firings)
	}
}

func TestSequence_RestartRecovery(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "seq.db")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	db1, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	eng1 := engine.New(db1, clk, log, 64)
	seqRule(t, eng1, "5s")
	_, _ = eng1.Ingest(ctx, domain.EventInput{ID: "a1", Type: "order.created", Key: "o1"})
	eng1.Close()
	db1.Close()

	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	eng2 := engine.New(db2, clk, log, 64)
	defer eng2.Close()
	clk.Advance(1 * time.Second)
	_, _ = eng2.Ingest(ctx, domain.EventInput{ID: "b1", Type: "order.paid", Key: "o1"})
	firings, _ := sqlite.CountFirings(ctx, eng2.DB())
	if firings != 1 {
		t.Fatalf("restart sequence complete: %d", firings)
	}
}
