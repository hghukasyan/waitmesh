package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"log/slog"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

func buildCrashWorker(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "waitmesh-crashworker")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/waitmesh-crashworker")
	cmd.Dir = filepath.Join("..", "..") // module root from internal/engine
	// When tests run from module root via go test ./internal/engine, cwd varies.
	// Resolve module root via go list.
	root, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	modRoot := string(bytesTrimSpace(root))
	cmd = exec.Command("go", "build", "-tags", "crashinject", "-o", bin, "./cmd/waitmesh-crashworker")
	cmd.Dir = modRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build worker: %v\n%s", err, out)
	}
	return bin
}

func bytesTrimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\t' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
}

func waitMarker(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("marker not found: %s", path)
}

func TestCrash_BeforeCommitNoPartialEvent(t *testing.T) {
	bin := buildCrashWorker(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "crash.db")

	// Seed rule without crash
	cmd := exec.Command(bin, dbPath, "seed-absence")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
	waitMarker(t, dbPath+".marker.seeded", 5*time.Second)

	// Crash before commit during ingest
	cmd = exec.Command(bin, dbPath, "ingest-start")
	cmd.Env = append(os.Environ(), "WAITMESH_CRASH_POINT=before_commit")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit from crash point")
	}

	// Recover: event must not exist; pending must be empty
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.New(db, clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), log, 64)
	defer eng.Close()
	ctx := context.Background()
	_, err = sqlite.GetEvent(ctx, db.SQL, "e1")
	if err != domain.ErrEventNotFound {
		t.Fatalf("event should be rolled back, err=%v", err)
	}
	pending, _ := sqlite.ListPending(ctx, db, 10)
	if len(pending) != 0 {
		t.Fatalf("orphaned pending: %d", len(pending))
	}
}

func TestCrash_AfterCommitDurable(t *testing.T) {
	bin := buildCrashWorker(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "crash2.db")

	cmd := exec.Command(bin, dbPath, "seed-absence")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}

	cmd = exec.Command(bin, dbPath, "ingest-start")
	cmd.Env = append(os.Environ(), "WAITMESH_CRASH_POINT=after_commit")
	_ = cmd.Run() // exit 90 after commit

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	ev, err := sqlite.GetEvent(ctx, db.SQL, "e1")
	if err != nil || ev.ID != "e1" {
		t.Fatalf("committed event missing: %v", err)
	}
	pending, _ := sqlite.ListPending(ctx, db, 10)
	if len(pending) != 1 {
		t.Fatalf("pending=%d", len(pending))
	}
}

func TestCrash_DuringTimerNoDuplicateFiring(t *testing.T) {
	bin := buildCrashWorker(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "crash3.db")

	cmd := exec.Command(bin, dbPath, "seed-absence")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
	cmd = exec.Command(bin, dbPath, "ingest-start")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ingest: %v\n%s", err, out)
	}

	cmd = exec.Command(bin, dbPath, "ingest-and-fire")
	cmd.Env = append(os.Environ(), "WAITMESH_CRASH_POINT=during_timer")
	_ = cmd.Run()

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 5, 0, time.UTC))
	eng := engine.New(db, clk, log, 64)
	defer eng.Close()
	ctx := context.Background()

	// May have fired once before crash (during_timer is after emit in same tx,
	// so commit includes firing) OR crashed before commit of timer tx.
	firings, _ := sqlite.CountFirings(ctx, eng.DB())
	n, err := eng.ProcessOverdueTimers(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	firings2, _ := sqlite.CountFirings(ctx, eng.DB())
	if firings2 > 1 {
		t.Fatalf("duplicate firings: before=%d after_sweep=%d extra=%d", firings, firings2, n)
	}
	if firings == 0 && firings2 != 1 {
		t.Fatalf("expected recovery fire, firings=%d", firings2)
	}
	if firings == 1 && n != 0 {
		t.Fatalf("duplicate sweep after committed fire: n=%d", n)
	}
}

func TestCrash_OutboxDeliveryRecoverable(t *testing.T) {
	bin := buildCrashWorker(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "crash4.db")

	for _, mode := range []string{"seed-absence", "ingest-and-fire"} {
		cmd := exec.Command(bin, dbPath, mode)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", mode, err, out)
		}
	}

	cmd := exec.Command(bin, dbPath, "poll-outbox")
	cmd.Env = append(os.Environ(), "WAITMESH_CRASH_POINT=after_commit")
	_ = cmd.Run()

	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.New(db, clock.System{}, log, 64)
	defer eng.Close()
	ctx := context.Background()

	// Lease may be held; advance by using nack path via expired lease — poll with past clock.
	// Use short lease expiry: reopen and wait — lease was 1 minute; force nack via SQL not needed.
	// Poll as different consumer always works.
	dels, err := eng.PollOutputs(ctx, "c-recover", 10, time.Minute)
	if err != nil || len(dels) != 1 {
		t.Fatalf("output must survive crash: %v err=%v", dels, err)
	}
	if err := eng.AckOutput(ctx, "c-recover", dels[0].Output.ID); err != nil {
		t.Fatal(err)
	}
}
