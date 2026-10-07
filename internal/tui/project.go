package tui

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

type projectField uint8

const (
	nameField projectField = iota
	descriptionField
	slugField
	projectFieldCount
)

// projectOverlay creates a project: a form, then a review that only y accepts.
type projectOverlay struct {
	inputs [projectFieldCount]textinput.Model
	focus  projectField
	step   editorStep
	gate   gate
	err    error
	gen    int
}

type projectCreatedMsg struct {
	gen     int
	name    string
	project domain.Project
	err     error
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (m *Model) openNewProject() tea.Cmd {
	if _, ok := m.projectCreator(); !ok {
		return m.readOnly()
	}
	var p projectOverlay
	for f, limit := range [projectFieldCount]int{64, 1024, 64} {
		p.inputs[f] = staticInput("", "")
		p.inputs[f].CharLimit = limit
	}
	p.inputs[slugField].Placeholder = "made from the name when empty"
	m.overlay = p.focusOn(nameField)
	return nil
}

func (p projectOverlay) focusOn(f projectField) projectOverlay {
	p.focus = f
	for i := range p.inputs {
		p.inputs[i].Blur()
	}
	p.inputs[f].Focus()
	return p
}

func (p projectOverlay) sized(m Model) projectOverlay {
	for i := range p.inputs {
		p.inputs[i].Width = max(10, m.rightWidth-4)
	}
	return p
}

func (p projectOverlay) draft() domain.NewProject {
	return domain.NewProject{
		Name:        strings.TrimSpace(p.inputs[nameField].Value()),
		Description: strings.TrimSpace(p.inputs[descriptionField].Value()),
		Slug:        strings.TrimSpace(p.inputs[slugField].Value()),
	}
}

func (p projectOverlay) title(Model) string { return "[4] New project" }

func (p projectOverlay) body(m Model, width, _ int) []string {
	if p.step == reviewing {
		return p.reviewLines(m, width)
	}
	p = p.sized(m)
	label := func(f projectField, name, note string) string {
		style := mutedStyle
		if p.focus == f {
			style = selectedStyle
		}
		return style.Render(name) + mutedStyle.Render(note)
	}
	lines := []string{
		label(nameField, "Name", ""), p.inputs[nameField].View(), "",
		label(descriptionField, "Description", " (optional)"), p.inputs[descriptionField].View(), "",
		label(slugField, "Slug", " (optional)"), p.inputs[slugField].View(),
	}
	if p.err != nil {
		lines = append(lines, "", errorStyle.Render(oneLine(p.err)))
	}
	if p.step == discarding {
		lines = append(lines, "", errorStyle.Render("Discard this project?"))
	}
	return lines
}

func (p projectOverlay) reviewLines(m Model, width int) []string {
	draft := p.draft()
	where := ""
	if m.cfg.SiteURL != "" {
		where = " on " + hostOf(m.cfg.SiteURL)
	}
	lines := []string{"Create project " + printable(draft.Name+where) + "?", ""}
	if draft.Description != "" {
		lines = append(lines, mutedStyle.Render("Description"))
		lines = append(lines, block(draft.Description, width)...)
	}
	lines = append(lines, mutedStyle.Render("Slug"))
	if draft.Slug == "" {
		lines = append(lines, mutedStyle.Render("  (made from the name)"))
	} else {
		lines = append(lines, "  "+printable(draft.Slug))
	}
	if slices.ContainsFunc(m.projects.items, func(other domain.Project) bool { return strings.EqualFold(other.Name, draft.Name) }) {
		lines = append(lines, "", errorStyle.Render("! ")+"Another project is already called "+printable(draft.Name))
	}
	return append(lines, p.gate.lines()...)
}

func (p projectOverlay) hints(keyMap) []key.Binding {
	switch p.step {
	case discarding:
		return []key.Binding{key.NewBinding(key.WithHelp("y", "discard")), key.NewBinding(key.WithHelp("n", "keep editing"))}
	case reviewing:
		return p.gate.hints("create")
	}
	return []key.Binding{
		key.NewBinding(key.WithHelp("tab", "next field")),
		key.NewBinding(key.WithHelp("ctrl+s", "review")),
		key.NewBinding(key.WithHelp("esc", "cancel")),
	}
}

func (p projectOverlay) inFlight() int {
	if p.step == reviewing && p.gate.step == gateWriting {
		return p.gen
	}
	return 0
}

func (p projectOverlay) rejected(err error) overlay {
	p.step, p.gate, p.err = editing, gate{}, err
	return p
}

func (p projectOverlay) update(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch p.step {
	case discarding:
		switch k.String() {
		case "y":
			m.overlay = nil
			return m, nil
		case "n", "esc":
			p.step = editing
		}
		m.overlay = p
		return m, nil
	case reviewing:
		return p.review(m, k)
	}

	p = p.sized(m)
	var cmd tea.Cmd
	switch {
	case k.Type == tea.KeyEsc && p.draft() != domain.NewProject{}:
		p.step = discarding
	case k.Type == tea.KeyEsc:
		m.overlay = nil
		return m, nil
	case k.Type == tea.KeyCtrlS, k.Type == tea.KeyEnter && p.focus == slugField:
		return p.submit(m)
	case k.Type == tea.KeyTab, k.Type == tea.KeyEnter:
		p = p.focusOn((p.focus + 1) % projectFieldCount)
	case k.Type == tea.KeyShiftTab:
		p = p.focusOn((p.focus + projectFieldCount - 1) % projectFieldCount)
	default:
		p.inputs[p.focus], cmd = p.inputs[p.focus].Update(k)
	}
	m.overlay = p
	return m, cmd
}

func (p projectOverlay) submit(m Model) (Model, tea.Cmd) {
	draft := p.draft()
	p.err = nil
	switch {
	case draft.Name == "":
		p = p.focusOn(nameField)
		p.err = errors.New("the name is empty")
	case draft.Slug != "" && (len(draft.Slug) < 5 || !slugPattern.MatchString(draft.Slug)):
		p = p.focusOn(slugField)
		p.err = errors.New("use 5 to 64 lowercase letters, digits and single hyphens in the slug")
	default:
		p.step, p.gate = reviewing, gate{}
	}
	m.overlay = p
	return m, nil
}

func (p projectOverlay) review(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if p.gate.step == gateWriting {
		return m, nil
	}
	switch k.String() {
	case "y":
		return p.start(m)
	case "n", "esc":
		p.step = editing
	}
	m.overlay = p
	return m, nil
}

func (p projectOverlay) start(m Model) (Model, tea.Cmd) {
	creator, ok := m.projectCreator()
	if !ok {
		m.overlay = nil
		return m, m.readOnly()
	}
	m.seq++
	p.gen, p.gate.step = m.seq, gateWriting
	m.overlay = p
	draft, gen, ctx := p.draft(), p.gen, m.load.ctx
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		project, err := creator.CreateProject(ctx, draft)
		return projectCreatedMsg{gen: gen, name: draft.Name, project: project, err: err}
	}
}

// projectCreated reloads the projects unless the server refused, and selects
// the new one once it is known to exist.
func (m Model) projectCreated(msg projectCreatedMsg) (Model, tea.Cmd) {
	if m.needsNewLogin(msg.err) {
		return m.expireSession()
	}
	p, ours := m.overlay.(projectOverlay)
	ours = ours && p.inFlight() == msg.gen
	if errors.Is(msg.err, domain.ErrRejected) {
		if ours {
			m.overlay = p.rejected(msg.err)
			return m, nil
		}
		return m, m.notify(toastError, "Could not create project "+msg.name+": "+oneLine(msg.err))
	}
	if ours {
		m.overlay = nil
	}
	var cmd tea.Cmd
	if msg.err != nil {
		cmd = m.notify(toastError, "Could not tell whether project "+msg.name+" was created ("+oneLine(msg.err)+"); reloaded")
	} else {
		cmd = m.notify(toastInfo, "Created project "+msg.name)
		m.createdProject, m.activePane = msg.project.ID, projectsPane
		m.projects.setQuery("")
	}
	return m, tea.Batch(cmd, m.load.projects(m.projects.begin()))
}
