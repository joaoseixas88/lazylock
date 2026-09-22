package domain

import (
	"context"
	"errors"
)

// ErrUnauthorized means the session is missing or expired. It is the one error
// the user can act on without leaving the app, so the TUI renders it apart.
var ErrUnauthorized = errors.New("unauthorized")

// Catalog is a read-only view of one configured connection. Each method loads
// exactly one pane, because that is the granularity real providers offer:
// whether a pane costs one request or ten is the adapter's business, not the
// TUI's.
//
// Implementations must be safe for concurrent use: the TUI issues these from
// tea.Cmd goroutines and may have several in flight at once.
type Catalog interface {
	// Projects lists every project the session can see, in display order.
	Projects(ctx context.Context) ([]Project, error)

	// Scopes lists every place secrets can live in a project: the full
	// environment x folder path product, already flattened.
	//
	// The order must be stable across calls, because the TUI's cursor depends
	// on it. The root path "/" must be present for every environment, even one
	// with no folders at all.
	Scopes(ctx context.Context, projectID string) ([]Scope, error)

	// Secrets lists the secrets stored at exactly one Scope. Values arrive
	// already decrypted; Secret.Hidden marks the ones the session may list but
	// not read.
	Secrets(ctx context.Context, at Scope) ([]Secret, error)
}
