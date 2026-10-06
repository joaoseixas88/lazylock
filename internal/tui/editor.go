package tui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

type editorMode uint8

const (
	creating editorMode = iota
	updating
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

// editorOverlay creates or edits one secret. Its value and comment are
// textareas, which keep their lines behind pointers, so copies of the overlay
// share them; that is harmless because the program only ever keeps the newest
// copy.
//
// A field the textarea cannot hold unchanged, or a value the session cannot
// read, is locked: it is never sent unless the user replaces it outright.
type editorOverlay struct {
	mode     editorMode
	at       domain.Scope
	loading  bool
	before   domain.Secret
	focus    field
	key      textinput.Model
	value    textarea.Model
	comment  textarea.Model
	loaded   fields
	locked   [fieldCount]bool
	replaced [fieldCount]bool
	step     editorStep
	gate     gate
	reveal   bool
	shadows  domain.Scope
	conflict bool
	err      error
	gen      int
}

type rawLoadedMsg struct {
	gen     int
	secrets []domain.Secret
	err     error
}

type conflictMsg struct {
	gen     int
	current domain.Secret
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

func newKeyInput(value string) textinput.Model {
	input := staticInput("", value)
	input.CharLimit = 500
	return input
}

func (m *Model) openCreate() tea.Cmd {
	if _, ok := m.writer(); !ok {
		return m.readOnly()
	}
	at, ok := m.scopes.current()
	if !ok || m.secrets.state != stateLoaded {
		return m.notify(toastError, "Select a path to create the secret in")
	}
	e := editorOverlay{mode: creating, at: at, gate: gate{env: at}}
	e.key, e.value, e.comment = newKeyInput(""), newArea("", 6), newArea("", 2)
	e.loaded = e.fields()
	m.overlay = e.focusOn(keyField)
	return nil
}

// openEdit loads the secret's raw value before anything can be typed: the
// list shows references expanded, and saving that would replace them.
func (m *Model) openEdit(s domain.Secret) tea.Cmd {
	w, ok := m.writer()
	if !ok {
		return m.readOnly()
	}
	at, ok := m.scopes.current()
	if !ok || m.secrets.state != stateLoaded {
		return m.notify(toastError, "Nothing to edit yet")
	}
	if from := s.ImportedFrom; from != (domain.Scope{}) {
		return m.notify(toastError, s.Key+" is imported; edit it in "+m.envName(from.EnvSlug)+" "+from.Path)
	}
	m.seq++
	m.overlay = editorOverlay{mode: updating, at: at, loading: true, before: s, gate: gate{env: at}, gen: m.seq}
	gen, ctx := m.seq, m.load.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		secrets, err := w.Raw(ctx, at)
		return rawLoadedMsg{gen: gen, secrets: secrets, err: err}
	}
}

func (m Model) rawLoaded(msg rawLoadedMsg) (Model, tea.Cmd) {
	e, ok := m.overlay.(editorOverlay)
	if !ok || !e.loading || e.gen != msg.gen {
		return m, nil
	}
	if m.needsNewLogin(msg.err) {
		return m.expireSession()
	}
	m.overlay = nil
	if msg.err != nil {
		return m, m.notify(toastError, "Could not load "+e.before.Key+": "+oneLine(msg.err))
	}
	i := slices.IndexFunc(msg.secrets, func(s domain.Secret) bool { return s.ID == e.before.ID })
	if i < 0 {
		return m, m.notify(toastError, e.before.Key+" no longer exists")
	}
	m.overlay = e.prefilled(msg.secrets[i])
	return m, nil
}

func (e editorOverlay) prefilled(s domain.Secret) editorOverlay {
	e.loading, e.before = false, s
	e.key, e.value, e.comment = newKeyInput(s.Key), newArea("", 6), newArea("", 2)
	e = e.holding(valueField, s).holding(commentField, s)
	e.loaded = fields{key: s.Key, value: s.Value, comment: s.Comment}
	return e.focusOn(valueField)
}

// holding puts s's value or comment in its field, and locks the field when the
// textarea would not hand it back unchanged (tabs, carriage returns, control
// characters) or when the value cannot be read at all.
func (e editorOverlay) holding(f field, s domain.Secret) editorOverlay {
	area, text := &e.value, s.Value
	if f == commentField {
		area, text = &e.comment, s.Comment
	}
	area.SetValue(text)
	e.locked[f] = area.Value() != text || (f == valueField && s.Hidden)
	e.replaced[f] = false
	if e.locked[f] {
		area.SetValue("")
	}
	return e
}

func (e editorOverlay) fields() fields {
	return fields{key: e.key.Value(), value: e.value.Value(), comment: e.comment.Value()}
}

func (e editorOverlay) changed(f field) bool {
	if e.replaced[f] {
		return true
	}
	if e.locked[f] {
		return false
	}
	now := e.fields()
	switch f {
	case keyField:
		return strings.TrimSpace(now.key) != strings.TrimSpace(e.loaded.key)
	case valueField:
		return now.value != e.loaded.value
	}
	return now.comment != e.loaded.comment
}

func (e editorOverlay) dirty() bool {
	return e.changed(keyField) || e.changed(valueField) || e.changed(commentField)
}

// change keeps only what the user changed, and drops a value edit the server's
// trim would undo anyway.
func (e editorOverlay) change(w domain.Writer) domain.Change {
	var c domain.Change
	now := e.fields()
	if e.changed(keyField) {
		key := strings.TrimSpace(now.key)
		c.NewKey = &key
	}
	if e.changed(valueField) && (e.replaced[valueField] || w.Normalize(now.value) != w.Normalize(e.loaded.value)) {
		c.Value = &now.value
	}
	if e.changed(commentField) {
		c.Comment = &now.comment
	}
	return c
}

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

func (e editorOverlay) where() string { return e.at.EnvName + " " + e.at.Path }

func (e editorOverlay) title(Model) string {
	if e.mode == updating {
		return "[4] Edit " + printable(e.before.Key) + " · " + printable(e.where())
	}
	return "[4] New secret · " + printable(e.where())
}

func (e editorOverlay) body(m Model, _, _ int) []string {
	switch {
	case e.loading:
		return []string{mutedStyle.Render("Loading " + printable(e.before.Key) + "…")}
	case e.step == reviewing:
		return e.reviewLines(m)
	}
	e = e.sized(m)
	label := func(f field, name string) string {
		if e.focus == f {
			return selectedStyle.Render(name)
		}
		return mutedStyle.Render(name)
	}
	area := func(f field, view string) []string {
		if !e.locked[f] {
			return strings.Split(view, "\n")
		}
		why := "it holds characters this editor would change"
		if f == valueField && e.before.Hidden {
			why = "no read access"
		}
		return []string{mutedStyle.Render("(kept as is: " + why + "; ctrl+r replaces it)")}
	}
	lines := []string{label(keyField, "Key"), e.key.View(), "", label(valueField, "Value")}
	lines = append(lines, area(valueField, e.value.View())...)
	lines = append(lines, "", label(commentField, "Comment"))
	lines = append(lines, area(commentField, e.comment.View())...)
	if e.err != nil {
		lines = append(lines, "", errorStyle.Render(oneLine(e.err)))
	}
	if e.step == discarding {
		lines = append(lines, "", errorStyle.Render("Discard your changes?"))
	}
	return lines
}

func (e editorOverlay) masked(value string, width int) []string {
	switch {
	case value == "":
		return []string{mutedStyle.Render("  (empty)")}
	case !e.reveal:
		return []string{"  ••••••••"}
	}
	return block(value, width)
}

func (e editorOverlay) reviewLines(m Model) []string {
	w, _ := m.writer()
	width := max(10, m.rightWidth-4)
	key := strings.TrimSpace(e.key.Value())
	var lines []string
	if e.mode == creating {
		lines = append(lines, "Create "+printable(key)+" in "+printable(e.where())+"?", "", mutedStyle.Render("Value"))
		lines = append(lines, e.masked(w.Normalize(e.value.Value()), width)...)
		if comment := strings.TrimSpace(e.comment.Value()); comment != "" {
			lines = append(lines, mutedStyle.Render("Comment"))
			lines = append(lines, block(comment, width)...)
		}
		if e.shadows != (domain.Scope{}) {
			lines = append(lines, "", errorStyle.Render("! ")+"Hides "+printable(key)+" imported from "+printable(m.envName(e.shadows.EnvSlug)+" "+e.shadows.Path))
		}
		return append(lines, e.gate.lines()...)
	}

	c := e.change(w)
	lines = append(lines, "Save "+printable(e.before.Key)+" in "+printable(e.where())+"?", "")
	if c.NewKey != nil {
		lines = append(lines, mutedStyle.Render("Key      ")+printable(e.before.Key)+" → "+printable(*c.NewKey))
	}
	if c.Value != nil {
		lines = append(lines, mutedStyle.Render("Value before"))
		if e.before.Hidden {
			lines = append(lines, mutedStyle.Render("  (no read access)"))
		} else {
			lines = append(lines, e.masked(e.before.Value, width)...)
		}
		lines = append(lines, mutedStyle.Render("Value after"))
		lines = append(lines, e.masked(w.Normalize(*c.Value), width)...)
	}
	if c.Comment != nil {
		lines = append(lines, mutedStyle.Render("Comment before"))
		lines = append(lines, block(e.before.Comment, width)...)
		lines = append(lines, mutedStyle.Render("Comment after"))
		lines = append(lines, block(*c.Comment, width)...)
	}
	if e.conflict {
		lines = append(lines, "", errorStyle.Render("! ")+"Someone else changed this secret after you opened it; the before side is what it holds now.")
	}
	return append(lines, e.gate.lines()...)
}

func (e editorOverlay) hints(k keyMap) []key.Binding {
	switch {
	case e.loading:
		return []key.Binding{key.NewBinding(key.WithHelp("esc", "cancel"))}
	case e.step == discarding:
		return []key.Binding{key.NewBinding(key.WithHelp("y", "discard")), key.NewBinding(key.WithHelp("n", "keep editing"))}
	case e.step == reviewing:
		hints := e.gate.hints(map[editorMode]string{creating: "create", updating: "save"}[e.mode])
		if e.gate.step == gateReview {
			hints = append(hints, key.NewBinding(key.WithHelp("space", "show values")))
		}
		return hints
	}
	hints := []key.Binding{key.NewBinding(key.WithHelp("tab", "next field"))}
	if e.focus != keyField && e.locked[e.focus] {
		hints = append(hints, key.NewBinding(key.WithHelp("ctrl+r", "replace")))
	}
	return append(hints, key.NewBinding(key.WithHelp("ctrl+s", "review")), key.NewBinding(key.WithHelp("esc", "cancel")))
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
	if e.loading {
		if k.Type == tea.KeyEsc {
			m.overlay = nil
		}
		return m, nil
	}
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
	locked := e.focus != keyField && e.locked[e.focus]
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
	case k.Type == tea.KeyCtrlR && locked:
		e.locked[e.focus], e.replaced[e.focus] = false, true
	case locked:
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
	w, ok := m.writer()
	if !ok {
		m.overlay = nil
		return m, m.readOnly()
	}
	key := strings.TrimSpace(e.key.Value())
	e.err, e.shadows = nil, domain.Scope{}
	newKey := e.mode == creating || e.changed(keyField)
	switch {
	case key == "":
		e.err = errors.New("the key is empty")
	case newKey && !keyPattern.MatchString(key):
		e.err = errors.New("use letters, digits, _ and - in the key")
	case e.mode == updating && e.change(w) == (domain.Change{}):
		e.err = errors.New("nothing to save")
	}
	for _, s := range m.secrets.items {
		switch {
		case e.err != nil || !newKey || s.Key != key:
		case s.ImportedFrom != (domain.Scope{}):
			e.shadows = s.ImportedFrom
		case e.mode == creating:
			e.err = errors.New(key + " already exists here; press e on it to edit it")
		case s.ID != e.before.ID:
			e.err = errors.New(key + " already exists here")
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
	where := " in " + e.where()
	if e.mode == creating {
		draft := domain.Draft{Key: strings.TrimSpace(e.key.Value()), Value: e.value.Value(), Comment: e.comment.Value()}
		msg := writtenMsg{gen: e.gen, at: e.at, done: "Created " + draft.Key + where, doing: "create " + draft.Key + where}
		return m, writeCmd(m.load.ctx, msg, func(ctx context.Context) (domain.Outcome, error) {
			return w.Create(ctx, e.at, draft)
		})
	}
	return m, e.save(m.load.ctx, w)
}

// save rereads the secret and writes in one command, so nothing can change
// between the version check and the write except on the server itself.
func (e editorOverlay) save(ctx context.Context, w domain.Writer) tea.Cmd {
	change, at, before := e.change(w), e.at, e.before
	msg := writtenMsg{gen: e.gen, at: at, done: "Saved " + before.Key + " in " + e.where(), doing: "save " + before.Key + " in " + e.where()}
	if change.NewKey != nil {
		msg.done = "Renamed " + before.Key + " to " + *change.NewKey + " in " + e.where()
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		fresh, err := w.Raw(ctx, at)
		if err != nil {
			if !errors.Is(err, domain.ErrUnauthorized) {
				err = fmt.Errorf("%w: could not reread it: %w", domain.ErrRejected, err)
			}
			msg.err = err
			return msg
		}
		i := slices.IndexFunc(fresh, func(s domain.Secret) bool { return s.ID == before.ID })
		if i < 0 {
			msg.err = fmt.Errorf("%w: %s no longer exists", domain.ErrRejected, before.Key)
			return msg
		}
		if fresh[i].Version != before.Version {
			return conflictMsg{gen: msg.gen, current: fresh[i]}
		}
		msg.outcome, msg.err = w.Update(ctx, at, fresh[i].Key, change)
		return msg
	}
}

func (m Model) conflicted(msg conflictMsg) (Model, tea.Cmd) {
	e, ok := m.overlay.(editorOverlay)
	if !ok || e.inFlight() != msg.gen {
		return m, m.notify(toastError, "Nothing was saved: "+msg.current.Key+" changed while saving")
	}
	m.overlay = e.refreshed(msg.current)
	return m, nil
}

// refreshed takes in what someone else wrote: fields the user left alone follow
// the secret as it is now, edited ones keep the user's text, and the review
// asks again with the current secret as its before side.
func (e editorOverlay) refreshed(current domain.Secret) editorOverlay {
	if !e.changed(keyField) {
		e.key.SetValue(current.Key)
		e.loaded.key = current.Key
	}
	if !e.changed(valueField) {
		e = e.holding(valueField, current)
		e.loaded.value = current.Value
	}
	if !e.changed(commentField) {
		e = e.holding(commentField, current)
		e.loaded.comment = current.Comment
	}
	e.before, e.conflict = current, true
	e.step, e.gate = reviewing, gate{env: e.at}
	return e
}
