package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

type editorStep uint8

const (
	editing editorStep = iota
	discarding
	reviewing
)

type field uint8

const (
	keyField field = iota
	valueField
	commentField
	fieldCount
)

type fields struct{ key, value, comment string }

// editorOverlay writes one secret. Its value and comment are textareas, which
// keep their lines behind pointers, so copies of the overlay share them; that
// is harmless because the program only ever keeps the newest copy.
type editorOverlay struct {
	at      domain.Scope
	focus   field
	key     textinput.Model
	value   textarea.Model
	comment textarea.Model
	loaded  fields
	step    editorStep
	gate    gate
	reveal  bool
	shadows domain.Scope
	err     error
	gen     int
}

func newArea(value string, height int) textarea.Model {
	area := textarea.New()
	area.Prompt = ""
	area.ShowLineNumbers = false
	area.CharLimit = 0
	area.MaxHeight = 0
	area.KeyMap.Paste.SetEnabled(false)
	area.FocusedStyle.CursorLine = lipgloss.NewStyle()
	area.Cursor.SetMode(cursor.CursorStatic)
	area.SetHeight(height)
	area.SetValue(value)
	area.Blur()
	return area
}

func (m *Model) openCreate() tea.Cmd {
	if _, ok := m.writer(); !ok {
		return m.readOnly()
	}
	at, ok := m.scopes.current()
	if !ok || m.secrets.state != stateLoaded {
		return m.notify(toastError, "Select a path to create the secret in")
	}
	e := editorOverlay{at: at, gate: gate{env: at}}
	e.key = staticInput("", "")
	e.key.CharLimit = 500
	e.value, e.comment = newArea("", 6), newArea("", 2)
	e.loaded = e.fields()
	m.overlay = e.focusOn(keyField)
	return nil
}

func (e editorOverlay) fields() fields {
	return fields{key: e.key.Value(), value: e.value.Value(), comment: e.comment.Value()}
}

func (e editorOverlay) dirty() bool { return e.fields() != e.loaded }

func (e editorOverlay) focusOn(f field) editorOverlay {
	e.focus = f
	e.key.Blur()
	e.value.Blur()
	e.comment.Blur()
	switch f {
	case keyField:
		e.key.Focus()
	case valueField:
		e.value.Focus()
	case commentField:
		e.comment.Focus()
	}
	return e
}

func (e editorOverlay) sized(m Model) editorOverlay {
	width := max(10, m.rightWidth-4)
	e.key.Width = width
	e.value.SetWidth(width)
	e.comment.SetWidth(width)
	return e
}

func (e editorOverlay) title(Model) string {
	return "[4] New secret · " + printable(e.at.EnvName+" "+e.at.Path)
}

func (e editorOverlay) body(m Model, _, _ int) []string {
	if e.step == reviewing {
		return e.reviewLines(m)
	}
	e = e.sized(m)
	label := func(f field, name string) string {
		if e.focus == f {
			return selectedStyle.Render(name)
		}
		return mutedStyle.Render(name)
	}
	lines := []string{label(keyField, "Key"), e.key.View(), "", label(valueField, "Value")}
	lines = append(lines, strings.Split(e.value.View(), "\n")...)
	lines = append(lines, "", label(commentField, "Comment"))
	lines = append(lines, strings.Split(e.comment.View(), "\n")...)
	if e.err != nil {
		lines = append(lines, "", errorStyle.Render(oneLine(e.err)))
	}
	if e.step == discarding {
		lines = append(lines, "", errorStyle.Render("Discard your changes?"))
	}
	return lines
}

func (e editorOverlay) reviewLines(m Model) []string {
	w, _ := m.writer()
	key := strings.TrimSpace(e.key.Value())
	lines := []string{"Create " + printable(key) + " in " + printable(e.at.EnvName+" "+e.at.Path) + "?", ""}
	value := e.value.Value()
	if w != nil {
		value = w.Normalize(value)
	}
	switch {
	case value == "":
		lines = append(lines, mutedStyle.Render("Value    ")+mutedStyle.Render("(empty)"))
	case !e.reveal:
		lines = append(lines, mutedStyle.Render("Value    ")+"••••••••  "+mutedStyle.Render("space shows it"))
	default:
		lines = append(lines, mutedStyle.Render("Value"))
		lines = append(lines, block(value, max(10, m.rightWidth-4))...)
	}
	if comment := strings.TrimSpace(e.comment.Value()); comment != "" {
		lines = append(lines, mutedStyle.Render("Comment"))
		lines = append(lines, block(comment, max(10, m.rightWidth-4))...)
	}
	if e.shadows != (domain.Scope{}) {
		lines = append(lines, "", errorStyle.Render("! ")+"Hides "+printable(key)+" imported from "+printable(m.envName(e.shadows.EnvSlug)+" "+e.shadows.Path))
	}
	return append(lines, e.gate.lines()...)
}

func (e editorOverlay) hints(keyMap) []key.Binding {
	switch e.step {
	case discarding:
		return []key.Binding{key.NewBinding(key.WithHelp("y", "discard")), key.NewBinding(key.WithHelp("n", "keep editing"))}
	case reviewing:
		hints := e.gate.hints("create")
		if e.gate.step == gateReview {
			hints = append(hints, key.NewBinding(key.WithHelp("space", "show value")))
		}
		return hints
	}
	return []key.Binding{
		key.NewBinding(key.WithHelp("tab", "next field")),
		key.NewBinding(key.WithHelp("ctrl+s", "review")),
		key.NewBinding(key.WithHelp("esc", "cancel")),
	}
}

func (e editorOverlay) inFlight() int {
	if e.step == reviewing && e.gate.step == gateWriting {
		return e.gen
	}
	return 0
}

func (e editorOverlay) rejected(err error) overlay {
	e.step, e.gate, e.err = editing, gate{env: e.at}, err
	return e
}

func (e editorOverlay) update(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	e = e.sized(m)
	switch e.step {
	case discarding:
		switch k.String() {
		case "y":
			m.overlay = nil
			return m, nil
		case "n", "esc":
			e.step = editing
		}
		m.overlay = e
		return m, nil
	case reviewing:
		return e.review(m, k)
	}

	var cmd tea.Cmd
	switch {
	case k.Type == tea.KeyEsc && e.dirty():
		e.step = discarding
	case k.Type == tea.KeyEsc:
		m.overlay = nil
		return m, nil
	case k.Type == tea.KeyCtrlS:
		return e.submit(m)
	case k.Type == tea.KeyTab:
		e = e.focusOn((e.focus + 1) % fieldCount)
	case k.Type == tea.KeyShiftTab:
		e = e.focusOn((e.focus + fieldCount - 1) % fieldCount)
	case k.Type == tea.KeyEnter && e.focus == keyField:
		e = e.focusOn(valueField)
	case e.focus == keyField:
		e.key, cmd = e.key.Update(k)
	case e.focus == valueField:
		e.value, cmd = e.value.Update(k)
	default:
		e.comment, cmd = e.comment.Update(k)
	}
	m.overlay = e
	return m, cmd
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (e editorOverlay) submit(m Model) (Model, tea.Cmd) {
	key := strings.TrimSpace(e.key.Value())
	e.err, e.shadows = nil, domain.Scope{}
	switch {
	case key == "":
		e.err = errors.New("the key is empty")
	case !keyPattern.MatchString(key):
		e.err = errors.New("use letters, digits, _ and - in the key")
	}
	for _, s := range m.secrets.items {
		switch {
		case e.err != nil || s.Key != key:
		case s.ImportedFrom == (domain.Scope{}):
			e.err = errors.New(key + " already exists here; press e on it to edit it")
		default:
			e.shadows = s.ImportedFrom
		}
	}
	if e.err == nil {
		e.step, e.gate, e.reveal = reviewing, gate{env: e.at}, false
	}
	m.overlay = e
	return m, nil
}

func (e editorOverlay) review(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch e.gate.step {
	case gateWriting:
		return m, nil
	case gateTyping:
		g, start, _, cmd := e.gate.typed(k)
		e.gate = g
		if start {
			return e.start(m)
		}
		m.overlay = e
		return m, cmd
	}
	switch k.String() {
	case "y":
		g, start := e.gate.accept()
		e.gate = g
		if start {
			return e.start(m)
		}
	case " ":
		e.reveal = !e.reveal
	case "n", "esc":
		e.step = editing
	}
	m.overlay = e
	return m, nil
}

func (e editorOverlay) start(m Model) (Model, tea.Cmd) {
	w, ok := m.writer()
	if !ok {
		m.overlay = nil
		return m, m.readOnly()
	}
	m.seq++
	e.gen, e.gate.step = m.seq, gateWriting
	m.overlay = e
	draft := domain.Draft{Key: strings.TrimSpace(e.key.Value()), Value: e.value.Value(), Comment: e.comment.Value()}
	where := " in " + e.at.EnvName + " " + e.at.Path
	msg := writtenMsg{gen: e.gen, at: e.at, done: "Created " + draft.Key + where, doing: "create " + draft.Key + where}
	return m, writeCmd(m.load.ctx, msg, func(ctx context.Context) (domain.Outcome, error) {
		return w.Create(ctx, e.at, draft)
	})
}
