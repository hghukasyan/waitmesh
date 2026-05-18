package engine_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/domain"
)

// Regression: after CreateRule commits, concurrent ingest of the start type must
// create pending matches even if the warm cache has not been refreshed yet.
func TestRuleCache_InvalidateBeforeCreate(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng, cleanup := testEngine(t, clk)
	defer cleanup()
	ctx := context.Background()

	// Warm empty cache via a no-op type ingest.
	_, _ = eng.Ingest(ctx, domain.EventInput{ID: "warm", Type: "noise", Key: "k"})

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := eng.CreateRule(ctx, domain.RuleInput{
			Name: "late", Type: domain.RuleAbsence,
			Start: "payment.initiated", Unless: "payment.completed",
			Within: "1m", Emit: "payment.timeout",
		})
		errCh <- err
	}()
	go func() {
		defer wg.Done()
		// Small delay so create is in flight / just committed.
		time.Sleep(2 * time.Millisecond)
		for i := 0; i < 20; i++ {
			_, err := eng.Ingest(ctx, domain.EventInput{
				ID: fmt.Sprintf("pay-%d", i), Type: "payment.initiated", Key: fmt.Sprintf("k-%d", i),
			})
			if err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	var pending int
	if err := eng.DB().SQL.QueryRow(`SELECT COUNT(*) FROM pending_matches WHERE status='pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending == 0 {
		t.Fatal("expected pending matches after concurrent create+ingest")
	}
}
