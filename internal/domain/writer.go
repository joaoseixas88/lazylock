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

	// Raw lists the secrets stored at a scope, without imports, with their
	// values as stored: references unexpanded. Editing starts from these, or
	// saving would replace a reference with what it resolved to.
	Raw(ctx context.Context, at Scope) ([]Secret, error)

	// Create stores a new secret at a scope.
	Create(ctx context.Context, at Scope, s Draft) (Outcome, error)

	// Update changes the secret stored at a scope under key, sending only the
	// fields c sets.
	Update(ctx context.Context, at Scope, key string, c Change) (Outcome, error)

	// Delete removes the secrets stored at a scope under keys: all of them or
	// none.
	Delete(ctx context.Context, at Scope, keys []string) (Outcome, error)

	// Upsert creates or overwrites the secrets at a scope, all of them or none.
	// A draft's comment is only written when it is not empty, so a secret that
	// already exists keeps its own.
	Upsert(ctx context.Context, at Scope, secrets []Draft) (Outcome, error)
}

// ProjectCreator creates projects. A Catalog that also implements it lets the
// TUI create them. Its errors mean what a Writer's do.
type ProjectCreator interface {
	CreateProject(ctx context.Context, p NewProject) (Project, error)
}

// NewProject is a project about to be created. An empty Slug leaves the choice
// to the provider.
type NewProject struct{ Name, Description, Slug string }

// Draft is a secret about to be written.
type Draft struct{ Key, Value, Comment string }

// Change is an edit to one secret. A nil field is left as it is.
type Change struct{ NewKey, Value, Comment *string }

// Outcome is what a write did. Pending means it became a change request that
// still needs an approval, so nothing has changed yet.
type Outcome struct{ Pending bool }

var ErrRejected = errors.New("rejected")
