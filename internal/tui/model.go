package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joaoseixas88/lazylock/internal/config"
	"github.com/joaoseixas88/lazylock/internal/credstore"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/infisical"
)

// domainUnauthorized is the sentinel the auth flow reacts to.
var domainUnauthorized = domain.ErrUnauthorized

type pane int

const (
	projectsPane pane = iota
	contextPane
	actionsPane
	secretsPane
)

var (
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

type Model struct {
	load loader
	keys keyMap

	projects list[domain.Project]
	scopes   list[domain.Scope]
	secrets  list[domain.Secret]

	activePane  pane
	revealValue bool
	overlay     overlay

	state   appState
	cfg     config.Config
	store   *credstore.Store
	client  *infisical.Client
	login   *infisical.BrowserLogin
	input   textinput.Model
	account string
	authErr error

	width, height, leftWidth, rightWidth, projectHeight, contextHeight, actionsHeight int
}

// New builds a model that browses a catalog directly, with no auth flow. It is
// what the -demo flag and the tests use.
func New(ctx context.Context, catalog domain.Catalog) Model {
	m := Model{
		load:  loader{catalog: catalog, ctx: ctx, timeout: 15 * time.Second, debounce: 120 * time.Millisecond},
		keys:  defaultKeys(),
		state: stateBrowsing,
	}
	m.projects.begin() // so the first frame says "Loading…" instead of "No items found."
	m.resize(100, 30)
	return m
}

// NewApp builds the real model: it may have to ask for the instance URL and log
// in before there is anything to browse.
func NewApp(ctx context.Context, cfg config.Config, store *credstore.Store) Model {
	m := New(ctx, nil)
	m.cfg, m.store, m.account = cfg, store, cfg.Account
	if cfg.Validate() != nil {
		m.state = stateSetup
		m.input = newInput("https://infisical.example.com", textinput.EchoNormal)
		return m
	}
	m.client = infisical.NewClient(cfg.APIBase())
	m.state = stateRestoring
	return m
}

func (m Model) Init() tea.Cmd {
	switch m.state {
	case stateBrowsing:
		return m.load.projects(m.projects.gen)
	case stateRestoring:
		return m.restoreSession()
	default:
		return textinput.Blink
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.handleAuth(msg); handled {
		return next, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		if m.state != stateBrowsing {
			return m.handleAuthKey(msg)
		}
		if key.Matches(msg, m.keys.ForceQuit) {
			return m, tea.Quit
		}
		if m.overlay != nil {
			return m.overlay.update(m, msg)
		}
		return m.handleKey(msg)
	case projectsLoadedMsg:
		return m.handleProjectsLoaded(msg)
	case scopesLoadedMsg:
		return m.handleScopesLoaded(msg)
	case secretsLoadedMsg:
		return m.handleSecretsLoaded(msg)
	case selectProjectMsg:
		if p, ok := m.projects.current(); ok && p.ID == msg.projectID {
			return m, m.loadScopes(p.ID)
		}
		return m, nil
	case selectScopeMsg:
		if s, ok := m.scopes.current(); ok && s == msg.at {
			return m, m.loadSecrets(s)
		}
		return m, nil
	}
	return m, nil // bubbletea's internal traffic; nothing to do
}

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case key.Matches(k, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(k, m.keys.Project):
		m.activePane = projectsPane
	case key.Matches(k, m.keys.Context):
		m.activePane = contextPane
	case key.Matches(k, m.keys.Actions):
		m.activePane = actionsPane
	case key.Matches(k, m.keys.Secrets):
		m.activePane = secretsPane
	case key.Matches(k, m.keys.Up):
		cmd = m.move(-1)
	case key.Matches(k, m.keys.Down):
		cmd = m.move(1)
	case key.Matches(k, m.keys.Retry):
		cmd = m.retry()
	case key.Matches(k, m.keys.Help):
		m.overlay = helpOverlay{}
	case m.activePane == secretsPane && key.Matches(k, m.keys.Reveal):
		m.revealValue = !m.revealValue
	}
	if m.activePane != secretsPane {
		m.revealValue = false
	}
	return m, cmd
}

func (m Model) handleProjectsLoaded(msg projectsLoadedMsg) (tea.Model, tea.Cmd) {
	if !m.projects.accept(msg.gen, msg.items, msg.err) {
		return m, nil
	}
	if m.needsNewLogin(msg.err) {
		return m.expireSession()
	}
	p, ok := m.projects.current()
	if !ok {
		m.scopes.reset()
		m.secrets.reset()
		return m, nil
	}
	return m, m.loadScopes(p.ID)
}

func (m Model) handleScopesLoaded(msg scopesLoadedMsg) (tea.Model, tea.Cmd) {
	if p, ok := m.projects.current(); !ok || p.ID != msg.projectID {
		return m, nil // answers for a project the user has left
	}
	if !m.scopes.accept(msg.gen, msg.items, msg.err) {
		return m, nil
	}
	if m.needsNewLogin(msg.err) {
		return m.expireSession()
	}
	s, ok := m.scopes.current()
	if !ok {
		m.secrets.reset()
		return m, nil
	}
	return m, m.loadSecrets(s)
}

func (m Model) handleSecretsLoaded(msg secretsLoadedMsg) (tea.Model, tea.Cmd) {
	if s, ok := m.scopes.current(); !ok || s != msg.at {
		return m, nil
	}
	if !m.secrets.accept(msg.gen, msg.items, msg.err) {
		return m, nil
	}
	if m.needsNewLogin(msg.err) {
		return m.expireSession()
	}
	m.revealValue = false // never reveal a value the user did not just ask for
	return m, nil
}

// loadScopes starts the pane [2] request and blanks pane [4], which can no
// longer be showing the right thing. The reset bumps the secrets generation, so
// a secrets reply already in flight for the previous project is dropped.
func (m *Model) loadScopes(projectID string) tea.Cmd {
	m.secrets.reset()
	m.scopes.cursor = 0
	return m.load.scopes(m.scopes.begin(), projectID)
}

func (m *Model) loadSecrets(at domain.Scope) tea.Cmd {
	return m.load.secrets(m.secrets.begin(), at)
}

func (m *Model) retry() tea.Cmd {
	switch m.activePane {
	case projectsPane:
		return m.load.projects(m.projects.begin())
	case contextPane:
		if p, ok := m.projects.current(); ok {
			return m.loadScopes(p.ID)
		}
	case secretsPane:
		if s, ok := m.scopes.current(); ok {
			return m.loadSecrets(s)
		}
	}
	return nil
}

func (m *Model) move(delta int) tea.Cmd {
	m.revealValue = false
	switch m.activePane {
	case projectsPane:
		before, _ := m.projects.current()
		m.projects.move(delta)
		after, ok := m.projects.current()
		if !ok || after.ID == before.ID {
			return nil // clamped at an end: nothing changed, so nothing to load
		}
		return m.load.settleProject(after.ID)
	case contextPane:
		before, _ := m.scopes.current()
		m.scopes.move(delta)
		after, ok := m.scopes.current()
		if !ok || after == before {
			return nil
		}
		return m.load.settleScope(after)
	case secretsPane:
		m.secrets.move(delta)
	}
	return nil
}

func (m Model) overlayHeight() int { return max(0, m.height-3) }

func (m *Model) resize(width, height int) {
	m.width, m.height = max(0, width), max(0, height)
	m.leftWidth = max(0, (m.width-1)/3)
	m.rightWidth = max(0, m.width-m.leftWidth-1)
	available := max(0, m.height-1) // The footer owns exactly one row.
	m.projectHeight = available / 3
	m.actionsHeight = available / 3
	m.contextHeight = available - m.projectHeight - m.actionsHeight
}
