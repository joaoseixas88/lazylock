package tui

import tea "github.com/charmbracelet/bubbletea"

type toastLevel uint8

const (
	toastInfo toastLevel = iota
	toastError
)

type toast struct {
	text  string
	level toastLevel
	gen   int
}

type toastExpiredMsg struct{ gen int }

func (m *Model) notify(level toastLevel, text string) tea.Cmd {
	m.toast = toast{text: text, level: level, gen: m.toast.gen + 1}
	return later(m.timers.toast, toastExpiredMsg{gen: m.toast.gen})
}
