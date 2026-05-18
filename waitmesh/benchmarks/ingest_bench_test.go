package benchmarks_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

func benchEng(b *testing.B, rules int) *engine.Engine {
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
	ctx := context.Background()
	for i := 0; i < rules; i++ {
		_, err := eng.CreateRule(ctx, domain.RuleInput{
			Name: fmt.Sprintf("r%d", i), Type: domain.RuleAbsence,
			Start: "match.start", Unless: "match.done", Within: "1h", Emit: "match.timeout",
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	return eng
}

func runIngestBench(b *testing.B, rules int) {
	eng := benchEng(b, rules)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		_, err := eng.Ingest(ctx, domain.EventInput{
			ID: fmt.Sprintf("e-%d", i), Type: "other", Key: "k",
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/s")
}

func BenchmarkIngest_0Rules(b *testing.B)   { runIngestBench(b, 0) }
func BenchmarkIngest_10Rules(b *testing.B)  { runIngestBench(b, 10) }
func BenchmarkIngest_100Rules(b *testing.B) { runIngestBench(b, 100) }

func BenchmarkIngest_1000Rules(b *testing.B) {
	if testing.Short() {
		b.Skip("1000 rules")
	}
	runIngestBench(b, 1000)
}
