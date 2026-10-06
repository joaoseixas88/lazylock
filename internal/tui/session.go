package tui

import (
	"os"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/config"
	"github.com/joaoseixas88/lazylock/internal/infisical"
)

type sessionForgottenMsg struct{ err error }

func (m *Model) askLogout() tea.Cmd {
	switch {
	case m.store == nil:
		return m.notify(toastError, "Logging out is "+errUnavailable.Error())
	case config.Token() != "":
		return m.notify(toastError, "LAZYLOCK_TOKEN is set; unset it to log out")
	}
	m.overlay = confirmOverlay{
		question: "Log out of " + hostOf(m.cfg.SiteURL) + "?",
		lines:    []string{"The stored session is deleted and the login screen opens."},
		yes: func(m Model) (Model, tea.Cmd) {
			store, site := m.store, m.cfg.SiteURL
			return m, func() tea.Msg { return sessionForgottenMsg{err: store.Delete(site)} }
		},
	}
	return nil
}

func (m *Model) askSwitchInstance() tea.Cmd {
	switch {
	case m.store == nil:
		return m.notify(toastError, "Switching instance is "+errUnavailable.Error())
	case os.Getenv(config.EnvSiteURL) != "":
		return m.notify(toastError, "LAZYLOCK_SITE_URL is set; unset it to switch instance")
	}
	m.overlay = confirmOverlay{
		question: "Switch to another Infisical instance?",
		lines:    []string{"The session for " + hostOf(m.cfg.SiteURL) + " stays saved; esc on the next screen comes back to it."},
		yes: func(m Model) (Model, tea.Cmd) {
			previous := m.cfg
			m.endSession()
			m.previous = &previous
			m.state = stateSetup
			m.authErr = nil
			m.input = newInput("https://infisical.example.com", textinput.EchoNormal)
			m.input.SetValue(previous.SiteURL)
			return m, textinput.Blink
		},
	}
	return nil
}

func (m Model) returnToPrevious() (Model, tea.Cmd) {
	m.cfg, m.previous = *m.previous, nil
	m.authErr = nil
	m.client = infisical.NewClient(m.cfg.APIBase())
	m.state = stateRestoring
	return m, m.restoreSession()
}
