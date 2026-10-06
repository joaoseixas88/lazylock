package tui

import tea "github.com/charmbracelet/bubbletea"

type reveal struct {
	all bool
	one string
	gen int
}

type remaskMsg struct{ gen int }

func (r reveal) shows(id string) bool { return r.all || (id != "" && r.one == id) }

func (r *reveal) mask() { r.all, r.one = false, "" }

func (m *Model) toggleReveal() tea.Cmd {
	s, ok := m.secrets.current()
	if !ok {
		return nil
	}
	return m.toggleRevealOf(s.ID)
}

func (m *Model) toggleRevealOf(id string) tea.Cmd {
	if m.reveal.shows(id) {
		m.reveal.mask()
		return nil
	}
	m.reveal.one = id
	return m.scheduleRemask()
}

func (m *Model) toggleRevealAll() tea.Cmd {
	if m.reveal.all {
		m.reveal.mask()
		return nil
	}
	m.reveal.all = true
	return m.scheduleRemask()
}

func (m *Model) scheduleRemask() tea.Cmd {
	m.reveal.gen++
	return later(m.timers.remask, remaskMsg{gen: m.reveal.gen})
}
