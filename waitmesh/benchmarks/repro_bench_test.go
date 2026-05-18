package benchmarks_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

type latencyHist []time.Duration

func (h latencyHist) report(b *testing.B, name string) {
	if len(h) == 0 {
		return
	}
	sort.Slice(h, func(i, j int) bool { return h[i] < h[j] })
	p := func(p float64) float64 {
		idx := int(float64(len(h)-1) * p)
		return float64(h[idx].Microseconds())
	}
	b.ReportMetric(p(0.50), name+"_p50_us")
	b.ReportMetric(p(0.95), name+"_p95_us")
	b.ReportMetric(p(0.99), name+"_p99_us")
}

func payload(size int) json.RawMessage {
	if size <= 0 {
		return json.RawMessage(`{"op":"bench","v":1}`)
	}
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = 'a' + byte(i%26)
	}
	b, _ := json.Marshal(map[string]string{"blob": string(buf)})
	return b
}

func openBench(b *testing.B) (*engine.Engine, *sqlite.DB, context.Context) {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "bench.db")
	db, err := sqlite.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	mode, _ := db.JournalMode()
	sync, _ := db.Synchronous()
	if testing.Verbose() {
		b.Logf("sqlite journal=%s synchronous=%d path=%s", mode, sync, path)
	}
	if mode != "wal" && mode != "WAL" {
		b.Fatalf("expected WAL, got %s", mode)
	}
	if sync < 2 {
		b.Fatalf("expected synchronous FULL, got %d", sync)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.New(db, clock.System{}, log, 8192)
	b.Cleanup(func() { eng.Close(); _ = db.Close() })
	return eng, db, context.Background()
}

func warmup(eng *engine.Engine, ctx context.Context, n int) {
	for i := 0; i < n; i++ {
		_, _ = eng.Ingest(ctx, domain.EventInput{
			ID: fmt.Sprintf("warm-%d", i), Type: "warmup", Key: "w", Data: payload(0),
		})
	}
}

func BenchmarkRepro_AbsenceIngest(b *testing.B) {
	eng, db, ctx := openBench(b)
	_, _ = eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs", Type: domain.RuleAbsence, Start: "a", Unless: "b", Within: "1h", Emit: "c",
	})
	warmup(eng, ctx, 200)
	var lat latencyHist
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	heap0 := ms.HeapAlloc

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		t0 := time.Now()
		_, err := eng.Ingest(ctx, domain.EventInput{
			ID: fmt.Sprintf("a-%d", i), Type: "a", Key: fmt.Sprintf("k-%d", i%100), Data: payload(64),
		})
		lat = append(lat, time.Since(t0))
		if err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	b.StopTimer()
	lat.report(b, "ingest")
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/s")
	runtime.ReadMemStats(&ms)
	b.ReportMetric(float64(ms.HeapAlloc-heap0)/1024/1024, "heap_delta_MB")
	_ = db
}

func BenchmarkRepro_RulesScale(b *testing.B) {
	for _, rules := range []int{1, 10, 100, 1000} {
		rules := rules
		b.Run(fmt.Sprintf("rules_%d", rules), func(b *testing.B) {
			if rules >= 1000 && testing.Short() {
				b.Skip("1000 rules")
			}
			eng, _, ctx := openBench(b)
			for i := 0; i < rules; i++ {
				_, err := eng.CreateRule(ctx, domain.RuleInput{
					Name: fmt.Sprintf("r%d", i), Type: domain.RuleAbsence,
					Start: "match.start", Unless: "match.done", Within: "1h", Emit: "t",
				})
				if err != nil {
					b.Fatal(err)
				}
			}
			warmup(eng, ctx, 100)
			var lat latencyHist
			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()
			for i := 0; i < b.N; i++ {
				t0 := time.Now()
				_, err := eng.Ingest(ctx, domain.EventInput{
					ID: fmt.Sprintf("e-%d", i), Type: "other", Key: "k", Data: payload(32),
				})
				lat = append(lat, time.Since(t0))
				if err != nil {
					b.Fatal(err)
				}
			}
			elapsed := time.Since(start)
			b.StopTimer()
			lat.report(b, "ingest")
			b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/s")
		})
	}
}

func BenchmarkRepro_ConcurrentPublishers(b *testing.B) {
	for _, pubs := range []int{1, 8, 32} {
		pubs := pubs
		b.Run(fmt.Sprintf("pubs_%d", pubs), func(b *testing.B) {
			eng, _, ctx := openBench(b)
			warmup(eng, ctx, 100)
			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()
			var wg sync.WaitGroup
			each := b.N / pubs
			if each < 1 {
				each = 1
			}
			errCh := make(chan error, pubs)
			for p := 0; p < pubs; p++ {
				wg.Add(1)
				go func(p int) {
					defer wg.Done()
					for i := 0; i < each; i++ {
						_, err := eng.Ingest(ctx, domain.EventInput{
							ID: fmt.Sprintf("p%d-%d", p, i), Type: "t", Key: "k", Data: payload(32),
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
				b.Fatal(err)
			}
			elapsed := time.Since(start)
			b.ReportMetric(float64(pubs*each)/elapsed.Seconds(), "events/s")
		})
	}
}

func BenchmarkRepro_PayloadSizes(b *testing.B) {
	for _, sz := range []int{0, 256, 4096} {
		sz := sz
		b.Run(fmt.Sprintf("payload_%d", sz), func(b *testing.B) {
			eng, _, ctx := openBench(b)
			warmup(eng, ctx, 50)
			data := payload(sz)
			var lat latencyHist
			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()
			for i := 0; i < b.N; i++ {
				t0 := time.Now()
				_, err := eng.Ingest(ctx, domain.EventInput{
					ID: fmt.Sprintf("e-%d", i), Type: "t", Key: "k", Data: data,
				})
				lat = append(lat, time.Since(t0))
				if err != nil {
					b.Fatal(err)
				}
			}
			elapsed := time.Since(start)
			b.StopTimer()
			lat.report(b, "ingest")
			b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/s")
		})
	}
}

func BenchmarkRepro_SequenceAndCount(b *testing.B) {
	b.Run("sequence", func(b *testing.B) {
		eng, _, ctx := openBench(b)
		_, _ = eng.CreateRule(ctx, domain.RuleInput{
			Name: "seq", Type: domain.RuleSequence, Start: "a", Then: "b", Within: "1h", Emit: "c",
		})
		warmup(eng, ctx, 50)
		var ingestLat, matchLat latencyHist
		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("k-%d", i)
			t0 := time.Now()
			_, err := eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("a-%d", i), Type: "a", Key: key, Data: payload(32)})
			ingestLat = append(ingestLat, time.Since(t0))
			if err != nil {
				b.Fatal(err)
			}
			t1 := time.Now()
			_, err = eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("b-%d", i), Type: "b", Key: key, Data: payload(32)})
			matchLat = append(matchLat, time.Since(t1))
			if err != nil {
				b.Fatal(err)
			}
		}
		elapsed := time.Since(start)
		b.StopTimer()
		ingestLat.report(b, "start")
		matchLat.report(b, "then")
		b.ReportMetric(float64(b.N*2)/elapsed.Seconds(), "events/s")
	})
	b.Run("count", func(b *testing.B) {
		eng, _, ctx := openBench(b)
		_, _ = eng.CreateRule(ctx, domain.RuleInput{
			Name: "cnt", Type: domain.RuleCount, Event: "click", Count: 3, Within: "1h", Emit: "burst",
		})
		warmup(eng, ctx, 50)
		var lat latencyHist
		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			t0 := time.Now()
			_, err := eng.Ingest(ctx, domain.EventInput{ID: fmt.Sprintf("c-%d", i), Type: "click", Key: "u", Data: payload(32)})
			lat = append(lat, time.Since(t0))
			if err != nil {
				b.Fatal(err)
			}
		}
		elapsed := time.Since(start)
		b.StopTimer()
		lat.report(b, "ingest")
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/s")
	})
}
