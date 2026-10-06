package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/config"
	"github.com/joaoseixas88/lazylock/internal/credstore"
	"github.com/joaoseixas88/lazylock/internal/infisical"
)

type appState uint8

const (
	stateBrowsing  appState = iota // the four panes
	stateSetup                     // no site URL yet
	stateRestoring                 // reading the stored session
	stateLogin                     // no usable session
)

type sessionRestoredMsg struct {
	session credstore.Session
	err     error
}

type siteCheckedMsg struct {
	siteURL string
	err     error
}

type loginStartedMsg struct {
	login *infisical.BrowserLogin
	err   error
}

type loginDoneMsg struct {
	creds infisical.Credentials
	err   error
}

type sessionReadyMsg struct{ err error }

var errSessionExpired = errors.New("your session expired; log in again")

// restoreSession reads the stored session and proves it still works. Reading
// the keyring can block on a desktop unlock prompt, which is why it is a
// command rather than something main does before the program starts.
func (m Model) restoreSession() tea.Cmd {
	cfg, store, client := m.cfg, m.store, m.client
	return func() tea.Msg {
		if token := config.Token(); token != "" {
			client.SetToken(token)
			return sessionRestoredMsg{session: credstore.Session{SiteURL: cfg.SiteURL, Token: token}}
		}
		session, err := store.Load(cfg.SiteURL)
		if err != nil {
			return sessionRestoredMsg{err: err}
		}
		client.SetToken(session.Token)
		return sessionRestoredMsg{session: session}
	}
}

// verifySession uses the projects call rather than /v1/auth/checkAuth, which
// runs with requireOrg off: a token can pass checkAuth and then fail every
// call that actually reads data.
func (m Model) verifySession() tea.Cmd {
	catalog := infisical.NewCatalog(m.client)
	ctx := m.load.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return sessionReadyMsg{err: catalog.VerifySession(ctx)}
	}
}

// checkSite asks an unauthenticated endpoint whether this host is an Infisical
// instance at all, so a typo reads as a typo instead of a puzzling 404 later.
func checkSite(ctx context.Context, raw string) tea.Cmd {
	return func() tea.Msg {
		siteURL, err := config.NormalizeSiteURL(raw)
		if err != nil {
			return siteCheckedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, siteURL+"/api/status", nil)
		if err != nil {
			return siteCheckedMsg{err: err}
		}
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			return siteCheckedMsg{err: fmt.Errorf("%s did not answer", siteURL)}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return siteCheckedMsg{err: fmt.Errorf("%s/api/status answered %d; is that an Infisical instance?", siteURL, resp.StatusCode)}
		}
		return siteCheckedMsg{siteURL: siteURL}
	}
}

func (m Model) startLogin() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		login, err := infisical.StartBrowserLogin(cfg.Origin(), cfg.LoginURL)
		return loginStartedMsg{login: login, err: err}
	}
}

func waitForCallback(login *infisical.BrowserLogin) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), infisical.LoginTimeout)
		defer cancel()
		creds, err := login.Wait(ctx)
		return loginDoneMsg{creds: creds, err: err}
	}
}

func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		// A browser that will not open is not a failure: the URL is on screen,
		// which is the whole flow on a remote session anyway.
		_ = infisical.OpenBrowser(url)
		return nil
	}
}

func submitPasted(pasted string) tea.Cmd {
	return func() tea.Msg {
		creds, err := infisical.DecodePastedToken(pasted)
		return loginDoneMsg{creds: creds, err: err}
	}
}

// handleAuth runs everything that happens before the panes are usable.
func (m Model) handleAuth(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case sessionRestoredMsg:
		if msg.err != nil {
			return m.toLogin(nil)
		}
		m.account = msg.session.Email
		return m, m.verifySession(), true

	case sessionReadyMsg:
		if m.needsNewLogin(msg.err) {
			next, cmd := m.expireSession()
			return next, cmd, true
		}
		m.state = stateBrowsing
		m.load.catalog = infisical.NewCatalog(m.client)
		if msg.err != nil {
			// A reachable instance that refused for another reason is a pane
			// error, not a login problem.
			m.projects.accept(m.projects.gen, nil, msg.err)
			return m, nil, true
		}
		return m, m.load.projects(m.projects.begin()), true

	case siteCheckedMsg:
		if msg.err != nil {
			m.authErr = msg.err
			return m, nil, true
		}
		m.cfg.SiteURL = msg.siteURL
		if err := config.Save(m.cfg); err != nil {
			m.authErr = err
			return m, nil, true
		}
		m.authErr = nil
		m.client = infisical.NewClient(m.cfg.APIBase())
		m.state = stateRestoring
		return m, m.restoreSession(), true

	case loginStartedMsg:
		if msg.err != nil {
			m.authErr = msg.err
			return m, nil, true
		}
		m.login = msg.login
		return m, tea.Batch(waitForCallback(msg.login), openBrowser(msg.login.URL)), true

	case loginDoneMsg:
		if msg.err != nil {
			m.authErr = msg.err
			return m, nil, true
		}
		m.closeLogin()
		m.account = msg.creds.Email
		m.client.SetToken(msg.creds.Token)
		if err := m.store.Save(credstore.Session{
			SiteURL: m.cfg.SiteURL, Email: msg.creds.Email, Token: msg.creds.Token,
		}); err != nil {
			m.authErr = err
			return m, nil, true
		}
		m.cfg.Account = msg.creds.Email
		_ = config.Save(m.cfg)
		m.authErr = nil
		m.state = stateRestoring
		return m, m.verifySession(), true
	}
	return m, nil, false
}

// needsNewLogin is false for a LAZYLOCK_TOKEN: it wins over any login on every
// start, so the user has to replace it.
func (m Model) needsNewLogin(err error) bool {
	return m.store != nil && config.Token() == "" && errors.Is(err, domainUnauthorized)
}

func (m Model) expireSession() (Model, tea.Cmd) {
	_ = m.store.Delete(m.cfg.SiteURL)
	m.endSession()
	next, cmd, _ := m.toLogin(errSessionExpired)
	return next, cmd
}

func (m *Model) endSession() {
	m.projects.reset()
	m.scopes.reset()
	m.resetSecrets()
	m.reveal.mask()
	m.overlay = nil
	m.filtering = false
	m.projects.query, m.scopes.query, m.secrets.query = "", "", ""
}

func (m Model) toLogin(err error) (Model, tea.Cmd, bool) {
	m.state = stateLogin
	m.authErr = err
	m.input = newInput("Paste the login token shown in the browser", textinput.EchoPassword)
	return m, m.startLogin(), true
}

func (m *Model) closeLogin() {
	if m.login != nil {
		m.login.Close()
		m.login = nil
	}
}

func newInput(placeholder string, echo textinput.EchoMode) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.Prompt = "› "
	input.EchoMode = echo
	input.CharLimit = 4096
	input.Focus()
	return input
}

// handleAuthKey drives the setup and login screens.
func (m Model) handleAuthKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		m.closeLogin()
		return m, tea.Quit
	case tea.KeyEnter:
		value := m.input.Value()
		if value == "" {
			return m, nil
		}
		m.input.Reset()
		m.authErr = nil
		if m.state == stateSetup {
			return m, checkSite(m.load.ctx, value)
		}
		return m, submitPasted(value)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}
