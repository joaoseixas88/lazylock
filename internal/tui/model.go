package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

type pane int

const (
	projectsPane pane = iota
	contextPane
	actionsPane
	secretsPane
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

type keyMap struct{ Up, Down, Project, Context, Actions, Secrets, Reveal, Quit key.Binding }

func defaultKeys() keyMap {
	return keyMap{
		Up: key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")), Down: key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Project: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "projects")), Context: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "context")),
		Actions: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "actions")), Secrets: key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "secrets")),
		Reveal: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "reveal")), Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

type context struct{ folderID, environment, folder string }

type Model struct {
	catalog                                                                           domain.Catalog
	keys                                                                              keyMap
	activePane                                                                        pane
	projectCursor, contextCursor, secretCursor                                        int
	revealValue                                                                       bool
	connectionID, projectID                                                           string
	width, height, leftWidth, rightWidth, projectHeight, contextHeight, actionsHeight int
}

func New(catalog domain.Catalog) Model {
	m := Model{catalog: catalog, keys: defaultKeys()}
	connections := catalog.Connections()
	if len(connections) > 0 {
		m.connectionID = connections[0].ID
		projects := m.projects()
		if len(projects) > 0 {
			m.projectID = projects[0].ID
		}
	}
	m.resize(100, 30)
	return m
}
func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.resize(size.Width, size.Height)
		return m, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
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
		m.move(-1)
	case key.Matches(k, m.keys.Down):
		m.move(1)
	case m.activePane == secretsPane && key.Matches(k, m.keys.Reveal):
		m.revealValue = !m.revealValue
	}
	if m.activePane != secretsPane {
		m.revealValue = false
	}
	return m, nil
}

func (m *Model) resize(width, height int) {
	m.width, m.height = max(0, width), max(0, height)
	m.leftWidth = max(0, (m.width-1)/3)
	m.rightWidth = max(0, m.width-m.leftWidth-1)
	available := max(0, m.height-1) // The footer owns exactly one row.
	m.projectHeight = available / 3
	m.actionsHeight = available / 3
	m.contextHeight = available - m.projectHeight - m.actionsHeight
}
func (m *Model) move(delta int) {
	switch m.activePane {
	case projectsPane:
		m.projectCursor = clamp(m.projectCursor+delta, len(m.projects()))
		if projects := m.projects(); len(projects) > 0 {
			m.projectID = projects[m.projectCursor].ID
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
func (m Model) contexts() (values []context) {
	for _, env := range m.catalog.Environments(m.projectID) {
		for _, folder := range m.catalog.Folders(env.ID, "") {
			values = append(values, context{folder.ID, env.Name, folder.Name})
		}
	}
	return
}
func (m Model) secrets() []domain.Secret {
	contexts := m.contexts()
	if len(contexts) == 0 {
		return nil
	}
	return m.catalog.Secrets(contexts[m.contextCursor].folderID)
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < 16 || m.height < 4 {
		return ansi.Truncate("Resize terminal · q quit", m.width, "")
	}
	left := lipgloss.JoinVertical(lipgloss.Left,
		m.panel("[1] Projects", m.projectItems(), projectsPane, m.leftWidth, m.projectHeight),
		m.panel("[2] Paths / Environments", m.contextItems(), contextPane, m.leftWidth, m.contextHeight),
		m.actionsPanel(),
	)
	right := m.panel("[4] Secrets", m.secretItems(), secretsPane, m.rightWidth, m.height-1)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	footer := mutedStyle.Render(ansi.Truncate("↑/k up  •  ↓/j down  •  space reveal  •  q quit", m.width, ""))
	return body + "\n" + footer
}
func (m Model) panel(title string, items []string, target pane, width, height int) string {
	lines := items
	if len(lines) == 0 {
		lines = []string{mutedStyle.Render("No items found.")}
	}
	for i, item := range lines {
		if target == m.activePane && i == m.cursorFor(target) {
			lines[i] = selectedStyle.Render("› ") + item
		} else {
			lines[i] = "  " + item
		}
	}
	// Keep the selected row visible when the terminal becomes shorter.
	capacity := max(0, height-2)
	if target != actionsPane && capacity > 0 {
		start := max(0, m.cursorFor(target)-capacity+1)
		lines = lines[min(start, len(lines)):]
	}
	return frame(title, lines, width, height, target == m.activePane)
}
func (m Model) actionsPanel() string {
	return frame("[3] Actions", []string{mutedStyle.Render("space  reveal value"), "", mutedStyle.Render("Export, copy, and edit"), mutedStyle.Render("arrive in the next milestone.")}, m.leftWidth, m.actionsHeight, m.activePane == actionsPane)
}

func frame(title string, lines []string, width, height int, focused bool) string {
	if width < 3 || height < 1 {
		return ""
	}
	border := mutedStyle
	if focused {
		border = selectedStyle
	}
	caption := ansi.Truncate(title, width-3, "")
	titleText := border.Render(caption)
	top := border.Render("╭─") + titleText + border.Render(strings.Repeat("─", max(0, width-lipgloss.Width(caption)-3))+"╮")
	if height == 1 {
		return top
	}
	innerWidth, innerHeight := width-2, height-2
	content := make([]string, 0, innerHeight)
	content = append(content, lines...)
	for len(content) < innerHeight {
		content = append(content, "")
	}
	content = content[:innerHeight]
	for i, line := range content {
		line = ansi.Truncate(strings.ReplaceAll(line, "\n", " "), innerWidth, "…")
		content[i] = border.Render("│") + line + strings.Repeat(" ", max(0, innerWidth-lipgloss.Width(line))) + border.Render("│")
	}
	bottom := border.Render("╰" + strings.Repeat("─", width-2) + "╯")
	return strings.Join(append([]string{top}, append(content, bottom)...), "\n")
}
func (m Model) cursorFor(target pane) int {
	if target == projectsPane {
		return m.projectCursor
	}
	if target == contextPane {
		return m.contextCursor
	}
	return m.secretCursor
}
func (m Model) projectItems() (items []string) {
	for _, project := range m.projects() {
		items = append(items, project.Name)
	}
	return
}
func (m Model) contextItems() (items []string) {
	for _, item := range m.contexts() {
		items = append(items, fmt.Sprintf("%s  %s", item.environment, mutedStyle.Render(item.folder)))
	}
	return
}
func (m Model) secretItems() (items []string) {
	for i, secret := range m.secrets() {
		value := "••••••••"
		if m.revealValue && i == m.secretCursor {
			value = secret.Value
		}
		items = append(items, fmt.Sprintf("%s=%s", secret.Key, value))
	}
	return
}
