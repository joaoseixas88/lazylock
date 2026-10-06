package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

type diffKind uint8

const (
	diffChanged diffKind = iota
	diffOnlyLeft
	diffOnlyRight
	diffUnknown
	diffSame
)

type diffRow struct {
	key         string
	kind        diffKind
	left, right domain.Secret
}

// diffSecrets never calls a value it cannot read equal: a hidden side makes
// the row unknown, whatever the other side holds.
func diffSecrets(left, right []domain.Secret) []diffRow {
	index := func(secrets []domain.Secret) map[string]domain.Secret {
		byKey := make(map[string]domain.Secret, len(secrets))
		for _, s := range secrets {
			byKey[s.Key] = s
		}
		return byKey
	}
	l, r := index(left), index(right)
	keys := make([]string, 0, len(l)+len(r))
	for k := range l {
		keys = append(keys, k)
	}
	for k := range r {
		if _, ok := l[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b string) int {
		if n := strings.Compare(strings.ToLower(a), strings.ToLower(b)); n != 0 {
			return n
		}
		return strings.Compare(a, b)
	})

	rows := make([]diffRow, 0, len(keys))
	for _, k := range keys {
		ls, inLeft := l[k]
		rs, inRight := r[k]
		row := diffRow{key: k, left: ls, right: rs}
		switch {
		case !inRight:
			row.kind = diffOnlyLeft
		case !inLeft:
			row.kind = diffOnlyRight
		case ls.Hidden || rs.Hidden:
			row.kind = diffUnknown
		case ls.Value == rs.Value:
			row.kind = diffSame
		default:
			row.kind = diffChanged
		}
		rows = append(rows, row)
	}
	return rows
}

type comparedMsg struct {
	gen         int
	left, right []domain.Secret
	err         error
}

func (l loader) compare(gen int, a, b domain.Scope) tea.Cmd {
	return func() tea.Msg {
		msg := comparedMsg{gen: gen}
		var leftErr, rightErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			l.call(func(ctx context.Context) error {
				msg.left, leftErr = l.catalog.Secrets(ctx, a)
				return nil
			})
		}()
		go func() {
			defer wg.Done()
			l.call(func(ctx context.Context) error {
				msg.right, rightErr = l.catalog.Secrets(ctx, b)
				return nil
			})
		}()
		wg.Wait()
		msg.err = errors.Join(leftErr, rightErr)
		return msg
	}
}

type compareOverlay struct {
	a, b       domain.Scope
	candidates []domain.Scope
	pick       int
	state      loadState
	rows       []diffRow
	err        error
	gen        int
	cursor     int
	offset     int
	hideSame   bool
}

func (m *Model) openCompare() tea.Cmd {
	at, ok := m.scopes.current()
	if !ok {
		return m.notify(toastError, "Select a path to compare")
	}
	candidates := m.sameFolderIn(at)
	switch len(candidates) {
	case 0:
		return m.notify(toastError, "No other environment has "+at.Path)
	case 1:
		return m.startCompare(compareOverlay{a: at}, candidates[0])
	}
	m.reveal.mask()
	m.overlay = compareOverlay{a: at, candidates: candidates}
	return nil
}

func (m *Model) startCompare(c compareOverlay, b domain.Scope) tea.Cmd {
	m.seq++
	c.b, c.state, c.gen, c.err, c.rows, c.cursor, c.offset = b, stateLoading, m.seq, nil, nil, 0, 0
	m.reveal.mask()
	m.overlay = c
	return m.load.compare(c.gen, c.a, b)
}

func (m Model) compared(msg comparedMsg) (Model, tea.Cmd) {
	c, ok := m.overlay.(compareOverlay)
	if !ok || c.gen != msg.gen || c.state != stateLoading {
		return m, nil
	}
	if m.needsNewLogin(msg.err) {
		return m.expireSession()
	}
	if msg.err != nil {
		c.state, c.err = stateFailed, msg.err
	} else {
		c.state, c.rows = stateLoaded, diffSecrets(msg.left, msg.right)
	}
	m.overlay = c
	return m, nil
}

func (c compareOverlay) visible() []diffRow {
	if !c.hideSame {
		return c.rows
	}
	var rows []diffRow
	for _, row := range c.rows {
		if row.kind != diffSame {
			rows = append(rows, row)
		}
	}
	return rows
}

func (c compareOverlay) title(Model) string {
	if c.b == (domain.Scope{}) {
		return "[4] Compare " + printable(c.a.EnvName+" "+c.a.Path) + " with…"
	}
	return "[4] Compare " + printable(c.a.EnvName+" ↔ "+c.b.EnvName+" "+c.a.Path)
}

func (c compareOverlay) body(m Model, _, height int) []string {
	switch {
	case c.b == (domain.Scope{}):
		lines := []string{mutedStyle.Render("Compare " + c.a.Path + " with")}
		for i, s := range c.candidates {
			marker := "  "
			if i == c.pick {
				marker = selectedStyle.Render("› ")
			}
			lines = append(lines, marker+printable(s.EnvName))
		}
		return lines
	case c.state == stateLoading:
		return []string{mutedStyle.Render("Loading…")}
	case c.state == stateFailed:
		return []string{errorStyle.Render("Error: " + oneLine(c.err)), mutedStyle.Render("r  retry")}
	}

	rows := c.visible()
	lines := []string{c.summary(), ""}
	if len(rows) == 0 {
		return append(lines, mutedStyle.Render("Both environments hold the same secrets here."))
	}
	body := make([]string, len(rows))
	for i, row := range rows {
		marker := "  "
		if i == c.cursor {
			marker = selectedStyle.Render("› ")
		}
		body[i] = marker + c.render(m, row)
	}
	return append(lines, scroll(body, c.offset, max(0, height-len(lines)))...)
}

func (c compareOverlay) summary() string {
	count := map[diffKind]int{}
	for _, row := range c.rows {
		count[row.kind]++
	}
	parts := []string{fmt.Sprintf("%d changed", count[diffChanged])}
	if n := count[diffOnlyLeft]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d only in %s", n, c.a.EnvName))
	}
	if n := count[diffOnlyRight]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d only in %s", n, c.b.EnvName))
	}
	if n := count[diffUnknown]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d unreadable", n))
	}
	parts = append(parts, fmt.Sprintf("%d same", count[diffSame]))
	return mutedStyle.Render(printable(strings.Join(parts, " · ")))
}

func (c compareOverlay) render(m Model, row diffRow) string {
	shown := m.reveal.shows("diff:" + row.key)
	value := func(s domain.Secret) string {
		switch {
		case !shown:
			return "••••••••"
		case s.Value == "":
			return mutedStyle.Render("(empty)")
		}
		first, _, multiline := strings.Cut(s.Value, "\n")
		if multiline {
			return printable(first) + mutedStyle.Render(" …")
		}
		return printable(s.Value)
	}
	key := printable(row.key)
	switch row.kind {
	case diffOnlyLeft:
		return errorStyle.Render("- "+key) + "  " + value(row.left) + mutedStyle.Render("  only in "+c.a.EnvName)
	case diffOnlyRight:
		return selectedStyle.Render("+ "+key) + "  " + value(row.right) + mutedStyle.Render("  only in "+c.b.EnvName)
	case diffUnknown:
		return mutedStyle.Render("? " + key + "  no read access on one side")
	case diffSame:
		return mutedStyle.Render("= "+key) + "  " + value(row.left)
	}
	return "~ " + key + "  " + value(row.left) + mutedStyle.Render("  →  ") + value(row.right)
}

func (compareOverlay) hints(k keyMap) []key.Binding {
	return []key.Binding{k.Reveal, k.RevealAll, key.NewBinding(key.WithHelp("s", "hide same")), k.Retry, k.Close}
}

func (c compareOverlay) update(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if key.Matches(k, m.keys.Close) {
		m.overlay = nil
		m.reveal.mask()
		return m, nil
	}
	if c.b == (domain.Scope{}) {
		switch {
		case key.Matches(k, m.keys.Up):
			c.pick = max(0, c.pick-1)
		case key.Matches(k, m.keys.Down):
			c.pick = min(len(c.candidates)-1, c.pick+1)
		case k.Type == tea.KeyEnter:
			cmd := m.startCompare(c, c.candidates[c.pick])
			return m, cmd
		}
		m.overlay = c
		return m, nil
	}

	var cmd tea.Cmd
	rows := c.visible()
	switch {
	case key.Matches(k, m.keys.Retry) && c.state != stateLoading:
		cmd = m.startCompare(c, c.b)
		return m, cmd
	case c.state != stateLoaded:
	case key.Matches(k, m.keys.Up):
		c.cursor = max(0, c.cursor-1)
		m.reveal.one = ""
	case key.Matches(k, m.keys.Down):
		c.cursor = min(max(0, len(rows)-1), c.cursor+1)
		m.reveal.one = ""
	case key.Matches(k, m.keys.Reveal) && c.cursor < len(rows):
		cmd = m.toggleRevealOf("diff:" + rows[c.cursor].key)
	case key.Matches(k, m.keys.RevealAll):
		cmd = m.toggleRevealAll()
	case k.String() == "s":
		c.hideSame = !c.hideSame
		c.cursor, c.offset = 0, 0
	}
	listHeight := max(1, m.overlayHeight()-2)
	c.offset = min(c.offset, c.cursor)
	if c.cursor >= c.offset+listHeight {
		c.offset = c.cursor - listHeight + 1
	}
	m.overlay = c
	return m, cmd
}
