package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

// run delivers msgs and drains every command they produce, so a test observes
// the same end state a running program would reach.
func run(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	queue := append([]tea.Msg(nil), msgs...)
	for i := 0; len(queue) > 0; i++ {
		if i > 128 {
			t.Fatal("commands did not settle: probable update loop")
		}
		next, cmd := m.Update(queue[0])
		queue = append(queue[1:], exec(t, cmd)...)
		m = next.(Model)
	}
	return m
}

// exec runs a command to completion, flattening tea.Batch. tea.Sequence is
// deliberately unsupported: bubbletea keeps sequenceMsg unexported, so no
// synchronous harness can decompose it. This app must never use tea.Sequence.
func exec(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, exec(t, c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// start brings a fresh model up the way the program would, by running Init and
// draining the cascade it triggers. The debounce is zeroed because tea.Tick
// blocks for its whole duration inside a synchronous harness.
func start(t *testing.T, m Model) Model {
	t.Helper()
	m.load.debounce = 0
	return run(t, m, exec(t, m.Init())...)
}

// booted returns a fully loaded model over the given catalog.
func booted(t *testing.T, cat domain.Catalog) Model {
	t.Helper()
	return start(t, New(context.Background(), cat))
}
