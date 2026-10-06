package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

type environment struct{ slug, name string }

func (m Model) environments() []environment {
	var envs []environment
	for _, s := range m.scopes.items {
		if !slices.ContainsFunc(envs, func(e environment) bool { return e.slug == s.EnvSlug }) {
			envs = append(envs, environment{slug: s.EnvSlug, name: s.EnvName})
		}
	}
	return envs
}

func (m *Model) cycleEnv(delta int) tea.Cmd {
	envs := m.environments()
	if m.scopes.state != stateLoaded || len(envs) < 2 {
		return nil
	}
	at := slices.IndexFunc(envs, func(e environment) bool { return e.slug == m.scopes.group })
	next := envs[((at+delta)%len(envs)+len(envs))%len(envs)]
	want := m.chosenPath
	if current, ok := m.scopes.current(); ok && want == "" {
		want = current.Path
	}
	return m.reselect(contextPane, func() {
		m.scopes.setGroup(next.slug)
		if want != "" {
			m.selectNearest(want)
		}
	})
}

// rememberPath records the folder the user picked in the paths pane, so that
// switching through an environment that lacks it, which falls back to a
// parent, does not lose it for the next environment that has it.
func (m *Model) rememberPath() {
	if at, ok := m.scopes.current(); ok && m.activePane == contextPane {
		m.chosenPath = at.Path
	}
}

// selectNearest selects path in the active environment, or else its closest
// ancestor there, or else leaves the cursor on the first row.
func (m *Model) selectNearest(path string) {
	for {
		if m.scopes.selectWhere(func(s domain.Scope) bool { return s.Path == path }) || path == "/" {
			return
		}
		path = parentPath(path)
	}
}

func parentPath(path string) string {
	if i := strings.LastIndex(strings.TrimSuffix(path, "/"), "/"); i > 0 {
		return path[:i]
	}
	return "/"
}
