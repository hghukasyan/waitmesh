package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/waitmesh/waitmesh/internal/domain"
)

func TestCanonicalHashStable(t *testing.T) {
	h1, err := domain.CanonicalContentHash("a", "k", json.RawMessage(`{"b":1,"a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	h2, err := domain.CanonicalContentHash("a", "k", json.RawMessage(`{"a":2,"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash not stable: %s vs %s", h1, h2)
	}
	h3, _ := domain.CanonicalContentHash("a", "k", json.RawMessage(`{"a":3,"b":1}`))
	if h1 == h3 {
		t.Fatal("different content should differ")
	}
}
