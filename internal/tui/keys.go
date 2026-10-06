package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up, Down                           key.Binding
	Project, Context, Actions, Secrets key.Binding
	Reveal, RevealAll, Copy, Retry     key.Binding
	Help, Close, Quit, ForceQuit       key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Project:   key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "projects")),
		Context:   key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "paths / environments")),
		Actions:   key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "actions")),
		Secrets:   key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "secrets")),
		Reveal:    key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "reveal")),
		RevealAll: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "reveal all")),
		Copy:      key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy value")),
		Retry:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Close:     key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc", "close")),
		Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		ForceQuit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit from anywhere")),
	}
}

type bindingGroup struct {
	name     string
	bindings []key.Binding
}

func (k keyMap) groups() []bindingGroup {
	return []bindingGroup{
		{"Panes", []key.Binding{k.Project, k.Context, k.Actions, k.Secrets, k.Up, k.Down, k.Retry}},
		{"Secrets", []key.Binding{k.Reveal, k.RevealAll, k.Copy}},
		{"General", []key.Binding{k.Help, k.Close, k.Quit, k.ForceQuit}},
	}
}

func (k keyMap) footer() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Reveal, k.RevealAll, k.Copy, k.Retry, k.Help, k.Quit}
}
