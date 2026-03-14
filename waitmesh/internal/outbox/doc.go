// Package outbox documents Waitmesh consumer delivery semantics.
//
// Implementation lives in internal/storage/sqlite (poll/ack tables) and
// internal/engine (PollOutputs / AckOutput). Delivery is at-least-once with
// stable output IDs; each consumer has independent leases and acknowledgments.
package outbox
