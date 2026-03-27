// Absence demo: 3s timeout vs cancel, with restart recovery.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

func main() {
	dir, err := os.MkdirTemp("", "waitmesh-payment-*")
	must(err)
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "demo.db")
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	clk := clock.NewFake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	db, err := sqlite.Open(path)
	must(err)
	eng := engine.New(db, clk, log, 64)

	fmt.Println("=== 1) Create 3-second absence rule ===")
	rule, err := eng.CreateRule(ctx, domain.RuleInput{
		Name: "missing-payment", Type: domain.RuleAbsence,
		Start: "payment.initiated", Unless: "payment.completed",
		Within: "3s", Emit: "payment.timeout",
	})
	must(err)
	fmt.Printf("rule id=%s within=%s emit=%s\n", rule.ID, rule.WithinRaw, rule.Emit)

	fmt.Println("=== 2) Publish payment.initiated ===")
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "pay-init-1", Type: "payment.initiated", Key: "ord-100"})
	must(err)

	fmt.Println("=== 3) Show pending match ===")
	pending, err := sqlite.ListPending(ctx, eng.DB(), 10)
	must(err)
	fmt.Printf("pending=%d key=%s deadline=%s\n", len(pending), pending[0].CorrelationKey, pending[0].Deadline)

	fmt.Println("=== 4) Wait for timeout (advance fake clock 3s) ===")
	clk.Advance(3 * time.Second)
	n, err := eng.ProcessOverdueTimers(ctx, 10)
	must(err)

	fmt.Println("=== 5) Show payment.timeout ===")
	outs, err := sqlite.ListOutputs(ctx, eng.DB(), 10)
	must(err)
	fmt.Printf("processed=%d outputs=%d type=%s key=%s\n", n, len(outs), outs[0].Type, outs[0].Key)
	obox, _ := sqlite.CountOutbox(ctx, eng.DB())
	fmt.Printf("outbox_rows=%d\n", obox)

	fmt.Println("=== 6) Repeat with payment.completed before deadline ===")
	clk.Advance(time.Second)
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "pay-init-2", Type: "payment.initiated", Key: "ord-200"})
	must(err)
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "pay-done-2", Type: "payment.completed", Key: "ord-200"})
	must(err)
	clk.Advance(3 * time.Second)
	n, err = eng.ProcessOverdueTimers(ctx, 10)
	must(err)

	fmt.Println("=== 7) Verify no timeout for cancelled path ===")
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	fmt.Printf("processed=%d total_firings=%d (expect 1 — only ord-100)\n", n, firings)
	if n != 0 || firings != 1 {
		panic(fmt.Sprintf("cancel path failed: processed=%d firings=%d", n, firings))
	}

	fmt.Println("=== bonus) Restart during pending timer ===")
	clk.Advance(time.Second)
	_, err = eng.Ingest(ctx, domain.EventInput{ID: "pay-init-3", Type: "payment.initiated", Key: "ord-300"})
	must(err)
	eng.Close()
	db.Close()

	clk.Advance(3 * time.Second)
	db2, err := sqlite.Open(path)
	must(err)
	defer db2.Close()
	eng2 := engine.New(db2, clk, log, 64)
	defer eng2.Close()
	n, err = eng2.ProcessOverdueTimers(ctx, 10)
	must(err)
	firings, _ = sqlite.CountFirings(ctx, eng2.DB())
	fmt.Printf("recovered processed=%d total_firings=%d (expect 2)\n", n, firings)
	if n != 1 || firings != 2 {
		panic("recovery failed")
	}
	fmt.Println("DEMO OK")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
