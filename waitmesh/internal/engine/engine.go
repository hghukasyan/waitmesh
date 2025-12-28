package engine

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

type Engine struct {
	db        *sqlite.DB
	clock     clock.Clock
	log       *slog.Logger
	queue     chan writeReq
	wg        sync.WaitGroup
	closed    chan struct{}
	closeOnce sync.Once
	ready     atomicBool
	rules     rulesCache
	idSeq     atomic.Uint64
	pruneSeq  atomic.Uint64
}

type writeReq struct {
	ctx context.Context
	fn  func(ctx context.Context, tx *sql.Tx) (any, error)
	res chan writeRes
}

type writeRes struct {
	val any
	err error
}

var writeResPool = sync.Pool{
	New: func() any { return make(chan writeRes, 1) },
}

type atomicBool struct {
	mu sync.RWMutex
	v  bool
}

func (a *atomicBool) Set(v bool) {
	a.mu.Lock()
	a.v = v
	a.mu.Unlock()
}

func (a *atomicBool) Get() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.v
}

func New(db *sqlite.DB, clk clock.Clock, log *slog.Logger, queueSize int) *Engine {
	if clk == nil {
		clk = clock.System{}
	}
	if log == nil {
		log = slog.Default()
	}
	if queueSize <= 0 {
		queueSize = 1024
	}
	e := &Engine{
		db:     db,
		clock:  clk,
		log:    log,
		queue:  make(chan writeReq, queueSize),
		closed: make(chan struct{}),
	}
	e.wg.Add(1)
	go e.writerLoop()
	e.ready.Set(true)
	return e
}

func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		e.ready.Set(false)
		close(e.closed)
		e.wg.Wait()
	})
}

func (e *Engine) Ready() bool { return e.ready.Get() }

func (e *Engine) DB() *sqlite.DB { return e.db }

func (e *Engine) Clock() clock.Clock { return e.clock }

func (e *Engine) writerLoop() {
	defer e.wg.Done()
	for {
		select {
		case <-e.closed:
			// drain remaining? drop with error
			for {
				select {
				case req := <-e.queue:
					req.res <- writeRes{err: domain.ErrNotReady}
				default:
					return
				}
			}
		case req := <-e.queue:
			var val any
			err := e.db.WithTx(req.ctx, func(tx *sql.Tx) error {
				var err error
				val, err = req.fn(req.ctx, tx)
				if err != nil {
					return err
				}
				maybeCrash("before_commit")
				return nil
			})
			if err == nil {
				maybeCrash("after_commit")
			}
			req.res <- writeRes{val: val, err: err}
		}
	}
}

func (e *Engine) do(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) (any, error)) (any, error) {
	if !e.ready.Get() {
		return nil, domain.ErrNotReady
	}
	resCh := writeResPool.Get().(chan writeRes)
	req := writeReq{ctx: ctx, fn: fn, res: resCh}
	select {
	case e.queue <- req:
	case <-ctx.Done():
		writeResPool.Put(resCh)
		return nil, ctx.Err()
	case <-e.closed:
		writeResPool.Put(resCh)
		return nil, domain.ErrNotReady
	default:
		writeResPool.Put(resCh)
		return nil, domain.ErrQueueFull
	}
	select {
	case r := <-resCh:
		writeResPool.Put(resCh)
		return r.val, r.err
	case <-ctx.Done():
		// Result may still be sent; drain asynchronously to recycle the channel.
		go func() {
			select {
			case <-resCh:
			case <-time.After(30 * time.Second):
			}
			writeResPool.Put(resCh)
		}()
		return nil, ctx.Err()
	}
}

type IngestResult struct {
	Event     domain.Event `json:"event"`
	Idempotent bool        `json:"idempotent"`
	Created   bool         `json:"created"`
}

func (e *Engine) Ingest(ctx context.Context, in domain.EventInput) (IngestResult, error) {
	if err := domain.ValidateEventInput(in); err != nil {
		return IngestResult{}, err
	}
	hash, err := domain.CanonicalContentHash(in.Type, in.Key, in.Data)
	if err != nil {
		return IngestResult{}, err
	}
	now := e.clock.Now()
	ev := domain.Event{
		ID:                strings.TrimSpace(in.ID),
		Type:              strings.TrimSpace(in.Type),
		Key:               strings.TrimSpace(in.Key),
		Timestamp:         now,
		ProducerTimestamp: in.ProducerTimestamp,
		Data:              in.Data,
		ContentHash:       hash,
	}

	val, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		existingHash, err := sqlite.LookupContentHash(ctx, tx, ev.ID)
		if err == nil {
			if existingHash != hash {
				return nil, domain.ErrEventConflict
			}
			existing, err := sqlite.GetEvent(ctx, tx, ev.ID)
			if err != nil {
				return nil, err
			}
			return IngestResult{Event: existing, Idempotent: true}, nil
		}
		if err != domain.ErrEventNotFound {
			return nil, err
		}
		if err := sqlite.InsertEvent(ctx, tx, ev); err != nil {
			return nil, err
		}
		if err := e.evaluate(ctx, tx, ev); err != nil {
			return nil, err
		}
		return IngestResult{Event: ev, Created: true}, nil
	})
	if err != nil {
		return IngestResult{}, err
	}
	return val.(IngestResult), nil
}

func (e *Engine) evaluate(ctx context.Context, tx *sql.Tx, ev domain.Event) error {
	rules, ok := e.rules.get(ev.Type)
	if !ok {
		var err error
		rules, err = sqlite.RulesForEventType(ctx, tx, ev.Type)
		if err != nil {
			return err
		}
	}
	for _, r := range rules {
		switch r.Type {
		case domain.RuleAbsence:
			if err := e.evalAbsence(ctx, tx, r, ev); err != nil {
				return err
			}
		case domain.RuleSequence:
			if err := e.evalSequence(ctx, tx, r, ev); err != nil {
				return err
			}
		case domain.RuleCount:
			if err := e.evalCount(ctx, tx, r, ev); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) evalAbsence(ctx context.Context, tx *sql.Tx, r domain.Rule, ev domain.Event) error {
	switch ev.Type {
	case r.Start:
		m := domain.PendingMatch{
			ID:             e.nextID("pm"),
			RuleID:         r.ID,
			RuleVersion:    r.Version,
			MatchType:      domain.RuleAbsence,
			CorrelationKey: ev.Key,
			StartEventID:   ev.ID,
			EmitType:       r.Emit,
			Deadline:       ev.Timestamp.Add(r.Within),
			Status:         domain.MatchPending,
			CreatedAt:      ev.Timestamp,
		}
		return sqlite.InsertPendingMatch(ctx, tx, m)
	case r.Unless:
		_, err := sqlite.CancelPendingByRuleKey(ctx, tx, r.ID, ev.Key, ev.Timestamp)
		return err
	}
	return nil
}

func (e *Engine) evalSequence(ctx context.Context, tx *sql.Tx, r domain.Rule, ev domain.Event) error {
	switch ev.Type {
	case r.Start:
		m := domain.PendingMatch{
			ID:             e.nextID("pm"),
			RuleID:         r.ID,
			RuleVersion:    r.Version,
			MatchType:      domain.RuleSequence,
			CorrelationKey: ev.Key,
			StartEventID:   ev.ID,
			EmitType:       r.Emit,
			Deadline:       ev.Timestamp.Add(r.Within),
			Status:         domain.MatchPending,
			CreatedAt:      ev.Timestamp,
		}
		return sqlite.InsertPendingMatch(ctx, tx, m)
	case r.Then:
		// Out-of-order B (no pending A) is a no-op. Overdue pending left for expire sweep.
		m, ok, err := sqlite.ClaimOldestSequenceMatch(ctx, tx, r.ID, ev.Key, ev.Timestamp)
		if err != nil || !ok {
			return err
		}
		return e.emitFromMatch(ctx, tx, m, ev.ID, ev.Timestamp)
	}
	return nil
}

func (e *Engine) evalCount(ctx context.Context, tx *sql.Tx, r domain.Rule, ev domain.Event) error {
	if ev.Type != r.Event {
		return nil
	}
	if err := sqlite.AddCounterEvent(ctx, tx, r.ID, ev.Key, ev.ID, ev.Timestamp); err != nil {
		return err
	}
	cutoff := ev.Timestamp.Add(-r.Within)
	// Filtered count is authoritative for the window. Prune opportunistically to
	// bound storage (every 32 inserts on this engine) without a DELETE each event.
	n, err := sqlite.CountInWindowSince(ctx, tx, r.ID, ev.Key, cutoff)
	if err != nil {
		return err
	}
	if e.pruneSeq.Add(1)%32 == 0 {
		if err := sqlite.PruneCounterWindow(ctx, tx, r.ID, ev.Key, cutoff); err != nil {
			return err
		}
	}
	if n < r.Count {
		return nil
	}
	last, err := sqlite.GetLastFireEventID(ctx, tx, r.ID, ev.Key)
	if err != nil {
		return err
	}
	// Watermark: fire at most once per cause event_id while count >= N.
	// Idempotent re-ingest of the same event_id cannot create a second firing.
	if last == ev.ID {
		return nil
	}
	firingID := domain.StableCountFiringID(r.ID, ev.Key, ev.ID)
	f := domain.Firing{
		ID:             firingID,
		RuleID:         r.ID,
		RuleVersion:    r.Version,
		CorrelationKey: ev.Key,
		CauseEventID:   ev.ID,
		EmitType:       r.Emit,
		FiredAt:        ev.Timestamp,
	}
	if err := sqlite.InsertFiring(ctx, tx, f); err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return err
	}
	data, _ := json.Marshal(map[string]any{
		"rule": r.Name, "count": n, "cause_event_id": ev.ID,
	})
	o := domain.OutputEvent{
		ID:        domain.StableOutputID(firingID),
		FiringID:  firingID,
		Type:      r.Emit,
		Key:       ev.Key,
		Timestamp: ev.Timestamp,
		Data:      data,
		CreatedAt: ev.Timestamp,
	}
	if err := sqlite.InsertOutputAndOutbox(ctx, tx, o); err != nil {
		return err
	}
	return sqlite.SetLastFireEventID(ctx, tx, r.ID, ev.Key, ev.ID, ev.Timestamp)
}

func (e *Engine) emitFromMatch(ctx context.Context, tx *sql.Tx, m domain.PendingMatch, causeID string, at time.Time) error {
	firingID := domain.StableFiringID(m.RuleID, m.ID)
	f := domain.Firing{
		ID:             firingID,
		RuleID:         m.RuleID,
		RuleVersion:    m.RuleVersion,
		CorrelationKey: m.CorrelationKey,
		CauseEventID:   causeID,
		PendingMatchID: m.ID,
		EmitType:       m.EmitType,
		FiredAt:        at,
	}
	if err := sqlite.InsertFiring(ctx, tx, f); err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return err
	}
	data, _ := json.Marshal(map[string]any{
		"pending_match_id": m.ID,
		"start_event_id":   m.StartEventID,
		"cause_event_id":   causeID,
	})
	o := domain.OutputEvent{
		ID:        domain.StableOutputID(firingID),
		FiringID:  firingID,
		Type:      m.EmitType,
		Key:       m.CorrelationKey,
		Timestamp: at,
		Data:      data,
		CreatedAt: at,
	}
	return sqlite.InsertOutputAndOutbox(ctx, tx, o)
}

// ProcessOverdueTimers claims due absence matches and expires sequence matches.
func (e *Engine) ProcessOverdueTimers(ctx context.Context, limit int) (int, error) {
	val, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		now := e.clock.Now()
		matches, err := sqlite.ListOverduePending(ctx, tx, now, limit)
		if err != nil {
			return 0, err
		}
		n := 0
		for _, m := range matches {
			switch m.MatchType {
			case domain.RuleAbsence:
				claimed, ok, err := sqlite.ClaimPendingForFire(ctx, tx, m.ID, now)
				if err != nil {
					return n, err
				}
				if !ok {
					continue
				}
				if err := e.emitFromMatch(ctx, tx, claimed, "", now); err != nil {
					return n, err
				}
				n++
				maybeCrash("during_timer")
			case domain.RuleSequence:
				ok, err := sqlite.ExpireOverdueSequence(ctx, tx, m.ID, now)
				if err != nil {
					return n, err
				}
				if ok {
					n++
				}
			}
		}
		return n, nil
	})
	if err != nil {
		return 0, err
	}
	return val.(int), nil
}

func (e *Engine) CreateRule(ctx context.Context, in domain.RuleInput) (domain.Rule, error) {
	r, err := domain.ParseRuleInput(in)
	if err != nil {
		return domain.Rule{}, err
	}
	now := e.clock.Now()
	r.ID = e.nextID("rule")
	r.CreatedAt = now
	r.UpdatedAt = now
	// Invalidate before mutate so concurrent ingest cannot use a stale warm cache
	// between commit and refresh (evaluate falls back to TX SQL when cold).
	e.rules.invalidate()
	val, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		if err := sqlite.InsertRule(ctx, tx, r); err != nil {
			return nil, err
		}
		return r, nil
	})
	if err != nil {
		return domain.Rule{}, err
	}
	e.refreshRuleCache(ctx)
	return val.(domain.Rule), nil
}

func (e *Engine) UpdateRule(ctx context.Context, id string, in domain.RuleInput) (domain.Rule, error) {
	parsed, err := domain.ParseRuleInput(in)
	if err != nil {
		return domain.Rule{}, err
	}
	e.rules.invalidate()
	val, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		cur, err := sqlite.GetRule(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		cur.Name = parsed.Name
		cur.Type = parsed.Type
		cur.Start = parsed.Start
		cur.Unless = parsed.Unless
		cur.Then = parsed.Then
		cur.Event = parsed.Event
		cur.Count = parsed.Count
		cur.Within = parsed.Within
		cur.WithinRaw = parsed.WithinRaw
		cur.Emit = parsed.Emit
		cur.Version++
		cur.UpdatedAt = e.clock.Now()
		if err := sqlite.UpdateRule(ctx, tx, cur); err != nil {
			return nil, err
		}
		return cur, nil
	})
	if err != nil {
		return domain.Rule{}, err
	}
	e.refreshRuleCache(ctx)
	return val.(domain.Rule), nil
}

func (e *Engine) SetRuleEnabled(ctx context.Context, id string, enabled bool) error {
	e.rules.invalidate()
	_, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		return nil, sqlite.SetRuleEnabled(ctx, tx, id, enabled, e.clock.Now())
	})
	if err != nil {
		return err
	}
	e.refreshRuleCache(ctx)
	return nil
}

func (e *Engine) DeleteRule(ctx context.Context, id string) error {
	e.rules.invalidate()
	_, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		return nil, sqlite.DeleteRule(ctx, tx, id)
	})
	if err != nil {
		return err
	}
	e.refreshRuleCache(ctx)
	return nil
}

func (e *Engine) RegisterConsumer(ctx context.Context, consumerID string) error {
	if strings.TrimSpace(consumerID) == "" {
		return domain.ErrConsumerRequired
	}
	_, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		return nil, sqlite.UpsertConsumer(ctx, tx, consumerID, e.clock.Now())
	})
	return err
}

func (e *Engine) PollOutputs(ctx context.Context, consumerID string, limit int, lease time.Duration) ([]sqlite.Delivery, error) {
	if strings.TrimSpace(consumerID) == "" {
		return nil, domain.ErrConsumerRequired
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	val, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		return sqlite.PollOutbox(ctx, tx, consumerID, limit, e.clock.Now(), lease)
	})
	if err != nil {
		return nil, err
	}
	return val.([]sqlite.Delivery), nil
}

func (e *Engine) AckOutput(ctx context.Context, consumerID, outputID string) error {
	if strings.TrimSpace(consumerID) == "" {
		return domain.ErrConsumerRequired
	}
	_, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		return nil, sqlite.AckOutput(ctx, tx, consumerID, outputID, e.clock.Now())
	})
	return err
}

func (e *Engine) NackOutput(ctx context.Context, consumerID, outputID string) error {
	if strings.TrimSpace(consumerID) == "" {
		return domain.ErrConsumerRequired
	}
	_, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		return nil, sqlite.NackOutput(ctx, tx, consumerID, outputID, e.clock.Now())
	})
	return err
}

// PurgeRetainedOutbox drops outbox rows acked by all registered consumers and older than retain.
func (e *Engine) PurgeRetainedOutbox(ctx context.Context, retain time.Duration) (int64, error) {
	if retain < 0 {
		retain = 0
	}
	val, err := e.do(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
		cutoff := e.clock.Now().Add(-retain)
		return sqlite.PurgeRetainedOutbox(ctx, tx, cutoff)
	})
	if err != nil {
		return 0, err
	}
	return val.(int64), nil
}

func (e *Engine) nextID(prefix string) string {
	n := e.idSeq.Add(1)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(time.Now().UnixNano())^n)
	return prefix + "_" + hex.EncodeToString(b[:]) + "_" + fmt.Sprintf("%d", n)
}

// newID keeps a package-level helper for tests/call sites without engine receiver.
func newID(prefix string) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(time.Now().UnixNano()))
	return prefix + "_" + hex.EncodeToString(b[:])
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE") || strings.Contains(s, "unique") || strings.Contains(s, "constraint failed")
}
