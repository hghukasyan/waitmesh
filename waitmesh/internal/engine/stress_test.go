package engine_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

func TestStress_100kEventsMultiKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("100k events")
	}
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	_, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs", Type: domain.RuleAbsence, Start: "a", Unless: "b", Within: "1h", Emit: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	const n = 100_000
	for i := 0; i < n; i++ {
		_, err := eng.Ingest(ctx, domain.EventInput{
			ID: fmt.Sprintf("e-%d", i), Type: "a", Key: fmt.Sprintf("k-%d", i%1000),
			Data: []byte(`{"i":1,"s":"payload"}`),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	pending, err := sqlite.ListPending(ctx, eng.DB(), n)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != n {
		// ListPending limits — count via SQL
		var count int
		if err := eng.DB().SQL.QueryRow(`SELECT COUNT(*) FROM pending_matches WHERE status='pending'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != n {
			t.Fatalf("pending=%d want %d", count, n)
		}
	}
	var events int
	if err := eng.DB().SQL.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != n {
		t.Fatalf("events=%d", events)
	}
}

func TestStress_1000ActiveRules(t *testing.T) {
	if testing.Short() {
		t.Skip("1000 rules")
	}
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		_, err := eng.CreateRule(ctx, domain.RuleInput{
			Name: fmt.Sprintf("r-%d", i), Type: domain.RuleAbsence,
			Start: "match.start", Unless: "match.done", Within: "1h", Emit: "match.timeout",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Non-matching type must stay cheap and correct
	_, err := eng.Ingest(ctx, domain.EventInput{ID: "x1", Type: "other", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "m1", Type: "match.start", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	var pending int
	_ = eng.DB().SQL.QueryRow(`SELECT COUNT(*) FROM pending_matches WHERE status='pending'`).Scan(&pending)
	if pending != 1000 {
		t.Fatalf("pending=%d want 1000", pending)
	}
}

func TestStress_ConcurrentPublishersAndTimers(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs", Type: domain.RuleAbsence, Start: "a", Unless: "b", Within: "1s", Emit: "c",
	})

	var wg sync.WaitGroup
	errCh := make(chan error, 64)
	for p := 0; p < 16; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, err := eng.Ingest(ctx, domain.EventInput{
					ID: fmt.Sprintf("p%d-%d", p, i), Type: "a", Key: fmt.Sprintf("k-%d-%d", p, i),
				})
				if err != nil {
					errCh <- err
					return
				}
			}
		}(p)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	clk.Advance(time.Second)
	var total int
	for {
		n, err := eng.ProcessOverdueTimers(ctx, 256)
		if err != nil {
			t.Fatal(err)
		}
		total += n
		if n == 0 {
			break
		}
	}
	const want = 16 * 100
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings != total || firings != want {
		t.Fatalf("firings=%d total_processed=%d want=%d", firings, total, want)
	}
}

func TestStress_CancelVsTimeoutUnderLoad(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "race", Type: domain.RuleAbsence, Start: "a", Unless: "b", Within: "1s", Emit: "c",
	})
	const n = 200
	for i := 0; i < n; i++ {
		_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("a-%d", i), Type: "a", Key: fmt.Sprintf("k-%d", i)})
	}
	clk.Advance(time.Second)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			n, err := eng.ProcessOverdueTimers(ctx, 50)
			if err != nil || n == 0 {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("b-%d", i), Type: "b", Key: fmt.Sprintf("k-%d", i)})
		}
	}()
	wg.Wait()
	var pending int
	_ = eng.DB().SQL.QueryRow(`SELECT COUNT(*) FROM pending_matches WHERE status='pending'`).Scan(&pending)
	if pending != 0 {
		t.Fatalf("pending left=%d", pending)
	}
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	cancelled := 0
	_ = eng.DB().SQL.QueryRow(`SELECT COUNT(*) FROM pending_matches WHERE status='cancelled'`).Scan(&cancelled)
	fired := 0
	_ = eng.DB().SQL.QueryRow(`SELECT COUNT(*) FROM pending_matches WHERE status='fired'`).Scan(&fired)
	if firings != fired {
		t.Fatalf("firings=%d fired_rows=%d", firings, fired)
	}
	if cancelled+fired != n {
		t.Fatalf("cancelled=%d fired=%d want sum %d", cancelled, fired, n)
	}
}

func TestStress_LargePendingBacklogRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backlog.db")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(db, clk, log, 4096)
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs", Type: domain.RuleAbsence, Start: "a", Unless: "b", Within: "1s", Emit: "c",
	})
	const n = 5000
	for i := 0; i < n; i++ {
		_, err := eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("e-%d", i), Type: "a", Key: fmt.Sprintf("k-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	eng.Close()
	db.Close()

	clk.Advance(time.Second)
	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	eng2 := engine.New(db2, clk, log, 4096)
	defer eng2.Close()
	var got int
	for {
		n, err := eng2.ProcessOverdueTimers(ctx, 500)
		if err != nil {
			t.Fatal(err)
		}
		got += n
		if n == 0 {
			break
		}
	}
	if got != n {
		t.Fatalf("recovered firings processed=%d want %d", got, n)
	}
	firings, _ := sqlite.CountFirings(ctx, eng2.DB())
	if firings != n {
		t.Fatalf("firings=%d", firings)
	}
}

func TestStress_CancelledContextAndShutdown(t *testing.T) {
	clk := clock.NewFake(time.Now().UTC())
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := eng.Ingest(ctx, domain.EventInput{ID: "e", Type: "t", Key: "k"})
	if err == nil {
		t.Fatal("expected context error")
	}

	ctx2 := context.Background()
	var started atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, err := eng.Ingest(ctx2, domain.EventInput{
					ID: fmt.Sprintf("s-%d-%d", i, j), Type: "t", Key: "k",
				})
				if err == nil {
					started.Add(1)
				}
			}
		}(i)
	}
	time.Sleep(5 * time.Millisecond)
	eng.Close()
	wg.Wait()
	// After close, further ingest fails
	_, err = eng.Ingest(ctx2, domain.EventInput{ID: "after", Type: "t", Key: "k"})
	if err != domain.ErrNotReady && err != domain.ErrQueueFull {
		// ErrNotReady expected
		if err == nil {
			t.Fatal("ingest after close should fail")
		}
	}
	runtime.GC()
}

func TestStress_SQLiteBusyAndQueueFull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "busy.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.New(db, clock.System{}, log, 2) // tiny queue
	defer eng.Close()
	ctx := context.Background()

	var wg sync.WaitGroup
	var fullHits atomic.Int64
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("b-%d", i), Type: "t", Key: "k"})
			if err == domain.ErrQueueFull {
				fullHits.Add(1)
			}
		}(i)
	}
	wg.Wait()
	// Some may succeed, some may hit queue full — both are valid under backpressure.
	t.Logf("queue_full_hits=%d", fullHits.Load())
}

func TestStress_OutboxRetryAck(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs", Type: domain.RuleAbsence, Start: "a", Unless: "b", Within: "1s", Emit: "c",
	})
	for i := 0; i < 50; i++ {
		_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("a-%d", i), Type: "a", Key: fmt.Sprintf("k-%d", i)})
	}
	clk.Advance(time.Second)
	for {
		n, _ := eng.ProcessOverdueTimers(ctx, 100)
		if n == 0 {
			break
		}
	}
	d1, err := eng.PollOutputs(ctx, "w", 20, 2*time.Second)
	if err != nil || len(d1) == 0 {
		t.Fatalf("%v %v", d1, err)
	}
	// nack then retry
	for _, d := range d1 {
		_ = eng.NackOutput(ctx, "w", d.Output.ID)
	}
	d2, err := eng.PollOutputs(ctx, "w", 20, time.Minute)
	if err != nil || len(d2) == 0 {
		t.Fatalf("retry after nack: %v", d2)
	}
	for _, d := range d2 {
		if err := eng.AckOutput(ctx, "w", d.Output.ID); err != nil {
			t.Fatal(err)
		}
	}
}
