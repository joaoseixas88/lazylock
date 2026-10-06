package tui

import (
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// staticInput never blinks: a blinking cursor schedules a tick on every
// keystroke, which nothing here needs.
func staticInput(prompt, value string) textinput.Model {
	input := textinput.New()
	input.Prompt = prompt
	input.CharLimit = 4096
	input.Cursor.SetMode(cursor.CursorStatic)
	input.Focus()
	input.SetValue(value)
	return input
}

func (m Model) activeQuery() string {
	switch m.activePane {
	case projectsPane:
		return m.projects.query
	case contextPane:
		return m.scopes.query
	case secretsPane:
		return m.secrets.query
	}
	return ""
}

func (m *Model) applyFilter(query string) tea.Cmd {
	defer m.rememberPath()
	return m.reselect(m.activePane, func() {
		switch m.activePane {
		case projectsPane:
			m.projects.setQuery(query)
		case contextPane:
			m.scopes.setQuery(query)
		case secretsPane:
			m.secrets.setQuery(query)
		}
	})
}

func (m Model) handleFilterKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.filtering = false
		return m, m.applyFilter("")
	case tea.KeyEnter:
		m.filtering = false
		return m, nil
	case tea.KeyUp:
		return m, m.move(-1)
	case tea.KeyDown:
		return m, m.move(1)
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(k)
	return m, tea.Batch(cmd, m.applyFilter(m.filterInput.Value()))
}
