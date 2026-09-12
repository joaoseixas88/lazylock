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

type level int

const (
	connectionsLevel level = iota
	projectsLevel
	environmentsLevel
	foldersLevel
	secretsLevel
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Back   key.Binding
	Reveal key.Binding
	Quit   key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Back:   key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
		Reveal: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "reveal")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Back, k.Reveal, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

type Model struct {
	catalog domain.Catalog
	help    help.Model
	keys    keyMap

	level  level
	cursor int

	selectedConnectionID  string
	selectedProjectID     string
	selectedEnvironmentID string
	selectedFolderID      string
	revealValue           bool
}

func New(catalog domain.Catalog) Model {
	return Model{catalog: catalog, help: help.New(), keys: defaultKeys()}
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
	case key.Matches(keyMsg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		m.revealValue = false
	case key.Matches(keyMsg, m.keys.Down):
		if m.cursor < m.itemCount()-1 {
			m.cursor++
		}
		m.revealValue = false
	case key.Matches(keyMsg, m.keys.Enter):
		m.open()
	case key.Matches(keyMsg, m.keys.Back):
		m.back()
	case m.level == secretsLevel && key.Matches(keyMsg, m.keys.Reveal):
		m.revealValue = !m.revealValue
	}
	return m, nil
}

func (m *Model) open() {
	if m.itemCount() == 0 || m.level == secretsLevel {
		return
	}
	switch m.level {
	case connectionsLevel:
		m.selectedConnectionID = m.catalog.Connections()[m.cursor].ID
	case projectsLevel:
		m.selectedProjectID = m.catalog.Projects(m.selectedConnectionID)[m.cursor].ID
	case environmentsLevel:
		m.selectedEnvironmentID = m.catalog.Environments(m.selectedProjectID)[m.cursor].ID
	case foldersLevel:
		m.selectedFolderID = m.catalog.Folders(m.selectedEnvironmentID, "")[m.cursor].ID
	}
	m.level++
	m.cursor = 0
	m.revealValue = false
}

func (m *Model) back() {
	if m.level == connectionsLevel {
		return
	}
	switch m.level {
	case secretsLevel:
		m.selectedFolderID = ""
	case foldersLevel:
		m.selectedEnvironmentID = ""
	case environmentsLevel:
		m.selectedProjectID = ""
	case projectsLevel:
		m.selectedConnectionID = ""
	}
	m.level--
	m.cursor = 0
	m.revealValue = false
}

func (m Model) itemCount() int {
	switch m.level {
	case connectionsLevel:
		return len(m.catalog.Connections())
	case projectsLevel:
		return len(m.catalog.Projects(m.selectedConnectionID))
	case environmentsLevel:
		return len(m.catalog.Environments(m.selectedProjectID))
	case foldersLevel:
		return len(m.catalog.Folders(m.selectedEnvironmentID, ""))
	case secretsLevel:
		return len(m.catalog.Secrets(m.selectedFolderID))
	}
	return 0
}

func (m Model) View() string {
	lines := []string{titleStyle.Render("LazyLock"), mutedStyle.Render(m.breadcrumb()), ""}
	items := m.items()
	if len(items) == 0 {
		lines = append(lines, mutedStyle.Render("No items found."))
	}
	for i, item := range items {
		prefix := "  "
		if i == m.cursor {
			prefix = selectedStyle.Render("› ")
		}
		lines = append(lines, prefix+item)
	}
	lines = append(lines, "", mutedStyle.Render(m.help.View(m.keys)))
	return strings.Join(lines, "\n")
}

func (m Model) breadcrumb() string {
	labels := []string{"Connections"}
	if m.selectedConnectionID != "" {
		labels = append(labels, "Projects")
	}
	if m.selectedProjectID != "" {
		labels = append(labels, "Environments")
	}
	if m.selectedEnvironmentID != "" {
		labels = append(labels, "Folders")
	}
	if m.selectedFolderID != "" {
		labels = append(labels, "Secrets")
	}
	return strings.Join(labels, " / ")
}

func (m Model) items() []string {
	var items []string
	switch m.level {
	case connectionsLevel:
		for _, item := range m.catalog.Connections() {
			items = append(items, fmt.Sprintf("%s  %s", item.Name, mutedStyle.Render(item.Provider)))
		}
	case projectsLevel:
		for _, item := range m.catalog.Projects(m.selectedConnectionID) {
			items = append(items, item.Name)
		}
	case environmentsLevel:
		for _, item := range m.catalog.Environments(m.selectedProjectID) {
			items = append(items, item.Name)
		}
	case foldersLevel:
		for _, item := range m.catalog.Folders(m.selectedEnvironmentID, "") {
			items = append(items, item.Name)
		}
	case secretsLevel:
		for _, item := range m.catalog.Secrets(m.selectedFolderID) {
			value := "••••••••"
			if m.revealValue && len(items) == m.cursor {
				value = item.Value
			}
			items = append(items, fmt.Sprintf("%s=%s", item.Key, value))
		}
	}
	return items
}
