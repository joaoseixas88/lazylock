package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// action is one entry of the actions pane. One with a binding runs by sending
// its key through handleKey, so the menu and the hotkey cannot drift apart; run
// is only for actions that have no key.
type action struct {
	binding key.Binding
	label   string
	run     func(Model) (Model, tea.Cmd)
}

func (a action) title() string {
	if a.label != "" {
		return a.label
	}
	return a.binding.Help().Desc
}

func (m Model) actions() []action {
	k := m.keys
	actions := []action{
		{binding: k.RevealAll},
		{binding: k.Copy},
		{binding: k.CopyLines},
		{binding: k.MarkAll},
		{binding: k.Export},
		{binding: k.Filter, label: "filter secrets"},
		{binding: k.Compare},
		{binding: k.Open},
	}
	if _, ok := m.writer(); ok {
		actions = append(actions, action{binding: k.New}, action{binding: k.Edit}, action{binding: k.Delete}, action{binding: k.CopyTo})
	}
	if _, ok := m.projectCreator(); ok {
		actions = append(actions, action{binding: k.NewProject})
	}
	return append(actions, []action{
		{binding: k.Logout},
		{label: "switch instance", run: func(m Model) (Model, tea.Cmd) {
			cmd := m.askSwitchInstance()
			return m, cmd
		}},
		{binding: k.Help},
	}...)
}

func (m Model) runAction(a action) (tea.Model, tea.Cmd) {
	m.activePane = secretsPane
	if a.run != nil {
		return a.run(m)
	}
	return m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(a.binding.Keys()[0])})
}

func (m Model) actionItems() []string {
	actions := m.actions()
	items := make([]string, len(actions))
	for i, a := range actions {
		items[i] = mutedStyle.Render(fmt.Sprintf("%-4s", a.binding.Help().Key)) + a.title()
	}
	return items
}
