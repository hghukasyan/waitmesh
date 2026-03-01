package engine_test

import (
	"context"
	"fmt"
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

func countRule(t *testing.T, eng *engine.Engine, n int, within string) domain.Rule {
	t.Helper()
	r, err := eng.CreateRule(context.Background(), domain.RuleInput{
		Name: fmt.Sprintf("burst-%d", n), Type: domain.RuleCount,
		Event: "click", Count: n, Within: within, Emit: "click.burst",
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCount_Threshold(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	countRule(t, eng, 3, "1m")

	for i := 1; i <= 2; i++ {
		_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("c%d", i), Type: "click", Key: "u1"})
		clk.Advance(time.Second)
	}
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 0 {
		t.Fatalf("below threshold: %d", firings)
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "c3", Type: "click", Key: "u1"})
	firings, _ = sqlite.CountFirings(ctx, eng.DB())
	if firings != 1 {
		t.Fatalf("at threshold: %d", firings)
	}
	outs, _ := sqlite.CountOutbox(ctx, eng.DB())
	if outs != 1 {
		t.Fatalf("outbox=%d", outs)
	}
}

func TestCount_SlidingWindowExpiration(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	countRule(t, eng, 3, "10s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "c1", Type: "click", Key: "u1"})
	clk.Advance(6 * time.Second)
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "c2", Type: "click", Key: "u1"})
	clk.Advance(6 * time.Second) // c1 falls out of 10s window (age 12s)
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "c3", Type: "click", Key: "u1"})
	// window has c2,c3 only → count 2
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 0 {
		t.Fatalf("expired window should not fire: %d", firings)
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "c4", Type: "click", Key: "u1"})
	firings, _ = sqlite.CountFirings(ctx, eng.DB())
	if firings != 1 {
		t.Fatalf("want fire after refill: %d", firings)
	}
}

func TestCount_RepeatedFiring(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	countRule(t, eng, 3, "1m")

	for i := 1; i <= 5; i++ {
		_, err := eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("c%d", i), Type: "click", Key: "u1"})
		if err != nil {
			t.Fatal(err)
		}
		clk.Advance(time.Second)
	}
	// Fires on events 3,4,5 (each new event while count>=3)
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 3 {
		t.Fatalf("repeated firings want 3 got %d", firings)
	}
}

func TestCount_NoDuplicateOnSameCause(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	countRule(t, eng, 2, "1m")
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "c1", Type: "click", Key: "u1"})
	clk.Advance(time.Second)
	in := domain.EventInput{ID: "c2", Type: "click", Key: "u1"}
	_, _ = eng.Ingest(ctx, in)
	_, _ = eng.Ingest(ctx, in) // idempotent
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 1 {
		t.Fatalf("firings=%d", firings)
	}
}

func TestCount_WindowBoundaryInclusive(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	countRule(t, eng, 2, "10s")

	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "c1", Type: "click", Key: "u1"})
	// Advance exactly 10s: cutoff = now-10s = start; prune is timestamp < cutoff,
	// so c1 at start remains (boundary inclusive).
	clk.Set(start.Add(10 * time.Second))
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "c2", Type: "click", Key: "u1"})
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 1 {
		t.Fatalf("inclusive window boundary: %d", firings)
	}
}

func TestCount_RestartRecovery(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "count.db")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	db1, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	eng1 := engine.New(db1, clk, log, 64)
	countRule(t, eng1, 3, "1m")
	_, _ = eng1.Ingest(ctx, domain.EventInput{ID: "c1", Type: "click", Key: "u1"})
	clk.Advance(time.Second)
	_, _ = eng1.Ingest(ctx, domain.EventInput{ID: "c2", Type: "click", Key: "u1"})
	eng1.Close()
	db1.Close()

	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	eng2 := engine.New(db2, clk, log, 64)
	defer eng2.Close()
	clk.Advance(time.Second)
	_, _ = eng2.Ingest(ctx, domain.EventInput{ID: "c3", Type: "click", Key: "u1"})
	firings, _ := sqlite.CountFirings(ctx, eng2.DB())
	if firings != 1 {
		t.Fatalf("restart count: %d", firings)
	}
}

func TestCount_IndependentKeys(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	countRule(t, eng, 2, "1m")
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a1", Type: "click", Key: "a"})
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "b1", Type: "click", Key: "b"})
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != 0 {
		t.Fatal("keys must not mix")
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "a2", Type: "click", Key: "a"})
	list, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(list) != 1 || list[0].CorrelationKey != "a" {
		t.Fatalf("%v", list)
	}
}
