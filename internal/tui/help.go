package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type helpOverlay struct{ offset int }

func (helpOverlay) title(Model) string { return "[4] Help" }

func (helpOverlay) lines(m Model) []string {
	var lines []string
	for i, group := range m.keys.groups() {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, selectedStyle.Render(group.name))
		for _, binding := range group.bindings {
			help := binding.Help()
			lines = append(lines, fmt.Sprintf("  %-8s %s", help.Key, help.Desc))
		}
	}
	return lines
}

func (h helpOverlay) body(m Model, _, height int) []string {
	return scroll(h.lines(m), h.offset, height)
}

func (helpOverlay) hints(k keyMap) []key.Binding {
	return []key.Binding{key.NewBinding(key.WithHelp("↑/↓", "scroll")), k.Close}
}

func (h helpOverlay) update(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Close, m.keys.Help):
		m.overlay = nil
		return m, nil
	case key.Matches(k, m.keys.Down):
		h.offset = clampOffset(h.offset+1, len(h.lines(m)), m.overlayHeight())
	case key.Matches(k, m.keys.Up):
		h.offset = max(0, h.offset-1)
	}
	m.overlay = h
	return m, nil
}
