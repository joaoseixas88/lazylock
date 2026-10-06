package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	m.timers = timers{}
	return run(t, m, exec(t, m.Init())...)
}

// booted returns a fully loaded model over the given catalog.
func booted(t *testing.T, cat domain.Catalog) Model {
	t.Helper()
	return start(t, New(context.Background(), cat))
}

func press(k string) tea.KeyMsg {
	switch k {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

var terminalSizes = [][2]int{{200, 50}, {80, 24}, {60, 10}, {40, 7}, {80, 4}, {120, 36}, {10, 2}, {1, 1}}

func assertFits(t *testing.T, view string, width, height int) {
	t.Helper()
	view = ansi.Strip(view)
	if lipgloss.Height(view) > height {
		t.Fatalf("height overflow: %d > %d", lipgloss.Height(view), height)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > width {
			t.Fatalf("width overflow: %q", line)
		}
	}
}
