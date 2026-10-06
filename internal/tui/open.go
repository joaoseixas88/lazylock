package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

type webLinker interface {
	WebURL(at domain.Scope) (string, error)
}

type openedMsg struct {
	what string
	err  error
}

func (m *Model) openScope() tea.Cmd {
	at, ok := m.scopes.current()
	if !ok {
		return m.notify(toastError, "Select a path to open")
	}
	linker, ok := m.load.catalog.(webLinker)
	if !ok {
		return m.notify(toastError, "Opening in the browser is "+errUnavailable.Error())
	}
	link, err := linker.WebURL(at)
	if err != nil {
		return m.notify(toastError, "Could not open "+at.EnvName+" "+at.Path+": "+err.Error())
	}
	open, what := m.fx.OpenURL, at.EnvName+" "+at.Path
	return func() tea.Msg {
		if open == nil {
			return openedMsg{what: what, err: errUnavailable}
		}
		return openedMsg{what: what, err: open(link)}
	}
}

func (m *Model) opened(msg openedMsg) tea.Cmd {
	if msg.err != nil {
		return m.notify(toastError, "Could not open "+msg.what+": "+msg.err.Error())
	}
	return m.notify(toastInfo, "Opened "+msg.what+" in the browser")
}

// openBrowser never reports a failure: the login URL is on screen, which is
// the whole flow on a remote session anyway.
func (m Model) openBrowser(url string) tea.Cmd {
	open := m.fx.OpenURL
	return func() tea.Msg {
		if open != nil {
			_ = open(url)
		}
		return nil
	}
}
