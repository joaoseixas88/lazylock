package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

const listedKeys = 8

type deleteOverlay struct {
	at      domain.Scope
	keys    []string
	leftOut []string
	gate    gate
	gen     int
}

func (m *Model) openDelete() tea.Cmd {
	if _, ok := m.writer(); !ok {
		return m.readOnly()
	}
	at, ok := m.scopes.current()
	if !ok || m.secrets.state != stateLoaded {
		return m.notify(toastError, "Nothing to delete yet")
	}
	d := deleteOverlay{at: at, gate: gate{env: at}}
	var from []string
	for _, s := range m.chosen() {
		if s.ImportedFrom != (domain.Scope{}) {
			d.leftOut = append(d.leftOut, s.Key)
			from = append(from, m.envName(s.ImportedFrom.EnvSlug)+" "+s.ImportedFrom.Path)
			continue
		}
		d.keys = append(d.keys, s.Key)
	}
	if len(d.keys) == 0 {
		if len(from) == 0 {
			return nil
		}
		return m.notify(toastError, strings.Join(d.leftOut, ", ")+" is imported; delete it in "+from[0])
	}
	m.overlay = d
	return nil
}

func (d deleteOverlay) subject() string {
	what := d.keys[0]
	if len(d.keys) > 1 {
		what = fmt.Sprintf("%d secrets", len(d.keys))
	}
	return what + " from " + d.at.EnvName + " " + d.at.Path
}

func (d deleteOverlay) title(Model) string {
	return "[4] Delete · " + printable(d.at.EnvName+" "+d.at.Path)
}

func (d deleteOverlay) body(Model, int, int) []string {
	lines := []string{"Delete " + printable(d.subject()) + "?", ""}
	for i, k := range d.keys {
		if i == listedKeys {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("  … and %d more", len(d.keys)-listedKeys)))
			break
		}
		lines = append(lines, "  "+printable(k))
	}
	if len(d.leftOut) > 0 {
		lines = append(lines, "", mutedStyle.Render("Left out, imported from elsewhere: "+printable(strings.Join(d.leftOut, ", "))))
	}
	return append(lines, d.gate.lines()...)
}

func (d deleteOverlay) hints(keyMap) []key.Binding { return d.gate.hints("delete") }

func (d deleteOverlay) inFlight() int {
	if d.gate.step == gateWriting {
		return d.gen
	}
	return 0
}

func (d deleteOverlay) rejected(err error) overlay {
	d.gate.step, d.gate.err = gateReview, err
	return d
}

func (d deleteOverlay) update(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch d.gate.step {
	case gateWriting:
		return m, nil
	case gateTyping:
		g, start, _, cmd := d.gate.typed(k)
		d.gate = g
		if start {
			return d.start(m)
		}
		m.overlay = d
		return m, cmd
	}
	switch k.String() {
	case "y":
		g, start := d.gate.accept()
		d.gate = g
		if start {
			return d.start(m)
		}
	case "n", "esc", "q":
		m.overlay = nil
		return m, nil
	}
	m.overlay = d
	return m, nil
}

func (d deleteOverlay) start(m Model) (Model, tea.Cmd) {
	w, ok := m.writer()
	if !ok {
		m.overlay = nil
		return m, m.readOnly()
	}
	m.seq++
	d.gen, d.gate.step = m.seq, gateWriting
	m.overlay = d
	msg := writtenMsg{gen: d.gen, at: d.at, done: "Deleted " + d.subject(), doing: "delete " + d.subject(), clearMarks: true}
	return m, writeCmd(m.load.ctx, msg, func(ctx context.Context) (domain.Outcome, error) {
		return w.Delete(ctx, d.at, d.keys)
	})
}
