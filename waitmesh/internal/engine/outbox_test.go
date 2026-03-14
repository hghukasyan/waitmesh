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

func fireOneAbsence(t *testing.T, eng *engine.Engine, clk *clock.Fake) string {
	t.Helper()
	ctx := context.Background()
	_, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "abs-out", Type: domain.RuleAbsence,
		Start: "a", Unless: "b", Within: "1s", Emit: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "i1", Type: "a", Key: "k"})
	clk.Advance(time.Second)
	n, err := eng.ProcessOverdueTimers(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	outs, err := sqlite.ListOutputs(ctx, eng.DB(), 10)
	if err != nil || len(outs) != 1 {
		t.Fatalf("outs=%v err=%v", outs, err)
	}
	return outs[0].ID
}

func TestOutbox_IndependentConsumers(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	outID := fireOneAbsence(t, eng, clk)

	d1, err := eng.PollOutputs(ctx, "cA", 10, time.Minute)
	if err != nil || len(d1) != 1 || d1[0].Output.ID != outID {
		t.Fatalf("d1=%v err=%v", d1, err)
	}
	if err := eng.AckOutput(ctx, "cA", outID); err != nil {
		t.Fatal(err)
	}
	d2, err := eng.PollOutputs(ctx, "cB", 10, time.Minute)
	if err != nil || len(d2) != 1 {
		t.Fatalf("cB must still see output: %v", d2)
	}
	d1b, _ := eng.PollOutputs(ctx, "cA", 10, time.Minute)
	if len(d1b) != 0 {
		t.Fatalf("acked cA: %v", d1b)
	}
}

func TestOutbox_LeaseExpiryRetry(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	_ = fireOneAbsence(t, eng, clk)

	d1, err := eng.PollOutputs(ctx, "worker", 10, 5*time.Second)
	if err != nil || len(d1) != 1 {
		t.Fatalf("%v %v", d1, err)
	}
	if d1[0].DeliveryCount != 1 {
		t.Fatalf("count=%d", d1[0].DeliveryCount)
	}
	// Still within lease
	d2, _ := eng.PollOutputs(ctx, "worker", 10, 5*time.Second)
	if len(d2) != 0 {
		t.Fatalf("leased: %v", d2)
	}
	clk.Advance(5 * time.Second) // lease expired (leased_until <= now)
	d3, err := eng.PollOutputs(ctx, "worker", 10, 5*time.Second)
	if err != nil || len(d3) != 1 {
		t.Fatalf("retry after lease: %v err=%v", d3, err)
	}
	if d3[0].DeliveryCount != 2 {
		t.Fatalf("delivery_count=%d", d3[0].DeliveryCount)
	}
}

func TestOutbox_NackImmediateRetry(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	outID := fireOneAbsence(t, eng, clk)

	d1, _ := eng.PollOutputs(ctx, "w", 10, time.Minute)
	if len(d1) != 1 {
		t.Fatal(d1)
	}
	if err := eng.NackOutput(ctx, "w", outID); err != nil {
		t.Fatal(err)
	}
	d2, err := eng.PollOutputs(ctx, "w", 10, time.Minute)
	if err != nil || len(d2) != 1 {
		t.Fatalf("nack retry: %v err=%v", d2, err)
	}
	if d2[0].DeliveryCount < 2 {
		t.Fatalf("count=%d", d2[0].DeliveryCount)
	}
}

func TestOutbox_ConsumerCrashRecovery(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "ob.db")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	db1, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	eng1 := engine.New(db1, clk, log, 64)
	outID := fireOneAbsence(t, eng1, clk)
	d1, _ := eng1.PollOutputs(ctx, "crashy", 10, time.Minute)
	if len(d1) != 1 {
		t.Fatal(d1)
	}
	eng1.Close()
	db1.Close()

	// Crash before ack: lease still held until expiry
	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	eng2 := engine.New(db2, clk, log, 64)
	defer eng2.Close()
	d2, _ := eng2.PollOutputs(ctx, "crashy", 10, time.Minute)
	if len(d2) != 0 {
		t.Fatalf("lease still active: %v", d2)
	}
	clk.Advance(time.Minute)
	d3, err := eng2.PollOutputs(ctx, "crashy", 10, time.Minute)
	if err != nil || len(d3) != 1 || d3[0].Output.ID != outID {
		t.Fatalf("recover: %v err=%v", d3, err)
	}
	if err := eng2.AckOutput(ctx, "crashy", outID); err != nil {
		t.Fatal(err)
	}
}

func TestOutbox_StableIDsAndAtomicWithFiring(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	outID := fireOneAbsence(t, eng, clk)
	firings, _ := sqlite.ListFirings(ctx, eng.DB(), 10)
	if len(firings) != 1 {
		t.Fatal(firings)
	}
	want := domain.StableOutputID(firings[0].ID)
	if outID != want {
		t.Fatalf("stable id: got %s want %s", outID, want)
	}
}

func TestOutbox_RegisterAndRetentionPurge(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()
	outID := fireOneAbsence(t, eng, clk)

	if err := eng.RegisterConsumer(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if err := eng.RegisterConsumer(ctx, "c2"); err != nil {
		t.Fatal(err)
	}
	consumers, err := sqlite.ListConsumers(ctx, eng.DB())
	if err != nil || len(consumers) != 2 {
		t.Fatalf("consumers=%v err=%v", consumers, err)
	}

	// Only c1 acked — must not purge
	_, _ = eng.PollOutputs(ctx, "c1", 10, time.Minute)
	_ = eng.AckOutput(ctx, "c1", outID)
	clk.Advance(time.Hour)
	n, err := eng.PurgeRetainedOutbox(ctx, 0)
	if err != nil || n != 0 {
		t.Fatalf("partial ack purge=%d err=%v", n, err)
	}

	_, _ = eng.PollOutputs(ctx, "c2", 10, time.Minute)
	_ = eng.AckOutput(ctx, "c2", outID)
	n, err = eng.PurgeRetainedOutbox(ctx, 0)
	if err != nil || n != 1 {
		t.Fatalf("full ack purge=%d err=%v", n, err)
	}
	obox, _ := sqlite.CountOutbox(ctx, eng.DB())
	if obox != 0 {
		t.Fatalf("outbox left=%d", obox)
	}
	// output_events retained for audit
	outs, _ := sqlite.ListOutputs(ctx, eng.DB(), 10)
	if len(outs) != 1 {
		t.Fatalf("outputs should remain: %d", len(outs))
	}
}

func TestOutbox_NoPurgeWithoutConsumers(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	_ = fireOneAbsence(t, eng, clk)
	clk.Advance(time.Hour)
	n, err := eng.PurgeRetainedOutbox(context.Background(), 0)
	if err != nil || n != 0 {
		t.Fatalf("safe default purge=%d err=%v", n, err)
	}
}
