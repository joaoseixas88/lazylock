package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

// Each loaded message carries its own err rather than sharing one errMsg: a
// generic error message would lose the routing (which pane? which generation?)
// and force the discriminator and the generation to be added back anyway.
type projectsLoadedMsg struct {
	gen   int
	items []domain.Project
	err   error
}

type scopesLoadedMsg struct {
	gen       int
	projectID string // identity guard: the project this answers for
	items     []domain.Scope
	err       error
}

type secretsLoadedMsg struct {
	gen   int
	at    domain.Scope // identity guard: the scope this answers for
	items []domain.Secret
	err   error
}

// selectProjectMsg and selectScopeMsg land after the cursor has been still for
// the debounce interval, so holding j through a list does not fire one request
// per keystroke. Each carries the selection it was scheduled for: a settle is
// validated against its own pane, so moving in one pane never voids the other's
// cascade the way a single shared counter would.
type selectProjectMsg struct{ projectID string }
type selectScopeMsg struct{ at domain.Scope }

// loader owns everything a command needs. It holds a context.Context, which a
// struct normally should not: tea.Cmd is func() tea.Msg with nowhere to pass
// one, and bubbletea never waits for commands on shutdown, so without a ctx a
// slow request outlives the program.
type loader struct {
	catalog  domain.Catalog
	ctx      context.Context
	timeout  time.Duration
	debounce time.Duration
}

func (l loader) call(fn func(context.Context) error) {
	ctx, cancel := context.WithTimeout(l.ctx, l.timeout)
	defer cancel()
	_ = fn(ctx)
}

func (l loader) projects(gen int) tea.Cmd {
	return func() tea.Msg {
		msg := projectsLoadedMsg{gen: gen}
		l.call(func(ctx context.Context) error {
			msg.items, msg.err = l.catalog.Projects(ctx)
			return nil
		})
		return msg
	}
}

func (l loader) scopes(gen int, projectID string) tea.Cmd {
	return func() tea.Msg {
		msg := scopesLoadedMsg{gen: gen, projectID: projectID}
		l.call(func(ctx context.Context) error {
			msg.items, msg.err = l.catalog.Scopes(ctx, projectID)
			return nil
		})
		return msg
	}
}

func (l loader) secrets(gen int, at domain.Scope) tea.Cmd {
	return func() tea.Msg {
		msg := secretsLoadedMsg{gen: gen, at: at}
		l.call(func(ctx context.Context) error {
			msg.items, msg.err = l.catalog.Secrets(ctx, at)
			return nil
		})
		return msg
	}
}

func (l loader) settleProject(id string) tea.Cmd {
	if l.debounce == 0 {
		return func() tea.Msg { return selectProjectMsg{projectID: id} }
	}
	return tea.Tick(l.debounce, func(time.Time) tea.Msg { return selectProjectMsg{projectID: id} })
}

func (l loader) settleScope(at domain.Scope) tea.Cmd {
	if l.debounce == 0 {
		return func() tea.Msg { return selectScopeMsg{at: at} }
	}
	return tea.Tick(l.debounce, func(time.Time) tea.Msg { return selectScopeMsg{at: at} })
}
