package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// overlay implementations use value receivers and hand themselves back through
// m.overlay, so a copied Model never shares overlay state with the original.
type overlay interface {
	title(m Model) string
	body(m Model, width, height int) []string
	hints(k keyMap) []key.Binding
	update(m Model, k tea.KeyMsg) (Model, tea.Cmd)
}

func clampOffset(offset, lines, height int) int {
	return min(max(0, offset), max(0, lines-height))
}

func scroll(lines []string, offset, height int) []string {
	return lines[clampOffset(offset, len(lines), height):]
}
