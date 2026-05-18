package benchmarks_test

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

func newBenchEng(b *testing.B) (*engine.Engine, context.Context) {
	b.Helper()
	dir := b.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "b.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.New(db, clock.System{}, log, 8192)
	b.Cleanup(eng.Close)
	return eng, context.Background()
}

func BenchmarkSequenceMatch(b *testing.B) {
	eng, ctx := newBenchEng(b)
	_, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "seq", Type: domain.RuleSequence,
		Start: "a", Then: "b", Within: "1h", Emit: "c",
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("k-%d", i)
		if _, err := eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("a-%d", i), Type: "a", Key: key}); err != nil {
			b.Fatal(err)
		}
		if _, err := eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("b-%d", i), Type: "b", Key: key}); err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N*2)/elapsed.Seconds(), "events/s")
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "matches/s")
}

func BenchmarkCountMatch(b *testing.B) {
	eng, ctx := newBenchEng(b)
	_, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "cnt", Type: domain.RuleCount,
		Event: "click", Count: 3, Within: "1h", Emit: "burst",
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		_, err := eng.Ingest(ctx, domain.EventInput{
			ID: fmt.Sprintf("c-%d", i), Type: "click", Key: "user",
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/s")
}

func BenchmarkMixedWorkload(b *testing.B) {
	eng, ctx := newBenchEng(b)
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs", Type: domain.RuleAbsence, Start: "x", Unless: "y", Within: "1h", Emit: "z",
	})
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "seq", Type: domain.RuleSequence, Start: "a", Then: "b", Within: "1h", Emit: "c",
	})
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "cnt", Type: domain.RuleCount, Event: "click", Count: 5, Within: "1h", Emit: "burst",
	})
	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		switch i % 4 {
		case 0:
			_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("x-%d", i), Type: "x", Key: fmt.Sprintf("k%d", i)})
		case 1:
			_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("a-%d", i), Type: "a", Key: fmt.Sprintf("k%d", i)})
		case 2:
			_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("cl-%d", i), Type: "click", Key: "u"})
		default:
			_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("n-%d", i), Type: "noise", Key: "n"})
		}
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/s")
}

func BenchmarkConcurrentProducers(b *testing.B) {
	eng, ctx := newBenchEng(b)
	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	var wg sync.WaitGroup
	workers := 8
	each := b.N / workers
	if each < 1 {
		each = 1
	}
	errCh := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				id := fmt.Sprintf("w%d-%d", w, i)
				if _, err := eng.Ingest(ctx, domain.EventInput{ID: id, Type: "t", Key: "k"}); err != nil {
					errCh <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		b.Fatal(err)
	}
	elapsed := time.Since(start)
	total := workers * each
	b.ReportMetric(float64(total)/elapsed.Seconds(), "events/s")
}

func BenchmarkOutboxDelivery(b *testing.B) {
	eng, ctx := newBenchEng(b)
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	// Rebuild with fake clock for deterministic fires
	eng.Close()
	dir := b.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "ob.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng = engine.New(db, clk, log, 8192)
	b.Cleanup(eng.Close)

	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs", Type: domain.RuleAbsence, Start: "a", Unless: "b", Within: "1s", Emit: "c",
	})
	// Seed N firings
	nSeed := b.N
	if nSeed > 5000 {
		nSeed = 5000
	}
	for i := 0; i < nSeed; i++ {
		_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("a-%d", i), Type: "a", Key: fmt.Sprintf("k%d", i)})
	}
	clk.Advance(time.Second)
	_, _ = eng.ProcessOverdueTimers(ctx, nSeed+10)

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	acked := 0
	for acked < nSeed {
		dels, err := eng.PollOutputs(ctx, "bench-consumer", 64, time.Minute)
		if err != nil {
			b.Fatal(err)
		}
		if len(dels) == 0 {
			break
		}
		for _, d := range dels {
			if err := eng.AckOutput(ctx, "bench-consumer", d.Output.ID); err != nil {
				b.Fatal(err)
			}
			acked++
		}
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(acked)/elapsed.Seconds(), "acks/s")
}

func BenchmarkRestartRecoveryLoad(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "rec.db")
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()

	db, err := sqlite.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	eng := engine.New(db, clk, log, 4096)
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs", Type: domain.RuleAbsence, Start: "a", Unless: "b", Within: "1s", Emit: "c",
	})
	const pending = 500
	for i := 0; i < pending; i++ {
		_, _ = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("a-%d", i), Type: "a", Key: fmt.Sprintf("k%d", i)})
	}
	eng.Close()
	db.Close()

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		clk.Set(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Second))
		db2, err := sqlite.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		eng2 := engine.New(db2, clk, log, 4096)
		_, err = eng2.ProcessOverdueTimers(ctx, pending+10)
		eng2.Close()
		db2.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "restarts/s")
}
