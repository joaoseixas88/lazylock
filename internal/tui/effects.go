package tui

import (
	"context"
	"errors"

	"github.com/joaoseixas88/lazylock/internal/clipboard"
)

// Effects are the side effects that reach outside the program. The zero value
// does nothing, so a model nobody wired up can never touch the user's system.
type Effects struct {
	Copy func(ctx context.Context, text string) (clipboard.Via, error)
}

var errUnavailable = errors.New("not available here")

func (m Model) WithEffects(fx Effects) Model {
	m.fx = fx
	return m
}
