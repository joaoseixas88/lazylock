package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

type pane int

const (
	projectsPane pane = iota
	contextPane
	secretsPane
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	panelStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	focusedStyle  = panelStyle.Copy().BorderForeground(lipgloss.Color("63"))
)

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Next   key.Binding
	Prev   key.Binding
	Reveal key.Binding
	Quit   key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Next:   key.NewBinding(key.WithKeys("tab", "right", "l"), key.WithHelp("tab/l", "next pane")),
		Prev:   key.NewBinding(key.WithKeys("shift+tab", "left", "h"), key.WithHelp("shift+tab/h", "previous pane")),
		Reveal: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "reveal")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Next, k.Prev, k.Reveal, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

type context struct {
	environmentID string
	folderID      string
	label         string
}

type Model struct {
	catalog domain.Catalog
	help    help.Model
	keys    keyMap

	activePane    pane
	projectCursor int
	contextCursor int
	secretCursor  int
	revealValue   bool

	connectionID string
	projectID    string
}

func New(catalog domain.Catalog) Model {
	model := Model{catalog: catalog, help: help.New(), keys: defaultKeys()}
	connections := catalog.Connections()
	if len(connections) > 0 {
		model.connectionID = connections[0].ID
		projects := catalog.Projects(model.connectionID)
		if len(projects) > 0 {
			model.projectID = projects[0].ID
		}
	}
	return model
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch {
	case key.Matches(keyMsg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(keyMsg, m.keys.Next):
		m.activePane = (m.activePane + 1) % 3
		m.revealValue = false
	case key.Matches(keyMsg, m.keys.Prev):
		m.activePane = (m.activePane + 2) % 3
		m.revealValue = false
	case key.Matches(keyMsg, m.keys.Up):
		m.move(-1)
	case key.Matches(keyMsg, m.keys.Down):
		m.move(1)
	case m.activePane == secretsPane && key.Matches(keyMsg, m.keys.Reveal):
		m.revealValue = !m.revealValue
	}
	return m, nil
}

func (m *Model) move(delta int) {
	switch m.activePane {
	case projectsPane:
		count := len(m.projects())
		m.projectCursor = clamp(m.projectCursor+delta, count)
		if count > 0 {
			m.projectID = m.projects()[m.projectCursor].ID
		}
		m.contextCursor, m.secretCursor = 0, 0
	case contextPane:
		m.contextCursor = clamp(m.contextCursor+delta, len(m.contexts()))
		m.secretCursor = 0
	case secretsPane:
		m.secretCursor = clamp(m.secretCursor+delta, len(m.secrets()))
	}
	m.revealValue = false
}

func clamp(value, count int) int {
	if count == 0 || value < 0 {
		return 0
	}
	if value >= count {
		return count - 1
	}
	return value
}

func (m Model) projects() []domain.Project { return m.catalog.Projects(m.connectionID) }

func (m Model) contexts() []context {
	var values []context
	for _, environment := range m.catalog.Environments(m.projectID) {
		for _, folder := range m.catalog.Folders(environment.ID, "") {
			values = append(values, context{
				environmentID: environment.ID,
				folderID:      folder.ID,
				label:         fmt.Sprintf("%s  %s", environment.Name, mutedStyle.Render(folder.Name)),
			})
		}
	}
	return values
}

func (m Model) secrets() []domain.Secret {
	contexts := m.contexts()
	if len(contexts) == 0 {
		return nil
	}
	return m.catalog.Secrets(contexts[m.contextCursor].folderID)
}

func (m Model) View() string {
	left := lipgloss.JoinVertical(lipgloss.Left,
		m.panel("Projects", m.projectItems(), projectsPane, 30, 9),
		m.panel("Paths / Environments", m.contextItems(), contextPane, 30, 9),
		m.actionsPanel(),
	)
	right := m.panel("Secrets", m.secretItems(), secretsPane, 62, 30)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	footer := mutedStyle.Render(m.help.View(m.keys))
	return strings.Join([]string{titleStyle.Render("LazyLock"), mutedStyle.Render(m.location()), body, footer}, "\n")
}

func (m Model) panel(title string, items []string, target pane, width, height int) string {
	content := titleStyle.Render(title) + "\n"
	if len(items) == 0 {
		content += "\n" + mutedStyle.Render("No items found.")
	}
	for i, item := range items {
		prefix := "  "
		if target == m.activePane && i == m.cursorFor(target) {
			prefix = selectedStyle.Render("› ")
		}
		content += "\n" + prefix + item
	}
	style := panelStyle
	if target == m.activePane {
		style = focusedStyle
	}
	return style.Width(width).Height(height).Render(content)
}

func (m Model) actionsPanel() string {
	content := titleStyle.Render("Actions") + "\n\n" + mutedStyle.Render("space  reveal value\n\nExport, copy, and edit\narrive in the next milestone.")
	return panelStyle.Width(30).Height(8).Render(content)
}

func (m Model) cursorFor(target pane) int {
	switch target {
	case projectsPane:
		return m.projectCursor
	case contextPane:
		return m.contextCursor
	default:
		return m.secretCursor
	}
}

func (m Model) projectItems() []string {
	projects := m.projects()
	items := make([]string, len(projects))
	for i, project := range projects {
		items[i] = project.Name
	}
	return items
}

func (m Model) contextItems() []string {
	contexts := m.contexts()
	items := make([]string, len(contexts))
	for i, item := range contexts {
		items[i] = item.label
	}
	return items
}

func (m Model) secretItems() []string {
	secrets := m.secrets()
	items := make([]string, len(secrets))
	for i, secret := range secrets {
		value := "••••••••"
		if m.revealValue && i == m.secretCursor {
			value = secret.Value
		}
		items[i] = fmt.Sprintf("%s=%s", secret.Key, value)
	}
	return items
}

func (m Model) location() string {
	contexts := m.contexts()
	if len(contexts) == 0 {
		return "Mock data"
	}
	return fmt.Sprintf("Mock data  /  %s", strings.ReplaceAll(contexts[m.contextCursor].label, "\x1b", ""))
}
