package engine

import (
	"context"
	"sync"

	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

// rulesCache holds immutable snapshots of enabled rules indexed by trigger type.
// evaluate and rule mutations are serialized on the writer goroutine.
// Cache is refreshed only AFTER a successful rule commit (never inside an open tx),
// so it never observes uncommitted state and never deadlocks MaxOpenConns(1).
type rulesCache struct {
	mu     sync.RWMutex
	byType map[string][]domain.Rule
}

func (c *rulesCache) get(eventType string) ([]domain.Rule, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.byType == nil {
		return nil, false
	}
	return c.byType[eventType], true
}

func (c *rulesCache) replace(byType map[string][]domain.Rule) {
	c.mu.Lock()
	c.byType = byType
	c.mu.Unlock()
}

func (c *rulesCache) invalidate() {
	c.mu.Lock()
	c.byType = nil
	c.mu.Unlock()
}

func (e *Engine) refreshRuleCache(ctx context.Context) {
	rules, err := sqlite.ListEnabledRules(ctx, e.db.SQL)
	if err != nil {
		e.rules.invalidate()
		e.log.Error("rule cache refresh failed", "err", err)
		return
	}
	byType := make(map[string][]domain.Rule, len(rules)*2)
	for _, r := range rules {
		r := r
		for _, t := range r.TriggerTypes() {
			byType[t] = append(byType[t], r)
		}
	}
	e.rules.replace(byType)
}
