package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxIDLen   = 128
	MaxTypeLen = 256
	MaxKeyLen  = 512
	MaxDataLen = 256 * 1024
)

var (
	ErrInvalidEvent     = errors.New("invalid event")
	ErrEventConflict    = errors.New("event id conflict")
	ErrEventNotFound    = errors.New("event not found")
	ErrRuleNotFound     = errors.New("rule not found")
	ErrInvalidRule      = errors.New("invalid rule")
	ErrQueueFull        = errors.New("write queue full")
	ErrNotReady         = errors.New("not ready")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrLeaseNotFound    = errors.New("lease not found")
	ErrConsumerRequired = errors.New("consumer id required")
)

// Event is an ingested input event.
type Event struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Key               string          `json:"key"`
	Timestamp         time.Time       `json:"timestamp"`
	ProducerTimestamp *time.Time      `json:"producer_timestamp,omitempty"`
	Data              json.RawMessage `json:"data,omitempty"`
	ContentHash       string          `json:"content_hash,omitempty"`
}

// EventInput is the publisher-facing payload (server sets Timestamp).
type EventInput struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Key               string          `json:"key"`
	ProducerTimestamp *time.Time      `json:"producer_timestamp,omitempty"`
	Data              json.RawMessage `json:"data,omitempty"`
}

func ValidateEventInput(in EventInput) error {
	if err := validateID(in.ID); err != nil {
		return err
	}
	if err := validateType(in.Type); err != nil {
		return err
	}
	if err := validateKey(in.Key); err != nil {
		return err
	}
	if len(in.Data) > MaxDataLen {
		return fmt.Errorf("%w: data too large", ErrInvalidEvent)
	}
	if len(in.Data) > 0 && !json.Valid(in.Data) {
		return fmt.Errorf("%w: data must be valid JSON", ErrInvalidEvent)
	}
	return nil
}

func CanonicalContentHash(typ, key string, data json.RawMessage) (string, error) {
	normalized, err := normalizeJSON(data)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte(typ))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(normalized)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum), nil
}

func normalizeJSON(data json.RawMessage) ([]byte, error) {
	if len(data) == 0 || string(data) == "null" {
		return []byte("null"), nil
	}
	// Fast path: already compact object/array/string/number/bool with sorted keys
	// is not guaranteed; always canonicalize via unmarshal/marshal for stability.
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	return json.Marshal(v) // map keys sorted by encoding/json
}

func validateID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || utf8.RuneCountInString(id) > MaxIDLen {
		return fmt.Errorf("%w: id", ErrInvalidEvent)
	}
	return nil
}

func validateType(t string) error {
	t = strings.TrimSpace(t)
	if t == "" || utf8.RuneCountInString(t) > MaxTypeLen {
		return fmt.Errorf("%w: type", ErrInvalidEvent)
	}
	return nil
}

func validateKey(k string) error {
	k = strings.TrimSpace(k)
	if k == "" || utf8.RuneCountInString(k) > MaxKeyLen {
		return fmt.Errorf("%w: key", ErrInvalidEvent)
	}
	return nil
}

// StableOutputID builds a deterministic output event id.
func StableOutputID(firingID string) string {
	return "out_" + firingID
}

// StableFiringID builds a deterministic firing id for absence/sequence.
func StableFiringID(ruleID, matchID string) string {
	h := sha256.Sum256([]byte(ruleID + "\x00" + matchID))
	return "fire_" + hex.EncodeToString(h[:16])
}

// StableCountFiringID builds a firing id for count rules.
func StableCountFiringID(ruleID, key, causeEventID string) string {
	h := sha256.Sum256([]byte(ruleID + "\x00" + key + "\x00" + causeEventID))
	return "fire_" + hex.EncodeToString(h[:16])
}
