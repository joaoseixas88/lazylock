package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/clipboard"
)

type copiedMsg struct {
	what string
	via  clipboard.Via
	err  error
}

func (m Model) copyText(what, text string) tea.Cmd {
	ctx, copyFn := m.load.ctx, m.fx.Copy
	return func() tea.Msg {
		if copyFn == nil {
			return copiedMsg{what: what, err: errUnavailable}
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		via, err := copyFn(ctx, text)
		return copiedMsg{what: what, via: via, err: err}
	}
}

func (m *Model) copyValue() tea.Cmd {
	s, ok := m.secrets.current()
	switch {
	case !ok:
		return nil
	case s.Hidden:
		return m.notify(toastError, s.Key+" has no read access")
	case s.Value == "":
		return m.notify(toastError, s.Key+" is empty; nothing to copy")
	}
	return m.copyText(s.Key, s.Value)
}

func (m *Model) copied(msg copiedMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		return m.notify(toastError, "Could not copy "+msg.what+": "+msg.err.Error())
	case msg.via == clipboard.Terminal:
		return m.notify(toastInfo, "Sent "+msg.what+" to the terminal clipboard")
	}
	return m.notify(toastInfo, "Copied "+msg.what)
}
