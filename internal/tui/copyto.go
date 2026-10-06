package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

type copyKind uint8

const (
	copyCreate copyKind = iota
	copyOverwrite
	copyOverwriteUnread
	copyHideImport
	copySame
)

type copyRow struct {
	key            string
	kind           copyKind
	value, comment string
	before         string
}

// maxUpsertBytes stays under the server's 1 MiB request body limit, with room
// for the JSON around the values.
const maxUpsertBytes = 900 << 10

// planCopy decides, per key, what copying it into the target would do. Values
// compare as the server stores them, so a copy that changes nothing is
// skipped, and a target value the session cannot read is never called equal.
func planCopy(keys []string, source, targetStored, targetVisible []domain.Secret, normalize func(string) string) []copyRow {
	byKey := func(secrets []domain.Secret) map[string]domain.Secret {
		out := make(map[string]domain.Secret, len(secrets))
		for _, s := range secrets {
			out[s.Key] = s
		}
		return out
	}
	from, stored, visible := byKey(source), byKey(targetStored), byKey(targetVisible)
	var rows []copyRow
	for _, k := range keys {
		src, ok := from[k]
		if !ok {
			continue
		}
		row := copyRow{key: k, value: src.Value, comment: src.Comment}
		dst, exists := stored[k]
		switch {
		case exists && dst.Hidden:
			row.kind = copyOverwriteUnread
		case exists && normalize(dst.Value) == normalize(src.Value):
			row.kind = copySame
		case exists:
			row.kind, row.before = copyOverwrite, dst.Value
		case visible[k].Key != "":
			row.kind = copyHideImport
		default:
			row.kind = copyCreate
		}
		rows = append(rows, row)
	}
	return rows
}

func writes(rows []copyRow) []domain.Draft {
	var drafts []domain.Draft
	for _, r := range rows {
		switch r.kind {
		case copySame:
		case copyCreate, copyHideImport:
			drafts = append(drafts, domain.Draft{Key: r.key, Value: r.value, Comment: r.comment})
		default:
			drafts = append(drafts, domain.Draft{Key: r.key, Value: r.value})
		}
	}
	return drafts
}

type copyPlannedMsg struct {
	gen  int
	rows []copyRow
	err  error
}

type copyReplannedMsg struct {
	gen  int
	rows []copyRow
}

type copyToOverlay struct {
	at, target domain.Scope
	candidates []domain.Scope
	pick       int
	keys       []string
	leftOut    []string
	loading    bool
	rows       []copyRow
	moved      bool
	reveal     bool
	gate       gate
	gen        int
}

func (m *Model) openCopyTo() tea.Cmd {
	if _, ok := m.writer(); !ok {
		return m.readOnly()
	}
	at, ok := m.scopes.current()
	if !ok || m.secrets.state != stateLoaded {
		return m.notify(toastError, "Nothing to copy yet")
	}
	c := copyToOverlay{at: at}
	for _, s := range m.chosen() {
		switch {
		case s.ImportedFrom != (domain.Scope{}):
			c.leftOut = append(c.leftOut, s.Key+" (imported)")
		case s.Hidden:
			c.leftOut = append(c.leftOut, s.Key+" (no read access)")
		default:
			c.keys = append(c.keys, s.Key)
		}
	}
	if len(c.keys) == 0 {
		return m.notify(toastError, "Nothing to copy: "+strings.Join(c.leftOut, ", "))
	}
	c.candidates = m.sameFolderIn(at)
	switch len(c.candidates) {
	case 0:
		return m.notify(toastError, "No other environment has "+at.Path)
	case 1:
		return m.planCopyTo(c, c.candidates[0])
	}
	m.overlay = c
	return nil
}

func (m *Model) planCopyTo(c copyToOverlay, target domain.Scope) tea.Cmd {
	w, _ := m.writer()
	m.seq++
	c.target, c.loading, c.gen, c.rows, c.moved, c.reveal = target, true, m.seq, nil, false, false
	c.gate = gate{env: target}
	m.overlay = c
	catalog, ctx, gen, at, keys := m.load.catalog, m.load.ctx, c.gen, c.at, c.keys
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		rows, err := loadCopyPlan(ctx, w, catalog, at, target, keys)
		return copyPlannedMsg{gen: gen, rows: rows, err: err}
	}
}

func loadCopyPlan(ctx context.Context, w domain.Writer, catalog domain.Catalog, at, target domain.Scope, keys []string) ([]copyRow, error) {
	source, err := w.Raw(ctx, at)
	if err != nil {
		return nil, err
	}
	stored, err := w.Raw(ctx, target)
	if err != nil {
		return nil, err
	}
	visible, err := catalog.Secrets(ctx, target)
	if err != nil {
		return nil, err
	}
	return planCopy(keys, source, stored, visible, w.Normalize), nil
}

func (m Model) copyPlanned(msg copyPlannedMsg) (Model, tea.Cmd) {
	c, ok := m.overlay.(copyToOverlay)
	if !ok || !c.loading || c.gen != msg.gen {
		return m, nil
	}
	if m.needsNewLogin(msg.err) {
		return m.expireSession()
	}
	if msg.err != nil {
		m.overlay = nil
		return m, m.notify(toastError, "Could not plan the copy: "+oneLine(msg.err))
	}
	c.loading, c.rows = false, msg.rows
	m.overlay = c
	return m, nil
}

func (m Model) copyReplanned(msg copyReplannedMsg) (Model, tea.Cmd) {
	c, ok := m.overlay.(copyToOverlay)
	if !ok || c.inFlight() != msg.gen {
		return m, m.notify(toastError, "Nothing was copied: the target changed while copying")
	}
	c.rows, c.moved, c.gate = msg.rows, true, gate{env: c.target}
	m.overlay = c
	return m, nil
}

func (c copyToOverlay) count(kinds ...copyKind) int {
	n := 0
	for _, r := range c.rows {
		if slices.Contains(kinds, r.kind) {
			n++
		}
	}
	return n
}

func (c copyToOverlay) summary() string {
	n := len(writes(c.rows))
	what := fmt.Sprintf("%d secrets", n)
	if n == 1 {
		what = "1 secret"
	}
	return what + " to " + c.target.EnvName + " " + c.target.Path
}

func (c copyToOverlay) title(Model) string {
	if c.target == (domain.Scope{}) {
		return "[4] Copy from " + printable(c.at.EnvName+" "+c.at.Path) + " to…"
	}
	return "[4] Copy " + printable(c.at.EnvName+" → "+c.target.EnvName+" "+c.at.Path)
}

func (c copyToOverlay) body(_ Model, _, _ int) []string {
	switch {
	case c.target == (domain.Scope{}):
		lines := []string{mutedStyle.Render("Copy " + printable(c.at.Path) + " to")}
		for i, s := range c.candidates {
			marker := "  "
			if i == c.pick {
				marker = selectedStyle.Render("› ")
			}
			lines = append(lines, marker+printable(s.EnvName))
		}
		return lines
	case c.loading:
		return []string{mutedStyle.Render("Comparing with " + printable(c.target.EnvName) + "…")}
	}

	value := func(v string) string {
		if !c.reveal {
			return "••••••••"
		}
		first, _, multiline := strings.Cut(v, "\n")
		if multiline {
			return printable(first) + mutedStyle.Render(" …")
		}
		return printable(v)
	}
	var lines []string
	if len(writes(c.rows)) == 0 {
		lines = append(lines, "Nothing to copy: "+printable(c.target.EnvName)+" already holds the same values.")
	} else {
		lines = append(lines, "Copy "+printable(c.summary())+"?")
	}
	lines = append(lines, "")
	for _, r := range c.rows {
		key := printable(r.key)
		switch r.kind {
		case copyCreate:
			lines = append(lines, selectedStyle.Render("+ ")+key+"  "+value(r.value)+mutedStyle.Render("  new"))
		case copyHideImport:
			lines = append(lines, selectedStyle.Render("+ ")+key+"  "+value(r.value)+mutedStyle.Render("  hides the imported one"))
		case copyOverwrite:
			lines = append(lines, errorStyle.Render("~ ")+key+"  "+value(r.before)+mutedStyle.Render("  →  ")+value(r.value))
		case copyOverwriteUnread:
			lines = append(lines, errorStyle.Render("~ ")+key+"  "+value(r.value)+mutedStyle.Render("  overwrites a value you cannot read"))
		case copySame:
			lines = append(lines, mutedStyle.Render("= "+key+"  same, skipped"))
		}
		if r.kind != copySame && strings.Contains(r.value, "${") {
			lines = append(lines, mutedStyle.Render("    references resolve in "+printable(c.target.EnvName)))
		}
	}
	if len(c.leftOut) > 0 {
		lines = append(lines, "", mutedStyle.Render("Left out: "+printable(strings.Join(c.leftOut, ", "))))
	}
	if c.moved {
		lines = append(lines, "", errorStyle.Render("! ")+printable(c.target.EnvName)+" changed since the review; this is what it holds now.")
	}
	return append(lines, c.gate.lines()...)
}

func (c copyToOverlay) hints(k keyMap) []key.Binding {
	switch {
	case c.target == (domain.Scope{}):
		return []key.Binding{key.NewBinding(key.WithHelp("↑/↓", "choose")), key.NewBinding(key.WithHelp("enter", "compare")), k.Close}
	case c.loading:
		return []key.Binding{key.NewBinding(key.WithHelp("esc", "cancel"))}
	}
	hints := c.gate.hints("copy")
	if c.gate.step == gateReview {
		hints = append(hints, key.NewBinding(key.WithHelp("space", "show values")))
	}
	return hints
}

func (c copyToOverlay) inFlight() int {
	if c.gate.step == gateWriting {
		return c.gen
	}
	return 0
}

func (c copyToOverlay) rejected(err error) overlay {
	c.gate.step, c.gate.err = gateReview, err
	return c
}

func (c copyToOverlay) update(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case c.target == (domain.Scope{}):
		switch {
		case key.Matches(k, m.keys.Up):
			c.pick = max(0, c.pick-1)
		case key.Matches(k, m.keys.Down):
			c.pick = min(len(c.candidates)-1, c.pick+1)
		case k.Type == tea.KeyEnter:
			cmd := m.planCopyTo(c, c.candidates[c.pick])
			return m, cmd
		case key.Matches(k, m.keys.Close):
			m.overlay = nil
			return m, nil
		}
		m.overlay = c
		return m, nil
	case c.loading:
		if k.Type == tea.KeyEsc {
			m.overlay = nil
		}
		return m, nil
	}
	switch c.gate.step {
	case gateWriting:
		return m, nil
	case gateTyping:
		g, start, _, cmd := c.gate.typed(k)
		c.gate = g
		if start {
			return c.start(m)
		}
		m.overlay = c
		return m, cmd
	}
	switch k.String() {
	case "y":
		if len(writes(c.rows)) == 0 {
			m.overlay = nil
			return m, nil
		}
		g, start := c.gate.accept()
		c.gate = g
		if start {
			return c.start(m)
		}
	case " ":
		c.reveal = !c.reveal
	case "n", "esc", "q":
		m.overlay = nil
		return m, nil
	}
	m.overlay = c
	return m, nil
}

// start plans again against the target as it is now and only writes when
// nothing moved since the review, in the same command, so the copy cannot
// overwrite something the user never saw.
func (c copyToOverlay) start(m Model) (Model, tea.Cmd) {
	w, ok := m.writer()
	if !ok {
		m.overlay = nil
		return m, m.readOnly()
	}
	m.seq++
	c.gen, c.gate.step = m.seq, gateWriting
	m.overlay = c
	created := c.count(copyCreate, copyHideImport)
	msg := writtenMsg{
		gen: c.gen, at: c.target, clearMarks: true,
		done:  fmt.Sprintf("Copied %s (%d new, %d overwritten)", c.summary(), created, len(writes(c.rows))-created),
		doing: "copy " + c.summary(),
	}
	catalog, ctx, confirmed := m.load.catalog, m.load.ctx, c.rows
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		fresh, err := loadCopyPlan(ctx, w, catalog, c.at, c.target, c.keys)
		if err != nil {
			if !errors.Is(err, domain.ErrUnauthorized) {
				err = fmt.Errorf("%w: could not compare again: %w", domain.ErrRejected, err)
			}
			msg.err = err
			return msg
		}
		if !slices.Equal(fresh, confirmed) {
			return copyReplannedMsg{gen: msg.gen, rows: fresh}
		}
		drafts := writes(fresh)
		size := 0
		for _, d := range drafts {
			size += len(d.Key) + len(d.Value) + len(d.Comment)
		}
		if size > maxUpsertBytes {
			msg.err = fmt.Errorf("%w: these secrets are too large for one request; copy fewer at a time", domain.ErrRejected)
			return msg
		}
		msg.outcome, msg.err = w.Upsert(ctx, c.target, drafts)
		return msg
	}
}
