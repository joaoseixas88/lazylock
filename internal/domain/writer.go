package domain

import (
	"context"
	"errors"
)

// Writer changes secrets. A Catalog that also implements it lets the TUI
// write; one that does not keeps it read-only.
//
// A write either applies, opens a change request (Outcome.Pending), or fails.
// An error wrapping ErrRejected means the provider answered and changed
// nothing; any other error means the outcome is unknown, since the request may
// have applied before the connection broke.
type Writer interface {
	// Normalize returns value as the provider will store it.
	Normalize(value string) string

	// Create stores a new secret at a scope.
	Create(ctx context.Context, at Scope, s Draft) (Outcome, error)

	// Delete removes the secrets stored at a scope under keys: all of them or
	// none.
	Delete(ctx context.Context, at Scope, keys []string) (Outcome, error)
}

// Draft is a secret about to be written.
type Draft struct{ Key, Value, Comment string }

// Outcome is what a write did. Pending means it became a change request that
// still needs an approval, so nothing has changed yet.
type Outcome struct{ Pending bool }

var ErrRejected = errors.New("rejected")
