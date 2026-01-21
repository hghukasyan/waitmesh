package scheduler

import (
	"container/heap"
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

// Scheduler processes overdue durable timers.
// SQLite pending_matches (indexed by status, deadline) is the source of truth.
// The in-memory min-heap is only a wake-up hint and may be stale after restart.
type Scheduler struct {
	eng    *engine.Engine
	clock  clock.Clock
	log    *slog.Logger
	maxWait time.Duration
	batch  int
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu   sync.Mutex
	heap deadlineHeap
}

func New(eng *engine.Engine, clk clock.Clock, log *slog.Logger) *Scheduler {
	if clk == nil {
		clk = clock.System{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		eng:     eng,
		clock:   clk,
		log:     log,
		maxWait: 50 * time.Millisecond,
		batch:   256,
		heap:    deadlineHeap{},
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go s.loop(ctx)
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context) {
	defer s.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := s.eng.ProcessOverdueTimers(ctx, s.batch)
		if err != nil && ctx.Err() == nil {
			s.log.Error("timer sweep failed", "err", err)
		}
		if n > 0 {
			s.log.Debug("processed overdue timers", "count", n)
		}
		s.refreshHeap(ctx)

		wait := s.nextWait()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Scheduler) nextWait() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heap.Len() == 0 {
		return s.maxWait
	}
	next := s.heap[0]
	d := next.Sub(s.clock.Now())
	if d <= 0 {
		return time.Millisecond // overdue: tight retry
	}
	if d > s.maxWait {
		return s.maxWait
	}
	return d
}

func (s *Scheduler) refreshHeap(ctx context.Context) {
	next, err := sqlite.NextDeadline(ctx, s.eng.DB())
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heap = deadlineHeap{}
	if next != nil {
		heap.Push(&s.heap, *next)
	}
}

// PeekNext returns the soonest known deadline from the heap cache (may be stale).
func (s *Scheduler) PeekNext() *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heap.Len() == 0 {
		return nil
	}
	t := s.heap[0]
	return &t
}

type deadlineHeap []time.Time

func (h deadlineHeap) Len() int           { return len(h) }
func (h deadlineHeap) Less(i, j int) bool { return h[i].Before(h[j]) }
func (h deadlineHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *deadlineHeap) Push(x any)        { *h = append(*h, x.(time.Time)) }
func (h *deadlineHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
