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
	fx   Effects

	writes bool

	projects list[domain.Project]
	scopes   list[domain.Scope]
	secrets  list[domain.Secret]

	activePane pane
	menuCursor int
	chosenPath string
	reveal     reveal
	marks      marks
	overlay    overlay
	toast      toast
	timers     timers
	seq        int

	filtering   bool
	filterInput textinput.Model

	state    appState
	cfg      config.Config
	previous *config.Config
	store    *credstore.Store
	client   *infisical.Client
	login    *infisical.BrowserLogin
	input    textinput.Model
	account  string
	authErr  error

	width, height, leftWidth, rightWidth, projectHeight, contextHeight, actionsHeight int
}

// New builds a model that browses a catalog directly, with no auth flow. It is
// what the -demo flag and the tests use.
func New(ctx context.Context, catalog domain.Catalog) Model {
	m := Model{
		load:   loader{catalog: catalog, ctx: ctx, timeout: 15 * time.Second, debounce: 120 * time.Millisecond},
		keys:   defaultKeys(),
		state:  stateBrowsing,
		timers: timers{remask: 30 * time.Second, toast: 4 * time.Second},
	}
	m.projects.label = func(p domain.Project) string { return p.Name }
	m.scopes.label = func(s domain.Scope) string { return s.Path }
	m.scopes.groupOf = func(s domain.Scope) string { return s.EnvSlug }
	m.secrets.label = func(s domain.Secret) string { return s.Key }
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
		if m.filtering {
			return m.handleFilterKey(msg)
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
	case copiedMsg:
		return m, m.copied(msg)
	case targetCheckedMsg:
		return m.targetChecked(msg)
	case exportedMsg:
		return m.exported(msg)
	case openedMsg:
		return m, m.opened(msg)
	case comparedMsg:
		return m.compared(msg)
	case writtenMsg:
		return m.written(msg)
	case rawLoadedMsg:
		return m.rawLoaded(msg)
	case conflictMsg:
		return m.conflicted(msg)
	case toastExpiredMsg:
		if msg.gen == m.toast.gen {
			m.toast.text = ""
		}
		return m, nil
	case remaskMsg:
		if msg.gen == m.reveal.gen {
			m.reveal.mask()
		}
		return m, nil
	case selectScopeMsg:
		if s, ok := m.scopes.current(); ok && s == msg.at && m.scopes.state == stateLoaded {
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
	case key.Matches(k, m.keys.Enter) && m.activePane == actionsPane:
		return m.runAction(m.actions()[m.menuCursor])
	case key.Matches(k, m.keys.Enter):
		m.enter()
	case m.activePane == secretsPane && key.Matches(k, m.keys.Reveal):
		cmd = m.toggleReveal()
	case m.activePane == secretsPane && key.Matches(k, m.keys.RevealAll):
		cmd = m.toggleRevealAll()
	case m.activePane == secretsPane && key.Matches(k, m.keys.Copy):
		cmd = m.copyValue()
	case m.activePane == secretsPane && key.Matches(k, m.keys.CopyLines):
		cmd = m.copyLines()
	case m.activePane == secretsPane && key.Matches(k, m.keys.Mark):
		m.toggleMark()
	case m.activePane == secretsPane && key.Matches(k, m.keys.MarkAll):
		m.toggleMarkAll()
	case key.Matches(k, m.keys.Export):
		cmd = m.openExport()
	case key.Matches(k, m.keys.Open):
		cmd = m.openScope()
	case key.Matches(k, m.keys.Logout):
		cmd = m.askLogout()
	case key.Matches(k, m.keys.Compare):
		cmd = m.openCompare()
	case m.activePane == secretsPane && key.Matches(k, m.keys.Delete):
		cmd = m.openDelete()
	case m.activePane == secretsPane && key.Matches(k, m.keys.Edit):
		if s, ok := m.secrets.current(); ok {
			cmd = m.openEdit(s)
		}
	case key.Matches(k, m.keys.New):
		cmd = m.openCreate()
	case key.Matches(k, m.keys.PrevEnv):
		cmd = m.cycleEnv(-1)
	case key.Matches(k, m.keys.NextEnv):
		cmd = m.cycleEnv(1)
	case key.Matches(k, m.keys.Filter) && m.activePane != actionsPane:
		m.filtering = true
		m.filterInput = staticInput("/", m.activeQuery())
	case key.Matches(k, m.keys.Back) && m.activeQuery() != "":
		cmd = m.applyFilter("")
	case key.Matches(k, m.keys.Back):
		m.marks = m.marks.with(nil)
	}
	if m.activePane != secretsPane {
		m.reveal.mask()
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
		m.resetSecrets()
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
		m.resetSecrets()
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
	m.reveal.mask() // never reveal a value the user did not just ask for
	if msg.err == nil {
		m.marks = m.marks.prune(msg.items)
	}
	return m, nil
}

// loadScopes starts the pane [2] request and blanks pane [4], which can no
// longer be showing the right thing. The reset bumps the secrets generation, so
// a secrets reply already in flight for the previous project is dropped.
func (m *Model) loadScopes(projectID string) tea.Cmd {
	m.chosenPath = ""
	m.secrets.reset()
	m.scopes.cursor = 0
	return m.load.scopes(m.scopes.begin(), projectID)
}

func (m *Model) loadSecrets(at domain.Scope) tea.Cmd {
	if at != m.marks.at {
		m.marks = marks{at: at}
	}
	return m.load.secrets(m.secrets.begin(), at)
}

func (m *Model) resetSecrets() {
	m.secrets.reset()
	m.marks = marks{}
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
	defer m.rememberPath()
	return m.reselect(m.activePane, func() {
		switch m.activePane {
		case projectsPane:
			m.projects.move(delta)
		case contextPane:
			m.scopes.move(delta)
		case secretsPane:
			m.secrets.move(delta)
		case actionsPane:
			m.menuCursor = clamp(m.menuCursor+delta, len(m.actions()))
		}
	})
}

// reselect applies change to pane p and, when that changes its selection,
// blanks the panes below at once and schedules their reload. Blanking now
// rather than at the reload keeps every action off the previous scope's data
// while the debounce runs.
func (m *Model) reselect(p pane, change func()) tea.Cmd {
	m.reveal.one = ""
	switch p {
	case projectsPane:
		before, had := m.projects.current()
		change()
		after, ok := m.projects.current()
		switch {
		case !ok && had:
			m.scopes.reset()
			m.resetSecrets()
		case ok && (!had || after.ID != before.ID):
			m.scopes.reset()
			m.scopes.begin()
			m.resetSecrets()
			return m.load.settleProject(after.ID)
		}
	case contextPane:
		before, had := m.scopes.current()
		change()
		after, ok := m.scopes.current()
		switch {
		case !ok && had:
			m.resetSecrets()
		case ok && (!had || after != before):
			m.resetSecrets()
			m.secrets.begin()
			return m.load.settleScope(after)
		}
	default:
		change()
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

func (m *Model) enter() {
	switch m.activePane {
	case projectsPane:
		m.activePane = contextPane
	case contextPane:
		m.activePane = secretsPane
	case secretsPane:
		if s, ok := m.secrets.current(); ok {
			m.overlay = detailsOverlay{id: s.ID}
		}
	}
}
