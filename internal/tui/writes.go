package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

func (m Model) WithWrites(on bool) Model {
	m.writes = on
	return m
}

func (m Model) writer() (domain.Writer, bool) {
	if !m.writes {
		return nil, false
	}
	w, ok := m.load.catalog.(domain.Writer)
	return w, ok
}

func (m Model) projectCreator() (domain.ProjectCreator, bool) {
	if !m.writes {
		return nil, false
	}
	c, ok := m.load.catalog.(domain.ProjectCreator)
	return c, ok
}

func (m *Model) readOnly() tea.Cmd {
	if !m.writes {
		return m.notify(toastError, "Read-only: LazyLock was started with -readonly")
	}
	return m.notify(toastError, "Read-only: this connection cannot write")
}

func looksLikeProduction(at domain.Scope) bool {
	return strings.Contains(strings.ToLower(at.EnvSlug), "prod") || strings.Contains(strings.ToLower(at.EnvName), "prod")
}

type gateStep uint8

const (
	gateReview gateStep = iota
	gateTyping
	gateWriting
)

// gate is how every write ends: a review accepted only with y, then, in an
// environment that looks like production, its name typed out, then the write
// itself, during which keys are ignored so a second y cannot send it twice.
type gate struct {
	step  gateStep
	env   domain.Scope
	input textinput.Model
	err   error
}

// accept moves past the review and reports whether the write starts now.
func (g gate) accept() (gate, bool) {
	g.err = nil
	if g.step == gateReview && looksLikeProduction(g.env) {
		g.step = gateTyping
		g.input = staticInput("› ", "")
		return g, false
	}
	g.step = gateWriting
	return g, true
}

// typed takes every key while the name is being typed, so letters that are
// hotkeys elsewhere land in the field. It reports start on a matching enter
// and back on esc.
func (g gate) typed(k tea.KeyMsg) (next gate, start, back bool, cmd tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		g.step, g.err = gateReview, nil
		return g, false, true, nil
	case tea.KeyEnter:
		typed := strings.TrimSpace(g.input.Value())
		if strings.EqualFold(typed, g.env.EnvName) || strings.EqualFold(typed, g.env.EnvSlug) {
			g.step = gateWriting
			return g, true, false, nil
		}
		g.err = errors.New("that is not " + g.env.EnvName)
		return g, false, false, nil
	}
	g.input, cmd = g.input.Update(k)
	return g, false, false, cmd
}

func (g gate) lines() []string {
	var lines []string
	switch g.step {
	case gateTyping:
		lines = append(lines, "", errorStyle.Render("This is "+printable(g.env.EnvName)+"."),
			"Type its name to confirm:", g.input.View())
	case gateWriting:
		lines = append(lines, "", mutedStyle.Render("Writing…"))
	}
	if g.err != nil {
		lines = append(lines, "", errorStyle.Render(oneLine(g.err)))
	}
	return lines
}

func (g gate) hints(yes string) []key.Binding {
	switch g.step {
	case gateTyping:
		return []key.Binding{key.NewBinding(key.WithHelp("enter", "confirm")), key.NewBinding(key.WithHelp("esc", "back"))}
	case gateWriting:
		return nil
	}
	return []key.Binding{key.NewBinding(key.WithHelp("y", yes)), key.NewBinding(key.WithHelp("n", "back"))}
}

// awaitingWrite is an overlay with a write in flight. rejected takes it back
// to its review with the server's answer, keeping whatever the user typed.
type awaitingWrite interface {
	overlay
	inFlight() int
	rejected(err error) overlay
}

type writtenMsg struct {
	gen        int
	at         domain.Scope
	done       string // "Deleted 2 secrets from Production /app"
	doing      string // "delete 2 secrets from Production /app"
	outcome    domain.Outcome
	err        error
	clearMarks bool
}

func writeCmd(ctx context.Context, msg writtenMsg, write func(context.Context) (domain.Outcome, error)) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		msg.outcome, msg.err = write(ctx)
		return msg
	}
}

// written never drops a result: whatever the overlay is doing, the user hears
// how the write ended. An error that is not a rejection leaves the outcome
// unknown, so nothing is retried and the list is reloaded to show the truth.
func (m Model) written(msg writtenMsg) (Model, tea.Cmd) {
	if m.needsNewLogin(msg.err) {
		return m.expireSession()
	}
	waiting, ours := m.overlay.(awaitingWrite)
	ours = ours && waiting.inFlight() == msg.gen
	rejected := errors.Is(msg.err, domain.ErrRejected)
	if ours && !(msg.err != nil && rejected) {
		m.overlay = nil
	}

	var cmd tea.Cmd
	switch {
	case msg.err != nil && rejected && ours:
		m.overlay = waiting.rejected(msg.err)
		return m, nil
	case msg.err != nil && rejected:
		return m, m.notify(toastError, "Could not "+msg.doing+": "+oneLine(msg.err))
	case msg.err != nil:
		cmd = m.notify(toastError, "Could not tell whether the request to "+msg.doing+" went through ("+oneLine(msg.err)+"); reloaded")
	case msg.outcome.Pending:
		return m, m.notify(toastInfo, "Opened a change request to "+msg.doing+"; nothing changes until it is approved")
	default:
		if msg.clearMarks {
			m.marks = m.marks.with(nil)
		}
		cmd = m.notify(toastInfo, msg.done)
	}
	return m, tea.Batch(cmd, m.reloadIfShowing(msg.at))
}

func (m *Model) reloadIfShowing(at domain.Scope) tea.Cmd {
	if current, ok := m.scopes.current(); ok && current == at && m.scopes.state == stateLoaded {
		return m.loadSecrets(current)
	}
	return nil
}
