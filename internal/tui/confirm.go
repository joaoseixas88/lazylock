package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type confirmOverlay struct {
	question string
	lines    []string
	yes      func(Model) (Model, tea.Cmd)
}

func (confirmOverlay) title(Model) string { return "[4] Confirm" }

func (c confirmOverlay) body(Model, int, int) []string {
	lines := []string{c.question, ""}
	for _, line := range c.lines {
		lines = append(lines, mutedStyle.Render(line))
	}
	return lines
}

func (confirmOverlay) hints(keyMap) []key.Binding {
	return []key.Binding{key.NewBinding(key.WithHelp("y", "yes")), key.NewBinding(key.WithHelp("n", "no"))}
}

func (c confirmOverlay) update(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "y", "enter":
		m.overlay = nil
		return c.yes(m)
	case "n", "esc", "q":
		m.overlay = nil
	}
	return m, nil
}
