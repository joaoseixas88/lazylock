package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/joaoseixas88/lazylock/internal/domain"
)

// detailsOverlay looks its secret up by ID on every frame instead of holding a
// copy, so it never shows a value the list no longer has.
type detailsOverlay struct {
	id     string
	offset int
}

func (d detailsOverlay) secret(m Model) (domain.Secret, bool) {
	for _, s := range m.secrets.items {
		if s.ID == d.id {
			return s, true
		}
	}
	return domain.Secret{}, false
}

func (d detailsOverlay) title(m Model) string {
	if s, ok := d.secret(m); ok {
		return "[4] " + printable(s.Key)
	}
	return "[4] Details"
}

func (d detailsOverlay) lines(m Model, width int) []string {
	s, ok := d.secret(m)
	if !ok {
		return []string{mutedStyle.Render("This secret no longer exists.")}
	}
	label := func(name string) string { return mutedStyle.Render(fmt.Sprintf("%-8s ", name)) }
	lines := []string{label("Key") + printable(s.Key)}
	switch {
	case s.Hidden:
		lines = append(lines, label("Value")+mutedStyle.Render("(no read access)"))
	case !m.reveal.shows(s.ID):
		lines = append(lines, label("Value")+"••••••••  "+mutedStyle.Render("space reveals"))
	case s.Value == "":
		lines = append(lines, label("Value")+mutedStyle.Render("(empty)"))
	default:
		lines = append(lines, label("Value"))
		lines = append(lines, block(s.Value, width)...)
	}
	if s.Comment != "" {
		lines = append(lines, label("Comment"))
		lines = append(lines, block(s.Comment, width)...)
	}
	if len(s.Tags) > 0 {
		lines = append(lines, label("Tags")+printable(strings.Join(s.Tags, ", ")))
	}
	if s.Version > 0 {
		lines = append(lines, label("Version")+strconv.Itoa(s.Version))
	}
	if from := s.ImportedFrom; from != (domain.Scope{}) {
		lines = append(lines, label("From")+printable(m.envName(from.EnvSlug)+" "+from.Path))
	}
	return lines
}

func block(text string, width int) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		wrapped := ansi.Hardwrap(printable(line), max(1, width-2), true)
		for _, part := range strings.Split(wrapped, "\n") {
			lines = append(lines, "  "+part)
		}
	}
	return lines
}

func (d detailsOverlay) body(m Model, width, height int) []string {
	return scroll(d.lines(m, width), d.offset, height)
}

func (detailsOverlay) hints(k keyMap) []key.Binding {
	return []key.Binding{k.Reveal, k.Copy, k.CopyLines, key.NewBinding(key.WithHelp("↑/↓", "scroll")), k.Close}
}

func (d detailsOverlay) update(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	s, found := d.secret(m)
	switch {
	case key.Matches(k, m.keys.Close, m.keys.Enter):
		m.overlay = nil
		return m, nil
	case key.Matches(k, m.keys.Down):
		d.offset = clampOffset(d.offset+1, len(d.lines(m, max(0, m.rightWidth-3))), m.overlayHeight())
	case key.Matches(k, m.keys.Up):
		d.offset = max(0, d.offset-1)
	case key.Matches(k, m.keys.Reveal):
		cmd = m.toggleRevealOf(d.id)
	case key.Matches(k, m.keys.RevealAll):
		cmd = m.toggleRevealAll()
	case found && key.Matches(k, m.keys.Copy):
		cmd = m.copySecret(s)
	case found && key.Matches(k, m.keys.CopyLines):
		cmd = m.copyLinesOf([]domain.Secret{s})
	}
	m.overlay = d
	return m, cmd
}

// printable keeps text from the server from driving the terminal: control
// characters are drawn as their Unicode pictures instead of being emitted.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20:
			return 0x2400 + r
		case r == 0x7f:
			return '␡'
		case r >= 0x80 && r < 0xa0:
			return '�'
		}
		return r
	}, s)
}
