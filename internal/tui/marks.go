package tui

import (
	"fmt"
	"maps"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/export"
)

// marks are copied on write: Model is passed by value, and two copies must
// never share one map.
type marks struct {
	at  domain.Scope
	ids map[string]bool
}

func (k marks) has(id string) bool { return k.ids[id] }

func (k marks) count() int { return len(k.ids) }

func (k marks) with(ids map[string]bool) marks {
	k.ids = ids
	return k
}

func (k marks) toggle(id string) marks {
	ids := maps.Clone(k.ids)
	if ids == nil {
		ids = map[string]bool{}
	}
	if ids[id] {
		delete(ids, id)
	} else {
		ids[id] = true
	}
	return k.with(ids)
}

func (k marks) prune(items []domain.Secret) marks {
	if len(k.ids) == 0 {
		return k
	}
	ids := map[string]bool{}
	for _, s := range items {
		if k.ids[s.ID] {
			ids[s.ID] = true
		}
	}
	return k.with(ids)
}

func (m *Model) toggleMark() {
	if s, ok := m.secrets.current(); ok {
		m.marks = m.marks.toggle(s.ID)
	}
}

func (m *Model) toggleMarkAll() {
	ids := map[string]bool{}
	for _, s := range m.secrets.items {
		ids[s.ID] = true
	}
	if len(ids) == m.marks.count() {
		ids = nil
	}
	m.marks = m.marks.with(ids)
}

// chosen is what an action applies to: the marked secrets in list order, or
// the selected one when nothing is marked.
func (m Model) chosen() []domain.Secret {
	if m.marks.count() == 0 {
		if s, ok := m.secrets.current(); ok {
			return []domain.Secret{s}
		}
		return nil
	}
	var picked []domain.Secret
	for _, s := range m.secrets.items {
		if m.marks.has(s.ID) {
			picked = append(picked, s)
		}
	}
	return picked
}

func (m *Model) copyLines() tea.Cmd {
	var lines, leftOut []string
	for _, s := range m.chosen() {
		if line, ok := export.Line(s); ok {
			lines = append(lines, line)
		} else {
			leftOut = append(leftOut, s.Key)
		}
	}
	if len(lines) == 0 {
		if len(leftOut) == 0 {
			return nil
		}
		return m.notify(toastError, "Nothing to copy: no read access to "+strings.Join(leftOut, ", "))
	}
	what := strings.SplitN(lines[0], "=", 2)[0] + " as KEY=value"
	if len(lines) > 1 {
		what = fmt.Sprintf("%d secrets as KEY=value", len(lines))
	}
	if len(leftOut) > 0 {
		what += " (left out " + strings.Join(leftOut, ", ") + ")"
	}
	return m.copyText(what, strings.Join(lines, "\n"))
}
