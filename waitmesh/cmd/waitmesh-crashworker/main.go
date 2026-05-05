// waitmesh-crashworker is a subprocess helper for crash/recovery tests.
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
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <dbpath> <mode>\n", os.Args[0])
		os.Exit(2)
	}
	dbPath := os.Args[1]
	mode := os.Args[2]
	_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := sqlite.Open(dbPath)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng := engine.New(db, clk, log, 256)
	defer eng.Close()
	ctx := context.Background()

	switch mode {
	case "seed-absence":
		_, err = eng.CreateRule(ctx, domain.RuleInput{
			Name: "crash-abs", Type: domain.RuleAbsence,
			Start: "a", Unless: "b", Within: "5s", Emit: "c",
		})
		must(err)
		writeMarker(dbPath, "seeded")
	case "ingest-start":
		_, err = eng.Ingest(ctx, domain.EventInput{ID: "e1", Type: "a", Key: "k1"})
		must(err)
		writeMarker(dbPath, "ingested")
	case "ingest-and-fire":
		_, err = eng.Ingest(ctx, domain.EventInput{ID: "e1", Type: "a", Key: "k1"})
		must(err)
		clk.Advance(5 * time.Second)
		_, err = eng.ProcessOverdueTimers(ctx, 10)
		must(err)
		writeMarker(dbPath, "fired")
	case "poll-outbox":
		_, err = eng.PollOutputs(ctx, "c1", 10, time.Minute)
		must(err)
		writeMarker(dbPath, "polled")
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", mode)
		os.Exit(2)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func writeMarker(dbPath, name string) {
	_ = os.WriteFile(dbPath+".marker."+name, []byte("ok"), 0o644)
}
