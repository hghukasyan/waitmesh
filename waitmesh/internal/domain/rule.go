package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type RuleType string

const (
	RuleAbsence  RuleType = "absence"
	RuleSequence RuleType = "sequence"
	RuleCount    RuleType = "count"
)

// Rule is a versioned condition definition.
type Rule struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      RuleType  `json:"type"`
	Enabled   bool      `json:"enabled"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Absence
	Start  string        `json:"start,omitempty"`
	Unless string        `json:"unless,omitempty"`
	Within time.Duration `json:"-"`
	Emit   string        `json:"emit,omitempty"`

	// Sequence
	Then string `json:"then,omitempty"`

	// Count
	Event string `json:"event,omitempty"`
	Count int    `json:"count,omitempty"`

	WithinRaw string `json:"within,omitempty"`
}

type RuleInput struct {
	Name   string          `json:"name"`
	Type   RuleType        `json:"type"`
	Start  string          `json:"start,omitempty"`
	Unless string          `json:"unless,omitempty"`
	Then   string          `json:"then,omitempty"`
	Event  string          `json:"event,omitempty"`
	Count  int             `json:"count,omitempty"`
	Within string          `json:"within"`
	Emit   string          `json:"emit"`
	Extra  json.RawMessage `json:"-"`
}

func (r Rule) MarshalJSON() ([]byte, error) {
	type alias struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Type      RuleType `json:"type"`
		Enabled   bool     `json:"enabled"`
		Version   int      `json:"version"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Start     string   `json:"start,omitempty"`
		Unless    string   `json:"unless,omitempty"`
		Then      string   `json:"then,omitempty"`
		Event     string   `json:"event,omitempty"`
		Count     int      `json:"count,omitempty"`
		Within    string   `json:"within,omitempty"`
		Emit      string   `json:"emit,omitempty"`
	}
	a := alias{
		ID: r.ID, Name: r.Name, Type: r.Type, Enabled: r.Enabled, Version: r.Version,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		Start: r.Start, Unless: r.Unless, Then: r.Then, Event: r.Event, Count: r.Count,
		Emit: r.Emit,
	}
	if r.WithinRaw != "" {
		a.Within = r.WithinRaw
	} else if r.Within > 0 {
		a.Within = r.Within.String()
	}
	return json.Marshal(a)
}

func ParseRuleInput(in RuleInput) (Rule, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > MaxTypeLen {
		return Rule{}, fmt.Errorf("%w: name", ErrInvalidRule)
	}
	d, err := time.ParseDuration(strings.TrimSpace(in.Within))
	if err != nil || d <= 0 {
		return Rule{}, fmt.Errorf("%w: within", ErrInvalidRule)
	}
	emit := strings.TrimSpace(in.Emit)
	if err := validateType(emit); err != nil {
		return Rule{}, fmt.Errorf("%w: emit", ErrInvalidRule)
	}

	r := Rule{
		Name:      name,
		Type:      in.Type,
		Enabled:   true,
		Version:   1,
		Within:    d,
		WithinRaw: in.Within,
		Emit:      emit,
	}

	switch in.Type {
	case RuleAbsence:
		if err := validateType(in.Start); err != nil {
			return Rule{}, fmt.Errorf("%w: start", ErrInvalidRule)
		}
		if err := validateType(in.Unless); err != nil {
			return Rule{}, fmt.Errorf("%w: unless", ErrInvalidRule)
		}
		r.Start = strings.TrimSpace(in.Start)
		r.Unless = strings.TrimSpace(in.Unless)
	case RuleSequence:
		if err := validateType(in.Start); err != nil {
			return Rule{}, fmt.Errorf("%w: start", ErrInvalidRule)
		}
		if err := validateType(in.Then); err != nil {
			return Rule{}, fmt.Errorf("%w: then", ErrInvalidRule)
		}
		r.Start = strings.TrimSpace(in.Start)
		r.Then = strings.TrimSpace(in.Then)
	case RuleCount:
		if err := validateType(in.Event); err != nil {
			return Rule{}, fmt.Errorf("%w: event", ErrInvalidRule)
		}
		if in.Count < 1 {
			return Rule{}, fmt.Errorf("%w: count must be >= 1", ErrInvalidRule)
		}
		r.Event = strings.TrimSpace(in.Event)
		r.Count = in.Count
	default:
		return Rule{}, fmt.Errorf("%w: unknown type %q", ErrInvalidRule, in.Type)
	}
	return r, nil
}

// TriggerTypes returns event types that cause this rule to create or advance state.
func (r Rule) TriggerTypes() []string {
	switch r.Type {
	case RuleAbsence:
		return []string{r.Start, r.Unless}
	case RuleSequence:
		return []string{r.Start, r.Then}
	case RuleCount:
		return []string{r.Event}
	default:
		return nil
	}
}

type MatchStatus string

const (
	MatchPending   MatchStatus = "pending"
	MatchFired     MatchStatus = "fired"
	MatchCancelled MatchStatus = "cancelled"
	MatchCompleted MatchStatus = "completed"
	MatchExpired   MatchStatus = "expired"
)

// PendingMatch is a durable timer / sequence waiter.
type PendingMatch struct {
	ID             string      `json:"id"`
	RuleID         string      `json:"rule_id"`
	RuleVersion    int         `json:"rule_version"`
	MatchType      RuleType    `json:"match_type"`
	CorrelationKey string      `json:"correlation_key"`
	StartEventID   string      `json:"start_event_id"`
	EmitType       string      `json:"emit_type"`
	Deadline       time.Time   `json:"deadline"`
	Status         MatchStatus `json:"status"`
	CreatedAt      time.Time   `json:"created_at"`
}

// Firing is a logical rule fire.
type Firing struct {
	ID             string    `json:"id"`
	RuleID         string    `json:"rule_id"`
	RuleVersion    int       `json:"rule_version"`
	CorrelationKey string    `json:"correlation_key"`
	CauseEventID   string    `json:"cause_event_id,omitempty"`
	PendingMatchID string    `json:"pending_match_id,omitempty"`
	EmitType       string    `json:"emit_type"`
	FiredAt        time.Time `json:"fired_at"`
}

// OutputEvent is a derived event for consumers.
type OutputEvent struct {
	ID        string          `json:"id"`
	FiringID  string          `json:"firing_id"`
	Type      string          `json:"type"`
	Key       string          `json:"key"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}
